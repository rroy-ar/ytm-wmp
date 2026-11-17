// Package audio owns the PCM stream coming out of ffmpeg.
//
// It deliberately exposes raw bytes instead of a beep or Ebitengine type. The
// UI/audio backend can change in Phase 1 without touching process management:
// the source is just an io.ReadCloser with a few pieces of lifecycle
// information attached.
package audio

import (
	"context"
	"errors"
	"fmt"

	"github.com/rroy-ar/ytm-wmp/internal/model"
)

// Output format produced by ffmpeg. Ebitengine's NewPlayerF32 consumes raw
// stereo float32 little-endian PCM directly, so no decoder sits between the
// pipe and the audio device.
const (
	SampleRate     = 48000
	Channels       = 2
	BytesPerSample = 4
	FrameSize      = Channels * BytesPerSample
)

// StreamInfo is a resolved, directly playable stream URL.
type StreamInfo = model.StreamInfo

// Resolver turns a catalog target (video ID, watch URL, or search query) into
// a stream URL. It is implemented by internal/ytdlp.
type Resolver interface {
	Resolve(ctx context.Context, target string) (StreamInfo, error)
}

// Source is one live ffmpeg process and its stdout pipe.
//
// Read returns the raw PCM bytes. Callers must drain the source and then call
// Close; Close also reaps the process, so it is safe to call more than once.
type Source interface {
	Read(p []byte) (int, error)
	Close() error

	// Err reports a non-EOF read error, if any.
	Err() error
	// Frames counts complete PCM frames read so far.
	Frames() int64
	// Done is closed when the read loop has ended.
	Done() <-chan struct{}
}

// ErrNoAudio means the stream ended without producing a single PCM frame.
// In practice this is the stale/expired URL signal: the URL resolved, but
// ffmpeg could not fetch any audio from it. Playback retries once.
var ErrNoAudio = errors.New("stream produced no audio")

// EndError classifies a source after playback has stopped.
//
// Precedence matters for recovery:
//  1. a real read error is reported as-is;
//  2. zero frames is ErrNoAudio (the stale-URL case), with process details;
//  3. a non-zero ffmpeg exit is a process failure;
//  4. otherwise the stream ended cleanly.
func EndError(src Source, closeErr error) error {
	if err := src.Err(); err != nil {
		if closeErr != nil {
			return fmt.Errorf("reading audio from ffmpeg: %w (%v)", err, closeErr)
		}
		return fmt.Errorf("reading audio from ffmpeg: %w", err)
	}

	if src.Frames() == 0 {
		if closeErr != nil {
			return fmt.Errorf("%w: %v", ErrNoAudio, closeErr)
		}
		return ErrNoAudio
	}

	if closeErr != nil {
		return fmt.Errorf("ffmpeg failed: %w", closeErr)
	}
	return nil
}
