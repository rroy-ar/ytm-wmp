// Package ytdlp implements catalog.Catalog on top of `yt-dlp -J`.
//
// It owns JSON parsing, bounded stdout execution, and short-lived search and
// track metadata caches so yt-dlp schema never leaks outside this package.
package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"ytm-wmp/internal/catalog"
	"ytm-wmp/internal/model"
)

const (
	// DefaultSearchLimit is used when Search is given limit <= 0.
	DefaultSearchLimit = 10
	// MaxSearchLimit caps how many search hits one ytsearch request retrieves.
	MaxSearchLimit = 50
	// DefaultPlaylistLimit caps how many tracks a playlist import extracts.
	DefaultPlaylistLimit = 100
	// DefaultSearchTTL caches identical search queries in memory.
	DefaultSearchTTL = 10 * time.Minute
	// DefaultTrackTTL caches track metadata keyed by video ID in memory.
	DefaultTrackTTL = 1 * time.Hour
	// DefaultMaxStdoutBytes caps yt-dlp -J stdout so runaway output cannot OOM.
	DefaultMaxStdoutBytes int64 = 16 << 20

	defaultBinary      = "yt-dlp"
	defaultStderrLimit = 64 << 10
	targetThumbDim     = 360
)

// Runner executes an external command and captures stdout, stderr, and error.
type Runner interface {
	Output(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// LimitedExecRunner invokes real subprocesses with bounded stdout and stderr.
type LimitedExecRunner struct {
	MaxBytes int64
}

// Output runs name with args and enforces MaxBytes on stdout.
func (r LimitedExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	maxBytes := r.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxStdoutBytes
	}

	cmd := exec.CommandContext(ctx, name, args...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderr := &limitedBuffer{max: defaultStderrLimit}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		_ = stdoutPipe.Close()
		return nil, nil, err
	}

	stdout, readErr := io.ReadAll(io.LimitReader(stdoutPipe, maxBytes+1))
	if int64(len(stdout)) > maxBytes {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		return nil, stderr.Bytes(), fmt.Errorf("%w (%d bytes)", catalog.ErrOutputTooLarge, maxBytes)
	}

	waitErr := cmd.Wait()
	if readErr != nil {
		return stdout, stderr.Bytes(), readErr
	}
	return stdout, stderr.Bytes(), waitErr
}

type limitedBuffer struct {
	max int
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.max <= 0 {
		return len(p), nil
	}
	remain := b.max - b.buf.Len()
	if remain > 0 {
		if len(p) > remain {
			b.buf.Write(p[:remain])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

type cachedTracks struct {
	tracks  []model.Track
	expires time.Time
}

type cachedTrack struct {
	track   model.Track
	expires time.Time
}

// Client implements catalog.Catalog using `yt-dlp -J`.
type Client struct {
	runner        Runner
	binary        string
	searchTTL     time.Duration
	trackTTL      time.Duration
	playlistLimit int
	maxStdout     int64
	now           func() time.Time

	mu          sync.Mutex
	searchCache map[string]cachedTracks
	trackCache  map[string]cachedTrack
}

// Option customizes a Client.
type Option func(*Client)

// WithRunner replaces the subprocess runner (used in tests).
func WithRunner(r Runner) Option {
	return func(c *Client) {
		if r != nil {
			c.runner = r
		}
	}
}

// WithBinary sets the yt-dlp executable path.
func WithBinary(path string) Option {
	return func(c *Client) {
		if path != "" {
			c.binary = path
		}
	}
}

// WithClock overrides the clock used for cache expiration.
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// WithSearchTTL changes how long search results are cached; <= 0 disables caching.
func WithSearchTTL(d time.Duration) Option {
	return func(c *Client) { c.searchTTL = d }
}

// WithTrackTTL changes how long track metadata is cached; <= 0 disables caching.
func WithTrackTTL(d time.Duration) Option {
	return func(c *Client) { c.trackTTL = d }
}

// WithPlaylistLimit sets the maximum number of items extracted from playlists.
func WithPlaylistLimit(n int) Option {
	return func(c *Client) {
		if n > 0 {
			c.playlistLimit = n
		}
	}
}

// WithMaxStdoutBytes sets the stdout cap enforced by LimitedExecRunner.
func WithMaxStdoutBytes(n int64) Option {
	return func(c *Client) {
		if n > 0 {
			c.maxStdout = n
		}
	}
}

// New creates a yt-dlp catalog Client.
func New(opts ...Option) *Client {
	c := &Client{
		binary:        defaultBinary,
		searchTTL:     DefaultSearchTTL,
		trackTTL:      DefaultTrackTTL,
		playlistLimit: DefaultPlaylistLimit,
		maxStdout:     DefaultMaxStdoutBytes,
		now:           time.Now,
		searchCache:   make(map[string]cachedTracks),
		trackCache:    make(map[string]cachedTrack),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if c.runner == nil {
		c.runner = LimitedExecRunner{MaxBytes: c.maxStdout}
	}
	return c
}

// SetBinary updates the yt-dlp executable path used for subsequent catalog commands.
func (c *Client) SetBinary(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultBinary
	}
	c.mu.Lock()
	c.binary = path
	c.mu.Unlock()
}

func (c *Client) currentBinary() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.binary
}

// Search queries YouTube via `yt-dlp -J --flat-playlist ytsearchN:<query>`.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]model.Track, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, catalog.ErrEmptyQuery
	}
	if limit <= 0 {
		limit = DefaultSearchLimit
	} else if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}

	key := fmt.Sprintf("%d:%s", limit, strings.ToLower(query))
	if c.searchTTL > 0 {
		now := c.now()
		c.mu.Lock()
		cached, ok := c.searchCache[key]
		c.mu.Unlock()
		if ok && now.Before(cached.expires) {
			return append([]model.Track(nil), cached.tracks...), nil
		}
	}

	args := []string{
		"-J",
		"--no-warnings",
		"--flat-playlist",
		"--playlist-end", strconv.Itoa(limit),
		fmt.Sprintf("ytsearch%d:%s", limit, query),
	}

	stdout, stderr, err := c.runner.Output(ctx, c.currentBinary(), args...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, ytdlpError("search", query, err, stderr)
	}

	var env ytdlpEnvelope
	if err := json.Unmarshal(stdout, &env); err != nil {
		return nil, fmt.Errorf("decoding yt-dlp search output for %q: %w", query, err)
	}

	tracks := c.extractEntries(env.Entries)
	c.cacheTracks(tracks)
	if c.searchTTL > 0 {
		c.mu.Lock()
		c.searchCache[key] = cachedTracks{
			tracks:  append([]model.Track(nil), tracks...),
			expires: c.now().Add(c.searchTTL),
		}
		c.mu.Unlock()
	}
	return append([]model.Track(nil), tracks...), nil
}

