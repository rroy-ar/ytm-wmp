// Package ytdlp resolves YouTube targets to direct audio stream URLs.
//
// It owns the process invocation and the URL cache, but no playback state. The
// Runner and clock are injectable so tests never invoke the real yt-dlp.
package ytdlp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"ytm-wmp/internal/model"
)

const (
	// DefaultTTL mirrors the PoC: YouTube signs URLs for roughly six hours, so
	// reuse them for five and resolve again before the signature is likely to
	// expire.
	DefaultTTL    = 5 * time.Hour
	defaultBinary = "yt-dlp"
)

// ErrResolve means yt-dlp could not produce a stream URL.
var ErrResolve = errors.New("yt-dlp could not resolve stream")

// Runner executes an external command and returns stdout, stderr, and the
// process error separately. Tests replace this with a fake.
type Runner interface {
	Output(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// ExecRunner is the production Runner.
type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.Output()
	return stdout, stderr.Bytes(), err
}

type cachedStream struct {
	info    model.StreamInfo
	expires time.Time
}

// Resolver resolves targets and caches the resulting URL by video ID.
type Resolver struct {
	runner Runner
	binary string
	ttl    time.Duration
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]cachedStream
}

// Option customizes a Resolver; it is only used by tests and main wiring.
type Option func(*Resolver)

// WithRunner replaces the command runner.
func WithRunner(runner Runner) Option {
	return func(r *Resolver) {
		if runner != nil {
			r.runner = runner
		}
	}
}

// WithClock replaces the time source used by the cache.
func WithClock(now func() time.Time) Option {
	return func(r *Resolver) {
		if now != nil {
			r.now = now
		}
	}
}

// WithTTL changes how long resolved URLs are reused. ttl <= 0 disables caching.
func WithTTL(ttl time.Duration) Option {
	return func(r *Resolver) { r.ttl = ttl }
}

// WithBinary sets the yt-dlp executable path.
func WithBinary(path string) Option {
	return func(r *Resolver) {
		if path != "" {
			r.binary = path
		}
	}
}

// New creates a Resolver with production defaults.
func New(opts ...Option) *Resolver {
	r := &Resolver{
		runner: ExecRunner{},
		binary: defaultBinary,
		ttl:    DefaultTTL,
		now:    time.Now,
		cache:  make(map[string]cachedStream),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Resolve returns a playable stream URL for target.
//
// target may be a YouTube video ID, a watch/short URL, or a yt-dlp search
// query. Cache entries are keyed by video ID when one can be extracted, so a
// bare ID and its watch URL share one entry.
func (r *Resolver) Resolve(ctx context.Context, target string) (model.StreamInfo, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return model.StreamInfo{}, errors.New("empty track target")
	}

	key := cacheKey(target)
	now := r.now()

	r.mu.Lock()
	cached, ok := r.cache[key]
	r.mu.Unlock()
	if ok && now.Before(cached.expires) {
		return cached.info, nil
	}

	args := []string{
		"-f", "bestaudio/best",
		"--no-warnings",
		"--no-playlist",
		"-g", resolveTarget(target),
	}

	stdout, stderr, err := r.runner.Output(ctx, r.binary, args...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return model.StreamInfo{}, ctxErr
		}
		return model.StreamInfo{}, resolveError(target, err, stderr)
	}

	streamURL := firstNonEmptyLine(stdout)
	if streamURL == "" {
		return model.StreamInfo{}, fmt.Errorf("%w for %q: yt-dlp returned no URL", ErrResolve, target)
	}

	info := model.StreamInfo{URL: streamURL}
	if r.ttl > 0 {
		info.ExpiresAt = r.now().Add(r.ttl)

		r.mu.Lock()
		r.cache[key] = cachedStream{info: info, expires: info.ExpiresAt}
		r.mu.Unlock()
	}

	return info, nil
}

// Forget drops a cached URL. Playback calls it after a zero-frame stream so the
// next attempt resolves a fresh signature.
func (r *Resolver) Forget(target string) {
	key := cacheKey(strings.TrimSpace(target))

	r.mu.Lock()
	delete(r.cache, key)
	r.mu.Unlock()
}

func cacheKey(target string) string {
	if id := videoID(target); id != "" {
		return id
	}
	return target
}

// resolveTarget turns a bare video ID into a URL yt-dlp reliably recognizes.
// URLs and search queries are passed through untouched.
func resolveTarget(target string) string {
	if isVideoID(target) {
		return "https://www.youtube.com/watch?v=" + target
	}
	return target
}

// VideoID extracts a YouTube video ID from a bare ID or common YouTube URL
// shapes. It returns "" for search queries and non-YouTube targets.
func VideoID(target string) string {
	return videoID(strings.TrimSpace(target))
}

// IsVideoID reports whether s matches the 11-character YouTube video ID shape.
func IsVideoID(s string) bool {
	return isVideoID(s)
}

// videoID extracts a YouTube video ID from the common URL shapes. It returns ""
// for search queries and other targets.
func videoID(target string) string {
	if isVideoID(target) {
		return target
	}

	u, err := url.Parse(target)
	if err != nil {
		return ""
	}

	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch host {
	case "youtu.be":
		id := strings.Trim(u.Path, "/")
		if isVideoID(id) {
			return id
		}
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		if id := u.Query().Get("v"); isVideoID(id) {
			return id
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 {
			switch parts[0] {
			case "shorts", "embed", "live":
				if isVideoID(parts[1]) {
					return parts[1]
				}
			}
		}
	}
	return ""
}

func isVideoID(s string) bool {
	if len(s) != 11 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func firstNonEmptyLine(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func resolveError(target string, err error, stderr []byte) error {
	msg := strings.TrimSpace(string(stderr))
	if len(msg) > 1000 {
		msg = "..." + msg[len(msg)-1000:]
	}
	if msg != "" {
		return fmt.Errorf("%w for %q: %v: %s", ErrResolve, target, err, msg)
	}
	return fmt.Errorf("%w for %q: %v", ErrResolve, target, err)
}
