package ui

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/rroy-ar/ytm-wmp/internal/artwork"
	"github.com/rroy-ar/ytm-wmp/internal/catalog"
	"github.com/rroy-ar/ytm-wmp/internal/model"
	"github.com/rroy-ar/ytm-wmp/internal/playback"
	"github.com/rroy-ar/ytm-wmp/internal/skin"
	"github.com/rroy-ar/ytm-wmp/internal/store"
)

type fakeUITabCatalog struct {
	fakeCatalog
	supported   []catalog.SearchTab
	collections map[catalog.SearchTab][]catalog.Collection
	details     map[string]catalog.CollectionDetail
}

func (f *fakeUITabCatalog) SupportedTabs() []catalog.SearchTab {
	if len(f.supported) > 0 {
		return f.supported
	}
	return []catalog.SearchTab{catalog.TabSongs, catalog.TabPlaylists, catalog.TabAlbums, catalog.TabArtists}
}

func (f *fakeUITabCatalog) SearchCollections(_ context.Context, tab catalog.SearchTab, _ string, _ int) ([]catalog.Collection, error) {
	return append([]catalog.Collection(nil), f.collections[tab]...), nil
}

func (f *fakeUITabCatalog) BrowseCollection(_ context.Context, col catalog.Collection) (catalog.CollectionDetail, error) {
	if d, ok := f.details[col.ID]; ok {
		return d, nil
	}
	return catalog.CollectionDetail{Collection: col}, nil
}

func TestBrowserTabsDetailDrillDownAndBackStack(t *testing.T) {
	cat := &fakeUITabCatalog{
		collections: map[catalog.SearchTab][]catalog.Collection{
			catalog.TabArtists: {{ID: "UC_daft", Kind: catalog.CollectionArtist, Title: "Daft Punk"}},
		},
		details: map[string]catalog.CollectionDetail{
			"UC_daft": {
				Collection: catalog.Collection{ID: "UC_daft", Kind: catalog.CollectionArtist, Title: "Daft Punk"},
				Tracks:     []model.Track{{ID: "FGBhQbmPwH8", Title: "One More Time", Artist: "Daft Punk"}},
				Albums:     []catalog.Collection{{ID: "MPREb_disc", Kind: catalog.CollectionAlbum, Title: "Discovery"}},
			},
			"MPREb_disc": {
				Collection: catalog.Collection{ID: "MPREb_disc", Kind: catalog.CollectionAlbum, Title: "Discovery"},
				Tracks: []model.Track{
					{ID: "0wRYvsfhsR8", Title: "Something About Us", Artist: "Daft Punk"},
					{ID: "FGBhQbmPwH8", Title: "One More Time", Artist: "Daft Punk"},
				},
			},
		},
	}
	ctrl := newFakeController()
	s, err := skin.Open(filepath.Join("..", "..", "assets", "dev-skin"))
	if err != nil {
		t.Fatal(err)
	}
	g := NewGame(
		context.Background(),
		s,
		ctrl,
		&fakeAudioLifecycle{},
		WithCatalog(cat),
		WithSearchDebounce(0),
	)
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatal(err)
	}

	g.searchQuery = "daft punk"
	g.onClick(WidgetBrowserTabArtists)
	waitGameCondition(t, g, func() bool {
		return len(g.collections) == 1 && g.collections[0].ID == "UC_daft"
	})

	// Click row 0 once to select, twice to open Artist detail.
	g.onClick(WidgetResultRow0)
	g.onClick(WidgetResultRow0)
	waitGameCondition(t, g, func() bool {
		return len(g.detailStack) == 1 && g.detailStack[0].detail.Collection.ID == "UC_daft"
	})

	// Toggle Artist detail from Songs to Albums, then open Discovery album.
	g.onClick(WidgetDetailModeToggle)
	if !g.detailStack[0].showAlbums {
		t.Fatal("WidgetDetailModeToggle did not switch artist detail to albums")
	}
	g.onClick(WidgetResultRow0)
	g.onClick(WidgetResultRow0)
	waitGameCondition(t, g, func() bool {
		return len(g.detailStack) == 2 && g.detailStack[1].detail.Collection.ID == "MPREb_disc"
	})

	// Render detail screen cleanly.
	screen := ebiten.NewImage(WindowWidth, WindowHeight)
	g.Draw(screen)

	// Import all tracks from album detail.
	g.onClick(WidgetBrowserImportAll)
	cmds := ctrl.sentCommands()
	if len(cmds) != 1 {
		t.Fatalf("cmds = %#v, want 1 Enqueue", cmds)
	}
	enq, ok := cmds[0].(playback.Enqueue)
	if !ok || len(enq.Tracks) != 2 || enq.Tracks[0].ID != "0wRYvsfhsR8" {
		t.Fatalf("album Enqueue = %#v", cmds[0])
	}

	// <BACK pops album -> artist -> search results.
	g.onClick(WidgetDetailBack)
	if len(g.detailStack) != 1 || g.detailStack[0].detail.Collection.ID != "UC_daft" {
		t.Fatalf("after first back, detailStack len=%d", len(g.detailStack))
	}
	// Queue album directly from artist's Albums list without opening it.
	g.onClick(WidgetBrowserQueue)
	waitGameCondition(t, g, func() bool {
		return len(ctrl.sentCommands()) == 1
	})
	if len(g.detailStack) != 1 {
		t.Fatalf("queueing album from artist detail should not push detailStack, len=%d", len(g.detailStack))
	}

	g.onClick(WidgetDetailBack)
	if len(g.detailStack) != 0 || len(g.collections) != 1 {
		t.Fatalf("after second back, detailStack len=%d collections=%d", len(g.detailStack), len(g.collections))
	}
}

