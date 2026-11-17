package store

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ytm-wmp/internal/model"
)

func TestDefaultConfigAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")

	// Missing file returns defaults without error.
	cfg, existed, err := Load(path)
	if err != nil {
		t.Fatalf("Load missing = %v", err)
	}
	if existed {
		t.Fatal("missing file reported existed=true")
	}
	if cfg.Version != CurrentVersion || cfg.Volume != 1.0 || cfg.Scale != 1 || !cfg.Autoplay {
		t.Fatalf("DefaultConfig = %+v", cfg)
	}

	cfg.Skin = "/path/to/winamp.wsz"
	cfg.Volume = 0.42
	cfg.Balance = -0.25
	cfg.Scale = 2
	cfg.BufferMs = 200
	cfg.CacheDir = "/tmp/art"
	cfg.CacheMaxMB = 128
	cfg.Windows[WindowEQ] = WindowState{Visible: true}
	cfg.Windows[WindowPlaylist] = WindowState{Visible: false}
	cfg.BrowserMode = true
	cfg.TimeRemaining = true
	cfg.Shuffle = true
	cfg.Repeat = RepeatAll
	cfg.Autoplay = false
	cfg.LastSearch = "daft punk"
	cfg.ToolPaths = ToolPaths{YTDLP: "/usr/local/bin/yt-dlp", FFmpeg: "/usr/local/bin/ffmpeg"}
	cfg.Queue = TrackRefsFromModel([]model.Track{
		{
			ID:           "https://www.youtube.com/watch?v=0wRYvsfhsR8",
			Title:        "Something About Us",
			Artist:       "Daft Punk",
			Album:        "Discovery",
			Duration:     232 * time.Second,
			ThumbnailURL: "https://i.ytimg.com/vi/0wRYvsfhsR8/hqdefault.jpg",
		},
		{
			ID:           "FGBhQbmPwH8",
			Title:        "One More Time",
			ThumbnailURL: "https://rr1---sn-example.googlevideo.com/videoplayback?sig=secret",
		},
	})
	cfg.QueueIndex = 1

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile = %v", err)
	}
	if strings.Contains(string(raw), "googlevideo.com") {
		t.Fatalf("config leaked signed stream URL:\n%s", string(raw))
	}

	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("Load saved = existed:%v err:%v", existed, err)
	}
	if loaded.Skin != cfg.Skin || loaded.Volume != 0.42 || loaded.Balance != -0.25 || loaded.Scale != 2 {
		t.Fatalf("loaded scalar mismatch: %+v", loaded)
	}
	if loaded.BufferMs != 200 || loaded.CacheDir != "/tmp/art" || loaded.CacheMaxMB != 128 {
		t.Fatalf("loaded cache/buffer mismatch: %+v", loaded)
	}
	if !loaded.Windows[WindowEQ].Visible || loaded.Windows[WindowPlaylist].Visible {
		t.Fatalf("loaded windows = %+v", loaded.Windows)
	}
	if !loaded.BrowserMode || !loaded.TimeRemaining || !loaded.Shuffle || loaded.Repeat != RepeatAll || loaded.Autoplay {
		t.Fatalf("loaded playback flags = %+v", loaded)
	}
	if loaded.LastSearch != "daft punk" || loaded.ToolPaths != cfg.ToolPaths {
		t.Fatalf("loaded search/tools = %+v", loaded)
	}
	if len(loaded.Queue) != 2 || loaded.Queue[0].ID != "0wRYvsfhsR8" || loaded.QueueIndex != 1 {
		t.Fatalf("loaded queue = %+v (idx=%d)", loaded.Queue, loaded.QueueIndex)
	}
	tracks := TracksToModel(loaded.Queue)
	if tracks[0].Duration != 232*time.Second || tracks[0].Artist != "Daft Punk" {
		t.Fatalf("TracksToModel[0] = %+v", tracks[0])
	}
}

