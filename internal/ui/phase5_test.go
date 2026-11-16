package ui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"ytm-wmp/internal/catalog"
	"ytm-wmp/internal/model"
	"ytm-wmp/internal/playback"
	"ytm-wmp/internal/skin"
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
		t.Fatalf("cmds = %#v, want 1 SetQueue", cmds)
	}
	sq, ok := cmds[0].(playback.SetQueue)
	if !ok || len(sq.Tracks) != 2 || sq.Tracks[0].ID != "0wRYvsfhsR8" {
		t.Fatalf("album SetQueue = %#v", cmds[0])
	}

	// <BACK pops album -> artist -> search results.
	g.onClick(WidgetDetailBack)
	if len(g.detailStack) != 1 || g.detailStack[0].detail.Collection.ID != "UC_daft" {
		t.Fatalf("after first back, detailStack len=%d", len(g.detailStack))
	}
	g.onClick(WidgetDetailBack)
	if len(g.detailStack) != 0 || len(g.collections) != 1 {
		t.Fatalf("after second back, detailStack len=%d collections=%d", len(g.detailStack), len(g.collections))
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
