package catalog

import (
	"context"
	"sync"
	"testing"
	"time"

	"ytm-wmp/internal/model"
)

type fakeCatalog struct {
	mu       sync.Mutex
	searches []string
	playlist []string
	tracks   []string
	searchFn func(ctx context.Context, query string, limit int) ([]model.Track, error)
	playFn   func(ctx context.Context, idOrURL string) ([]model.Track, error)
	trackFn  func(ctx context.Context, id string) (model.Track, error)
}

func (f *fakeCatalog) Search(ctx context.Context, query string, limit int) ([]model.Track, error) {
	f.mu.Lock()
	f.searches = append(f.searches, query)
	fn := f.searchFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, query, limit)
	}
	return []model.Track{{ID: "id-" + query, Title: query}}, nil
}

func (f *fakeCatalog) Playlist(ctx context.Context, idOrURL string) ([]model.Track, error) {
	f.mu.Lock()
	f.playlist = append(f.playlist, idOrURL)
	fn := f.playFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, idOrURL)
	}
	return []model.Track{{ID: "pl-1", Title: "Playlist Track"}}, nil
}

func (f *fakeCatalog) Track(ctx context.Context, id string) (model.Track, error) {
	f.mu.Lock()
	f.tracks = append(f.tracks, id)
	fn := f.trackFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, id)
	}
	return model.Track{}, ErrNotFound
}

func (f *fakeCatalog) searchQueries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.searches...)
}

func (f *fakeCatalog) playlistTargets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.playlist...)
}

func (f *fakeCatalog) trackTargets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tracks...)
}

