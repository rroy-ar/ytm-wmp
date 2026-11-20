package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	catalogytdlp "github.com/rroy-ar/ytm-wmp/internal/catalog/ytdlp"
	catalogytmusic "github.com/rroy-ar/ytm-wmp/internal/catalog/ytmusic"
	"github.com/rroy-ar/ytm-wmp/internal/model"
	"github.com/rroy-ar/ytm-wmp/internal/playback"
	"github.com/rroy-ar/ytm-wmp/internal/store"
	"github.com/rroy-ar/ytm-wmp/internal/ui"
)

func TestChooseSkinPrefersExplicitPath(t *testing.T) {
	const explicit = "/tmp/my-skin.wsz"
	if got := chooseSkin(explicit); got != explicit {
		t.Fatalf("chooseSkin(%q) = %q, want explicit path", explicit, got)
	}
}

func TestChooseSkinFindsDevSkinFromRepoRoot(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatal(err)
		}
	}()

	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	if got := chooseSkin(""); got != devSkinPath {
		t.Fatalf("chooseSkin(\"\") = %q, want %q", got, devSkinPath)
	}
}

func TestInitialTracks(t *testing.T) {
	got := initialTracks(nil, nil)
	if len(got) != 1 || got[0].ID != defaultTarget {
		t.Fatalf("initialTracks(nil, nil) = %+v, want [%s]", got, defaultTarget)
	}

	got = initialTracks([]string{"id1", "id2"}, nil)
	if len(got) != 2 || got[0].ID != "id1" || got[1].ID != "id2" {
		t.Fatalf("initialTracks(flags, nil) = %+v", got)
	}

	got = initialTracks([]string{"id1"}, []string{"", "id2"})
	if len(got) != 2 || got[0].ID != "id1" || got[1].ID != "id2" {
		t.Fatalf("initialTracks(flags, args) = %+v", got)
	}

	got = initialTracks([]string{"https://www.youtube.com/watch?v=0wRYvsfhsR8&t=12"}, nil)
	if len(got) != 1 || got[0].ID != "0wRYvsfhsR8" {
		t.Fatalf("initialTracks(watch URL) = %+v, want normalized ID 0wRYvsfhsR8", got)
	}
}

type fakeStartupCatalog struct {
	playlist []model.Track
}

func (f fakeStartupCatalog) Search(context.Context, string, int) ([]model.Track, error) {
	return nil, nil
}
func (f fakeStartupCatalog) Playlist(context.Context, string) ([]model.Track, error) {
	return f.playlist, nil
}
func (f fakeStartupCatalog) Track(context.Context, string) (model.Track, error) {
	return model.Track{}, nil
}