func TestMigrateV0AndRejectFutureVersion(t *testing.T) {
	// v0 omits version and volume/autoplay; migration fills defaults.
	v0 := []byte(`{"skin":"cool.wsz","repeat":"ONE","queue":[{"id":"0wRYvsfhsR8"}],"queue_index":99}`)
	cfg, err := Migrate(v0)
	if err != nil {
		t.Fatalf("Migrate(v0) = %v", err)
	}
	if cfg.Version != CurrentVersion {
		t.Fatalf("Version = %d, want %d", cfg.Version, CurrentVersion)
	}
	if cfg.Volume != 1.0 || !cfg.Autoplay || cfg.Scale != 1 {
		t.Fatalf("v0 defaults not preserved: %+v", cfg)
	}
	if cfg.Repeat != RepeatOne {
		t.Fatalf("Repeat = %q, want %q", cfg.Repeat, RepeatOne)
	}
	if cfg.QueueIndex != 0 {
		t.Fatalf("QueueIndex = %d, want clamped 0", cfg.QueueIndex)
	}

	// Explicit volume=0 must NOT be overwritten by default 1.0.
	muted := []byte(`{"version":1,"volume":0,"autoplay":false}`)
	cfgMuted, err := Migrate(muted)
	if err != nil {
		t.Fatalf("Migrate(muted) = %v", err)
	}
	if cfgMuted.Volume != 0 || cfgMuted.Autoplay {
		t.Fatalf("explicit zero/false overwritten: %+v", cfgMuted)
	}

	// Future version is rejected cleanly.
	future := []byte(`{"version":999}`)
	if _, err := Migrate(future); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("Migrate(future) err = %v, want ErrUnsupportedVersion", err)
	}
}

func TestDebouncedSaver(t *testing.T) {
	now := time.Date(2026, 11, 16, 18, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "config.json")
	saver := NewDebouncedSaver(path, 500*time.Millisecond, func() time.Time { return now })

	vol := 0.5
	build := func() Config {
		c := DefaultConfig()
		c.Volume = vol
		return c
	}

	saver.MarkDirty()
	if !saver.Dirty() {
		t.Fatal("expected Dirty() after MarkDirty")
	}
	if err := saver.MaybeFlush(build); err != nil {
		t.Fatalf("MaybeFlush early = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config written before debounce elapsed: err=%v", err)
	}

	now = now.Add(600 * time.Millisecond)
	if err := saver.MaybeFlush(build); err != nil {
		t.Fatalf("MaybeFlush due = %v", err)
	}
	if saver.Dirty() {
		t.Fatal("expected Dirty() cleared after MaybeFlush")
	}

	loaded, existed, err := Load(path)
	if err != nil || !existed || loaded.Volume != 0.5 {
		t.Fatalf("Load after MaybeFlush = (%+v, %v, %v)", loaded, existed, err)
	}

	// Explicit Flush writes immediately regardless of deadline.
	vol = 0.9
	saver.MarkDirty()
	if err := saver.Flush(build); err != nil {
		t.Fatalf("Flush = %v", err)
	}
	loaded, _, err = Load(path)
	if err != nil || loaded.Volume != 0.9 {
		t.Fatalf("Load after Flush = (%+v, %v)", loaded, err)
	}
	// Verify valid JSON.
	raw, _ := os.ReadFile(path)
	if !json.Valid(raw) {
		t.Fatalf("saved config is invalid JSON: %s", raw)
	}
}

func TestSanitizeScale(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{0, 1.0},
		{-1, 1.0},
		{math.NaN(), 1.0},
		{math.Inf(1), 1.0},
		{1.0, 1.0},
		{1.1, 1.1},
		{1.24, 1.2},
		{1.26, 1.3},
		{1.9, 1.9},
		{2.0, 2.0},
		{2.5, 2.0},
	}
	for _, tc := range cases {
		got := SanitizeScale(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeScale(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}

	cfg, err := Migrate([]byte(`{"version":1,"scale":1.3}`))
	if err != nil {
		t.Fatalf("Migrate = %v", err)
	}
	if cfg.Scale != 1.3 {
		t.Fatalf("cfg.Scale = %v, want 1.3", cfg.Scale)
	}
}