func TestBrowserQueueAlbumFromSearchResults(t *testing.T) {
	ctrl := newFakeController()
	cat := &fakeUITabCatalog{
		collections: map[catalog.SearchTab][]catalog.Collection{
			catalog.TabAlbums: {{ID: "MPREb_disc", Title: "Discovery", Kind: catalog.CollectionAlbum}},
		},
		details: map[string]catalog.CollectionDetail{
			"MPREb_disc": {
				Collection: catalog.Collection{ID: "MPREb_disc", Title: "Discovery", Kind: catalog.CollectionAlbum},
				Tracks: []model.Track{
					{ID: "0wRYvsfhsR8", Title: "Something About Us"},
					{ID: "FGBhQbmPwH8", Title: "One More Time"},
				},
			},
		},
	}

	g := NewGame(context.Background(), nil, ctrl, &fakeAudioLifecycle{}, WithCatalog(cat), WithSearchDebounce(0))
	defer g.Close()

	g.focusSearch()
	g.onClick(WidgetBrowserTabAlbums)
	g.typeSearchText("discovery")
	g.submitSearch()
	waitGameCondition(t, g, func() bool {
		return len(g.collections) == 1
	})

	g.onClick(WidgetBrowserQueue)
	waitGameCondition(t, g, func() bool {
		return len(ctrl.sentCommands()) == 1
	})

	if len(g.detailStack) != 0 {
		t.Fatalf("queueing album from search results should not open detailStack, len=%d", len(g.detailStack))
	}
	enq, ok := ctrl.sentCommands()[0].(playback.Enqueue)
	if !ok || len(enq.Tracks) != 2 || enq.Tracks[0].ID != "0wRYvsfhsR8" || enq.Tracks[1].ID != "FGBhQbmPwH8" {
		t.Fatalf("album Enqueue = %#v", ctrl.sentCommands()[0])
	}

	// Right-clicking an album row also queues it without opening detail view.
	g.onBrowserRowRightClick(0)
	waitGameCondition(t, g, func() bool {
		return len(ctrl.sentCommands()) == 2
	})
	if len(g.detailStack) != 0 {
		t.Fatalf("right-clicking album should not open detailStack, len=%d", len(g.detailStack))
	}
}

