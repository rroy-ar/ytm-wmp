// Package artwork fetches, decodes, resizes, and caches track thumbnails.
//
// It stays pure Go (no Ebitengine imports) so background workers and CLI tools
// can decode JPEG, PNG, and WebP images off the UI thread before uploading GPU
// textures.
package artwork

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// ListSize is the square thumbnail edge in pixels for search/list rows.
	ListSize = 64
	// NowPlayingSize is the square thumbnail edge in pixels for detail preview.
	NowPlayingSize = 300

	// DefaultTimeout bounds thumbnail HTTP fetches.
	DefaultTimeout = 10 * time.Second
	// DefaultMaxBytes caps downloaded and cached image payload size.
	DefaultMaxBytes int64 = 4 << 20
	// DefaultLRUCapacity is the number of decoded Bundles kept in memory.
	DefaultLRUCapacity = 64
	// MaxDimension guards against malformed huge image headers.
	MaxDimension = 4096
	// DefaultUserAgent is sent on thumbnail HTTP requests.
	DefaultUserAgent = "github.com/rroy-ar/ytm-wmp/0.1"

	maxPixels = 16_000_000
)

var (
	// ErrTooLarge means a downloaded or cached thumbnail exceeded MaxBytes.
	ErrTooLarge = errors.New("artwork exceeds byte limit")
	// ErrInvalidImage means a thumbnail exceeded dimensions or failed decoding.
	ErrInvalidImage = errors.New("invalid artwork image")
)

// Bundle holds decoded and resized thumbnails for one video ID.
type Bundle struct {
	VideoID    string
	List       image.Image
	NowPlaying image.Image
}

type inflightCall struct {
	done   chan struct{}
	bundle Bundle
	err    error
}

type lruEntry struct {
	key    string
	bundle Bundle
}

// Option customizes a Cache.
type Option func(*Cache)

// WithCacheDir sets the on-disk thumbnail cache directory. Empty disables disk caching.
func WithCacheDir(dir string) Option {
	return func(c *Cache) { c.cacheDir = dir }
}

// WithHTTPClient overrides the HTTP client used to fetch thumbnails.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Cache) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithTimeout sets the per-request fetch timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Cache) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithLRUCapacity sets how many decoded Bundles stay in memory.
func WithLRUCapacity(n int) Option {
	return func(c *Cache) {
		if n >= 0 {
			c.lruCap = n
		}
	}
}

// WithMaxBytes caps downloaded and cached thumbnail payload size.
func WithMaxBytes(n int64) Option {
	return func(c *Cache) {
		if n > 0 {
			c.maxBytes = n
		}
	}
}

