package ui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"ytm-wmp/internal/skin"
)

func TestPlayStartsThenPauseAndResume(t *testing.T) {
	runner := newFakeRunner()
	control := &fakeControl{}
	g := NewGame(context.Background(), "track", nil, runner, control)

	g.onClick(WidgetPlay)
	waitStarted(t, runner)

	if !g.started || g.paused {
		t.Fatalf("state after play: started=%v paused=%v", g.started, g.paused)
	}

	g.onClick(WidgetPause)
	if control.pauseCount() != 1 {
		t.Fatalf("pause calls = %d, want 1", control.pauseCount())
	}
	if !g.paused {
		t.Fatal("game should be paused")
	}

	g.onClick(WidgetPlay)
	if control.resumeCount() != 1 {
		t.Fatalf("resume calls = %d, want 1", control.resumeCount())
	}
	if g.paused {
		t.Fatal("game should have resumed")
	}

	runner.result <- nil
	waitForPlaybackEnd(t, g)
	if g.started {
		t.Fatal("started should be false after playback ends")
	}
}

func TestPlayWhileAlreadyPlayingDoesNotResume(t *testing.T) {
	runner := newFakeRunner()
	control := &fakeControl{}
	g := NewGame(context.Background(), "track", nil, runner, control)

	g.onClick(WidgetPlay)
	waitStarted(t, runner)
	g.onClick(WidgetPlay)

	if control.resumeCount() != 0 {
		t.Fatalf("resume calls = %d, want 0", control.resumeCount())
	}
	runner.result <- nil
	waitForPlaybackEnd(t, g)
}

func TestPauseWhileIdleIsIgnored(t *testing.T) {
	g := NewGame(context.Background(), "track", nil, newFakeRunner(), &fakeControl{})
	g.onClick(WidgetPause)
	if g.paused {
		t.Fatal("pausing an idle player should do nothing")
	}
}

func TestPlaybackErrorBecomesVisibleText(t *testing.T) {
	runner := newFakeRunner()
	g := NewGame(context.Background(), "track", nil, runner, &fakeControl{})

	g.onClick(WidgetPlay)
	waitStarted(t, runner)
	g.errText = ""

	runner.result <- errors.New("yt-dlp not found")
	waitForPlaybackEnd(t, g)

	if g.started {
		t.Fatal("started should be false after an error")
	}
	if !strings.Contains(g.errText, "yt-dlp not found") {
		t.Fatalf("errText = %q, want the playback error", g.errText)
	}

	// A new play attempt clears the old error before starting.
	g.onClick(WidgetPlay)
	if g.errText != "" {
		t.Fatalf("errText = %q, want cleared on retry", g.errText)
	}
	g.Close()
}

func TestCloseCancelsPlaybackAndClosesSink(t *testing.T) {
	runner := newFakeRunner()
	control := &fakeControl{}
	g := NewGame(context.Background(), "track", nil, runner, control)

	g.onClick(WidgetPlay)
	waitStarted(t, runner)
	g.Close()

	if control.closeCount() != 1 {
		t.Fatalf("close calls = %d, want 1", control.closeCount())
	}
	select {
	case <-runner.ctxDone:
	case <-time.After(time.Second):
		t.Fatal("playback context was not canceled")
	}
}

func TestFirstUpdateInitializesAudioAndSkinView(t *testing.T) {
	s, err := skin.Open(filepath.Join("..", "..", "assets", "dev-skin"))
	if err != nil {
		t.Fatalf("Open(dev skin) = %v", err)
	}
	control := &fakeControl{}
	g := NewGame(context.Background(), "track", s, newFakeRunner(), control)
	defer g.Close()

	if err := g.Update(); err != nil {
		t.Fatalf("Update = %v", err)
	}
	if control.initCount() != 1 {
		t.Fatalf("audio Init calls = %d, want 1", control.initCount())
	}
	if g.view == nil {
		t.Fatal("skin view was not created on the first Update")
	}

	// A second tick must not create another audio context or view.
	if err := g.Update(); err != nil {
		t.Fatalf("second Update = %v", err)
	}
	if control.initCount() != 1 {
		t.Fatalf("audio Init calls after two ticks = %d, want 1", control.initCount())
	}
}

func TestDrawRendersSkinAndDebugOverlay(t *testing.T) {
	s, err := skin.Open(filepath.Join("..", "..", "assets", "dev-skin"))
	if err != nil {
		t.Fatalf("Open(dev skin) = %v", err)
	}
	g := NewGame(context.Background(), "track", s, newFakeRunner(), &fakeControl{})
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatalf("Update = %v", err)
	}

	screen := ebiten.NewImage(WindowWidth, WindowHeight)
	g.Draw(screen)

	// Pixel readback panics until the game loop has started, so this is a
	// panic/smoke test rather than a golden-image test. The live smoke run and
	// the screenshot review cover visual output.
	g.toggleDebug()
	g.Draw(screen)

}

func TestToggleDebug(t *testing.T) {
	g := &Game{}
	g.toggleDebug()
	if !g.debug {
		t.Fatal("debug should be on")
	}
	g.toggleDebug()
	if g.debug {
		t.Fatal("debug should be off")
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("abcdef\nxy", 3)
	want := []string{"abc", "def", "xy"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("wrapText = %q, want %q", got, want)
	}

	got = wrapText("abcdef", 2)
	want = []string{"ab", "cd", "ef"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("wrapText = %q, want %q", got, want)
	}
}

type fakeRunner struct {
	started sync.Once
	start   chan struct{}
	result  chan error
	ctxDone chan struct{}
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		start:   make(chan struct{}),
		result:  make(chan error, 1),
		ctxDone: make(chan struct{}),
	}
}

func (r *fakeRunner) Play(ctx context.Context, _ string) error {
	r.started.Do(func() { close(r.start) })
	select {
	case err := <-r.result:
		return err
	case <-ctx.Done():
		close(r.ctxDone)
		return ctx.Err()
	}
}

func waitStarted(t *testing.T, runner *fakeRunner) {
	t.Helper()
	select {
	case <-runner.start:
	case <-time.After(time.Second):
		t.Fatal("playback did not start")
	}
}

func waitForPlaybackEnd(t *testing.T, g *Game) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		g.drainEvents()
		if !g.started {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("playback result was not observed")
}

type fakeControl struct {
	mu      sync.Mutex
	initErr error
	inits   int
	pauses  int
	resumes int
	closes  int
}

func (c *fakeControl) Init() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inits++
	return c.initErr
}

func (c *fakeControl) Pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pauses++
}

func (c *fakeControl) Resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resumes++
}

func (c *fakeControl) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closes++
}

func (c *fakeControl) initCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inits
}

func (c *fakeControl) pauseCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pauses
}

func (c *fakeControl) resumeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resumes
}

func (c *fakeControl) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closes
}
