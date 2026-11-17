package catalog

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/rroy-ar/ytm-wmp/internal/model"
)

const (
	// DefaultDebounce coalesces typing bursts before launching a catalog process.
	DefaultDebounce = 275 * time.Millisecond
	// DefaultSearchLimit is the default number of tracks requested by Searcher.
	DefaultSearchLimit = 10
)

// SearchResult is the outcome of a single search, playlist import, or
// collection browse generation.
type SearchResult struct {
	Generation  uint64
	Tab         SearchTab
	Query       string
	Tracks      []model.Track
	Collections []Collection
	Detail      *CollectionDetail
	IsPlaylist  bool
	IsYouTube   bool
	Err         error
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

// SetQuery schedules a debounced song search (or clears results if query is
// empty) and cancels any previous pending timer or in-flight search.
func (s *Searcher) SetQuery(query string) uint64 {
	return s.SetQueryForTab(query, TabSongs)
}

// SetQueryForTab schedules a debounced search for tab (or clears results if
// query is empty) and cancels any previous pending timer or in-flight search.
func (s *Searcher) SetQueryForTab(query string, tab SearchTab) uint64 {
	if tab == "" {
		tab = TabSongs
	}
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
		s.emit(SearchResult{Generation: gen, Tab: tab, Query: ""})
		return gen
	}
	if s.cat == nil {
		s.mu.Unlock()
		return gen
	}
	if s.debounce == 0 {
		s.startWorkerLocked(gen, q, tab)
		s.mu.Unlock()
		return gen
	}

	s.timer = time.AfterFunc(s.debounce, func() {
		s.onTimer(gen, q, tab)
	})
	s.mu.Unlock()
	return gen
}

// Submit cancels any pending timer or in-flight search and starts a song
// lookup immediately without waiting for the debounce window.
func (s *Searcher) Submit(query string) uint64 {
	return s.SubmitForTab(query, TabSongs)
}

// SubmitForTab cancels any pending timer or in-flight search and starts a
// lookup for tab immediately without waiting for the debounce window.
func (s *Searcher) SubmitForTab(query string, tab SearchTab) uint64 {
	if tab == "" {
		tab = TabSongs
	}
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
		s.emit(SearchResult{Generation: gen, Tab: tab, Query: ""})
		return gen
	}
	if s.cat == nil {
		s.mu.Unlock()
		return gen
	}
	s.startWorkerLocked(gen, q, tab)
	s.mu.Unlock()
	return gen
}

// Browse opens a Collection (playlist, album, or artist) asynchronously under a
// new generation tag.
func (s *Searcher) Browse(col Collection) uint64 {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0
	}
	s.stopPendingLocked()
	s.gen++
	gen := s.gen
	if s.cat == nil {
		s.mu.Unlock()
		return gen
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	s.mu.Unlock()

	go s.runBrowseWorker(ctx, cancel, gen, col)
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

func (s *Searcher) onTimer(gen uint64, q string, tab SearchTab) {
	s.mu.Lock()
	if s.closed || gen != s.gen {
		s.mu.Unlock()
		return
	}
	s.timer = nil
	s.startWorkerLocked(gen, q, tab)
	s.mu.Unlock()
}

func (s *Searcher) startWorkerLocked(gen uint64, q string, tab SearchTab) {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go s.runWorker(ctx, cancel, gen, q, tab)
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

func (s *Searcher) runWorker(ctx context.Context, cancel context.CancelFunc, gen uint64, q string, tab SearchTab) {
	defer s.wg.Done()
	defer cancel()

	target, isYouTube := ParseSearchTarget(q)
	if target == "" {
		s.mu.Lock()
		stale := s.closed || gen != s.gen
		s.mu.Unlock()
		if !stale {
			s.emit(SearchResult{Generation: gen, Tab: tab, Query: ""})
		}
		return
	}

	activeCat := s.cat
	if isYouTube && s.ytCat != nil {
		activeCat = s.ytCat
	}

	isPlaylist := IsPlaylistTarget(target)
	var (
		tracks      []model.Track
		collections []Collection
		err         error
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
	} else if tab != "" && tab != TabSongs {
		if tc, ok := activeCat.(TabCatalog); ok && SupportsTab(activeCat, tab) {
			collections, err = tc.SearchCollections(ctx, tab, target, s.limit)
		} else {
			err = errors.New("tab not supported by this catalog backend")
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
		Generation:  gen,
		Tab:         tab,
		Query:       q,
		Tracks:      tracks,
		Collections: collections,
		IsPlaylist:  isPlaylist,
		IsYouTube:   isYouTube,
		Err:         err,
	})
}

func (s *Searcher) runBrowseWorker(ctx context.Context, cancel context.CancelFunc, gen uint64, col Collection) {
	defer s.wg.Done()
	defer cancel()

	var (
		detail CollectionDetail
		err    error
	)
	if tc, ok := s.cat.(TabCatalog); ok {
		detail, err = tc.BrowseCollection(ctx, col)
	} else if col.Kind == CollectionPlaylist {
		var tracks []model.Track
		tracks, err = s.cat.Playlist(ctx, col.ID)
		detail = CollectionDetail{Collection: col, Tracks: tracks}
	} else {
		err = errors.New("browsing collections is not supported by this catalog backend")
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

	var tab SearchTab
	switch col.Kind {
	case CollectionPlaylist:
		tab = TabPlaylists
	case CollectionAlbum:
		tab = TabAlbums
	case CollectionArtist:
		tab = TabArtists
	}

	s.emit(SearchResult{
		Generation: gen,
		Tab:        tab,
		Detail:     &detail,
		Err:        err,
	})
}

// SupportsTab reports whether cat supports searching tab.
func SupportsTab(cat Catalog, tab SearchTab) bool {
	if tab == "" || tab == TabSongs {
		return cat != nil
	}
	tc, ok := cat.(TabCatalog)
	if !ok {
		return false
	}
	for _, supported := range tc.SupportedTabs() {
		if supported == tab {
			return true
		}
	}
	return false
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