// WithUserAgent sets the HTTP User-Agent header sent during fetches.
func WithUserAgent(ua string) Option {
	return func(c *Cache) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// Cache manages an in-memory LRU of resized artwork and an optional disk cache
// of original thumbnail bytes keyed by video ID.
type Cache struct {
	cacheDir   string
	httpClient *http.Client
	timeout    time.Duration
	lruCap     int
	maxBytes   int64
	userAgent  string

	mu       sync.Mutex
	lruMap   map[string]*list.Element
	lruList  *list.List
	inflight map[string]*inflightCall
}

// NewCache creates an artwork Cache with production defaults.
func NewCache(opts ...Option) *Cache {
	c := &Cache{
		httpClient: &http.Client{},
		timeout:    DefaultTimeout,
		lruCap:     DefaultLRUCapacity,
		maxBytes:   DefaultMaxBytes,
		userAgent:  DefaultUserAgent,
		lruMap:     make(map[string]*list.Element),
		lruList:    list.New(),
		inflight:   make(map[string]*inflightCall),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// LookupMemory returns a decoded Bundle from the in-memory LRU without I/O.
func (c *Cache) LookupMemory(videoID string) (Bundle, bool) {
	id := strings.TrimSpace(videoID)
	if id == "" || c.lruCap <= 0 {
		return Bundle{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.lruMap[id]
	if !ok {
		return Bundle{}, false
	}
	c.lruList.MoveToFront(elem)
	return elem.Value.(*lruEntry).bundle, true
}

// Get returns resized artwork for videoID, checking memory, disk cache, and
// finally fetching thumbnailURL over HTTP(S). Concurrent calls for the same
// videoID share one fetch.
func (c *Cache) Get(ctx context.Context, videoID, thumbnailURL string) (Bundle, error) {
	id := strings.TrimSpace(videoID)
	if id == "" {
		return Bundle{}, errors.New("empty video ID")
	}
	if b, ok := c.LookupMemory(id); ok {
		return b, nil
	}

	c.mu.Lock()
	if existing, ok := c.inflight[id]; ok {
		c.mu.Unlock()
		select {
		case <-existing.done:
			return existing.bundle, existing.err
		case <-ctx.Done():
			return Bundle{}, ctx.Err()
		}
	}
	call := &inflightCall{done: make(chan struct{})}
	c.inflight[id] = call
	c.mu.Unlock()

	bundle, err := c.loadOrFetch(ctx, id, strings.TrimSpace(thumbnailURL))
	if err == nil {
		c.putMemory(id, bundle)
	}

	c.mu.Lock()
	delete(c.inflight, id)
	call.bundle = bundle
	call.err = err
	close(call.done)
	c.mu.Unlock()

	return bundle, err
}

func (c *Cache) loadOrFetch(ctx context.Context, id, thumbnailURL string) (Bundle, error) {
	dir := c.CacheDir()
	if diskPath := diskPathForDir(dir, id); diskPath != "" {
		if raw, err := c.readDisk(diskPath); err == nil {
			if b, err := decodeAndResize(id, raw); err == nil {
				return b, nil
			}
			_ = os.Remove(diskPath)
		}
	}

	if thumbnailURL == "" {
		return Bundle{}, fmt.Errorf("missing thumbnail URL for %q", id)
	}

	raw, err := c.fetch(ctx, thumbnailURL)
	if err != nil {
		return Bundle{}, err
	}
	b, err := decodeAndResize(id, raw)
	if err != nil {
		return Bundle{}, err
	}
	if dir := c.CacheDir(); dir != "" {
		if diskPath := diskPathForDir(dir, id); diskPath != "" {
			_ = writeDiskAtomic(dir, diskPath, raw)
		}
	}
	return b, nil
}

// CacheDir returns the currently configured on-disk cache directory.
func (c *Cache) CacheDir() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cacheDir
}

// SetCacheDir updates the on-disk cache directory at runtime. Empty disables disk caching.
func (c *Cache) SetCacheDir(dir string) {
	c.mu.Lock()
	c.cacheDir = strings.TrimSpace(dir)
	c.mu.Unlock()
}

// DiskStats returns the number of cached thumbnail files and total bytes on disk.
func (c *Cache) DiskStats() (files int, totalBytes int64, err error) {
	dir := c.CacheDir()
	if dir == "" {
		return 0, 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".img") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files++
		totalBytes += info.Size()
	}
	return files, totalBytes, nil
}

// PruneDisk evicts the oldest cached .img files until total disk usage is <= maxBytes.
// If maxBytes <= 0, PruneDisk is a no-op.
func (c *Cache) PruneDisk(maxBytes int64) error {
	if maxBytes <= 0 {
		return nil
	}
	dir := c.CacheDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	type fileRec struct {
		path    string
		size    int64
		modTime time.Time
	}
	var recs []fileRec
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".img") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
		recs = append(recs, fileRec{
			path:    filepath.Join(dir, entry.Name()),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	}
	if total <= maxBytes {
		return nil
	}
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].modTime.Before(recs[j].modTime)
	})
	for _, rec := range recs {
		if total <= maxBytes {
			break
		}
		if err := os.Remove(rec.path); err == nil || errors.Is(err, os.ErrNotExist) {
			total -= rec.size
		}
	}
	return nil
}

// ClearDisk removes all cached .img files from disk and clears the in-memory LRU.
func (c *Cache) ClearDisk() error {
	c.mu.Lock()
	dir := c.cacheDir
	c.lruMap = make(map[string]*list.Element)
	c.lruList = list.New()
	c.mu.Unlock()

	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".img") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func diskPathForDir(dir, id string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, safeFilename(id)+".img")
}

