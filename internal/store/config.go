// Package store implements versioned atomic JSON configuration persistence
// and safe file writes for ytm-wmp.
//
// It stays pure Go (no Ebitengine, playback, or ytdlp imports) and stores only
// track IDs and display metadata, never signed stream URLs or cookies.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"ytm-wmp/internal/model"
)

const (
	// CurrentVersion is the latest supported config.json schema version.
	CurrentVersion = 1

	// Window keys inside Config.Windows.
	WindowMain     = "main"
	WindowEQ       = "eq"
	WindowPlaylist = "playlist"

	// Repeat mode strings persisted in Config.Repeat.
	RepeatOff = "off"
	RepeatAll = "all"
	RepeatOne = "one"

	// DefaultCacheMaxMB is the default artwork disk cache cap in megabytes.
	DefaultCacheMaxMB = 256
	// DefaultSaveDebounce is how long UI state changes wait before flushing.
	DefaultSaveDebounce = 500 * time.Millisecond
)

// ErrUnsupportedVersion indicates config.json was written by a newer schema.
var ErrUnsupportedVersion = errors.New("unsupported config schema version")

// WindowState records visibility and optional coordinates for a rack panel.
type WindowState struct {
	Visible bool `json:"visible"`
	X       int  `json:"x,omitempty"`
	Y       int  `json:"y,omitempty"`
}

// ToolPaths stores optional user-configured overrides for external binaries.
type ToolPaths struct {
	YTDLP  string `json:"yt_dlp,omitempty"`
	FFmpeg string `json:"ffmpeg,omitempty"`
}

