// Package playback composes a stream resolver, the ffmpeg source, and an audio
// sink. Phase 2 replaces this synchronous single-track player with the command/
// event actor; the interfaces here are deliberately narrow so that swap does
// not leak into main or the catalog.
package playback

import (
	"context"
	"errors"
	"time"

	"ytm-wmp/internal/audio"
)

// Resolver is the consumer-owned view of the yt-dlp resolver.
type Resolver interface {
	Resolve(ctx context.Context, target string) (audio.StreamInfo, error)
	Forget(target string)
}

// Opener starts a source for a resolved stream URL.
type Opener interface {
	Open(ctx context.Context, streamURL string, offset time.Duration) (audio.Source, error)
}

// Sink consumes a source. It returns when playback ends, fails, or ctx is
// canceled.
type Sink interface {
	Play(ctx context.Context, src audio.Source) error
}

// Player plays one target at a time.
type Player struct {
	resolver Resolver
	opener   Opener
	sink     Sink
}

// New wires the three collaborators. They are expected to be non-nil.
func New(resolver Resolver, opener Opener, sink Sink) *Player {
	return &Player{resolver: resolver, opener: opener, sink: sink}
}

// Play resolves and plays target. If the stream produces zero PCM frames, the
// cached URL is discarded and the target is resolved once more, preserving the
// PoC's stale-URL recovery.
func (p *Player) Play(ctx context.Context, target string) error {
	const maxAttempts = 2

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		info, err := p.resolver.Resolve(ctx, target)
		if err != nil {
			return err
		}

		src, err := p.opener.Open(ctx, info.URL, 0)
		if err != nil {
			return err
		}

		playErr := p.sink.Play(ctx, src)
		closeErr := src.Close()

		// Cancellation wins over the process errors it causes.
		if err := ctx.Err(); err != nil {
			return err
		}
		if playErr != nil {
			return playErr
		}

		err = audio.EndError(src, closeErr)
		if err == nil {
			return nil
		}
		if attempt == maxAttempts || !errors.Is(err, audio.ErrNoAudio) {
			return err
		}

		p.resolver.Forget(target)
	}

	return nil // unreachable: the loop either returns or retries
}