func TestStartupTracksWithQueryAndPlaylist(t *testing.T) {
	cat := fakeStartupCatalog{playlist: []model.Track{{ID: "pl1"}, {ID: "pl2"}}}

	got, err := startupTracks(context.Background(), cat, nil, nil, "PL123", "")
	if err != nil || len(got) != 2 || got[0].ID != "pl1" {
		t.Fatalf("startupTracks playlist = %+v, err=%v", got, err)
	}

	got, err = startupTracks(context.Background(), cat, nil, nil, "", "daft punk")
	if err != nil || len(got) != 0 {
		t.Fatalf("startupTracks query = %+v, err=%v, want empty queue", got, err)
	}

	m3uFile := filepath.Join(t.TempDir(), "test.m3u")
	if err := os.WriteFile(m3uFile, []byte("#EXTM3U\nhttps://www.youtube.com/watch?v=0wRYvsfhsR8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = startupTracks(context.Background(), cat, nil, nil, m3uFile, "")
	if err != nil || len(got) != 1 || got[0].ID != "0wRYvsfhsR8" {
		t.Fatalf("startupTracks m3u = %+v, err=%v", got, err)
	}
}

func TestNewCatalogSelection(t *testing.T) {
	cat, name, err := newCatalog("", "")
	if err != nil || name != "ytmusic" {
		t.Fatalf("newCatalog(\"\") = (%T, %q, %v)", cat, name, err)
	}
	if _, ok := cat.(*catalogytmusic.Client); !ok {
		t.Fatalf("newCatalog(\"\") type = %T, want *ytmusic.Client", cat)
	}

	cat, name, err = newCatalog("YTMUSIC", "")
	if err != nil || name != "ytmusic" {
		t.Fatalf("newCatalog(YTMUSIC) = (%T, %q, %v)", cat, name, err)
	}
	if _, ok := cat.(*catalogytmusic.Client); !ok {
		t.Fatalf("newCatalog(YTMUSIC) type = %T, want *ytmusic.Client", cat)
	}

	cat, name, err = newCatalog("ytdlp", "/usr/bin/yt-dlp")
	if err != nil || name != "ytdlp" {
		t.Fatalf("newCatalog(ytdlp) = (%T, %q, %v)", cat, name, err)
	}
	if _, ok := cat.(*catalogytdlp.Client); !ok {
		t.Fatalf("newCatalog(ytdlp) type = %T, want *ytdlp.Client", cat)
	}

	if _, _, err := newCatalog("bogus", ""); err == nil {
		t.Fatal("newCatalog(bogus) succeeded, want error")
	}

	if opts := controllerOptions(catalogytmusic.New(), true); len(opts) != 2 {
		t.Fatalf("controllerOptions(ytmusic, true) len = %d, want 2", len(opts))
	}
	if opts := controllerOptions(catalogytdlp.New(), true); len(opts) != 0 {
		t.Fatalf("controllerOptions(ytdlp, true) len = %d, want 0", len(opts))
	}
}

func TestResolveStartupQueueRestoresPersistedAndRespectsCLI(t *testing.T) {
	cfg := store.DefaultConfig()
	cfg.Queue = []store.TrackRef{{ID: "FGBhQbmPwH8", Title: "One More Time"}, {ID: "0wRYvsfhsR8", Title: "Something About Us"}}
	cfg.QueueIndex = 1

	// When config exists and no CLI flags given, restore saved queue & index.
	tracks, idx, err := resolveStartupQueue(context.Background(), nil, nil, nil, "", "", cfg, true)
	if err != nil || len(tracks) != 2 || idx != 1 || tracks[0].ID != "FGBhQbmPwH8" {
		t.Fatalf("restored queue = (%+v, %d, %v)", tracks, idx, err)
	}

	// Explicit -url overrides restored queue.
	tracks, idx, err = resolveStartupQueue(context.Background(), nil, []string{"dQw4w9WgXcQ"}, nil, "", "", cfg, true)
	if err != nil || len(tracks) != 1 || idx != 0 || tracks[0].ID != "dQw4w9WgXcQ" {
		t.Fatalf("CLI override queue = (%+v, %d, %v)", tracks, idx, err)
	}

	// First run (no config file) seeds defaultTarget.
	tracks, idx, err = resolveStartupQueue(context.Background(), nil, nil, nil, "", "", store.DefaultConfig(), false)
	if err != nil || len(tracks) != 1 || idx != 0 || tracks[0].ID != defaultTarget {
		t.Fatalf("first run queue = (%+v, %d, %v)", tracks, idx, err)
	}
}

func TestLoadStartupSkinFallsBackWhenSavedSkinMissing(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}

	s, usedPath, warn, err := loadStartupSkin("", "/nonexistent/missing.wsz")
	if err != nil || s == nil {
		t.Fatalf("loadStartupSkin fallback failed: %v", err)
	}
	if usedPath != devSkinPath || !strings.Contains(warn, "missing.wsz") {
		t.Fatalf("fallback = path:%q warn:%q", usedPath, warn)
	}
}

func TestCheckToolStatusReportsMissingDependencyActionably(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	st := checkToolStatus(context.Background(), "yt-dlp", "")
	if st.OK || !strings.Contains(st.Error, "yt-dlp") || !strings.Contains(st.Hint, "yt-dlp") {
		t.Fatalf("missing tool status = %+v", st)
	}
}

func TestInitialCanvasHeightAndRepeatMode(t *testing.T) {
	cfg := store.DefaultConfig()
	if h := initialCanvasHeight(cfg); h != ui.MainWindowHeight+ui.PlaylistHeight {
		t.Fatalf("default height = %d, want %d", h, ui.MainWindowHeight+ui.PlaylistHeight)
	}
	cfg.Windows[store.WindowEQ] = store.WindowState{Visible: true}
	if h := initialCanvasHeight(cfg); h != ui.MainWindowHeight+ui.EQWindowHeight+ui.PlaylistHeight {
		t.Fatalf("with EQ height = %d", h)
	}
	cfg.Windows[store.WindowMain] = store.WindowState{Visible: true, Shaded: true}
	cfg.Windows[store.WindowEQ] = store.WindowState{Visible: true, Shaded: true}
	cfg.Windows[store.WindowPlaylist] = store.WindowState{Visible: true, Shaded: true}
	if h := initialCanvasHeight(cfg); h != 3*ui.ShadeWindowHeight {
		t.Fatalf("all shaded height = %d, want %d", h, 3*ui.ShadeWindowHeight)
	}
	if parseRepeatMode("all") != playback.RepeatAll || parseRepeatMode("one") != playback.RepeatOne || parseRepeatMode("off") != playback.RepeatOff {
		t.Fatal("parseRepeatMode mismatch")
	}
}
