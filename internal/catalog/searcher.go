package catalog

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"ytm-wmp/internal/model"
)

const (
	// DefaultDebounce coalesces typing bursts before launching a catalog process.
	DefaultDebounce = 275 * time.Millisecond
	// DefaultSearchLimit is the default number of tracks requested by Searcher.
	DefaultSearchLimit = 10
)

// SearchResult is the outcome of a single search or playlist import generation.
type SearchResult struct {
	Generation uint64
	Query      string
	Tracks     []model.Track
	IsPlaylist bool
	IsYouTube  bool
	Err        error
}

// SearcherOption configures a Searcher.
type SearcherOption func(*Searcher)

// WithYouTubeCatalog sets the fallback Catalog used for `yt:` / `ytdlp:`
// prefixed queries (for tracks only found on regular YouTube).
func WithYouTubeCatalog(cat Catalog) SearcherOption {
	return func(s *Searcher) {
		s.ytCat = cat
	}
}

// WithDebounce overrides the typing debounce duration.
func WithDebounce(d time.Duration) SearcherOption {
	return func(s *Searcher) {
		if d >= 0 {
			s.debounce = d
		}
	}
}

// WithLimit sets the search result limit requested from Catalog.Search.
func WithLimit(limit int) SearcherOption {
	return func(s *Searcher) {
		if limit > 0 {
			s.limit = limit
		}
	}
}

// Searcher runs debounced, generation-tagged async searches and playlist
// imports so the UI never blocks or renders stale results.
type Searcher struct {
	cat      Catalog
	ytCat    Catalog
	debounce time.Duration
	limit    int
	results  chan SearchResult

	mu     sync.Mutex
	closed bool
	gen    uint64
	timer  *time.Timer
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewSearcher creates a Searcher backed by cat.
func NewSearcher(cat Catalog, opts ...SearcherOption) *Searcher {
	s := &Searcher{
		cat:      cat,
		debounce: DefaultDebounce,
		limit:    DefaultSearchLimit,
		results:  make(chan SearchResult, 8),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// Results receives completed non-stale SearchResults.
func (s *Searcher) Results() <-chan SearchResult {
	return s.results
}

// SetQuery schedules a debounced search (or clears results if query is empty)
// and cancels any previous pending timer or in-flight search.
func (s *Searcher) SetQuery(query string) uint64 {
	q := strings.TrimSpace(query)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0
	}
	s.stopPendingLocked()
	s.gen++
	gen := s.gen

	if q == "" {
		s.mu.Unlock()
		s.emit(SearchResult{Generation: gen, Query: ""})
		return gen
	}
	if s.cat == nil {
		s.mu.Unlock()
		return gen
	}
	if s.debounce == 0 {
		s.startWorkerLocked(gen, q)
		s.mu.Unlock()
		return gen
	}

	s.timer = time.AfterFunc(s.debounce, func() {
		s.onTimer(gen, q)
	})
	s.mu.Unlock()
	return gen
}

// Submit cancels any pending timer or in-flight search and starts a new
// lookup immediately without waiting for the debounce window.
func (s *Searcher) Submit(query string) uint64 {
	q := strings.TrimSpace(query)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0
	}
	s.stopPendingLocked()
	s.gen++
	gen := s.gen

	if q == "" {
		s.mu.Unlock()
		s.emit(SearchResult{Generation: gen, Query: ""})
		return gen
	}
	if s.cat == nil {
		s.mu.Unlock()
		return gen
	}
	s.startWorkerLocked(gen, q)
	s.mu.Unlock()
	return gen
}

// Cancel stops any pending debounce timer and cancels any in-flight search.
func (s *Searcher) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.stopPendingLocked()
	s.gen++
}

// Close cancels pending work and waits for all background search goroutines.
func (s *Searcher) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.wg.Wait()
		return
	}
	s.closed = true
	s.gen++
	s.stopPendingLocked()
	s.mu.Unlock()

	s.wg.Wait()
}

func (s *Searcher) stopPendingLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *Searcher) onTimer(gen uint64, q string) {
	s.mu.Lock()
	if s.closed || gen != s.gen {
		s.mu.Unlock()
		return
	}
	s.timer = nil
	s.startWorkerLocked(gen, q)
	s.mu.Unlock()
}

func (s *Searcher) startWorkerLocked(gen uint64, q string) {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go s.runWorker(ctx, cancel, gen, q)
}

// ParseSearchTarget splits an optional `yt:` or `ytdlp:` prefix from rawQuery,
// returning the trimmed target query and whether the regular YouTube catalog
// was requested.
func ParseSearchTarget(rawQuery string) (target string, isYouTube bool) {
	q := strings.TrimSpace(rawQuery)
	lower := strings.ToLower(q)
	switch {
	case strings.HasPrefix(lower, "yt:"):
		return strings.TrimSpace(q[3:]), true
	case strings.HasPrefix(lower, "ytdlp:"):
		return strings.TrimSpace(q[6:]), true
	default:
		return q, false
	}
}

func (s *Searcher) runWorker(ctx context.Context, cancel context.CancelFunc, gen uint64, q string) {
	defer s.wg.Done()
	defer cancel()

	target, isYouTube := ParseSearchTarget(q)
	if target == "" {
		s.mu.Lock()
		stale := s.closed || gen != s.gen
		s.mu.Unlock()
		if !stale {
			s.emit(SearchResult{Generation: gen, Query: ""})
		}
		return
	}

	activeCat := s.cat
	if isYouTube && s.ytCat != nil {
		activeCat = s.ytCat
	}

	isPlaylist := IsPlaylistTarget(target)
	var (
		tracks []model.Track
		err    error
	)
	if isPlaylist {
		tracks, err = activeCat.Playlist(ctx, target)
	} else if vid := model.VideoID(target); vid != "" && !model.IsVideoID(target) {
		tr, trackErr := activeCat.Track(ctx, vid)
		if trackErr == nil {
			tracks = []model.Track{tr}
		} else if errors.Is(trackErr, context.Canceled) {
			return
		} else {
			tracks, err = activeCat.Search(ctx, vid, s.limit)
		}
	} else {
		tracks, err = activeCat.Search(ctx, target, s.limit)
	}

	if errors.Is(err, context.Canceled) {
		return
	}

	s.mu.Lock()
	stale := s.closed || gen != s.gen
	s.mu.Unlock()
	if stale {
		return
	}

	s.emit(SearchResult{
		Generation: gen,
		Query:      q,
		Tracks:     tracks,
		IsPlaylist: isPlaylist,
		IsYouTube:  isYouTube,
		Err:        err,
	})
}

func (s *Searcher) emit(res SearchResult) {
	select {
	case s.results <- res:
		return
	default:
	}
	// If the consumer fell behind, drop the oldest result so the latest
	// generation is never blocked or discarded.
	select {
	case <-s.results:
	default:
	}
	select {
	case s.results <- res:
	default:
	}
}
