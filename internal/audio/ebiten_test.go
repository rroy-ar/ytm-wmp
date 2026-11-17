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

func TestEbitenSinkPauseDuringResolveStartsPaused(t *testing.T) {
	player := newFakePlayer(&recorder{})
	created := make(chan struct{})
	release := make(chan struct{})
	sink := newEbitenSink(func(io.Reader) (pcmPlayer, error) {
		close(created)
		<-release
		return player, nil
	})
	src := newSinkSource()

	errCh := make(chan error, 1)
	go func() { errCh <- sink.Play(context.Background(), src) }()
	<-created

	// The user clicked pause while yt-dlp was still resolving.
	sink.Pause()
	close(release)
	waitForPublishedPlayer(t, sink)

	if got := player.playCount(); got != 0 {
		t.Fatalf("Play calls before resume = %d, want 0", got)
	}
	sink.Resume()
	if got := player.playCount(); got != 1 {
		t.Fatalf("Play calls after resume = %d, want 1", got)
	}

	src.finish()
	if err := waitPlayback(t, errCh); err != nil {
		t.Fatalf("Play = %v, want nil", err)
	}
}

func TestEbitenSinkBeginClearsStalePause(t *testing.T) {
	player := newFakePlayer(&recorder{})
	src := newSinkSource()
	sink := newEbitenSink(factoryFor(player))

	// A pause left over from a failed resolve must not affect the next attempt.
	sink.Pause()
	sink.Begin()

	errCh := make(chan error, 1)
	go func() { errCh <- sink.Play(context.Background(), src) }()
	waitPlayerStart(t, player)

	if got := player.playCount(); got != 1 {
		t.Fatalf("Play calls = %d, want 1", got)
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

func TestEbitenSinkSkipsPlayForCanceledContext(t *testing.T) {
	calls := 0
	sink := newEbitenSink(func(io.Reader) (pcmPlayer, error) {
		calls++
		return newFakePlayer(&recorder{}), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sink.Play(ctx, newSinkSource())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Play = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("factory calls = %d, want 0", calls)
	}
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

func TestEbitenSinkSetBufferSize(t *testing.T) {
	player := newFakePlayer(&recorder{})
	src := newSinkSource()
	sink := newEbitenSink(factoryFor(player))

	sink.SetBufferSize(250 * time.Millisecond)
	if got := sink.BufferSize(); got != 250*time.Millisecond {
		t.Fatalf("BufferSize = %v, want 250ms", got)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- sink.Play(context.Background(), src) }()
	waitPlayerStart(t, player)

	if got := player.bufferSizeValue(); got != 250*time.Millisecond {
		t.Fatalf("player bufferSize on Play = %v, want 250ms", got)
	}
	sink.SetBufferSize(400 * time.Millisecond)
	if got := player.bufferSizeValue(); got != 400*time.Millisecond {
		t.Fatalf("player bufferSize after SetBufferSize = %v, want 400ms", got)
	}
	src.finish()
	if err := waitPlayback(t, errCh); err != nil {
		t.Fatalf("Play = %v", err)
	}
}

func TestEbitenSinkVolumeAndPosition(t *testing.T) {
	player := newFakePlayer(&recorder{})
	player.position = 12 * time.Second
	src := newSinkSource()
	sink := newEbitenSink(factoryFor(player))

	if got := sink.Volume(); got != 1 {
		t.Fatalf("default Volume = %v, want 1", got)
	}
	sink.SetVolume(0.4)
	if got := sink.Volume(); got != 0.4 {
		t.Fatalf("Volume = %v, want 0.4", got)
	}
	sink.SetVolume(2.5)
	if got := sink.Volume(); got != 1 {
		t.Fatalf("clamped high Volume = %v, want 1", got)
	}
	sink.SetVolume(-0.5)
	if got := sink.Volume(); got != 0 {
		t.Fatalf("clamped low Volume = %v, want 0", got)
	}
	sink.SetVolume(0.75)

	errCh := make(chan error, 1)
	go func() { errCh <- sink.Play(context.Background(), src) }()
	waitPlayerStart(t, player)

	if got := player.Volume(); got != 0.75 {
		t.Fatalf("player.Volume() = %v, want 0.75 applied on start", got)
	}
	if got := sink.Position(); got != 12*time.Second {
		t.Fatalf("sink.Position() = %v, want 12s", got)
	}

	sink.SetVolume(0.5)
	if got := player.Volume(); got != 0.5 {
		t.Fatalf("player.Volume() after update = %v, want 0.5", got)
	}

	src.finish()
	if err := waitPlayback(t, errCh); err != nil {
		t.Fatalf("Play = %v, want nil", err)
	}
	// Position stays preserved until Begin() is called for the next stream.
	if got := sink.Position(); got != 12*time.Second {
		t.Fatalf("sink.Position() after EOF = %v, want 12s", got)
	}
	sink.Begin()
	if got := sink.Position(); got != 0 {
		t.Fatalf("sink.Position() after Begin = %v, want 0", got)
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
	volume        float64
	bufferSize    time.Duration
	position      time.Duration
	startedSignal chan struct{}
	startOnce     sync.Once
}

func newFakePlayer(recorder *recorder) *fakePlayer {
	return &fakePlayer{
		recorder:      recorder,
		volume:        1,
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

func (p *fakePlayer) Position() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.position
}

func (p *fakePlayer) SetVolume(v float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.volume = v
}

func (p *fakePlayer) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

func (p *fakePlayer) SetBufferSize(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.bufferSize = d
}

func (p *fakePlayer) bufferSizeValue() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bufferSize
}

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

func waitForPublishedPlayer(t *testing.T, sink *EbitenSink) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		published := sink.player != nil
		sink.mu.Unlock()
		if published {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("player was not published")
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

type pcmSinkSource struct {
	*sinkSource
	data []byte
}

func (s *pcmSinkSource) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.data)
	s.data = s.data[n:]
	return n, nil
}

func TestEbitenSinkEQAndVisTap(t *testing.T) {
	pcm := makeSinePCM(1000, 128, 0.25)
	src := &pcmSinkSource{
		sinkSource: newSinkSource(),
		data:       append([]byte(nil), pcm...),
	}
	player := newFakePlayer(&recorder{})
	var readOut []byte

	sink := newEbitenSink(func(r io.Reader) (pcmPlayer, error) {
		var err error
		readOut, err = io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		return player, nil
	})
	sink.SetEQ(EQSettings{Enabled: true, PreampDB: 6})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- sink.Play(ctx, src)
	}()

	waitForPublishedPlayer(t, sink)

	if len(readOut) != len(pcm) {
		t.Fatalf("readOut len = %d, want %d", len(readOut), len(pcm))
	}
	// Preamp +6 dB should double amplitude (~2x RMS).
	gain := pcmRMS(readOut) / pcmRMS(pcm)
	if gain < 1.8 || gain > 2.2 {
		t.Fatalf("expected ~2x preamp gain, got %.2f", gain)
	}

	vis := make([]float32, 128)
	n := sink.ReadVisSamples(vis)
	if n != 128 {
		t.Fatalf("ReadVisSamples = %d, want 128", n)
	}

	// Updating EQ live on an active sink updates EQ() and active EQReader.
	sink.SetEQ(EQSettings{Enabled: false})
	if sink.EQ().Enabled {
		t.Fatalf("expected EQ().Enabled == false")
	}

	src.finish()
	if err := <-done; err != nil {
		t.Fatalf("Play returned error: %v", err)
	}
	if n := sink.ReadVisSamples(vis); n != 0 {
		t.Fatalf("expected visRing reset after teardown, got %d samples", n)
	}
}
