package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"sync"
	"time"

	ebitenaudio "github.com/hajimehoshi/ebiten/v2/audio"
)

// pcmPlayer is the subset of *ebitenaudio.Player the sink owns. Keeping it as
// an interface lets the lifecycle and teardown-order tests run without an audio
// device.
type pcmPlayer interface {
	Play()
	Pause()
	PauseAndStopReading()
	Position() time.Duration
	SetVolume(float64)
	Volume() float64
}

type playerFactory func(io.Reader) (pcmPlayer, error)

// EbitenSink feeds the live ffmpeg PCM pipe to Ebitengine's audio context.
//
// Pause uses Player.Pause: it stops the clock but keeps the source and its
// buffer alive for an instant resume. Stopping and shutdown use
// PauseAndStopReading after closing the ffmpeg pipe, because that call blocks
// until an in-flight read returns.
type EbitenSink struct {
	initOnce sync.Once
	initErr  error
	factory  playerFactory

	mu           sync.Mutex
	player       pcmPlayer
	src          Source
	pausePending bool
	volumeSet    bool
	volume       float64
	lastPos      time.Duration

	teardownMu sync.Mutex
}

// NewEbitenSink creates the Ebitengine-backed sink. Init must run after the
// Ebitengine game has started; it creates the process-wide 48 kHz context on
// the first Update tick.
func NewEbitenSink() *EbitenSink {
	return &EbitenSink{volumeSet: true, volume: 1}
}

// newEbitenSink injects a player factory for tests.
func newEbitenSink(factory playerFactory) *EbitenSink {
	s := &EbitenSink{factory: factory, volumeSet: true, volume: 1}
	if factory != nil {
		// Mark initialization done so tests that call Play directly do not
		// create a real audio device.
		s.initOnce.Do(func() {})
	}
	return s
}

// Init creates or adopts the audio context exactly once.
//
// Ebitengine allows one context per process. If another component already made
// one, reuse it only when the sample rate matches; silently accepting 44.1 kHz
// would make every stream play at the wrong speed.
func (s *EbitenSink) Init() error {
	s.initOnce.Do(func() {
		ctx := ebitenaudio.CurrentContext()
		if ctx == nil {
			ctx = ebitenaudio.NewContext(SampleRate)
		} else if ctx.SampleRate() != SampleRate {
			s.initErr = fmt.Errorf("audio context already exists at %d Hz, want %d Hz", ctx.SampleRate(), SampleRate)
			return
		}
		s.factory = func(r io.Reader) (pcmPlayer, error) {
			player, err := ctx.NewPlayerF32(r)
			if err != nil {
				return nil, err
			}
			return player, nil
		}
	})
	return s.initErr
}

// Play consumes src until it ends or ctx is canceled.
//
// The player asks the ffmpeg pipe for data on its own goroutine. On teardown we
// close the source first: a read blocked on a stalled network pipe must return
// before PauseAndStopReading is allowed to wait for it.
func (s *EbitenSink) Play(ctx context.Context, src Source) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.Init(); err != nil {
		return err
	}

	s.mu.Lock()
	factory := s.factory
	startPaused := s.pausePending
	s.pausePending = false
	s.mu.Unlock()
	if factory == nil {
		return errors.New("audio context is not initialized")
	}

	player, err := factory(src)
	if err != nil {
		return fmt.Errorf("creating audio player: %w", err)
	}

	// Publish the player before starting it so a pause arriving immediately
	// after a click cannot race past the pending Play call. A pause requested
	// while yt-dlp was resolving is applied here instead of being lost.
	s.mu.Lock()
	s.player = player
	s.src = src
	s.lastPos = 0
	vol := 1.0
	if s.volumeSet {
		vol = s.volume
	}
	player.SetVolume(vol)
	paused := startPaused || s.pausePending
	s.pausePending = false
	if paused {
		player.Pause()
	} else {
		player.Play()
	}
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		slog.Debug("audio sink canceled", "err", ctx.Err(), "frames", src.Frames())
		s.teardown(player, src)
		return ctx.Err()
	case <-src.Done():
		slog.Debug("audio source done", "err", src.Err(), "frames", src.Frames())
		s.teardown(player, src)
		return nil
	}
}

// Begin clears pause intent and cached position left over from a previous
// session. The playback layer calls it once per new play attempt.
func (s *EbitenSink) Begin() {
	s.mu.Lock()
	s.pausePending = false
	s.lastPos = 0
	s.mu.Unlock()
}

// SetVolume clamps volume to [0, 1] and applies it to the active player as well
// as future players created by Play.
func (s *EbitenSink) SetVolume(volume float64) {
	volume = clampVolume(volume)
	s.mu.Lock()
	s.volumeSet = true
	s.volume = volume
	player := s.player
	s.mu.Unlock()

	if player != nil {
		player.SetVolume(volume)
	}
}

// Volume returns the configured output volume in [0, 1].
func (s *EbitenSink) Volume() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.volumeSet {
		return 1
	}
	return s.volume
}

// Position returns the playback position of the active stream (or the final
// position captured before teardown until the next Begin).
func (s *EbitenSink) Position() time.Duration {
	s.mu.Lock()
	player := s.player
	lastPos := s.lastPos
	s.mu.Unlock()

	if player != nil {
		return player.Position()
	}
	return lastPos
}

// Pause stops the clock but keeps ffmpeg and the buffered pipe alive. If the
// player has not been created yet (resolution is still running), the request is
// remembered and applied before the first Play call.
func (s *EbitenSink) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pausePending = true
	if s.player != nil {
		s.player.Pause()
	}
}

// Resume continues a paused player without recreating the stream.
func (s *EbitenSink) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pausePending = false
	if s.player != nil {
		s.player.Play()
	}
}

// Close stops the current player and releases ffmpeg. It is safe to call more
// than once and while Play is unwinding.
func (s *EbitenSink) Close() {
	s.mu.Lock()
	player := s.player
	src := s.src
	s.player = nil
	s.src = nil
	s.mu.Unlock()

	if player != nil {
		s.teardown(player, src)
	}
}

func (s *EbitenSink) teardown(player pcmPlayer, src Source) {
	s.teardownMu.Lock()
	defer s.teardownMu.Unlock()

	pos := player.Position()
	if src != nil {
		_ = src.Close()
	}
	player.PauseAndStopReading()

	s.mu.Lock()
	if s.player == player {
		s.lastPos = pos
		s.player = nil
		s.src = nil
		s.pausePending = false
	}
	s.mu.Unlock()
}

func clampVolume(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
