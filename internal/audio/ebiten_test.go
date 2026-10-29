package audio

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEbitenSinkPlaysUntilSourceEnds(t *testing.T) {
	recorder := &recorder{}
	player := newFakePlayer(recorder)
	src := newSinkSource()
	sink := newEbitenSink(factoryFor(player))

	errCh := make(chan error, 1)
	go func() { errCh <- sink.Play(context.Background(), src) }()
	waitPlayerStart(t, player)

	src.finish()
	if err := waitPlayback(t, errCh); err != nil {
		t.Fatalf("Play = %v, want nil", err)
	}
	if got := player.stopCount(); got != 1 {
		t.Fatalf("PauseAndStopReading calls = %d, want 1", got)
	}
	if got := src.closeCount(); got != 1 {
		t.Fatalf("source Close calls = %d, want 1", got)
	}
}

func TestEbitenSinkCancelClosesSourceBeforeStoppingReader(t *testing.T) {
	recorder := &recorder{}
	player := newFakePlayer(recorder)
	src := newSinkSource()
	src.recorder = recorder
	sink := newEbitenSink(factoryFor(player))

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- sink.Play(ctx, src) }()
	waitPlayerStart(t, player)

	cancel()
	if err := waitPlayback(t, errCh); !errors.Is(err, context.Canceled) {
		t.Fatalf("Play = %v, want context.Canceled", err)
	}

	events := recorder.snapshot()
	sourceIndex := indexOfEvent(events, "source.close")
	stopIndex := indexOfEvent(events, "player.stop-reading")
	if sourceIndex < 0 || stopIndex < 0 {
		t.Fatalf("events = %v, want source.close and player.stop-reading", events)
	}
	if sourceIndex > stopIndex {
		t.Fatalf("events = %v: source must close before PauseAndStopReading", events)
	}
}

func TestEbitenSinkPauseAndResumeKeepTheSamePlayer(t *testing.T) {
	player := newFakePlayer(&recorder{})
	src := newSinkSource()
	sink := newEbitenSink(factoryFor(player))

	errCh := make(chan error, 1)
	go func() { errCh <- sink.Play(context.Background(), src) }()
	waitPlayerStart(t, player)

	sink.Pause()
	if got := player.pauseCount(); got != 1 {
		t.Fatalf("Pause calls = %d, want 1", got)
	}
	sink.Resume()
	if got := player.playCount(); got != 2 {
		t.Fatalf("Play calls = %d, want 2 (initial + resume)", got)
	}

	src.finish()
	if err := waitPlayback(t, errCh); err != nil {
		t.Fatalf("Play = %v, want nil", err)
	}
}

func TestEbitenSinkCloseStopsCurrentPlayback(t *testing.T) {
	player := newFakePlayer(&recorder{})
	src := newSinkSource()
	sink := newEbitenSink(factoryFor(player))

	errCh := make(chan error, 1)
	go func() { errCh <- sink.Play(context.Background(), src) }()
	waitPlayerStart(t, player)

	sink.Close()
	if got := player.stopCount(); got < 1 {
		t.Fatalf("PauseAndStopReading calls = %d, want at least 1", got)
	}
	if err := waitPlayback(t, errCh); err != nil {
		t.Fatalf("Play = %v, want nil after Close", err)
	}

	// Closing again must not touch a player that is already gone.
	sink.Close()
}

func TestEbitenSinkReportsFactoryErrors(t *testing.T) {
	sink := newEbitenSink(func(io.Reader) (pcmPlayer, error) {
		return nil, errors.New("no audio device")
	})

	err := sink.Play(context.Background(), newSinkSource())
	if err == nil || !strings.Contains(err.Error(), "no audio device") {
		t.Fatalf("Play = %v, want factory error", err)
	}
}

func TestEbitenSinkRejectsUninitializedFactory(t *testing.T) {
	sink := &EbitenSink{}
	sink.initOnce.Do(func() {}) // skip the real audio context in this unit test

	err := sink.Play(context.Background(), newSinkSource())
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("Play = %v, want uninitialized context error", err)
	}
}

type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(event string) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func indexOfEvent(events []string, want string) int {
	for i, event := range events {
		if event == want {
			return i
		}
	}
	return -1
}

type fakePlayer struct {
	recorder *recorder

	mu            sync.Mutex
	plays         int
	pauses        int
	stops         int
	playing       bool
	startedSignal chan struct{}
	startOnce     sync.Once
}

func newFakePlayer(recorder *recorder) *fakePlayer {
	return &fakePlayer{
		recorder:      recorder,
		startedSignal: make(chan struct{}),
	}
}

func (p *fakePlayer) Play() {
	p.mu.Lock()
	p.plays++
	p.playing = true
	p.mu.Unlock()
	p.startOnce.Do(func() { close(p.startedSignal) })
	p.recorder.add("player.play")
}

func (p *fakePlayer) Pause() {
	p.mu.Lock()
	p.pauses++
	p.playing = false
	p.mu.Unlock()
	p.recorder.add("player.pause")
}

func (p *fakePlayer) PauseAndStopReading() {
	p.mu.Lock()
	p.stops++
	p.playing = false
	p.mu.Unlock()
	p.recorder.add("player.stop-reading")
}

func (p *fakePlayer) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

func (p *fakePlayer) Position() time.Duration { return 0 }

func (p *fakePlayer) playCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.plays
}

func (p *fakePlayer) pauseCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pauses
}

func (p *fakePlayer) stopCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stops
}

func factoryFor(player *fakePlayer) playerFactory {
	return func(io.Reader) (pcmPlayer, error) { return player, nil }
}

func waitPlayerStart(t *testing.T, player *fakePlayer) {
	t.Helper()
	select {
	case <-player.startedSignal:
	case <-time.After(time.Second):
		t.Fatal("player did not start")
	}
}

func waitPlayback(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(time.Second):
		t.Fatal("Play did not return")
		return nil
	}
}

type sinkSource struct {
	recorder *recorder

	done      chan struct{}
	doneOnce  sync.Once
	closeOnce sync.Once
	closes    atomic.Int32
}

func newSinkSource() *sinkSource {
	return &sinkSource{done: make(chan struct{})}
}

func (s *sinkSource) Read([]byte) (int, error) { return 0, io.EOF }
func (s *sinkSource) Err() error               { return nil }
func (s *sinkSource) Frames() int64            { return 1 }
func (s *sinkSource) Done() <-chan struct{}    { return s.done }

func (s *sinkSource) Close() error {
	s.closeOnce.Do(func() {
		s.closes.Add(1)
		if s.recorder != nil {
			s.recorder.add("source.close")
		}
	})
	s.finish()
	return nil
}

func (s *sinkSource) finish() {
	s.doneOnce.Do(func() { close(s.done) })
}

func (s *sinkSource) closeCount() int {
	return int(s.closes.Load())
}