func TestBrowserTabUnsupportedAlbumsArtistsOnPlainCatalog(t *testing.T) {
	cat := newFakeCatalog() // plain catalog without TabCatalog
	g := NewGame(context.Background(), nil, newFakeController(), &fakeAudioLifecycle{}, WithCatalog(cat))
	defer g.Close()

	g.onClick(WidgetBrowserTabAlbums)
	if g.activeTab != browserTabSongs {
		t.Fatalf("activeTab = %v, want browserTabSongs when Albums unsupported", g.activeTab)
	}
	if g.searchStatus != "Tab needs InnerTube" {
		t.Fatalf("searchStatus = %q, want unsupported notice", g.searchStatus)
	}
}

func TestBrowserQueueTabActions(t *testing.T) {
	ctrl := newFakeController()
	ctrl.setSnapshot(playback.Snapshot{
		State: playback.StatePlaying,
		Index: 0,
		Queue: []model.Track{
			{ID: "t0", Title: "First", Duration: time.Minute},
			{ID: "t1", Title: "Second", Duration: 2 * time.Minute},
		},
	})
	g := NewGame(context.Background(), nil, ctrl, &fakeAudioLifecycle{})
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatal(err)
	}

	g.onClick(WidgetBrowserTabQueue)
	if g.activeTab != browserTabQueue {
		t.Fatalf("activeTab = %v, want browserTabQueue", g.activeTab)
	}

	// Select row 1 and move it up -> MoveTrack{1, 0}.
	g.onClick(WidgetResultRow1)
	if g.queueSelected != 1 {
		t.Fatalf("queueSelected = %d, want 1", g.queueSelected)
	}
	g.onClick(WidgetQueueMoveUp)
	g.onClick(WidgetBrowserPlay)
	g.onClick(WidgetQueueRemove)
	g.onClick(WidgetQueueClear)

	cmds := ctrl.sentCommands()
	if len(cmds) != 4 {
		t.Fatalf("queue tab cmds = %#v, want 4", cmds)
	}
	if mv, ok := cmds[0].(playback.MoveTrack); !ok || mv.From != 1 || mv.To != 0 {
		t.Fatalf("cmd[0] = %#v, want MoveTrack{1, 0}", cmds[0])
	}
	if pi, ok := cmds[1].(playback.PlayIndex); !ok || pi.Index != 0 {
		t.Fatalf("cmd[1] = %#v, want PlayIndex{0}", cmds[1])
	}
	if rm, ok := cmds[2].(playback.RemoveIndices); !ok || len(rm.Indices) != 1 || rm.Indices[0] != 0 {
		t.Fatalf("cmd[2] = %#v, want RemoveIndices{[0]}", cmds[2])
	}
	if sq, ok := cmds[3].(playback.SetQueue); !ok || len(sq.Tracks) != 0 || sq.Index != -1 {
		t.Fatalf("cmd[3] = %#v, want empty SetQueue", cmds[3])
	}

	screen := ebiten.NewImage(WindowWidth, WindowHeight)
	g.Draw(screen)
}

func TestToastsAndMissingDependencyAlert(t *testing.T) {
	g := NewGame(
		context.Background(),
		nil,
		newFakeController(),
		&fakeAudioLifecycle{},
		WithToolStatus(
			ToolStatus{Name: "yt-dlp", OK: false, Error: "yt-dlp missing"},
			ToolStatus{Name: "ffmpeg", OK: true, Version: "7.0"},
		),
	)
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatal(err)
	}

	toast, ok := g.activeToast()
	if !ok || toast.kind != toastError || !strings.Contains(toast.message, "yt-dlp") || toast.actionLabel != "SETTINGS" {
		t.Fatalf("missing dep toast = (%+v, %v)", toast, ok)
	}

	// Draw toast over window.
	g.Draw(ebiten.NewImage(WindowWidth, WindowHeight))

	// Clicking SETTINGS action opens Settings panel.
	g.onClick(WidgetToastAction)
	if !g.showSettings || !g.browserMode || g.hidePL {
		t.Fatalf("ToastAction did not open Settings (showSettings=%v browserMode=%v hidePL=%v)", g.showSettings, g.browserMode, g.hidePL)
	}

	// Info toast expires when deadline passes; dismiss clears active toast.
	g.pushToast(toastInfo, "hello", 10*time.Millisecond)
	g.expireToasts(time.Now().Add(time.Second))
	if _, ok := g.activeToast(); ok {
		t.Fatal("expired info toast still active")
	}

	g.setError("network failed")
	if _, ok := g.activeToast(); !ok || g.errText == "" {
		t.Fatal("setError did not publish toast")
	}
	g.onClick(WidgetToastDismiss)
	if _, ok := g.activeToast(); ok || g.errText != "" {
		t.Fatalf("dismiss did not clear error toast (errText=%q)", g.errText)
	}
}

