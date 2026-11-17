package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultFFmpeg      = "ffmpeg"
	defaultStderrLimit = 8 << 10

	// After stdout reaches EOF the process should exit almost immediately.
	// This is only a safety valve against a wedged ffmpeg; the audio data is
	// already complete at that point.
	eofExitTimeout = 2 * time.Second
)

// Opener starts ffmpeg for a resolved stream URL.
//
// The zero value is usable and uses "ffmpeg" from PATH. The binary path is
// injectable so main can wire in the exact executable found by internal/deps.
type Opener struct {
	FFmpeg      string
	StderrLimit int // bytes kept from the tail of stderr; 0 means 8 KiB
}

// Open starts an ffmpeg source using the ffmpeg found on PATH.
func Open(ctx context.Context, streamURL string, offset time.Duration) (Source, error) {
	return (Opener{}).Open(ctx, streamURL, offset)
}

// Open starts ffmpeg and returns its PCM stdout as a Source.
//
// offset > 0 is a fast input seek: ffmpeg starts decoding near the target
// instead of downloading everything before it. Seeking is never done on the
// live pipe; callers recreate the process.
func (o Opener) Open(ctx context.Context, streamURL string, offset time.Duration) (Source, error) {
	if strings.TrimSpace(streamURL) == "" {
		return nil, errors.New("empty stream URL")
	}

	binary := o.FFmpeg
	if binary == "" {
		binary = defaultFFmpeg
	}

	cmd := exec.CommandContext(ctx, binary, ffmpegArgs(streamURL, offset)...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating ffmpeg stdout pipe: %w", err)
	}

	stderr := newRingBuffer(o.stderrLimit())
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		return nil, fmt.Errorf("starting %s: %w", binary, err)
	}

	return &ffmpegSource{
		cmd:       cmd,
		stdout:    stdout,
		stderr:    stderr,
		streamURL: streamURL,
		openedAt:  time.Now(),
		done:      make(chan struct{}),
	}, nil
}

// ffmpegArgs is pure so table-driven tests can verify the process contract
// without running ffmpeg.
func ffmpegArgs(streamURL string, offset time.Duration) []string {
	args := []string{
		"-nostdin",
		"-loglevel", "error",
		// Without nobuffer, ffmpeg retains several input blocks before
		// emitting the first stdout bytes. This is the PoC's low-latency
		// setting and should stay close to the output stage.
		"-fflags", "+nobuffer",
		// HTTP reconnect options apply to the next input, so they must come
		// before -i.
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
	}

	if offset > 0 {
		// Input-side -ss is fast. Keep millisecond precision; if accuracy is
		// poor in practice, move it after -i in Phase 2.
		args = append(args, "-ss", strconv.FormatFloat(offset.Seconds(), 'f', 3, 64))
	}

	return append(args,
		"-i", streamURL,
		"-vn",
		"-f", "f32le",
		"-ac", strconv.Itoa(Channels),
		"-ar", strconv.Itoa(SampleRate),
		"-",
	)
}

func (o Opener) stderrLimit() int {
	if o.StderrLimit > 0 {
		return o.StderrLimit
	}
	return defaultStderrLimit
}

type ffmpegSource struct {
	cmd       *exec.Cmd
	stdout    io.ReadCloser
	stderr    *ringBuffer
	streamURL string
	openedAt  time.Time
	done      chan struct{}

	bytesRead    atomic.Int64
	eof          atomic.Bool
	firstPCMOnce sync.Once

	errMu   sync.Mutex
	readErr error

	doneOnce  sync.Once
	closeOnce sync.Once
	closeErr  error
}

func (s *ffmpegSource) Read(p []byte) (int, error) {
	n, err := s.stdout.Read(p)
	if n > 0 {
		s.bytesRead.Add(int64(n))
		s.firstPCMOnce.Do(func() {
			slog.Debug("ffmpeg first pcm", "first_pcm_ms", time.Since(s.openedAt).Milliseconds())
		})
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			s.eof.Store(true)
		}
		s.doneOnce.Do(func() { close(s.done) })

		// A closed pipe is how teardown unblocks a reader. It is not a
		// playback failure, and returning os.ErrClosed to ebiten/oto would
		// surface as a fatal audio context error from RunGame.
		if errors.Is(err, os.ErrClosed) {
			return n, io.EOF
		}
		if !errors.Is(err, io.EOF) {
			s.errMu.Lock()
			if s.readErr == nil {
				s.readErr = err
			}
			s.errMu.Unlock()
		}
	}
	return n, err
}

func (s *ffmpegSource) Close() error {
	s.closeOnce.Do(func() {
		// Closing stdout first makes a reader blocked in Read return. Wait
		// must run after the reader can no longer use the pipe.
		_ = s.stdout.Close()

		if s.eof.Load() {
			// The reader saw EOF, so all PCM data arrived. Do not kill the
			// process in the tiny window between closing stdout and exiting;
			// that would turn a normal track end into "signal: killed".
			s.waitAfterEOF()
		} else {
			// Cancellation or an explicit stop: kill first so a stalled
			// network read cannot make Wait hang forever.
			if s.cmd.Process != nil {
				_ = s.cmd.Process.Kill()
			}
			if err := s.cmd.Wait(); err != nil {
				s.closeErr = &ProcessError{Err: err, Stderr: s.stderrText()}
			}
		}
		s.doneOnce.Do(func() { close(s.done) })
	})
	return s.closeErr
}

func (s *ffmpegSource) waitAfterEOF() {
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			s.closeErr = &ProcessError{Err: err, Stderr: s.stderrText()}
		}
	case <-time.After(eofExitTimeout):
		// EOF means every byte the player needed was already read. Reap a
		// wedged process, but do not report it as a playback failure.
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		<-done
	}
}

func (s *ffmpegSource) Err() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.readErr
}

func (s *ffmpegSource) Frames() int64 {
	return s.bytesRead.Load() / FrameSize
}

func (s *ffmpegSource) Done() <-chan struct{} {
	return s.done
}

// Stderr returns the bounded tail of ffmpeg's stderr output, with the signed
// stream URL redacted.
func (s *ffmpegSource) Stderr() string {
	return s.stderrText()
}

func (s *ffmpegSource) stderrText() string {
	return redactStreamURL(s.stderr.String(), s.streamURL)
}

func redactStreamURL(stderr, streamURL string) string {
	if streamURL == "" {
		return stderr
	}
	return strings.ReplaceAll(stderr, streamURL, "<stream-url>")
}

// ProcessError is a non-zero ffmpeg exit with the captured stderr tail.
type ProcessError struct {
	Err    error
	Stderr string
}

func (e *ProcessError) Error() string {
	if msg := strings.TrimSpace(e.Stderr); msg != "" {
		return fmt.Sprintf("ffmpeg: %v: %s", e.Err, tail(msg, 1000))
	}
	return fmt.Sprintf("ffmpeg: %v", e.Err)
}

func (e *ProcessError) Unwrap() error { return e.Err }

// ringBuffer keeps the last max bytes written to it. ffmpeg can be chatty, so
// the tail is the useful part when reporting why it failed.
type ringBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func newRingBuffer(max int) *ringBuffer {
	if max <= 0 {
		max = defaultStderrLimit
	}
	return &ringBuffer{max: max}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(p)
	if n >= r.max {
		r.buf = append(r.buf[:0], p[n-r.max:]...)
		return n, nil
	}

	r.buf = append(r.buf, p...)
	if len(r.buf) > r.max {
		copy(r.buf, r.buf[len(r.buf)-r.max:])
		r.buf = r.buf[:r.max]
	}
	return n, nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
