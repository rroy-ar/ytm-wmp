package audio

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
)

// beepBuffer is the PoC's ~100 ms mixer buffer. It is small enough for fast
// start and large enough to absorb scheduling jitter.
const beepBuffer = 100 * time.Millisecond

// BeepPlayer is the Phase 0 playback sink.
//
// Phase 1 replaces it with an Ebitengine audio player so the UI and audio share
// one event loop. Keeping it behind Source means the rest of the code does not
// care which backend is in use.
type BeepPlayer struct {
	once    sync.Once
	initErr error
}

// NewBeepPlayer creates the sink and starts opening the audio device in the
// background. The device does not depend on the stream URL, so warming it while
// yt-dlp resolves preserves the PoC's short time-to-first-audio.
func NewBeepPlayer() *BeepPlayer {
	p := &BeepPlayer{}
	go func() { _ = p.Init() }()
	return p
}

// Init opens the speaker exactly once. Concurrent calls are safe.
func (p *BeepPlayer) Init() error {
	p.once.Do(func() {
		sampleRate := beep.SampleRate(SampleRate)
		p.initErr = speaker.Init(sampleRate, sampleRate.N(beepBuffer))
	})
	return p.initErr
}

// Play feeds src to the speaker until it ends or ctx is canceled.
//
// On cancellation the source is closed before speaker.Clear: Clear waits for
// an in-flight read, and closing the pipe is what releases that read.
func (p *BeepPlayer) Play(ctx context.Context, src Source) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.Init(); err != nil {
		return fmt.Errorf("opening audio device: %w", err)
	}

	format := beep.Format{
		SampleRate:  beep.SampleRate(SampleRate),
		NumChannels: Channels,
		Precision:   BytesPerSample,
	}

	done := make(chan struct{})
	streamer := &sourceStreamer{src: src, format: format}
	speaker.Play(beep.Seq(streamer, beep.Callback(func() { close(done) })))

	select {
	case <-ctx.Done():
		_ = src.Close()
		speaker.Clear()
		return ctx.Err()
	case <-done:
		return nil
	}
}

// sourceStreamer decodes the raw s16le pipe into beep's float samples.
// io.ReadFull is intentional: beep asks for a fixed block, and a short read
// only happens at EOF or on failure.
type sourceStreamer struct {
	src    Source
	format beep.Format
	buf    []byte
}

func (s *sourceStreamer) Stream(samples [][2]float64) (int, bool) {
	width := s.format.Width()
	need := len(samples) * width
	if cap(s.buf) < need {
		s.buf = make([]byte, need)
	}
	buf := s.buf[:need]

	read, err := io.ReadFull(s.src, buf)

	n := 0
	for i := 0; i+width <= read; i += width {
		samples[n], _ = s.format.DecodeSigned(buf[i:read])
		n++
	}

	if err != nil {
		return n, false
	}
	return n, true
}

func (s *sourceStreamer) Err() error {
	return s.src.Err()
}