func TestSettingsScaleBufferCacheAndTools(t *testing.T) {
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cacheDir, "sample.img"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	artCache := artwork.NewCache(artwork.WithCacheDir(cacheDir))

	var resizes []image.Point
	var buffers []time.Duration
	var appliedTools [][2]string

	g := NewGame(
		context.Background(),
		nil,
		newFakeController(),
		&fakeAudioLifecycle{},
		WithArtwork(artCache),
		WithWindowResizer(func(w, h int) { resizes = append(resizes, image.Pt(w, h)) }),
		WithBufferApplier(func(d time.Duration) { buffers = append(buffers, d) }),
		WithToolStatus(
			ToolStatus{Name: "yt-dlp", OK: false, Error: "missing"},
			ToolStatus{Name: "ffmpeg", OK: true, ResolvedPath: "/usr/bin/ffmpeg", Version: "7.0"},
		),
		WithToolChecker(func(_ context.Context, name, override string) ToolStatus {
			return ToolStatus{Name: name, Configured: override, ResolvedPath: override, Version: "2026.11", OK: override != ""}
		}),
		WithToolApplier(func(yt, ff string) {
			appliedTools = append(appliedTools, [2]string{yt, ff})
		}),
	)
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatal(err)
	}

	g.onClick(WidgetBrowserSettings)
	if !g.showSettings {
		t.Fatal("WidgetBrowserSettings did not open Settings")
	}

	// Scale cycle 1x -> 1.10x -> ... -> 2x -> 1x.
	wantScales := []float64{1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 2.0, 1.0}
	for i, want := range wantScales {
		g.onClick(WidgetSettingsScale)
		if g.scale != want {
			t.Fatalf("step %d: scale = %v, want %v", i, g.scale, want)
		}
		wantW, wantH := ScaledWindowSize(WindowWidth, WindowHeight, want)
		if len(resizes) != i+1 || resizes[i] != image.Pt(wantW, wantH) {
			t.Fatalf("step %d: resizes = %v, want last (%d, %d)", i, resizes, wantW, wantH)
		}
	}
	if got := formatScaleLabel(1.0); got != "1X" {
		t.Fatalf("formatScaleLabel(1.0) = %q, want 1X", got)
	}
	if got := formatScaleLabel(1.1); got != "1.10X" {
		t.Fatalf("formatScaleLabel(1.1) = %q, want 1.10X", got)
	}
	if got := formatScaleLabel(2.0); got != "2X" {
		t.Fatalf("formatScaleLabel(2.0) = %q, want 2X", got)
	}

	// Buffer cycle: 0 -> 100ms.
	g.onClick(WidgetSettingsBuffer)
	if g.bufferMs != 100 || len(buffers) != 1 || buffers[0] != 100*time.Millisecond {
		t.Fatalf("bufferMs=%d buffers=%v", g.bufferMs, buffers)
	}

	// Cache stats & clear.
	waitGameCondition(t, g, func() bool {
		return g.cacheFiles == 1 && g.cacheBytes == 2048
	})
	g.onClick(WidgetSettingsCacheClear)
	waitGameCondition(t, g, func() bool {
		return g.cacheFiles == 0 && g.cacheBytes == 0
	})

	// Edit yt-dlp path inline and apply -> resolves OK, clears deps toast, updates applier.
	g.onClick(WidgetSettingsYTDLPEdit)
	if g.editTarget != editTargetYTDLP {
		t.Fatalf("editTarget = %v, want editTargetYTDLP", g.editTarget)
	}
	g.editField.SetText("/opt/bin/yt-dlp")
	g.onClick(WidgetSettingsInlineApply)
	waitGameCondition(t, g, func() bool {
		return g.ytdlpStatus.OK && g.ytdlpStatus.ResolvedPath == "/opt/bin/yt-dlp"
	})
	if len(appliedTools) == 0 || appliedTools[len(appliedTools)-1][0] != "/opt/bin/yt-dlp" {
		t.Fatalf("appliedTools = %v", appliedTools)
	}

	// Render Settings panel + inline editor cleanly.
	g.onClick(WidgetSettingsCacheEdit)
	g.Draw(ebiten.NewImage(WindowWidth, WindowHeight))

	g.onClick(WidgetSettingsClose)
	if g.showSettings {
		t.Fatal("WidgetSettingsClose did not close Settings")
	}
}