func TestIsPlaylistTarget(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://www.youtube.com/playlist?list=PL1234567890abcd", true},
		{"https://music.youtube.com/playlist?list=OLAK5uy_1234567890", true},
		{"https://www.youtube.com/watch?list=PL1234567890abcd", true},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL1234567890abcd", false},
		{"PL1234567890abcdef", true},
		{"OLAK5uy_1234567890abcdef", true},
		{"VLPL1234567890abcdef", true},
		{"MPREb_K8qWMWVqXGi", true},
		{"RDCLAK5uy_1234567890abcdef", true},
		{"https://music.youtube.com/browse/MPREb_K8qWMWVqXGi", true},
		{"https://music.youtube.com/browse/VLPL1234567890abcdef", true},
		{"UU1234567890abcdef", true},
		{"FL1234567890abcdef", true},
		{"LL1234567890abcdef", true},
		{"PLshort", false},
		{"PL bad spaces 12345", false},
		{"daft punk discovery", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsPlaylistTarget(tt.input); got != tt.want {
				t.Fatalf("IsPlaylistTarget(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSearcherDebounceCoalescesTypingBursts(t *testing.T) {
	cat := &fakeCatalog{}
	s := NewSearcher(cat, WithDebounce(25*time.Millisecond), WithLimit(10))
	defer s.Close()

	s.SetQuery("d")
	s.SetQuery("da")
	s.SetQuery("daf")
	gen := s.SetQuery("daft")

	select {
	case res := <-s.Results():
		if res.Generation != gen {
			t.Fatalf("Generation = %d, want %d", res.Generation, gen)
		}
		if res.Query != "daft" || res.IsPlaylist {
			t.Fatalf("got result %+v, want query=daft isPlaylist=false", res)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for debounced search result")
	}

	got := cat.searchQueries()
	if len(got) != 1 || got[0] != "daft" {
		t.Fatalf("Search calls = %v, want only [daft]", got)
	}
}

func TestSearcherSubmitBypassesDebounceAndCancelsPendingTimer(t *testing.T) {
	cat := &fakeCatalog{}
	s := NewSearcher(cat, WithDebounce(2*time.Second))
	defer s.Close()

	s.SetQuery("pending")
	gen := s.Submit("immediate")

	select {
	case res := <-s.Results():
		if res.Generation != gen || res.Query != "immediate" {
			t.Fatalf("got result %+v, want gen=%d query=immediate", res, gen)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Submit did not run immediately")
	}

	got := cat.searchQueries()
	if len(got) != 1 || got[0] != "immediate" {
		t.Fatalf("Search calls = %v, want [immediate]", got)
	}
}

func TestSearcherCancelsAndDropsStaleGenerations(t *testing.T) {
	gen1Started := make(chan struct{})
	gen1Canceled := make(chan struct{})
	releaseGen1 := make(chan struct{})

	cat := &fakeCatalog{
		searchFn: func(ctx context.Context, query string, limit int) ([]model.Track, error) {
			if query == "slow" {
				close(gen1Started)
				select {
				case <-ctx.Done():
					close(gen1Canceled)
					<-releaseGen1
					// Even if the catalog ignores ctx.Err and returns tracks,
					// Searcher must drop the stale generation.
					return []model.Track{{ID: "stale"}}, nil
				case <-time.After(2 * time.Second):
					return []model.Track{{ID: "stale-timeout"}}, nil
				}
			}
			return []model.Track{{ID: "fresh", Title: query}}, nil
		},
	}

	s := NewSearcher(cat, WithDebounce(0))
	defer s.Close()

	s.Submit("slow")
	<-gen1Started

	gen2 := s.Submit("fast")
	<-gen1Canceled

	select {
	case res := <-s.Results():
		if res.Generation != gen2 || len(res.Tracks) != 1 || res.Tracks[0].ID != "fresh" {
			t.Fatalf("got %+v, want fresh gen=%d", res, gen2)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fast result")
	}

	close(releaseGen1)
	time.Sleep(20 * time.Millisecond)

	select {
	case extra := <-s.Results():
		t.Fatalf("unexpected stale result emitted: %+v", extra)
	default:
	}
}

func TestSearcherRoutesPlaylistTargetsAndEmptyQueries(t *testing.T) {
	cat := &fakeCatalog{}
	s := NewSearcher(cat, WithDebounce(0))
	defer s.Close()

	plURL := "https://www.youtube.com/playlist?list=PL1234567890abcdef"
	s.Submit(plURL)

	select {
	case res := <-s.Results():
		if !res.IsPlaylist || len(res.Tracks) != 1 || res.Tracks[0].ID != "pl-1" {
			t.Fatalf("got %+v, want playlist result", res)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for playlist result")
	}

	if got := cat.playlistTargets(); len(got) != 1 || got[0] != plURL {
		t.Fatalf("Playlist calls = %v, want [%s]", got, plURL)
	}

	emptyGen := s.SetQuery("   ")
	select {
	case res := <-s.Results():
		if res.Generation != emptyGen || res.Query != "" || len(res.Tracks) != 0 || res.Err != nil {
			t.Fatalf("empty query result = %+v, want clean empty result", res)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for empty clear result")
	}
}

func TestSearcherCloseWaitsForWorkers(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	cat := &fakeCatalog{
		searchFn: func(ctx context.Context, query string, limit int) ([]model.Track, error) {
			close(started)
			<-ctx.Done()
			time.Sleep(15 * time.Millisecond)
			close(canceled)
			return nil, ctx.Err()
		},
	}

	s := NewSearcher(cat)
	s.Submit("hello")
	<-started

	s.Close()
	select {
	case <-canceled:
	default:
		t.Fatal("Close returned before worker completed cleanup")
	}

	// Idempotent second Close and no-op SetQuery after Close.
	s.Close()
	if got := s.SetQuery("after-close"); got != 0 {
		t.Fatalf("SetQuery after Close = %d, want 0", got)
	}
}

func TestSearcherRoutesWatchURLToTrack(t *testing.T) {
	watchURL := "https://www.youtube.com/watch?v=oz3LxqoyQFQ&list=RDoz3LxqoyQFQ&start_radio=1"
	cat := &fakeCatalog{
		trackFn: func(_ context.Context, id string) (model.Track, error) {
			return model.Track{ID: id, Title: "Watch Track"}, nil
		},
	}
	s := NewSearcher(cat, WithDebounce(0))
	defer s.Close()

	s.Submit(watchURL)

	select {
	case res := <-s.Results():
		if res.Err != nil {
			t.Fatalf("Results err = %v, want nil", res.Err)
		}
		if res.IsPlaylist {
			t.Fatalf("IsPlaylist = true, want false")
		}
		if len(res.Tracks) != 1 || res.Tracks[0].ID != "oz3LxqoyQFQ" || res.Tracks[0].Title != "Watch Track" {
			t.Fatalf("Tracks = %+v, want single track oz3LxqoyQFQ", res.Tracks)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for watch URL track result")
	}

	if got := cat.trackTargets(); len(got) != 1 || got[0] != "oz3LxqoyQFQ" {
		t.Fatalf("Track calls = %v, want [oz3LxqoyQFQ]", got)
	}
	if got := cat.searchQueries(); len(got) != 0 {
		t.Fatalf("Search calls = %v, want none when Track succeeds", got)
	}

	// When Track returns ErrNotFound, Searcher falls back to Search with the video ID.
	cat.mu.Lock()
	cat.trackFn = func(context.Context, string) (model.Track, error) {
		return model.Track{}, ErrNotFound
	}
	cat.mu.Unlock()

	s.Submit(watchURL)
	select {
	case res := <-s.Results():
		if res.Err != nil {
			t.Fatalf("fallback Results err = %v, want nil", res.Err)
		}
		if len(res.Tracks) != 1 || res.Tracks[0].ID != "id-oz3LxqoyQFQ" {
			t.Fatalf("fallback Tracks = %+v, want search fallback for oz3LxqoyQFQ", res.Tracks)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fallback watch URL search result")
	}
	if got := cat.searchQueries(); len(got) != 1 || got[0] != "oz3LxqoyQFQ" {
		t.Fatalf("fallback Search calls = %v, want [oz3LxqoyQFQ]", got)
	}
}

func TestParseSearchTargetAndYouTubePrefixRouting(t *testing.T) {
	cases := []struct {
		raw       string
		wantQuery string
		wantYT    bool
	}{
		{"daft punk", "daft punk", false},
		{"yt: obscure live set", "obscure live set", true},
		{"YT:obscure live set", "obscure live set", true},
		{"ytdlp: rare cover", "rare cover", true},
		{"yt:   ", "", true},
		{"https://www.youtube.com/watch?v=0wRYvsfhsR8", "https://www.youtube.com/watch?v=0wRYvsfhsR8", false},
	}
	for _, tc := range cases {
		gotQuery, gotYT := ParseSearchTarget(tc.raw)
		if gotQuery != tc.wantQuery || gotYT != tc.wantYT {
			t.Errorf("ParseSearchTarget(%q) = (%q, %v), want (%q, %v)", tc.raw, gotQuery, gotYT, tc.wantQuery, tc.wantYT)
		}
	}

	primary := &fakeCatalog{}
	ytCat := &fakeCatalog{}
	s := NewSearcher(primary, WithYouTubeCatalog(ytCat), WithDebounce(0))
	defer s.Close()

	// Prefix with empty remainder emits a clean empty result without calling either catalog.
	s.Submit("yt: ")
	select {
	case res := <-s.Results():
		if res.Query != "" || res.Err != nil || len(res.Tracks) != 0 {
			t.Fatalf("yt: empty remainder = %+v, want empty result", res)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for empty prefix result")
	}

	// Prefixed search routes to ytCat with stripped query.
	s.Submit("yt: rare remix")
	select {
	case res := <-s.Results():
		if res.Err != nil || !res.IsYouTube || len(res.Tracks) != 1 || res.Tracks[0].Title != "rare remix" {
			t.Fatalf("yt: result = %+v, want IsYouTube=true title='rare remix'", res)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for yt: search result")
	}

	// Unprefixed search stays on primary catalog.
	s.Submit("normal song")
	select {
	case res := <-s.Results():
		if res.Err != nil || res.IsYouTube || len(res.Tracks) != 1 || res.Tracks[0].Title != "normal song" {
			t.Fatalf("normal result = %+v, want IsYouTube=false title='normal song'", res)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for normal search result")
	}

	if got := ytCat.searchQueries(); len(got) != 1 || got[0] != "rare remix" {
		t.Fatalf("ytCat searches = %v, want [rare remix]", got)
	}
	if got := primary.searchQueries(); len(got) != 1 || got[0] != "normal song" {
		t.Fatalf("primary searches = %v, want [normal song]", got)
	}
}
