package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"ytm-wmp/internal/artwork"
	"ytm-wmp/internal/model"
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

func TestSearchTypingDebouncesAndPopulatesTenResultsWithoutTransportKeys(t *testing.T) {
	ctrl := newFakeController()
	cat := newFakeCatalog()
	cat.searchTracks = make([]model.Track, 10)
	for i := range cat.searchTracks {
		cat.searchTracks[i] = model.Track{
			ID:       fmt.Sprintf("track-%02d", i),
			Title:    fmt.Sprintf("Track %02d", i),
			Artist:   "Artist",
			Duration: time.Duration(180+i) * time.Second,
		}
	}

	g := NewGame(
		context.Background(),
		nil,
		ctrl,
		&fakeAudioLifecycle{},
		WithCatalog(cat),
		WithSearchDebounce(15*time.Millisecond),
	)
	defer g.Close()

	g.onClick(WidgetEject)
	if !g.searchFocused {
		t.Fatal("clicking Eject must focus search box")
	}

	// Include letters that normally act as transport hotkeys (s, z, b, r, h, space).
	g.typeSearchText("daft sbzrh x")
	g.backspaceSearch()
	g.backspaceSearch()

	waitGameCondition(t, g, func() bool {
		return len(g.results) == 10
	})

	if queries := cat.searchQueries(); len(queries) != 1 || queries[0] != "daft sbzrh" {
		t.Fatalf("debounced queries = %v, want [daft sbzrh]", queries)
	}
	if cmds := ctrl.sentCommands(); len(cmds) != 0 {
		t.Fatalf("typing into focused search sent unexpected playback commands: %#v", cmds)
	}
}

func TestBrowserResultSelectionPlayQueueAndImportAll(t *testing.T) {
	ctrl := newFakeController()
	g := NewGame(context.Background(), nil, ctrl, &fakeAudioLifecycle{})
	defer g.Close()

	g.results = []model.Track{
		{ID: "id-0", Title: "Zero"},
		{ID: "id-1", Title: "One"},
		{ID: "id-2", Title: "Two"},
	}

	// Clicking row 1 selects it without playing yet.
	g.onClick(WidgetResultRow1)
	if g.selectedResult != 1 {
		t.Fatalf("selectedResult = %d, want 1", g.selectedResult)
	}
	if cmds := ctrl.sentCommands(); len(cmds) != 0 {
		t.Fatalf("first click sent commands %#v, want none", cmds)
	}

	// Clicking an already-selected row plays it.
	g.onClick(WidgetResultRow1)
	// Clicking Queue enqueues selected track.
	g.onClick(WidgetBrowserQueue)
	// Clicking Play plays selected track.
	g.onClick(WidgetBrowserPlay)
	// Clicking ImportAll replaces queue at index 0.
	g.onClick(WidgetBrowserImportAll)

	cmds := ctrl.sentCommands()
	if len(cmds) != 4 {
		t.Fatalf("len(cmds) = %d, want 4 (%#v)", len(cmds), cmds)
	}

	sq0, ok := cmds[0].(playback.SetQueue)
	if !ok || sq0.Index != 1 || !sq0.AutoPlay || len(sq0.Tracks) != 3 {
		t.Fatalf("cmd[0] = %#v, want SetQueue index 1 AutoPlay", cmds[0])
	}
	enq, ok := cmds[1].(playback.Enqueue)
	if !ok || len(enq.Tracks) != 1 || enq.Tracks[0].ID != "id-1" {
		t.Fatalf("cmd[1] = %#v, want Enqueue id-1", cmds[1])
	}
	sq2, ok := cmds[2].(playback.SetQueue)
	if !ok || sq2.Index != 1 || !sq2.AutoPlay {
		t.Fatalf("cmd[2] = %#v, want SetQueue index 1 AutoPlay", cmds[2])
	}
	sq3, ok := cmds[3].(playback.SetQueue)
	if !ok || sq3.Index != 0 || !sq3.AutoPlay || len(sq3.Tracks) != 3 {
		t.Fatalf("cmd[3] = %#v, want SetQueue index 0 AutoPlay", cmds[3])
	}
}