// Track fetches metadata for a single video ID or watch URL.
func (c *Client) Track(ctx context.Context, idOrURL string) (model.Track, error) {
	idOrURL = strings.TrimSpace(idOrURL)
	if idOrURL == "" {
		return model.Track{}, catalog.ErrEmptyQuery
	}

	vid := model.VideoID(idOrURL)
	if vid != "" && c.trackTTL > 0 {
		now := c.now()
		c.mu.Lock()
		cached, ok := c.trackCache[vid]
		c.mu.Unlock()
		if ok && now.Before(cached.expires) {
			return cached.track, nil
		}
	}

	target := idOrURL
	if model.IsVideoID(idOrURL) {
		target = "https://www.youtube.com/watch?v=" + idOrURL
	}

	args := []string{
		"-J",
		"--no-warnings",
		"--no-playlist",
		target,
	}

	stdout, stderr, err := c.runner.Output(ctx, c.currentBinary(), args...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return model.Track{}, ctxErr
		}
		return model.Track{}, ytdlpError("track", idOrURL, err, stderr)
	}

	var entry ytdlpEntry
	if err := json.Unmarshal(stdout, &entry); err != nil {
		return model.Track{}, fmt.Errorf("decoding yt-dlp track output for %q: %w", idOrURL, err)
	}

	track, ok := toTrack(entry)
	if !ok {
		return model.Track{}, fmt.Errorf("%w: %q", catalog.ErrNotFound, idOrURL)
	}
	c.cacheTracks([]model.Track{track})
	return track, nil
}

// Playlist extracts tracks from a YouTube or YouTube Music playlist URL or ID.
func (c *Client) Playlist(ctx context.Context, idOrURL string) ([]model.Track, error) {
	idOrURL = strings.TrimSpace(idOrURL)
	if idOrURL == "" {
		return nil, catalog.ErrEmptyQuery
	}

	target := idOrURL
	lower := strings.ToLower(idOrURL)
	if catalog.IsPlaylistTarget(idOrURL) && !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		target = "https://www.youtube.com/playlist?list=" + idOrURL
	}

	args := []string{
		"-J",
		"--no-warnings",
		"--flat-playlist",
		"--playlist-end", strconv.Itoa(c.playlistLimit),
		target,
	}

	stdout, stderr, err := c.runner.Output(ctx, c.currentBinary(), args...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, ytdlpError("playlist", idOrURL, err, stderr)
	}

	var env ytdlpEnvelope
	if err := json.Unmarshal(stdout, &env); err != nil {
		return nil, fmt.Errorf("decoding yt-dlp playlist output for %q: %w", idOrURL, err)
	}

	var tracks []model.Track
	if len(env.Entries) > 0 {
		tracks = c.extractEntries(env.Entries)
	} else if env.ID != "" && env.Type != "playlist" {
		if track, ok := toTrack(env.ytdlpEntry); ok {
			tracks = append(tracks, track)
		}
	}

	c.cacheTracks(tracks)
	return tracks, nil
}

