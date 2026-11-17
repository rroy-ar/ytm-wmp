package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rroy-ar/ytm-wmp/internal/artwork"
	"github.com/rroy-ar/ytm-wmp/internal/catalog"
	catalogytmusic "github.com/rroy-ar/ytm-wmp/internal/catalog/ytmusic"
	"github.com/rroy-ar/ytm-wmp/internal/model"
)

type fakeCatalog struct {
	searchTracks   []model.Track
	playlistTracks []model.Track
	track          model.Track
}

func (f *fakeCatalog) Search(context.Context, string, int) ([]model.Track, error) {
	return f.searchTracks, nil
}

func (f *fakeCatalog) Playlist(context.Context, string) ([]model.Track, error) {
	return f.playlistTracks, nil
}

func (f *fakeCatalog) Track(context.Context, string) (model.Track, error) {
	return f.track, nil
}

func TestExecuteSearchTextAndJSON(t *testing.T) {
	cat := &fakeCatalog{
		searchTracks: []model.Track{
			{
				ID:           "FGBhQbmPwH8",
				Title:        "One More Time",
				Artist:       "Daft Punk",
				Duration:     320 * time.Second,
				ThumbnailURL: "https://i.ytimg.com/vi/FGBhQbmPwH8/hqdefault.jpg",
			},
		},
	}

	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"search", "-limit", "5", "daft", "punk"}, &out, &errOut, cat, nil); err != nil {
		t.Fatalf("search text = %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "FGBhQbmPwH8") || !strings.Contains(text, "5:20") || !strings.Contains(text, "One More Time") {
		t.Fatalf("unexpected search table output:\n%s", text)
	}

	out.Reset()
	if err := execute(context.Background(), []string{"search", "-json", "daft punk"}, &out, &errOut, cat, nil); err != nil {
		t.Fatalf("search json = %v", err)
	}
	var decoded []model.Track
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if len(decoded) != 1 || decoded[0].ID != "FGBhQbmPwH8" {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestExecuteTrackAndPlaylistLimit(t *testing.T) {
	cat := &fakeCatalog{
		track: model.Track{ID: "0wRYvsfhsR8", Title: "Something About Us", Artist: "Daft Punk", Duration: 232 * time.Second},
		playlistTracks: []model.Track{
			{ID: "t1", Title: "One"},
			{ID: "t2", Title: "Two"},
			{ID: "t3", Title: "Three"},
		},
	}

	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"track", "-json", "0wRYvsfhsR8"}, &out, &errOut, cat, nil); err != nil {
		t.Fatalf("track = %v", err)
	}
	var tr model.Track
	if err := json.Unmarshal(out.Bytes(), &tr); err != nil {
		t.Fatalf("track json: %v", err)
	}
	if tr.ID != "0wRYvsfhsR8" {
		t.Fatalf("track = %+v", tr)
	}

	out.Reset()
	if err := execute(context.Background(), []string{"playlist", "-limit", "2", "-json", "PL123"}, &out, &errOut, cat, nil); err != nil {
		t.Fatalf("playlist = %v", err)
	}
	var pl []model.Track
	if err := json.Unmarshal(out.Bytes(), &pl); err != nil {
		t.Fatalf("playlist json: %v", err)
	}
	if len(pl) != 2 {
		t.Fatalf("len(playlist) = %d, want 2", len(pl))
	}
}

func TestExecuteArtworkAndErrors(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	img.SetRGBA(0, 0, color.RGBA{R: 0x33, G: 0xaa, B: 0x77, A: 0xff})
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pngBuf.Bytes())
	}))
	defer srv.Close()

	cache := artwork.NewCache(artwork.WithCacheDir(t.TempDir()))
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"artwork", "dQw4w9WgXcQ", srv.URL}, &out, &errOut, nil, cache); err != nil {
		t.Fatalf("artwork = %v", err)
	}
	if !strings.Contains(out.String(), "list=64x64") || !strings.Contains(out.String(), "now_playing=300x300") {
		t.Fatalf("artwork output = %q", out.String())
	}

	out.Reset()
	catCache := artwork.NewCache(artwork.WithCacheDir(t.TempDir()))
	cat := &fakeCatalog{
		track: model.Track{
			ID:           "0wRYvsfhsR8",
			ThumbnailURL: srv.URL,
		},
	}
	if err := execute(context.Background(), []string{"artwork", "0wRYvsfhsR8"}, &out, &errOut, cat, catCache); err != nil {
		t.Fatalf("artwork via catalog = %v", err)
	}
	if !strings.Contains(out.String(), "video_id=0wRYvsfhsR8") || !strings.Contains(out.String(), "list=64x64") || !strings.Contains(out.String(), "now_playing=300x300") {
		t.Fatalf("artwork via catalog output = %q", out.String())
	}

	if err := execute(context.Background(), nil, &out, &errOut, nil, nil); err == nil {
		t.Fatal("empty args succeeded, want error")
	}
	if err := execute(context.Background(), []string{"nope"}, &out, &errOut, nil, nil); err == nil {
		t.Fatal("unknown subcommand succeeded, want error")
	}
}