// TrackRef persists a queue entry by YouTube video ID and display metadata,
// never a signed stream URL.
type TrackRef struct {
	ID           string `json:"id"`
	Title        string `json:"title,omitempty"`
	Artist       string `json:"artist,omitempty"`
	Album        string `json:"album,omitempty"`
	DurationSec  int64  `json:"duration_sec,omitempty"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

// TrackRefFromModel converts a model.Track into a persistence-safe TrackRef.
func TrackRefFromModel(tr model.Track) TrackRef {
	id := model.NormalizeTrackID(tr.ID)
	var durSec int64
	if tr.Duration > 0 {
		durSec = int64(math.Round(tr.Duration.Seconds()))
	}
	thumb := strings.TrimSpace(tr.ThumbnailURL)
	if strings.Contains(strings.ToLower(thumb), "googlevideo.com") {
		thumb = ""
	}
	return TrackRef{
		ID:           id,
		Title:        strings.TrimSpace(tr.Title),
		Artist:       strings.TrimSpace(tr.Artist),
		Album:        strings.TrimSpace(tr.Album),
		DurationSec:  durSec,
		ThumbnailURL: thumb,
	}
}

// ToModel converts a persisted TrackRef back into a model.Track.
func (r TrackRef) ToModel() model.Track {
	id := model.NormalizeTrackID(r.ID)
	var dur time.Duration
	if r.DurationSec > 0 {
		dur = time.Duration(r.DurationSec) * time.Second
	}
	return model.Track{
		ID:           id,
		Title:        r.Title,
		Artist:       r.Artist,
		Album:        r.Album,
		Duration:     dur,
		ThumbnailURL: r.ThumbnailURL,
	}
}

// TrackRefsFromModel converts a slice of model.Tracks, skipping blank IDs.
func TrackRefsFromModel(tracks []model.Track) []TrackRef {
	if len(tracks) == 0 {
		return nil
	}
	out := make([]TrackRef, 0, len(tracks))
	for _, tr := range tracks {
		ref := TrackRefFromModel(tr)
		if ref.ID != "" {
			out = append(out, ref)
		}
	}
	return out
}

// TracksToModel converts persisted TrackRefs back into model.Tracks.
func TracksToModel(refs []TrackRef) []model.Track {
	if len(refs) == 0 {
		return nil
	}
	out := make([]model.Track, 0, len(refs))
	for _, ref := range refs {
		tr := ref.ToModel()
		if tr.ID != "" {
			out = append(out, tr)
		}
	}
	return out
}

// Config is the versioned application state stored in config.json.
type Config struct {
	Version       int                    `json:"version"`
	Skin          string                 `json:"skin,omitempty"`
	Volume        float64                `json:"volume"`
	Balance       float64                `json:"balance,omitempty"`
	Scale         int                    `json:"scale"`
	BufferMs      int                    `json:"buffer_ms,omitempty"`
	CacheDir      string                 `json:"cache_dir,omitempty"`
	CacheMaxMB    int                    `json:"cache_max_mb"`
	Windows       map[string]WindowState `json:"windows"`
	BrowserMode   bool                   `json:"browser_mode,omitempty"`
	TimeRemaining bool                   `json:"time_remaining,omitempty"`
	Queue         []TrackRef             `json:"queue,omitempty"`
	QueueIndex    int                    `json:"queue_index"`
	Shuffle       bool                   `json:"shuffle,omitempty"`
	Repeat        string                 `json:"repeat"`
	Autoplay      bool                   `json:"autoplay"`
	LastSearch    string                 `json:"last_search,omitempty"`
	ToolPaths     ToolPaths              `json:"tool_paths,omitempty"`
}

// DefaultConfig returns fresh v1 defaults matching the classic Winamp rack.
func DefaultConfig() Config {
	return Config{
		Version:    CurrentVersion,
		Volume:     1.0,
		Scale:      1,
		CacheMaxMB: DefaultCacheMaxMB,
		Windows: map[string]WindowState{
			WindowMain:     {Visible: true},
			WindowEQ:       {Visible: false},
			WindowPlaylist: {Visible: true},
		},
		QueueIndex: -1,
		Repeat:     RepeatOff,
		Autoplay:   true,
	}
}

// Migrate decodes raw config JSON, upgrades older schema versions, and clamps
// fields into safe ranges.
func Migrate(raw []byte) (Config, error) {
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return Config{}, fmt.Errorf("parsing config version: %w", err)
	}
	if probe.Version < 0 || probe.Version > CurrentVersion {
		return Config{}, fmt.Errorf("%w: got %d, max %d", ErrUnsupportedVersion, probe.Version, CurrentVersion)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config: %w", err)
	}

	// Version 0 -> 1 migration: defaults were already pre-populated before unmarshal.
	cfg.Version = CurrentVersion
	return Sanitize(cfg), nil
}

// Sanitize clamps numeric values, normalizes repeat/scale/window defaults, and
// filters invalid queue entries.
func Sanitize(cfg Config) Config {
	cfg.Version = CurrentVersion
	cfg.Skin = strings.TrimSpace(cfg.Skin)
	cfg.CacheDir = strings.TrimSpace(cfg.CacheDir)
	cfg.ToolPaths.YTDLP = strings.TrimSpace(cfg.ToolPaths.YTDLP)
	cfg.ToolPaths.FFmpeg = strings.TrimSpace(cfg.ToolPaths.FFmpeg)

	if math.IsNaN(cfg.Volume) || cfg.Volume < 0 {
		cfg.Volume = 0
	} else if cfg.Volume > 1 {
		cfg.Volume = 1
	}

	if math.IsNaN(cfg.Balance) || cfg.Balance < -1 {
		cfg.Balance = -1
	} else if cfg.Balance > 1 {
		cfg.Balance = 1
	}

	if cfg.Scale != 2 {
		cfg.Scale = 1
	}

	if cfg.BufferMs < 0 {
		cfg.BufferMs = 0
	} else if cfg.BufferMs > 5000 {
		cfg.BufferMs = 5000
	}

	if cfg.CacheMaxMB < 0 {
		cfg.CacheMaxMB = 0
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Repeat)) {
	case RepeatAll:
		cfg.Repeat = RepeatAll
	case RepeatOne:
		cfg.Repeat = RepeatOne
	default:
		cfg.Repeat = RepeatOff
	}

	if cfg.Windows == nil {
		cfg.Windows = make(map[string]WindowState, 3)
	}
	if _, ok := cfg.Windows[WindowMain]; !ok {
		cfg.Windows[WindowMain] = WindowState{Visible: true}
	}
	if _, ok := cfg.Windows[WindowEQ]; !ok {
		cfg.Windows[WindowEQ] = WindowState{Visible: false}
	}
	if _, ok := cfg.Windows[WindowPlaylist]; !ok {
		cfg.Windows[WindowPlaylist] = WindowState{Visible: true}
	}

	if len(cfg.Queue) > 0 {
		clean := make([]TrackRef, 0, len(cfg.Queue))
		for _, ref := range cfg.Queue {
			norm := TrackRefFromModel(ref.ToModel())
			if norm.ID != "" {
				clean = append(clean, norm)
			}
		}
		cfg.Queue = clean
	}
	if len(cfg.Queue) == 0 {
		cfg.Queue = nil
		cfg.QueueIndex = -1
	} else if cfg.QueueIndex < 0 {
		cfg.QueueIndex = 0
	} else if cfg.QueueIndex >= len(cfg.Queue) {
		cfg.QueueIndex = len(cfg.Queue) - 1
	}

	return cfg
}

// Load reads and migrates config.json from path. When path does not exist, it
// returns DefaultConfig(), existed=false, err=nil.
func Load(path string) (cfg Config, existed bool, err error) {
	if strings.TrimSpace(path) == "" {
		return DefaultConfig(), false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultConfig(), false, nil
		}
		return DefaultConfig(), false, err
	}
	cfg, err = Migrate(raw)
	if err != nil {
		return DefaultConfig(), true, err
	}
	return cfg, true, nil
}

// Save sanitizes, marshals, and atomically writes cfg to path.
func Save(path string, cfg Config) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	cfg = Sanitize(cfg)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return AtomicWriteFile(path, data)
}

// AtomicWriteFile writes data to a temporary file in the destination directory,
// syncs it to disk, and renames it atomically over path.
func AtomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if runtime.GOOS == "windows" {
			_ = os.Remove(path)
			if err2 := os.Rename(tmpPath, path); err2 == nil {
				cleanup = false
				return nil
			}
		}
		return err
	}
	cleanup = false
	return nil
}

// DebouncedSaver batches rapid configuration updates on the UI thread and
// flushes atomically once the debounce period elapses or on explicit Flush.
type DebouncedSaver struct {
	path     string
	debounce time.Duration
	now      func() time.Time

	dirty  bool
	saveAt time.Time
}

// NewDebouncedSaver creates a DebouncedSaver targeting path.
func NewDebouncedSaver(path string, debounce time.Duration, now func() time.Time) *DebouncedSaver {
	if debounce <= 0 {
		debounce = DefaultSaveDebounce
	}
	if now == nil {
		now = time.Now
	}
	return &DebouncedSaver{
		path:     path,
		debounce: debounce,
		now:      now,
	}
}

// MarkDirty schedules a save after the debounce interval.
func (d *DebouncedSaver) MarkDirty() {
	if d == nil || d.path == "" {
		return
	}
	d.dirty = true
	d.saveAt = d.now().Add(d.debounce)
}

// Dirty reports whether an unpersisted change is pending.
func (d *DebouncedSaver) Dirty() bool {
	return d != nil && d.dirty
}

// MaybeFlush saves buildCfg() if dirty and the debounce deadline has passed.
func (d *DebouncedSaver) MaybeFlush(buildCfg func() Config) error {
	if d == nil || !d.dirty || d.path == "" || d.now().Before(d.saveAt) {
		return nil
	}
	return d.Flush(buildCfg)
}

// Flush immediately saves buildCfg() if dirty, clearing the dirty flag on success.
func (d *DebouncedSaver) Flush(buildCfg func() Config) error {
	if d == nil || !d.dirty || d.path == "" {
		return nil
	}
	cfg := buildCfg()
	if err := Save(d.path, cfg); err != nil {
		return err
	}
	d.dirty = false
	return nil
}