func (c *Client) extractEntries(entries []*ytdlpEntry) []model.Track {
	tracks := make([]model.Track, 0, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		if track, ok := toTrack(*entry); ok {
			tracks = append(tracks, track)
		}
	}
	return tracks
}

func (c *Client) cacheTracks(tracks []model.Track) {
	if c.trackTTL <= 0 || len(tracks) == 0 {
		return
	}
	expires := c.now().Add(c.trackTTL)
	c.mu.Lock()
	for _, tr := range tracks {
		if tr.ID == "" {
			continue
		}
		c.trackCache[tr.ID] = cachedTrack{track: tr, expires: expires}
	}
	c.mu.Unlock()
}

type ytdlpEnvelope struct {
	ytdlpEntry
	Type    string        `json:"_type"`
	Entries []*ytdlpEntry `json:"entries"`
}

type ytdlpEntry struct {
	ID         string           `json:"id"`
	URL        string           `json:"url"`
	WebpageURL string           `json:"webpage_url"`
	Title      string           `json:"title"`
	Track      string           `json:"track"`
	Artist     string           `json:"artist"`
	Creator    string           `json:"creator"`
	Channel    string           `json:"channel"`
	Uploader   string           `json:"uploader"`
	Album      string           `json:"album"`
	Duration   float64          `json:"duration"`
	Thumbnail  string           `json:"thumbnail"`
	Thumbnails []ytdlpThumbnail `json:"thumbnails"`
}

type ytdlpThumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func toTrack(e ytdlpEntry) (model.Track, bool) {
	id := model.NormalizeTrackID(e.ID)
	if id == "" {
		id = model.VideoID(e.URL)
	}
	if id == "" {
		id = model.VideoID(e.WebpageURL)
	}
	if id == "" {
		return model.Track{}, false
	}

	var dur time.Duration
	if e.Duration > 0 {
		dur = time.Duration(e.Duration * float64(time.Second))
	}

	return model.Track{
		ID:           id,
		Title:        firstNonEmpty(e.Title, e.Track, id),
		Artist:       firstNonEmpty(e.Artist, e.Creator, e.Channel, e.Uploader),
		Album:        strings.TrimSpace(e.Album),
		Duration:     dur,
		ThumbnailURL: pickThumbnail(e.Thumbnails, e.Thumbnail, id),
	}, true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func pickThumbnail(thumbs []ytdlpThumbnail, fallbackURL, id string) string {
	bestURL := ""
	bestDist := 0
	bestNonWebP := false
	hasSized := false
	lastHTTP := ""

	for _, t := range thumbs {
		u := strings.TrimSpace(t.URL)
		if !isHTTPURL(u) {
			continue
		}
		lastHTTP = u

		if t.Width <= 0 && t.Height <= 0 {
			continue
		}
		dim := t.Width
		if t.Height > dim {
			dim = t.Height
		}
		dist := dim - targetThumbDim
		if dist < 0 {
			dist = -dist
		}
		nonWebP := !isWebPURL(u)

		if !hasSized || dist < bestDist || (dist == bestDist && nonWebP && !bestNonWebP) {
			hasSized = true
			bestDist = dist
			bestNonWebP = nonWebP
			bestURL = u
		}
	}

	if hasSized {
		return bestURL
	}
	if lastHTTP != "" {
		return lastHTTP
	}
	if fb := strings.TrimSpace(fallbackURL); isHTTPURL(fb) {
		return fb
	}
	return model.DefaultThumbnailURL(id)
}

func isHTTPURL(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func isWebPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.HasSuffix(strings.ToLower(raw), ".webp")
	}
	return strings.EqualFold(path.Ext(u.Path), ".webp")
}

func ytdlpError(op, target string, err error, stderr []byte) error {
	msg := strings.TrimSpace(string(stderr))
	if len(msg) > 1000 {
		msg = "..." + msg[len(msg)-1000:]
	}
	if msg != "" {
		return fmt.Errorf("yt-dlp %s %q: %w: %s", op, target, err, msg)
	}
	return fmt.Errorf("yt-dlp %s %q: %w", op, target, err)
}