func TestConfigPersistenceColdStartRoundTrip(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	ctrl := newFakeController()
	ctrl.setSnapshot(playback.Snapshot{
		State:    playback.StatePaused,
		Volume:   0.65,
		Index:    1,
		Shuffle:  true,
		Repeat:   playback.RepeatOne,
		Autoplay: false,
		Queue: []model.Track{
			{ID: "0wRYvsfhsR8", Title: "Something About Us", Artist: "Daft Punk", Duration: 232 * time.Second},
			{ID: "FGBhQbmPwH8", Title: "One More Time", Artist: "Daft Punk", Duration: 320 * time.Second},
		},
	})

	g1 := NewGame(
		context.Background(),
		nil,
		ctrl,
		&fakeAudioLifecycle{},
		WithConfigPath(cfgPath),
		WithSkinPath("/skins/retro.wsz"),
	)
	if err := g1.Update(); err != nil {
		t.Fatal(err)
	}
	g1.onClick(WidgetMainEQ)
	g1.onClick(WidgetMainTime)
	g1.onClick(WidgetBrowserSettings)
	g1.onClick(WidgetSettingsScale)
	g1.onClick(WidgetSettingsBuffer)
	g1.onClick(WidgetSettingsClose)
	g1.searchQuery = "daft punk"
	g1.browserMode = true
	g1.markConfigDirty()
	g1.Close()

	loaded, existed, err := store.Load(cfgPath)
	if err != nil || !existed {
		t.Fatalf("store.Load = (%+v, %v, %v)", loaded, existed, err)
	}
	if loaded.Skin != "/skins/retro.wsz" || loaded.Volume != 0.65 || loaded.Scale != 1.1 || loaded.BufferMs != 100 {
		t.Fatalf("persisted config scalars = %+v", loaded)
	}
	if !loaded.Windows[store.WindowEQ].Visible || !loaded.Windows[store.WindowPlaylist].Visible || !loaded.TimeRemaining || !loaded.BrowserMode {
		t.Fatalf("persisted layout/flags = %+v", loaded)
	}
	if len(loaded.Queue) != 2 || loaded.QueueIndex != 1 || !loaded.Shuffle || loaded.Repeat != store.RepeatOne || loaded.Autoplay || loaded.LastSearch != "daft punk" {
		t.Fatalf("persisted playback/search = %+v", loaded)
	}

	g2 := NewGame(
		context.Background(),
		nil,
		newFakeController(),
		&fakeAudioLifecycle{},
		WithInitialConfig(loaded),
	)
	defer g2.Close()
	if !g2.showEQ || g2.hidePL || !g2.browserMode || !g2.timeRemaining || g2.scale != 1.1 || g2.bufferMs != 100 || g2.searchQuery != "daft punk" {
		t.Fatalf("restored Game state mismatch: showEQ=%v hidePL=%v browser=%v rem=%v scale=%v buf=%d query=%q",
			g2.showEQ, g2.hidePL, g2.browserMode, g2.timeRemaining, g2.scale, g2.bufferMs, g2.searchQuery)
	}
}
