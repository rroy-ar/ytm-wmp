package artwork

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// minimal 4x4 WebP generated with ffmpeg.
const sampleWebPBase64 = "UklGRjoAAABXRUJQVlA4IC4AAADwAQCdASoEAAQAAgA0JaACdLoB+AAETAAA/uQBf/M0THMftX/+dg2JdrwAAAAA"

func makeTestImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestFetchDecodeAndResizeFormats(t *testing.T) {
	webpBytes, err := base64.StdEncoding.DecodeString(sampleWebPBase64)
	if err != nil {
		t.Fatalf("decode webp fixture: %v", err)
	}

	fixtures := map[string][]byte{
		"/jpeg": encodeJPEG(t, makeTestImage(160, 90, color.RGBA{R: 0x30, G: 0x90, B: 0xe0, A: 0xff})),
		"/png":  encodePNG(t, makeTestImage(120, 120, color.RGBA{R: 0xe0, G: 0x60, B: 0x20, A: 0xff})),
		"/webp": webpBytes,
	}

	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		payload, ok := fixtures[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	cache := NewCache(WithCacheDir(t.TempDir()), WithUserAgent("ytm-test/1.0"))

	for id, path := range map[string]string{"vid_jpg": "/jpeg", "vid_png": "/png", "vid_webp": "/webp"} {
		b, err := cache.Get(context.Background(), id, srv.URL+path)
		if err != nil {
			t.Fatalf("Get(%s) = %v", id, err)
		}
		if b.List.Bounds().Size() != image.Pt(ListSize, ListSize) {
			t.Fatalf("%s List size = %v, want %dx%d", id, b.List.Bounds().Size(), ListSize, ListSize)
		}
		if b.NowPlaying.Bounds().Size() != image.Pt(NowPlayingSize, NowPlayingSize) {
			t.Fatalf("%s NowPlaying size = %v, want %dx%d", id, b.NowPlaying.Bounds().Size(), NowPlayingSize, NowPlayingSize)
		}
	}
	if gotUA != "ytm-test/1.0" {
		t.Fatalf("User-Agent = %q, want ytm-test/1.0", gotUA)
	}
}

func TestDiskCachePersistsAcrossCacheInstances(t *testing.T) {
	payload := encodePNG(t, makeTestImage(80, 80, color.RGBA{R: 0x10, G: 0xc0, B: 0x70, A: 0xff}))
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(payload)
	}))

	dir := t.TempDir()
	c1 := NewCache(WithCacheDir(dir))
	if _, err := c1.Get(context.Background(), "dQw4w9WgXcQ", srv.URL+"/thumb.png"); err != nil {
		t.Fatalf("first Get = %v", err)
	}
	srv.Close()

	// Fresh cache with empty LRU must still load from disk even without a URL.
	c2 := NewCache(WithCacheDir(dir))
	b, err := c2.Get(context.Background(), "dQw4w9WgXcQ", "")
	if err != nil {
		t.Fatalf("second Get from disk = %v", err)
	}
	if b.List.Bounds().Size() != image.Pt(ListSize, ListSize) {
		t.Fatalf("List size = %v, want %dx%d", b.List.Bounds().Size(), ListSize, ListSize)
	}
	if hits.Load() != 1 {
		t.Fatalf("HTTP hits = %d, want 1", hits.Load())
	}
}

func TestMemoryLRUEviction(t *testing.T) {
	payload := encodePNG(t, makeTestImage(32, 32, color.RGBA{R: 0xff, G: 0x20, B: 0x40, A: 0xff}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	cache := NewCache(WithLRUCapacity(2))
	if _, err := cache.Get(context.Background(), "id1", srv.URL+"/1.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Get(context.Background(), "id2", srv.URL+"/2.png"); err != nil {
		t.Fatal(err)
	}
	// Promote id1 so id2 becomes least recently used.
	if _, ok := cache.LookupMemory("id1"); !ok {
		t.Fatal("LookupMemory(id1) = false, want true")
	}
	if _, err := cache.Get(context.Background(), "id3", srv.URL+"/3.png"); err != nil {
		t.Fatal(err)
	}

	if _, ok := cache.LookupMemory("id2"); ok {
		t.Fatal("id2 should have been evicted from LRU")
	}
	if _, ok := cache.LookupMemory("id1"); !ok {
		t.Fatal("id1 should remain in LRU after promotion")
	}
	if _, ok := cache.LookupMemory("id3"); !ok {
		t.Fatal("id3 should be in LRU")
	}
}

func TestPathTraversalSanitizationStaysInsideCacheDir(t *testing.T) {
	payload := encodePNG(t, makeTestImage(16, 16, color.RGBA{A: 0xff}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cache := NewCache(WithCacheDir(dir))
	if _, err := cache.Get(context.Background(), "../../etc/passwd", srv.URL+"/x.png"); err != nil {
		t.Fatalf("Get = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 hashed file inside cacheDir", len(entries))
	}
	if filepath.Base(entries[0].Name()) == "passwd.img" {
		t.Fatalf("unexpected unsanitized file name %q", entries[0].Name())
	}
}

func TestFetchEnforcesSizeHTTPStatusAndTimeout(t *testing.T) {
	payload := encodePNG(t, makeTestImage(100, 100, color.RGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			_, _ = w.Write(payload)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/slow":
			time.Sleep(150 * time.Millisecond)
			_, _ = w.Write(payload)
		}
	}))
	defer srv.Close()

	smallCap := NewCache(WithMaxBytes(32))
	if _, err := smallCap.Get(context.Background(), "big", srv.URL+"/big"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized fetch err = %v, want ErrTooLarge", err)
	}

	normal := NewCache(WithTimeout(40 * time.Millisecond))
	if _, err := normal.Get(context.Background(), "missing", srv.URL+"/missing"); err == nil {
		t.Fatal("HTTP 404 succeeded, want error")
	}
	if _, err := normal.Get(context.Background(), "slow", srv.URL+"/slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow fetch err = %v, want DeadlineExceeded", err)
	}
}

func TestPlaceholderReturnsRequestedSquareSize(t *testing.T) {
	p := Placeholder(ListSize)
	if p.Bounds().Size() != image.Pt(ListSize, ListSize) {
		t.Fatalf("Placeholder size = %v, want %dx%d", p.Bounds().Size(), ListSize, ListSize)
	}
}
