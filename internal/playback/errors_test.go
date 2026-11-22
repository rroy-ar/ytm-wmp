package playback

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/rroy-ar/ytm-wmp/internal/audio"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantKind error
		contains string
	}{
		{
			name:     "canceled",
			err:      context.Canceled,
			wantKind: ErrCanceled,
			contains: "canceled",
		},
		{
			name:     "missing tool",
			err:      exec.ErrNotFound,
			wantKind: ErrToolMissing,
			contains: "PATH",
		},
		{
			name:     "stale stream",
			err:      audio.ErrNoAudio,
			wantKind: ErrStaleStream,
			contains: "expired",
		},
		{
			name:     "ffmpeg process error",
			err:      &audio.ProcessError{Err: errors.New("exit status 1"), Stderr: "invalid data found"},
			wantKind: ErrFFmpeg,
			contains: "decoder",
		},
		{
			name:     "ffmpeg network error",
			err:      &audio.ProcessError{Err: errors.New("exit status 1"), Stderr: "Connection refused"},
			wantKind: ErrNetwork,
			contains: "Network",
		},
		{
			name:     "yt-dlp network timeout",
			err:      errors.New("yt-dlp: ERROR: Unable to download webpage: timed out"),
			wantKind: ErrNetwork,
			contains: "Network",
		},
		{
			name:     "resolve deadline exceeded",
			err:      context.DeadlineExceeded,
			wantKind: ErrNetwork,
			contains: "timed out",
		},
		{
			name:     "yt-dlp rate limit 429",
			err:      errors.New("yt-dlp: ERROR: HTTP Error 429: Too Many Requests"),
			wantKind: ErrNetwork,
			contains: "rate-limited",
		},
		{
			name:     "ffmpeg rate limit 429",
			err:      &audio.ProcessError{Err: errors.New("exit status 1"), Stderr: "HTTP error 429 Too Many Requests"},
			wantKind: ErrNetwork,
			contains: "rate-limited",
		},
		{
			name:     "bot verification check",
			err:      errors.New("ERROR: [youtube] dQw4w9WgXcQ: Sign in to confirm you're not a bot"),
			wantKind: ErrUnavailable,
			contains: "bot verification",
		},
		{
			name:     "age restricted",
			err:      errors.New("ERROR: [youtube] id: Sign in to confirm your age. This video may be inappropriate"),
			wantKind: ErrUnavailable,
			contains: "age-restricted",
		},
		{
			name:     "members only",
			err:      errors.New("ERROR: [youtube] id: Join this channel to get access to members-only content"),
			wantKind: ErrUnavailable,
			contains: "members-only",
		},
		{
			name:     "webpage error does not trigger age-restricted",
			err:      errors.New("yt-dlp: ERROR: failed to parse webpage metadata"),
			wantKind: ErrUnavailable,
			contains: "Could not play track",
		},
		{
			name:     "unavailable track",
			err:      errors.New("yt-dlp could not resolve stream: Video unavailable"),
			wantKind: ErrUnavailable,
			contains: "unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pe := ClassifyError(tt.err)
			if pe == nil {
				t.Fatal("ClassifyError = nil")
			}
			if !errors.Is(pe, tt.wantKind) {
				t.Fatalf("errors.Is(pe, %v) = false (got Kind=%v)", tt.wantKind, pe.Kind)
			}
			if !errors.Is(pe, tt.err) {
				t.Fatalf("errors.Is(pe, original) = false")
			}
			if !strings.Contains(pe.Error(), tt.contains) {
				t.Fatalf("Error() = %q, want substring %q", pe.Error(), tt.contains)
			}
		})
	}
}