func (c *Cache) readDisk(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, c.maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > c.maxBytes {
		return nil, fmt.Errorf("%w (%d bytes)", ErrTooLarge, c.maxBytes)
	}
	return raw, nil
}

func writeDiskAtomic(dir, dstPath string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".art-*")
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
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (c *Cache) fetch(ctx context.Context, rawURL string) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building artwork request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("artwork HTTP %d for %q", resp.StatusCode, rawURL)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > c.maxBytes {
		return nil, fmt.Errorf("%w (%d bytes)", ErrTooLarge, c.maxBytes)
	}
	return raw, nil
}

func (c *Cache) putMemory(id string, b Bundle) {
	if c.lruCap <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.lruMap[id]; ok {
		elem.Value.(*lruEntry).bundle = b
		c.lruList.MoveToFront(elem)
		return
	}
	elem := c.lruList.PushFront(&lruEntry{key: id, bundle: b})
	c.lruMap[id] = elem
	for c.lruList.Len() > c.lruCap {
		oldest := c.lruList.Back()
		if oldest == nil {
			break
		}
		entry := c.lruList.Remove(oldest).(*lruEntry)
		delete(c.lruMap, entry.key)
	}
}

func decodeAndResize(videoID string, raw []byte) (Bundle, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return Bundle{}, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxDimension || cfg.Height > MaxDimension {
		return Bundle{}, fmt.Errorf("%w: dimensions %dx%d out of range", ErrInvalidImage, cfg.Width, cfg.Height)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return Bundle{}, fmt.Errorf("%w: pixel count exceeds budget", ErrInvalidImage)
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return Bundle{}, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}

	return Bundle{
		VideoID:    videoID,
		List:       resizeSquare(src, ListSize),
		NowPlaying: resizeSquare(src, NowPlayingSize),
	}, nil
}

func resizeSquare(src image.Image, size int) *image.RGBA {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	side := w
	if h < side {
		side = h
	}
	if side <= 0 {
		return image.NewRGBA(image.Rect(0, 0, size, size))
	}

	srcX := bounds.Min.X + (w-side)/2
	srcY := bounds.Min.Y + (h-side)/2
	crop := image.Rect(srcX, srcY, srcX+side, srcY+side)

	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)
	return dst
}

// Placeholder generates a deterministic square placeholder image for UI display
// while real track artwork is loading or unavailable.
func Placeholder(size int) *image.RGBA {
	if size <= 0 {
		size = ListSize
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{R: 0x16, G: 0x20, B: 0x2a, A: 0xff}
	border := color.RGBA{R: 0x2a, G: 0x5c, B: 0x4a, A: 0xff}
	ring := color.RGBA{R: 0x38, G: 0xb0, B: 0x68, A: 0xff}
	hub := color.RGBA{R: 0x0c, G: 0x12, B: 0x18, A: 0xff}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if x == 0 || y == 0 || x == size-1 || y == size-1 {
				img.SetRGBA(x, y, border)
				continue
			}
			img.SetRGBA(x, y, bg)
		}
	}

	cx, cy := size/2, size/2
	outerR := (size * 3) / 10
	innerR := size / 10
	for y := cy - outerR; y <= cy+outerR; y++ {
		for x := cx - outerR; x <= cx+outerR; x++ {
			dx, dy := x-cx, y-cy
			d2 := dx*dx + dy*dy
			if d2 <= innerR*innerR {
				img.SetRGBA(x, y, hub)
			} else if d2 <= outerR*outerR {
				img.SetRGBA(x, y, ring)
			}
		}
	}
	return img
}

func safeFilename(id string) string {
	if len(id) > 0 && len(id) <= 64 {
		clean := true
		for _, r := range id {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= 'A' && r <= 'Z':
			case r >= '0' && r <= '9':
			case r == '_', r == '-':
			default:
				clean = false
			}
			if !clean {
				break
			}
		}
		if clean {
			return id
		}
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}
