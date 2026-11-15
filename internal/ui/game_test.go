package ui

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"ytm-wmp/internal/playback"
	"ytm-wmp/internal/skin"
)

func TestTransportButtonsSendPlaybackCommands(t *testing.T) {
	ctrl := newFakeController()
	sink := &fakeAudioLifecycle{}
	g := NewGame(context.Background(), nil, ctrl, sink)
	defer g.Close()

	g.onClick(WidgetPlay)
	g.onClick(WidgetPause)
	g.onClick(WidgetStop)
	g.onClick(WidgetNext)
	g.onClick(WidgetPrevious)

	cmds := ctrl.sentCommands()
	if len(cmds) != 5 {
		t.Fatalf("commands = %T (%d), want 5 commands", cmds, len(cmds))
	}
	if _, ok := cmds[0].(playback.Play); !ok {
		t.Fatalf("cmd[0] = %T, want playback.Play", cmds[0])
	}
	if _, ok := cmds[1].(playback.Pause); !ok {
		t.Fatalf("cmd[1] = %T, want playback.Pause", cmds[1])
	}
	if _, ok := cmds[2].(playback.Stop); !ok {
		t.Fatalf("cmd[2] = %T, want playback.Stop", cmds[2])
	}
	if _, ok := cmds[3].(playback.Next); !ok {
		t.Fatalf("cmd[3] = %T, want playback.Next", cmds[3])
	}
	if _, ok := cmds[4].(playback.Prev); !ok {
		t.Fatalf("cmd[4] = %T, want playback.Prev", cmds[4])
	}
}

func TestPlaybackErrorEventBecomesVisibleTextAndClearsOnPlay(t *testing.T) {
	ctrl := newFakeController()
	g := NewGame(context.Background(), nil, ctrl, &fakeAudioLifecycle{})
	defer g.Close()

	ctrl.events <- playback.PlaybackError{Message: "yt-dlp not found"}
	g.drainEvents()
	if !strings.Contains(g.errText, "yt-dlp not found") {
		t.Fatalf("errText = %q, want playback error", g.errText)
	}

	g.onClick(WidgetPlay)
	if g.errText != "" {
		t.Fatalf("errText = %q, want cleared on Play", g.errText)
	}
}

func TestCloseStopsActorBeforeClosingSink(t *testing.T) {
	ctrl := newFakeController()
	sink := &fakeAudioLifecycle{}
	g := NewGame(context.Background(), nil, ctrl, sink)

	ctrl.waitRunning(t)
	g.Close()

	if sink.closeCount() != 1 {
		t.Fatalf("close calls = %d, want 1", sink.closeCount())
	}
	select {
	case <-ctrl.exited:
	case <-time.After(time.Second):
		t.Fatal("actor Run did not exit before Close returned")
	}
}

func TestFirstUpdateInitializesAudioAndSkinView(t *testing.T) {
	s, err := skin.Open(filepath.Join("..", "..", "assets", "dev-skin"))
	if err != nil {
		t.Fatalf("Open(dev skin) = %v", err)
	}
	sink := &fakeAudioLifecycle{}
	g := NewGame(context.Background(), s, newFakeController(), sink)
	defer g.Close()

	if err := g.Update(); err != nil {
		t.Fatalf("Update = %v", err)
	}
	if sink.initCount() != 1 {
		t.Fatalf("audio Init calls = %d, want 1", sink.initCount())
	}
	if g.view == nil {
		t.Fatal("skin view was not created on the first Update")
	}

	// A second tick must not create another audio context or view.
	if err := g.Update(); err != nil {
		t.Fatalf("second Update = %v", err)
	}
	if sink.initCount() != 1 {
		t.Fatalf("audio Init calls after two ticks = %d, want 1", sink.initCount())
	}
}

func TestDrawRendersSkinAndDebugOverlay(t *testing.T) {
	s, err := skin.Open(filepath.Join("..", "..", "assets", "dev-skin"))
	if err != nil {
		t.Fatalf("Open(dev skin) = %v", err)
	}
	g := NewGame(context.Background(), s, newFakeController(), &fakeAudioLifecycle{})
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatalf("Update = %v", err)
	}

	screen := ebiten.NewImage(WindowWidth, WindowHeight)
	g.Draw(screen)

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

type fakeController struct {
	events  chan playback.Event
	started chan struct{}
	exited  chan struct{}

	mu   sync.Mutex
	cmds []playback.Command
	snap playback.Snapshot
}

func newFakeController() *fakeController {
	return &fakeController{
		events:  make(chan playback.Event, 16),
		started: make(chan struct{}),
		exited:  make(chan struct{}),
		snap:    playback.Snapshot{State: playback.StateIdle, Index: -1, Volume: 1},
	}
}

func (c *fakeController) Run(ctx context.Context) {
	close(c.started)
	<-ctx.Done()
	close(c.exited)
}

func (c *fakeController) Send(cmd playback.Command) bool {
	c.mu.Lock()
	c.cmds = append(c.cmds, cmd)
	c.mu.Unlock()
	return true
}

func (c *fakeController) Events() <-chan playback.Event { return c.events }

func (c *fakeController) Snapshot() playback.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap
}

func (c *fakeController) sentCommands() []playback.Command {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]playback.Command(nil), c.cmds...)
}

func (c *fakeController) waitRunning(t *testing.T) {
	t.Helper()
	select {
	case <-c.started:
	case <-time.After(time.Second):
		t.Fatal("actor Run did not start")
	}
}

type fakeAudioLifecycle struct {
	mu      sync.Mutex
	initErr error
	inits   int
	closes  int
}

func (c *fakeAudioLifecycle) Init() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inits++
	return c.initErr
}

func (c *fakeAudioLifecycle) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closes++
}

func (c *fakeAudioLifecycle) initCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inits
}

func (c *fakeAudioLifecycle) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closes
}