func TestExecuteCatalogFlagSelection(t *testing.T) {
	cat := &fakeCatalog{
		searchTracks: []model.Track{{ID: "FGBhQbmPwH8", Title: "One More Time"}},
	}

	var gotBackends []string
	factory := func(_ context.Context, backend string) (catalog.Catalog, error) {
		gotBackends = append(gotBackends, backend)
		return cat, nil
	}

	var out, errOut bytes.Buffer
	// Default selects ytmusic.
	if err := executeWithFactory(context.Background(), []string{"search", "daft punk"}, &out, &errOut, nil, nil, factory); err != nil {
		t.Fatalf("default search = %v", err)
	}
	// Global flag before subcommand selects ytdlp.
	if err := executeWithFactory(context.Background(), []string{"-catalog", "ytdlp", "search", "daft punk"}, &out, &errOut, nil, nil, factory); err != nil {
		t.Fatalf("global -catalog ytdlp = %v", err)
	}
	// Subcommand flag overrides global default.
	if err := executeWithFactory(context.Background(), []string{"search", "-catalog", "ytdlp", "daft punk"}, &out, &errOut, nil, nil, factory); err != nil {
		t.Fatalf("subcommand -catalog ytdlp = %v", err)
	}
	// Subcommand flag overrides global flag.
	if err := executeWithFactory(context.Background(), []string{"-catalog", "ytdlp", "search", "-catalog", "ytmusic", "daft punk"}, &out, &errOut, nil, nil, factory); err != nil {
		t.Fatalf("subcommand override -catalog ytmusic = %v", err)
	}

	want := []string{"ytmusic", "ytdlp", "ytdlp", "ytmusic"}
	if len(gotBackends) != len(want) {
		t.Fatalf("gotBackends = %v, want %v", gotBackends, want)
	}
	for i := range want {
		if gotBackends[i] != want[i] {
			t.Fatalf("gotBackends[%d] = %q, want %q", i, gotBackends[i], want[i])
		}
	}

	// Default factory instantiates pure-Go ytmusic without requiring yt-dlp.
	defaultCat, err := defaultCatalogFactory(context.Background(), "ytmusic")
	if err != nil {
		t.Fatalf("defaultCatalogFactory(ytmusic) = %v", err)
	}
	if _, ok := defaultCat.(*catalogytmusic.Client); !ok {
		t.Fatalf("defaultCatalogFactory(ytmusic) = %T, want *ytmusic.Client", defaultCat)
	}

	// Invalid -catalog fails cleanly both before and inside subcommands.
	if err := execute(context.Background(), []string{"-catalog", "bogus", "search", "daft"}, &out, &errOut, nil, nil); err == nil {
		t.Fatal("invalid global -catalog succeeded, want error")
	}
	if err := execute(context.Background(), []string{"search", "-catalog", "bogus", "daft"}, &out, &errOut, nil, nil); err == nil {
		t.Fatal("invalid subcommand -catalog succeeded, want error")
	}
}

type fakeRadioCatalog struct {
	fakeCatalog
	radioTracks []model.Track
}

func (f *fakeRadioCatalog) Related(context.Context, string) ([]model.Track, error) {
	return f.radioTracks, nil
}

func TestExecuteRadio(t *testing.T) {
	radioCat := &fakeRadioCatalog{
		radioTracks: []model.Track{
			{ID: "r1", Title: "Radio 1"},
			{ID: "r2", Title: "Radio 2"},
		},
	}

	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"radio", "-limit", "1", "-json", "0wRYvsfhsR8"}, &out, &errOut, radioCat, nil); err != nil {
		t.Fatalf("radio = %v", err)
	}
	var decoded []model.Track
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("radio json: %v", err)
	}
	if len(decoded) != 1 || decoded[0].ID != "r1" {
		t.Fatalf("decoded = %+v, want [r1]", decoded)
	}

	// Catalog without RadioProvider returns a clear error.
	if err := execute(context.Background(), []string{"radio", "0wRYvsfhsR8"}, &out, &errOut, &fakeCatalog{}, nil); err == nil {
		t.Fatal("radio without RadioProvider succeeded, want error")
	}
}