func TestBrowserPlaylistURLImport(t *testing.T) {
	ctrl := newFakeController()
	cat := newFakeCatalog()
	cat.playlistTracks = []model.Track{
		{ID: "p1", Title: "Playlist One"},
		{ID: "p2", Title: "Playlist Two"},
	}

	g := NewGame(
		context.Background(),
		nil,
		ctrl,
		&fakeAudioLifecycle{},
		WithCatalog(cat),
		WithSearchDebounce(0),
	)
	defer g.Close()

	g.focusSearch()
	g.typeSearchText("https://www.youtube.com/playlist?list=PL1234567890abcdef")
	g.submitSearch()

	waitGameCondition(t, g, func() bool {
		return len(g.results) == 2 && len(ctrl.sentCommands()) == 1
	})

	sq, ok := ctrl.sentCommands()[0].(playback.SetQueue)
	if !ok || len(sq.Tracks) != 2 || sq.Index != 0 || sq.AutoPlay {
		t.Fatalf("playlist import cmd = %#v, want SetQueue(2 tracks, index 0, AutoPlay=false)", ctrl.sentCommands()[0])
	}
}

func TestMetadataEnrichmentAndArtworkUploadAndDraw(t *testing.T) {
	ctrl := newFakeController()
	cat := newFakeCatalog()
	cat.trackFn = func(_ context.Context, id string) (model.Track, error) {
		return model.Track{
			ID:           "0wRYvsfhsR8",
			Title:        "Something About Us",
			Artist:       "Daft Punk",
			Duration:     232 * time.Second,
			ThumbnailURL: "https://thumb.example/art.jpg",
		}, nil
	}
	art := newFakeArtwork()

	g := NewGame(
		context.Background(),
		nil,
		ctrl,
		&fakeAudioLifecycle{},
		WithCatalog(cat),
		WithArtwork(art),
	)
	defer g.Close()

	ctrl.setSnapshot(playback.Snapshot{
		State: playback.StatePlaying,
		Track: model.Track{ID: "https://www.youtube.com/watch?v=0wRYvsfhsR8"},
		Queue: []model.Track{{ID: "https://www.youtube.com/watch?v=0wRYvsfhsR8"}},
		Index: 0,
	})

	waitGameCondition(t, g, func() bool {
		if _, ok := g.artTex["0wRYvsfhsR8"]; !ok {
			return false
		}
		for _, cmd := range ctrl.sentCommands() {
			if _, ok := cmd.(playback.UpdateTrack); ok {
				return true
			}
		}
		return false
	})

	screen := ebiten.NewImage(WindowWidth, WindowHeight)
	g.Draw(screen)

	// Toggle now-playing artwork modal and draw placeholder fallback for missing art.
	g.onClick(WidgetNowPlayingArt)
	if !g.showArtModal {
		t.Fatal("showArtModal should toggle true")
	}
	g.Draw(screen)
}

func waitGameCondition(t *testing.T, g *Game, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := g.Update(); err != nil {
			t.Fatalf("Update = %v", err)
		}
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for UI condition")
}

type fakeCatalog struct {
	mu             sync.Mutex
	searches       []string
	searchTracks   []model.Track
	playlistTracks []model.Track
	trackFn        func(ctx context.Context, id string) (model.Track, error)
}

func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{}
}

func (f *fakeCatalog) Search(_ context.Context, query string, _ int) ([]model.Track, error) {
	f.mu.Lock()
	f.searches = append(f.searches, query)
	tracks := append([]model.Track(nil), f.searchTracks...)
	f.mu.Unlock()
	return tracks, nil
}

func (f *fakeCatalog) Playlist(context.Context, string) ([]model.Track, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.Track(nil), f.playlistTracks...), nil
}

func (f *fakeCatalog) Track(ctx context.Context, id string) (model.Track, error) {
	f.mu.Lock()
	fn := f.trackFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, id)
	}
	return model.Track{ID: id}, nil
}

func (f *fakeCatalog) searchQueries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.searches...)
}

type fakeArtwork struct{}

func newFakeArtwork() *fakeArtwork { return &fakeArtwork{} }

func (f *fakeArtwork) LookupMemory(string) (artwork.Bundle, bool) {
	return artwork.Bundle{}, false
}

func (f *fakeArtwork) Get(_ context.Context, videoID, thumbnailURL string) (artwork.Bundle, error) {
	if thumbnailURL == "" {
		return artwork.Bundle{}, fmt.Errorf("missing thumbnail URL")
	}
	list := image.NewRGBA(image.Rect(0, 0, artwork.ListSize, artwork.ListSize))
	now := image.NewRGBA(image.Rect(0, 0, artwork.NowPlayingSize, artwork.NowPlayingSize))
	c := color.RGBA{R: 0x38, G: 0xc0, B: 0x70, A: 0xff}
	list.SetRGBA(0, 0, c)
	now.SetRGBA(0, 0, c)
	return artwork.Bundle{VideoID: videoID, List: list, NowPlaying: now}, nil
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

func (c *fakeController) setSnapshot(snap playback.Snapshot) {
	c.mu.Lock()
	c.snap = snap
	c.mu.Unlock()
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
