package playback

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"ytm-wmp/internal/audio"
)

func TestPlayerPlaysSuccessfully(t *testing.T) {
	resolver := &fakeResolver{info: audio.StreamInfo{URL: "https://stream.example/1"}}
	src := &fakeSource{frames: 10}
	opener := &fakeOpener{sources: []audio.Source{src}}
	sink := &fakeSink{}

	err := New(resolver, opener, sink).Play(context.Background(), "track")
	if err != nil {
		t.Fatalf("Play = %v, want nil", err)
	}
	if resolver.resolves != 1 {
		t.Fatalf("resolves = %d, want 1", resolver.resolves)
	}
	if opener.calls != 1 {
		t.Fatalf("opens = %d, want 1", opener.calls)
	}
	if sink.played != 1 {
		t.Fatalf("plays = %d, want 1", sink.played)
	}
}

func TestPlayerRetriesStaleURLOnce(t *testing.T) {
	resolver := &fakeResolver{info: audio.StreamInfo{URL: "https://stream.example/stale"}}
	opener := &fakeOpener{sources: []audio.Source{
		&fakeSource{}, // zero frames: cached URL is stale
		&fakeSource{frames: 10},
	}}
	sink := &fakeSink{}

	err := New(resolver, opener, sink).Play(context.Background(), "track")
	if err != nil {
		t.Fatalf("Play = %v, want nil after retry", err)
	}
	if resolver.resolves != 2 {
		t.Fatalf("resolves = %d, want 2", resolver.resolves)
	}
	if resolver.forgets != 1 {
		t.Fatalf("forgets = %d, want 1", resolver.forgets)
	}
	if resolver.forgotTarget != "track" {
		t.Fatalf("forgot target = %q, want %q", resolver.forgotTarget, "track")
	}
	if opener.calls != 2 {
		t.Fatalf("opens = %d, want 2", opener.calls)
	}
	if sink.played != 2 {
		t.Fatalf("plays = %d, want 2", sink.played)
	}
}

func TestPlayerReturnsNoAudioAfterSecondAttempt(t *testing.T) {
	resolver := &fakeResolver{info: audio.StreamInfo{URL: "https://stream.example/stale"}}
	opener := &fakeOpener{sources: []audio.Source{
		&fakeSource{},
		&fakeSource{},
	}}
	sink := &fakeSink{}

	err := New(resolver, opener, sink).Play(context.Background(), "track")
	if !errors.Is(err, audio.ErrNoAudio) {
		t.Fatalf("Play = %v, want ErrNoAudio", err)
	}
	if resolver.resolves != 2 {
		t.Fatalf("resolves = %d, want exactly 2 attempts", resolver.resolves)
	}
	if opener.calls != 2 {
		t.Fatalf("opens = %d, want exactly 2 attempts", opener.calls)
	}
}

func TestPlayerDoesNotRetryReadErrors(t *testing.T) {
	readFailure := errors.New("network died")
	resolver := &fakeResolver{info: audio.StreamInfo{URL: "https://stream.example/1"}}
	opener := &fakeOpener{sources: []audio.Source{&fakeSource{readErr: readFailure}}}
	sink := &fakeSink{}

	err := New(resolver, opener, sink).Play(context.Background(), "track")
	if !errors.Is(err, readFailure) {
		t.Fatalf("Play = %v, want read failure", err)
	}
	if resolver.resolves != 1 {
		t.Fatalf("resolves = %d, want 1 (no retry)", resolver.resolves)
	}
}

func TestPlayerDoesNotRetryProcessErrorsAfterAudio(t *testing.T) {
	processFailure := errors.New("exit status 1")
	resolver := &fakeResolver{info: audio.StreamInfo{URL: "https://stream.example/1"}}
	opener := &fakeOpener{sources: []audio.Source{&fakeSource{frames: 10, closeErr: processFailure}}}
	sink := &fakeSink{}

	err := New(resolver, opener, sink).Play(context.Background(), "track")
	if !errors.Is(err, processFailure) {
		t.Fatalf("Play = %v, want process failure", err)
	}
	if resolver.resolves != 1 {
		t.Fatalf("resolves = %d, want 1 (no retry)", resolver.resolves)
	}
}

func TestPlayerReturnsResolverError(t *testing.T) {
	resolveFailure := errors.New("video unavailable")
	resolver := &fakeResolver{err: resolveFailure}
	opener := &fakeOpener{}
	sink := &fakeSink{}

	err := New(resolver, opener, sink).Play(context.Background(), "track")
	if !errors.Is(err, resolveFailure) {
		t.Fatalf("Play = %v, want resolve failure", err)
	}
	if opener.calls != 0 {
		t.Fatalf("opens = %d, want 0", opener.calls)
	}
}

func TestPlayerReturnsOpenError(t *testing.T) {
	openFailure := errors.New("ffmpeg missing")
	resolver := &fakeResolver{info: audio.StreamInfo{URL: "https://stream.example/1"}}
	opener := &fakeOpener{err: openFailure}
	sink := &fakeSink{}

	err := New(resolver, opener, sink).Play(context.Background(), "track")
	if !errors.Is(err, openFailure) {
		t.Fatalf("Play = %v, want open failure", err)
	}
	if resolver.resolves != 1 {
		t.Fatalf("resolves = %d, want 1", resolver.resolves)
	}
}

func TestPlayerHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resolver := &fakeResolver{info: audio.StreamInfo{URL: "https://stream.example/1"}}
	opener := &fakeOpener{sources: []audio.Source{&fakeSource{frames: 10}}}
	sink := &fakeSink{}

	err := New(resolver, opener, sink).Play(ctx, "track")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Play = %v, want context.Canceled", err)
	}
}

type fakeResolver struct {
	info         audio.StreamInfo
	err          error
	resolves     int
	forgets      int
	forgotTarget string
}

func (r *fakeResolver) Resolve(context.Context, string) (audio.StreamInfo, error) {
	r.resolves++
	return r.info, r.err
}

func (r *fakeResolver) Forget(target string) {
	r.forgets++
	r.forgotTarget = target
}

type fakeOpener struct {
	sources []audio.Source
	err     error
	calls   int
}

func (o *fakeOpener) Open(context.Context, string, time.Duration) (audio.Source, error) {
	o.calls++
	if o.err != nil {
		return nil, o.err
	}
	if len(o.sources) == 0 {
		return &fakeSource{frames: 1}, nil
	}
	src := o.sources[0]
	o.sources = o.sources[1:]
	return src, nil
}

type fakeSink struct {
	err    error
	played int
}

func (s *fakeSink) Play(context.Context, audio.Source) error {
	s.played++
	return s.err
}

type fakeSource struct {
	frames   int64
	readErr  error
	closeErr error
}

func (s *fakeSource) Read([]byte) (int, error) { return 0, io.EOF }
func (s *fakeSource) Close() error             { return s.closeErr }
func (s *fakeSource) Err() error               { return s.readErr }
func (s *fakeSource) Frames() int64            { return s.frames }
func (s *fakeSource) Done() <-chan struct{}    { return nil }
