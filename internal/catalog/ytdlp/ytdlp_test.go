package ytdlp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"ytm-wmp/internal/catalog"
)

type fakeRunner struct {
	mu       sync.Mutex
	calls    int
	lastArgs []string
	fn       func(call int) (stdout, stderr []byte, err error)
}

func (f *fakeRunner) Output(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
	f.mu.Lock()
	f.calls++
	f.lastArgs = append([]string(nil), args...)
	call := f.calls
	fn := f.fn
	f.mu.Unlock()

	if fn != nil {
		return fn(call)
	}
	return nil, nil, nil
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeRunner) args() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lastArgs...)
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestSearchDecodesTenResultsAndBuildsFlags(t *testing.T) {
	fixture := readFixture(t, "search_results.json")
	runner := &fakeRunner{fn: func(int) ([]byte, []byte, error) {
		return fixture, nil, nil
	}}
	client := New(WithRunner(runner))

	tracks, err := client.Search(context.Background(), "  daft punk  ", 10)
	if err != nil {
		t.Fatalf("Search = %v", err)
	}

	wantArgs := []string{"-J", "--no-warnings", "--flat-playlist", "--playlist-end", "10", "ytsearch10:daft punk"}
	if !reflect.DeepEqual(runner.args(), wantArgs) {
		t.Fatalf("args = %q, want %q", runner.args(), wantArgs)
	}

	if len(tracks) != 10 {
		t.Fatalf("len(tracks) = %d, want 10", len(tracks))
	}

	first := tracks[0]
	if first.ID != "FGBhQbmPwH8" || first.Title != "One More Time" || first.Artist != "Daft Punk" || first.Album != "Discovery" {
		t.Fatalf("first track = %+v", first)
	}
	if first.Duration != 320*time.Second {
		t.Fatalf("first duration = %v, want 320s", first.Duration)
	}
	// On ties at 480x360, .jpg must win over .webp.
	if first.ThumbnailURL != "https://i.ytimg.com/vi/FGBhQbmPwH8/hqdefault.jpg" {
		t.Fatalf("first thumbnail = %q, want hqdefault.jpg", first.ThumbnailURL)
	}

	// Entry without explicit thumbnails falls back to i.ytimg.com hqdefault.jpg.
	if tracks[3].ID != "gN2hntZBIUQ" || tracks[3].Artist != "Daft Punk" || tracks[3].ThumbnailURL != "https://i.ytimg.com/vi/gN2hntZBIUQ/hqdefault.jpg" {
		t.Fatalf("track[3] = %+v", tracks[3])
	}
}

func TestSearchCacheHitAndExpiry(t *testing.T) {
	now := time.Date(2026, 11, 15, 20, 0, 0, 0, time.UTC)
	fixture := readFixture(t, "search_results.json")
	runner := &fakeRunner{fn: func(int) ([]byte, []byte, error) {
		return fixture, nil, nil
	}}
	client := New(
		WithRunner(runner),
		WithClock(func() time.Time { return now }),
		WithSearchTTL(10*time.Minute),
	)

	first, err := client.Search(context.Background(), "Daft Punk", 10)
	if err != nil {
		t.Fatalf("first Search = %v", err)
	}
	first[0].Title = "mutated-by-caller"

	second, err := client.Search(context.Background(), "daft punk", 10)
	if err != nil {
		t.Fatalf("second Search = %v", err)
	}
	if runner.callCount() != 1 {
		t.Fatalf("runner calls = %d, want 1 cached hit", runner.callCount())
	}
	if second[0].Title != "One More Time" {
		t.Fatalf("second[0].Title = %q, want cloned slice unmutated", second[0].Title)
	}

	now = now.Add(11 * time.Minute)
	if _, err := client.Search(context.Background(), "daft punk", 10); err != nil {
		t.Fatalf("third Search = %v", err)
	}
	if runner.callCount() != 2 {
		t.Fatalf("runner calls after TTL = %d, want 2", runner.callCount())
	}
}

