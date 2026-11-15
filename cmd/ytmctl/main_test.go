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

	"ytm-wmp/internal/artwork"
	"ytm-wmp/internal/model"
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

	if err := execute(context.Background(), nil, &out, &errOut, nil, nil); err == nil {
		t.Fatal("empty args succeeded, want error")
	}
	if err := execute(context.Background(), []string{"nope"}, &out, &errOut, nil, nil); err == nil {
		t.Fatal("unknown subcommand succeeded, want error")
	}
}
