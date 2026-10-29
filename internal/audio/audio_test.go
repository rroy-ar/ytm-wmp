package audio

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFFmpegArgs(t *testing.T) {
	t.Run("no offset", func(t *testing.T) {
		args := ffmpegArgs("https://example.com/audio?a=1&b=2", 0)

		assertContainsPair(t, args, "-i", "https://example.com/audio?a=1&b=2")
		assertContainsPair(t, args, "-f", "s16le")
		assertContainsPair(t, args, "-ac", "2")
		assertContainsPair(t, args, "-ar", "48000")
		assertNoValue(t, args, "-ss")

		if got, want := args[len(args)-1], "-"; got != want {
			t.Fatalf("last arg = %q, want %q (stdout output)", got, want)
		}
		if indexOf(args, "-i") < indexOf(args, "-reconnect") {
			t.Fatalf("args = %q: reconnect options must precede -i", args)
		}
	})

	t.Run("seek offset precedes input", func(t *testing.T) {
		args := ffmpegArgs("https://example.com/audio", 90*time.Second)

		assertContainsPair(t, args, "-ss", "90.000")
		if indexOf(args, "-ss") > indexOf(args, "-i") {
			t.Fatalf("args = %q: -ss must be an input option (before -i)", args)
		}
	})

	t.Run("fractional offset", func(t *testing.T) {
		args := ffmpegArgs("https://example.com/audio", 1500*time.Millisecond)
		assertContainsPair(t, args, "-ss", "1.500")
	})

	t.Run("negative offset is ignored", func(t *testing.T) {
		args := ffmpegArgs("https://example.com/audio", -time.Second)
		assertNoValue(t, args, "-ss")
	})
}

func TestRingBufferKeepsTail(t *testing.T) {
	ring := newRingBuffer(5)

	if n, err := ring.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatalf("Write = %d, %v; want 3, nil", n, err)
	}
	if n, err := ring.Write([]byte("def")); n != 3 || err != nil {
		t.Fatalf("Write = %d, %v; want 3, nil", n, err)
	}
	if got, want := ring.String(), "bcdef"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}

	if n, err := ring.Write([]byte("0123456789")); n != 10 || err != nil {
		t.Fatalf("Write = %d, %v; want 10, nil", n, err)
	}
	if got, want := ring.String(), "56789"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}
}

func TestRingBufferConcurrentWrites(t *testing.T) {
	ring := newRingBuffer(32)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ring.Write([]byte("x"))
		}()
	}
	wg.Wait()

	if got := len(ring.String()); got != 32 {
		t.Fatalf("len(String()) = %d, want 32", got)
	}
}

func TestRedactStreamURL(t *testing.T) {
	const streamURL = "https://rr.example/videoplayback?expire=1&sig=secret"

	got := redactStreamURL("403 for "+streamURL+": denied", streamURL)
	if strings.Contains(got, "sig=secret") {
		t.Fatalf("redacted stderr still contains signed URL: %q", got)
	}
	if want := "403 for <stream-url>: denied"; got != want {
		t.Fatalf("redacted stderr = %q, want %q", got, want)
	}

	if got := redactStreamURL("plain error", ""); got != "plain error" {
		t.Fatalf("redactStreamURL with empty URL = %q, want unchanged", got)
	}
}

func TestEndError(t *testing.T) {
	readFailure := errors.New("network read failed")
	processFailure := errors.New("exit status 1")

	tests := []struct {
		name     string
		src      *fakeSource
		closeErr error
		wantIs   error
		contains string
	}{
		{
			name: "clean stream",
			src:  &fakeSource{frames: 42},
		},
		{
			name:   "zero frames is stale",
			src:    &fakeSource{},
			wantIs: ErrNoAudio,
		},
		{
			name:     "zero frames with process detail",
			src:      &fakeSource{},
			closeErr: &ProcessError{Err: processFailure, Stderr: "HTTP error 403 Forbidden"},
			wantIs:   ErrNoAudio,
			contains: "403 Forbidden",
		},
		{
			name:     "read error wins over zero frames",
			src:      &fakeSource{readErr: readFailure},
			closeErr: &ProcessError{Err: processFailure},
			wantIs:   readFailure,
			contains: "reading audio",
		},
		{
			name:     "process error after audio",
			src:      &fakeSource{frames: 42},
			closeErr: &ProcessError{Err: processFailure, Stderr: "connection reset"},
			contains: "connection reset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := EndError(tt.src, tt.closeErr)
			if tt.wantIs == nil && tt.contains == "" {
				if err != nil {
					t.Fatalf("EndError = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("EndError = nil, want an error")
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Fatalf("EndError = %v, want errors.Is(..., %v)", err, tt.wantIs)
			}
			if tt.contains != "" && !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("EndError = %v, want it to contain %q", err, tt.contains)
			}
		})
	}
}

func assertContainsPair(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return
		}
	}
	t.Fatalf("args = %q, want pair %q %q", args, flag, value)
}

func assertNoValue(t *testing.T, args []string, flag string) {
	t.Helper()
	if indexOf(args, flag) >= 0 {
		t.Fatalf("args = %q, want no %q", args, flag)
	}
}

func indexOf(args []string, value string) int {
	for i, arg := range args {
		if arg == value {
			return i
		}
	}
	return -1
}

type fakeSource struct {
	frames  int64
	readErr error
}

func (f *fakeSource) Read([]byte) (int, error) { return 0, io.EOF }
func (f *fakeSource) Close() error             { return nil }
func (f *fakeSource) Err() error               { return f.readErr }
func (f *fakeSource) Frames() int64            { return f.frames }
func (f *fakeSource) Done() <-chan struct{}    { return nil }