func TestTrackLookupAndCachePopulationFromSearch(t *testing.T) {
	searchFixture := readFixture(t, "search_results.json")
	trackFixture := readFixture(t, "track_metadata.json")
	runner := &fakeRunner{fn: func(call int) ([]byte, []byte, error) {
		if call == 1 {
			return searchFixture, nil, nil
		}
		return trackFixture, nil, nil
	}}
	client := New(WithRunner(runner))

	if _, err := client.Search(context.Background(), "daft punk", 10); err != nil {
		t.Fatalf("Search = %v", err)
	}

	// Track found in search result should hit the track cache immediately.
	cached, err := client.Track(context.Background(), "https://www.youtube.com/watch?v=FGBhQbmPwH8")
	if err != nil {
		t.Fatalf("cached Track = %v", err)
	}
	if cached.Title != "One More Time" || runner.callCount() != 1 {
		t.Fatalf("cached = %+v, calls = %d, want 1", cached, runner.callCount())
	}

	// Uncached bare ID should resolve via watch URL and populate cache.
	tr, err := client.Track(context.Background(), "0wRYvsfhsR8")
	if err != nil {
		t.Fatalf("uncached Track = %v", err)
	}
	wantArgs := []string{"-J", "--no-warnings", "--no-playlist", "https://www.youtube.com/watch?v=0wRYvsfhsR8"}
	if !reflect.DeepEqual(runner.args(), wantArgs) {
		t.Fatalf("track args = %q, want %q", runner.args(), wantArgs)
	}
	if tr.ID != "0wRYvsfhsR8" || tr.Title != "Something About Us" || tr.Duration != 232*time.Second {
		t.Fatalf("tr = %+v", tr)
	}
	if tr.ThumbnailURL != "https://i.ytimg.com/vi/0wRYvsfhsR8/mqdefault.jpg" {
		t.Fatalf("ThumbnailURL = %q, want closest to 360px", tr.ThumbnailURL)
	}
}

func TestPlaylistExtractionAndArgs(t *testing.T) {
	fixture := readFixture(t, "playlist.json")
	runner := &fakeRunner{fn: func(int) ([]byte, []byte, error) {
		return fixture, nil, nil
	}}
	client := New(WithRunner(runner), WithPlaylistLimit(50))

	tracks, err := client.Playlist(context.Background(), "PL1234567890abcdef")
	if err != nil {
		t.Fatalf("Playlist = %v", err)
	}

	wantArgs := []string{"-J", "--no-warnings", "--flat-playlist", "--playlist-end", "50", "https://www.youtube.com/playlist?list=PL1234567890abcdef"}
	if !reflect.DeepEqual(runner.args(), wantArgs) {
		t.Fatalf("playlist args = %q, want %q", runner.args(), wantArgs)
	}
	if len(tracks) != 2 {
		t.Fatalf("len(tracks) = %d, want 2 (skipping private entry)", len(tracks))
	}
	if tracks[1].ID != "A2VpR8HahKc" {
		t.Fatalf("tracks[1].ID = %q, want A2VpR8HahKc extracted from URL", tracks[1].ID)
	}
}

func TestEmptyQueryReturnsErrEmptyQuery(t *testing.T) {
	client := New(WithRunner(&fakeRunner{}))

	if _, err := client.Search(context.Background(), "  ", 10); !errors.Is(err, catalog.ErrEmptyQuery) {
		t.Fatalf("Search empty = %v, want ErrEmptyQuery", err)
	}
	if _, err := client.Track(context.Background(), ""); !errors.Is(err, catalog.ErrEmptyQuery) {
		t.Fatalf("Track empty = %v, want ErrEmptyQuery", err)
	}
	if _, err := client.Playlist(context.Background(), "\t"); !errors.Is(err, catalog.ErrEmptyQuery) {
		t.Fatalf("Playlist empty = %v, want ErrEmptyQuery", err)
	}
}

func TestLimitedExecRunnerEnforcesStdoutCap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake yt-dlp is not available on Windows")
	}

	script := filepath.Join(t.TempDir(), "big-stdout")
	content := "#!/bin/sh\ndd if=/dev/zero bs=1024 count=64 2>/dev/null\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	runner := LimitedExecRunner{MaxBytes: 4096}
	_, _, err := runner.Output(context.Background(), script)
	if !errors.Is(err, catalog.ErrOutputTooLarge) {
		t.Fatalf("Output err = %v, want ErrOutputTooLarge", err)
	}
}

func TestLiveCatalogSearch(t *testing.T) {
	if os.Getenv("YTM_LIVE") != "1" && os.Getenv("YTM_WMP_LIVE_TEST") != "1" {
		t.Skip("set YTM_LIVE=1 to run live yt-dlp catalog test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := New()
	tracks, err := client.Search(ctx, "daft punk something about us", 5)
	if err != nil {
		t.Fatalf("live Search = %v", err)
	}
	if len(tracks) == 0 {
		t.Fatal("live Search returned 0 tracks")
	}
	if tracks[0].ID == "" || tracks[0].Title == "" {
		t.Fatalf("live Search returned incomplete track: %+v", tracks[0])
	}
}
