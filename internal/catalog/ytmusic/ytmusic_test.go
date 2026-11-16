package ytmusic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ytm-wmp/internal/catalog"
)

type recordedRequest struct {
	method string
	path   string
	body   innerTubeRequest
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func newTestServer(t *testing.T, handler func(endpoint string, req innerTubeRequest) (int, []byte)) (*httptest.Server, *[]recordedRequest, *sync.Mutex) {
	t.Helper()
	var (
		mu   sync.Mutex
		reqs []recordedRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var parsed innerTubeRequest
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		mu.Lock()
		reqs = append(reqs, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			body:   parsed,
		})
		mu.Unlock()

		status, payload := handler(r.URL.Path, parsed)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs, &mu
}

func TestSearchDecodesTenResultsAndSendsWebRemixPayload(t *testing.T) {
	fixture := readFixture(t, "search_results.json")
	srv, reqs, mu := newTestServer(t, func(endpoint string, _ innerTubeRequest) (int, []byte) {
		if endpoint != "/search" {
			return http.StatusNotFound, nil
		}
		return http.StatusOK, fixture
	})

	client := New(WithBaseURL(srv.URL))
	tracks, err := client.Search(context.Background(), "  daft punk  ", 10)
	if err != nil {
		t.Fatalf("Search = %v", err)
	}

	mu.Lock()
	if len(*reqs) != 1 {
		mu.Unlock()
		t.Fatalf("request count = %d, want 1", len(*reqs))
	}
	gotReq := (*reqs)[0]
	mu.Unlock()

	if gotReq.method != http.MethodPost || gotReq.path != "/search" {
		t.Fatalf("method/path = %s %s, want POST /search", gotReq.method, gotReq.path)
	}
	if gotReq.body.Context.Client.ClientName != DefaultClientName || gotReq.body.Context.Client.ClientVersion != DefaultClientVersion {
		t.Fatalf("client context = %+v", gotReq.body.Context.Client)
	}
	if gotReq.body.Query != "daft punk" || gotReq.body.Params != songsSearchParams {
		t.Fatalf("query/params = %q / %q", gotReq.body.Query, gotReq.body.Params)
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
	if first.ThumbnailURL != "https://i.ytimg.com/vi/FGBhQbmPwH8/hqdefault.jpg" {
		t.Fatalf("first thumbnail = %q, want hqdefault.jpg (.jpg over .webp tie)", first.ThumbnailURL)
	}

	second := tracks[1]
	if second.ID != "khnokW3Mw24" || second.Artist != "Daft Punk & Julian Casablancas" || second.Album != "Random Access Memories" {
		t.Fatalf("second track = %+v", second)
	}
	if second.Duration != 338*time.Second {
		t.Fatalf("second duration = %v, want 338s", second.Duration)
	}

	// Track without explicit thumbnail falls back to i.ytimg.com hqdefault.jpg.
	if tracks[4].ID != "gN2hntZBIUQ" || tracks[4].ThumbnailURL != "https://i.ytimg.com/vi/gN2hntZBIUQ/hqdefault.jpg" {
		t.Fatalf("tracks[4] = %+v", tracks[4])
	}
}

func TestSearchCacheHitAndExpiry(t *testing.T) {
	now := time.Date(2026, 11, 15, 20, 0, 0, 0, time.UTC)
	fixture := readFixture(t, "search_results.json")
	srv, reqs, mu := newTestServer(t, func(string, innerTubeRequest) (int, []byte) {
		return http.StatusOK, fixture
	})

	client := New(
		WithBaseURL(srv.URL),
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
	mu.Lock()
	calls := len(*reqs)
	mu.Unlock()
	if calls != 1 {
		t.Fatalf("HTTP calls = %d, want 1 cached hit", calls)
	}
	if second[0].Title != "One More Time" {
		t.Fatalf("second[0].Title = %q, want cloned slice unmutated", second[0].Title)
	}

	now = now.Add(11 * time.Minute)
	if _, err := client.Search(context.Background(), "daft punk", 10); err != nil {
		t.Fatalf("third Search = %v", err)
	}
	mu.Lock()
	calls = len(*reqs)
	mu.Unlock()
	if calls != 2 {
		t.Fatalf("HTTP calls after TTL = %d, want 2", calls)
	}
}

func TestTrackLookupAndCachePopulationFromSearch(t *testing.T) {
	searchFixture := readFixture(t, "search_results.json")
	nextFixture := readFixture(t, "track_next.json")

	srv, reqs, mu := newTestServer(t, func(endpoint string, _ innerTubeRequest) (int, []byte) {
		switch endpoint {
		case "/search":
			return http.StatusOK, searchFixture
		case "/next":
			return http.StatusOK, nextFixture
		default:
			return http.StatusNotFound, nil
		}
	})

	client := New(WithBaseURL(srv.URL))

	if _, err := client.Search(context.Background(), "daft punk", 10); err != nil {
		t.Fatalf("Search = %v", err)
	}

	// Track found in search result should hit trackCache without an HTTP call.
	cached, err := client.Track(context.Background(), "https://music.youtube.com/watch?v=FGBhQbmPwH8")
	if err != nil {
		t.Fatalf("cached Track = %v", err)
	}
	mu.Lock()
	calls := len(*reqs)
	mu.Unlock()
	if cached.Title != "One More Time" || calls != 1 {
		t.Fatalf("cached = %+v, calls = %d, want 1", cached, calls)
	}

	// Uncached bare ID should resolve via /next.
	tr, err := client.Track(context.Background(), "0wRYvsfhsR8")
	if err != nil {
		t.Fatalf("uncached Track = %v", err)
	}
	mu.Lock()
	last := (*reqs)[len(*reqs)-1]
	mu.Unlock()
	if last.path != "/next" || last.body.VideoID != "0wRYvsfhsR8" {
		t.Fatalf("last request = %+v", last)
	}
	if tr.ID != "0wRYvsfhsR8" || tr.Title != "Something About Us" || tr.Artist != "Daft Punk" || tr.Album != "Discovery" || tr.Duration != 232*time.Second {
		t.Fatalf("tr = %+v", tr)
	}
	if tr.ThumbnailURL != "https://i.ytimg.com/vi/0wRYvsfhsR8/mqdefault.jpg" {
		t.Fatalf("ThumbnailURL = %q, want closest to 360px", tr.ThumbnailURL)
	}
}

func TestPlaylistBrowseAndAlbumFallback(t *testing.T) {
	browseFixture := readFixture(t, "playlist_browse.json")
	nextFixture := readFixture(t, "track_next.json")

	srv, reqs, mu := newTestServer(t, func(endpoint string, req innerTubeRequest) (int, []byte) {
		switch endpoint {
		case "/browse":
			if req.BrowseID == "VLEMPTY_PLAYLIST_1234" {
				return http.StatusOK, []byte(`{"contents":{}}`)
			}
			return http.StatusOK, browseFixture
		case "/next":
			return http.StatusOK, nextFixture
		default:
			return http.StatusNotFound, nil
		}
	})

	client := New(WithBaseURL(srv.URL), WithPlaylistLimit(50))

	// Bare PL... gets prefixed with VL... for /browse.
	tracks, err := client.Playlist(context.Background(), "PL1234567890abcdef")
	if err != nil {
		t.Fatalf("Playlist(PL...) = %v", err)
	}
	mu.Lock()
	gotBrowseID := (*reqs)[0].body.BrowseID
	mu.Unlock()
	if gotBrowseID != "VLPL1234567890abcdef" {
		t.Fatalf("BrowseID = %q, want VLPL1234567890abcdef", gotBrowseID)
	}
	if len(tracks) != 2 {
		t.Fatalf("len(tracks) = %d, want 2 (skipping private item)", len(tracks))
	}
	if tracks[0].ID != "zKSsP2084nU" || tracks[0].Title != "Give Life Back to Music" || tracks[0].Artist != "Daft Punk" || tracks[0].Duration != 276*time.Second {
		t.Fatalf("tracks[0] = %+v", tracks[0])
	}
	// Item without artist column inherits artist from header.
	if tracks[1].ID != "A2VpR8HahKc" || tracks[1].Artist != "Daft Punk" {
		t.Fatalf("tracks[1] = %+v", tracks[1])
	}

	// Album browse ID (MPREb_...) keeps MPREb_ without VL prefix and inherits album + thumbnail.
	albumTracks, err := client.Playlist(context.Background(), "https://music.youtube.com/browse/MPREb_K8qWMWVqXGi")
	if err != nil {
		t.Fatalf("Playlist(album) = %v", err)
	}
	mu.Lock()
	gotAlbumBrowseID := (*reqs)[1].body.BrowseID
	mu.Unlock()
	if gotAlbumBrowseID != "MPREb_K8qWMWVqXGi" {
		t.Fatalf("album BrowseID = %q, want MPREb_K8qWMWVqXGi", gotAlbumBrowseID)
	}
	if albumTracks[0].Album != "Random Access Memories" {
		t.Fatalf("albumTracks[0].Album = %q, want Random Access Memories", albumTracks[0].Album)
	}
	if albumTracks[0].ThumbnailURL != "https://yt3.googleusercontent.com/ram=w302-h302-l90-rj" {
		t.Fatalf("albumTracks[0].ThumbnailURL = %q, want w302 header fallback", albumTracks[0].ThumbnailURL)
	}

	// Empty browse response for a playlist falls back to /next with playlistId.
	fallbackTracks, err := client.Playlist(context.Background(), "https://music.youtube.com/playlist?list=EMPTY_PLAYLIST_1234")
	if err != nil {
		t.Fatalf("Playlist(fallback) = %v", err)
	}
	mu.Lock()
	last := (*reqs)[len(*reqs)-1]
	mu.Unlock()
	if last.path != "/next" || last.body.PlaylistID != "EMPTY_PLAYLIST_1234" {
		t.Fatalf("fallback request = %+v", last)
	}
	if len(fallbackTracks) != 2 || fallbackTracks[0].ID != "0wRYvsfhsR8" {
		t.Fatalf("fallbackTracks = %+v", fallbackTracks)
	}
}

func TestEmptyQueryAndNotFound(t *testing.T) {
	srv, _, _ := newTestServer(t, func(string, innerTubeRequest) (int, []byte) {
		return http.StatusOK, []byte(`{}`)
	})
	client := New(WithBaseURL(srv.URL))

	if _, err := client.Search(context.Background(), "  ", 10); !errors.Is(err, catalog.ErrEmptyQuery) {
		t.Fatalf("Search empty = %v, want ErrEmptyQuery", err)
	}
	if _, err := client.Track(context.Background(), ""); !errors.Is(err, catalog.ErrEmptyQuery) {
		t.Fatalf("Track empty = %v, want ErrEmptyQuery", err)
	}
	if _, err := client.Playlist(context.Background(), "\t"); !errors.Is(err, catalog.ErrEmptyQuery) {
		t.Fatalf("Playlist empty = %v, want ErrEmptyQuery", err)
	}

	if _, err := client.Track(context.Background(), "not-a-valid-video-id"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("Track invalid ID = %v, want ErrNotFound", err)
	}
	if _, err := client.Track(context.Background(), "0wRYvsfhsR8"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("Track empty response = %v, want ErrNotFound", err)
	}
	if _, err := client.Related(context.Background(), " "); !errors.Is(err, catalog.ErrEmptyQuery) {
		t.Fatalf("Related empty = %v, want ErrEmptyQuery", err)
	}
	if _, err := client.Related(context.Background(), "not-a-valid-video-id"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("Related invalid ID = %v, want ErrNotFound", err)
	}
}

func TestRelatedRadioAndAutomixFallback(t *testing.T) {
	nextFixture := readFixture(t, "track_next.json")
	automixFixture := []byte(`{
	  "contents": {
	    "singleColumnMusicWatchNextResultsRenderer": {
	      "tabbedRenderer": {
	        "watchNextTabbedResultsRenderer": {
	          "tabs": [{
	            "tabRenderer": {
	              "content": {
	                "musicQueueRenderer": {
	                  "content": {
	                    "playlistPanelRenderer": {
	                      "contents": [
	                        {"playlistPanelVideoRenderer": {"videoId": "FGBhQbmPwH8", "title": {"runs": [{"text": "One More Time"}]}}},
	                        {"automixPreviewVideoRenderer": {"content": {"automixPlaylistVideoRenderer": {"navigationEndpoint": {"watchPlaylistEndpoint": {"playlistId": "RDAMVMFGBhQbmPwH8", "params": "wAEB"}}}}}}
	                      ]
	                    }
	                  }
	                }
	              }
	            }
	          }]
	        }
	      }
	    }
	  }
	}`)
	automixTracksFixture := []byte(`{
	  "contents": {
	    "singleColumnMusicWatchNextResultsRenderer": {
	      "playlist": {
	        "playlistPanelRenderer": {
	          "contents": [
	            {"playlistPanelVideoRenderer": {"videoId": "FGBhQbmPwH8", "title": {"runs": [{"text": "One More Time"}]}}},
	            {"playlistPanelVideoRenderer": {"videoId": "K0HSD_i2DvA", "title": {"runs": [{"text": "Around the World"}]}, "shortBylineText": {"runs": [{"text": "Daft Punk"}]}, "lengthText": {"runs": [{"text": "7:09"}]}}}
	          ]
	        }
	      }
	    }
	  }
	}`)

	srv, reqs, mu := newTestServer(t, func(endpoint string, req innerTubeRequest) (int, []byte) {
		if endpoint != "/next" {
			return http.StatusNotFound, nil
		}
		switch {
		case req.VideoID == "0wRYvsfhsR8" && req.PlaylistID == "RDAMVM0wRYvsfhsR8":
			return http.StatusOK, nextFixture
		case req.VideoID == "FGBhQbmPwH8" && req.PlaylistID == "RDAMVMFGBhQbmPwH8" && req.Params == "":
			return http.StatusOK, []byte(`{"contents":{}}`)
		case req.VideoID == "FGBhQbmPwH8" && req.PlaylistID == "":
			return http.StatusOK, automixFixture
		case req.VideoID == "FGBhQbmPwH8" && req.PlaylistID == "RDAMVMFGBhQbmPwH8" && req.Params == "wAEB":
			return http.StatusOK, automixTracksFixture
		default:
			return http.StatusOK, []byte(`{"contents":{}}`)
		}
	})

	now := time.Unix(1000, 0)
	client := New(WithBaseURL(srv.URL), WithClock(func() time.Time { return now }))

	tracks, err := client.Related(context.Background(), "https://music.youtube.com/watch?v=0wRYvsfhsR8")
	if err != nil {
		t.Fatalf("Related = %v", err)
	}
	if len(tracks) != 1 || tracks[0].ID != "FGBhQbmPwH8" || tracks[0].Title != "One More Time" {
		t.Fatalf("Related(0wRYvsfhsR8) = %+v, want single non-seed track FGBhQbmPwH8", tracks)
	}
	mu.Lock()
	firstReq := (*reqs)[0]
	mu.Unlock()
	if firstReq.path != "/next" || firstReq.body.PlaylistID != "RDAMVM0wRYvsfhsR8" || !firstReq.body.IsAudioOnly {
		t.Fatalf("first request = %+v", firstReq)
	}

	// Second call within TTL should hit radioCache without a new HTTP request.
	cached, err := client.Related(context.Background(), "0wRYvsfhsR8")
	if err != nil {
		t.Fatalf("cached Related = %v", err)
	}
	if len(cached) != 1 || cached[0].ID != "FGBhQbmPwH8" {
		t.Fatalf("cached Related = %+v", cached)
	}
	mu.Lock()
	callsAfterCache := len(*reqs)
	mu.Unlock()
	if callsAfterCache != 1 {
		t.Fatalf("HTTP calls = %d, want 1 (cached)", callsAfterCache)
	}

	// Fallback to automixPreviewVideoRenderer when RDAMVM returns empty.
	fallback, err := client.Related(context.Background(), "FGBhQbmPwH8")
	if err != nil {
		t.Fatalf("Related fallback = %v", err)
	}
	if len(fallback) != 1 || fallback[0].ID != "K0HSD_i2DvA" || fallback[0].Title != "Around the World" {
		t.Fatalf("fallback = %+v", fallback)
	}
}

func TestMaxResponseBytesEnforced(t *testing.T) {
	huge := make([]byte, 8192)
	for i := range huge {
		huge[i] = ' '
	}
	srv, _, _ := newTestServer(t, func(string, innerTubeRequest) (int, []byte) {
		return http.StatusOK, huge
	})
	client := New(WithBaseURL(srv.URL), WithMaxResponseBytes(1024))

	_, err := client.Search(context.Background(), "daft punk", 10)
	if !errors.Is(err, catalog.ErrOutputTooLarge) {
		t.Fatalf("Search oversized err = %v, want ErrOutputTooLarge", err)
	}
}

func TestSearchCollectionsAndBrowseArtistAlbum(t *testing.T) {
	albumSearchJSON := []byte(`{
		"contents": {
			"tabbedSearchResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [{
									"musicShelfRenderer": {
										"contents": [{
											"musicResponsiveListItemRenderer": {
												"navigationEndpoint": {
													"browseEndpoint": {
														"browseId": "MPREb_K8qWMWVqXGi",
														"browseEndpointContextSupportedConfigs": {
															"browseEndpointContextMusicConfig": {
																"pageType": "MUSIC_PAGE_TYPE_ALBUM"
															}
														}
													}
												},
												"flexColumns": [
													{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Discovery"}]}}},
													{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Album • Daft Punk • 2001"}]}}}
												],
												"thumbnail": {
													"musicThumbnailRenderer": {
														"thumbnail": {"thumbnails": [{"url": "https://example.com/alb.jpg", "width": 360, "height": 360}]}
													}
												}
											}
										}]
									}
								}]
							}
						}
					}
				}]
			}
		}
	}`)

	artistBrowseJSON := []byte(`{
		"header": {
			"musicImmersiveHeaderRenderer": {
				"title": {"runs": [{"text": "Daft Punk"}]},
				"thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [{"url": "https://example.com/art.jpg", "width": 360, "height": 360}]}}}
			}
		},
		"contents": {
			"singleColumnBrowseResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [
									{
										"musicShelfRenderer": {
											"contents": [{
												"musicResponsiveListItemRenderer": {
													"playlistItemData": {"videoId": "FGBhQbmPwH8"},
													"flexColumns": [
														{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "One More Time"}]}}},
														{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [
															{"text": "Discovery", "navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_123", "browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig": {"pageType": "MUSIC_PAGE_TYPE_ALBUM"}}}}},
															{"text": " • 5:20"}
														]}}}
													]
												}
											}]
										}
									},
									{
										"musicCarouselShelfRenderer": {
											"contents": [{
												"musicTwoRowItemRenderer": {
													"title": {"runs": [{"text": "Homework"}]},
													"subtitle": {"runs": [{"text": "Album • 1997"}]},
													"navigationEndpoint": {
														"browseEndpoint": {
															"browseId": "MPREb_homework",
															"browseEndpointContextSupportedConfigs": {
																"browseEndpointContextMusicConfig": {"pageType": "MUSIC_PAGE_TYPE_ALBUM"}
															}
														}
													}
												}
											}]
										}
									}
								]
							}
						}
					}
				}]
			}
		}
	}`)

	browseFixture := readFixture(t, "playlist_browse.json")
	srv, reqs, mu := newTestServer(t, func(endpoint string, req innerTubeRequest) (int, []byte) {
		switch endpoint {
		case "/search":
			return http.StatusOK, albumSearchJSON
		case "/browse":
			if req.BrowseID == "UCdaftpunk123" {
				return http.StatusOK, artistBrowseJSON
			}
			return http.StatusOK, browseFixture
		default:
			return http.StatusNotFound, nil
		}
	})

	client := New(WithBaseURL(srv.URL))

	tabs := client.SupportedTabs()
	if len(tabs) != 4 {
		t.Fatalf("SupportedTabs = %v, want 4 tabs", tabs)
	}

	cols, err := client.SearchCollections(context.Background(), catalog.TabAlbums, "discovery", 10)
	if err != nil {
		t.Fatalf("SearchCollections(albums) = %v", err)
	}
	if len(cols) != 1 || cols[0].ID != "MPREb_K8qWMWVqXGi" || cols[0].Kind != catalog.CollectionAlbum || cols[0].Title != "Discovery" {
		t.Fatalf("albums = %+v", cols)
	}
	mu.Lock()
	if (*reqs)[0].body.Params != albumsSearchParams {
		mu.Unlock()
		t.Fatalf("album search params = %q, want %q", (*reqs)[0].body.Params, albumsSearchParams)
	}
	mu.Unlock()

	// Second call hits collectionCache.
	cols2, err := client.SearchCollections(context.Background(), catalog.TabAlbums, "discovery", 10)
	if err != nil || len(cols2) != 1 {
		t.Fatalf("cached SearchCollections = %+v, err=%v", cols2, err)
	}
	mu.Lock()
	if len(*reqs) != 1 {
		mu.Unlock()
		t.Fatalf("requests = %d, want 1 (cached)", len(*reqs))
	}
	mu.Unlock()

	// Browse Artist returns top tracks and discography albums.
	artistDetail, err := client.BrowseCollection(context.Background(), catalog.Collection{
		ID:   "UCdaftpunk123",
		Kind: catalog.CollectionArtist,
	})
	if err != nil {
		t.Fatalf("BrowseCollection(artist) = %v", err)
	}
	if artistDetail.Collection.Title != "Daft Punk" {
		t.Fatalf("artist title = %q, want Daft Punk", artistDetail.Collection.Title)
	}
	if len(artistDetail.Tracks) != 1 || artistDetail.Tracks[0].ID != "FGBhQbmPwH8" || artistDetail.Tracks[0].Artist != "Daft Punk" {
		t.Fatalf("artist tracks = %+v", artistDetail.Tracks)
	}
	if len(artistDetail.Albums) != 1 || artistDetail.Albums[0].ID != "MPREb_homework" || artistDetail.Albums[0].Kind != catalog.CollectionAlbum {
		t.Fatalf("artist albums = %+v", artistDetail.Albums)
	}

	// Browse Playlist/Album uses browse tracks.
	plDetail, err := client.BrowseCollection(context.Background(), catalog.Collection{
		ID:   "PL1234567890abcdef",
		Kind: catalog.CollectionPlaylist,
	})
	if err != nil || len(plDetail.Tracks) == 0 {
		t.Fatalf("BrowseCollection(playlist) = %+v, err=%v", plDetail, err)
	}
}

func TestLiveCatalogSearchAndTrack(t *testing.T) {
	if os.Getenv("YTM_LIVE") != "1" && os.Getenv("YTM_WMP_LIVE_TEST") != "1" {
		t.Skip("set YTM_LIVE=1 to run live InnerTube catalog test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client := New()

	tracks, err := client.Search(ctx, "daft punk something about us", 5)
	if err != nil {
		t.Fatalf("live Search = %v", err)
	}
	if len(tracks) == 0 {
		t.Fatal("live Search returned 0 tracks")
	}
	first := tracks[0]
	if first.ID == "" || first.Title == "" || first.Artist == "" || first.Duration <= 0 || first.ThumbnailURL == "" {
		t.Fatalf("live Search returned incomplete track: %+v", first)
	}

	// Exercise live /next uncached using a fresh client without search cache.
	fresh := New()
	tr, err := fresh.Track(ctx, first.ID)
	if err != nil {
		t.Fatalf("live Track(%s) = %v", first.ID, err)
	}
	if tr.ID != first.ID || tr.Title == "" || tr.Artist == "" || tr.Duration <= 0 || tr.ThumbnailURL == "" {
		t.Fatalf("live Track returned incomplete track: %+v", tr)
	}

	// Exercise live /browse on a known album browseId.
	albumTracks, err := fresh.Playlist(ctx, "MPREb_K8qWMWVqXGi")
	if err != nil {
		t.Fatalf("live Playlist(album) = %v", err)
	}
	if len(albumTracks) == 0 {
		t.Fatal("live Playlist(album) returned 0 tracks")
	}
	if albumTracks[0].ID == "" || albumTracks[0].Title == "" || albumTracks[0].Artist == "" || albumTracks[0].ThumbnailURL == "" {
		t.Fatalf("live Playlist returned incomplete track: %+v", albumTracks[0])
	}

	radioTracks, err := fresh.Related(ctx, first.ID)
	if err != nil {
		t.Fatalf("live Related(%s) = %v", first.ID, err)
	}
	if len(radioTracks) == 0 {
		t.Fatal("live Related returned 0 tracks")
	}
	for _, rt := range radioTracks {
		if rt.ID == first.ID {
			t.Fatalf("live Related included seed track %q", first.ID)
		}
	}
}
