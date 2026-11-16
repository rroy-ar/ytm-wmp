// Package ytmusic implements catalog.Catalog using YouTube Music's pure-Go
// InnerTube WEB_REMIX JSON endpoints (`search`, `next`, and `browse`).
//
// It relies solely on public endpoints (no cookies or account auth) and never
// resolves playable stream URLs; stream resolution stays in internal/ytdlp.
package ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"ytm-wmp/internal/catalog"
	"ytm-wmp/internal/model"
)

const (
	// DefaultBaseURL is the public YouTube Music InnerTube v1 endpoint base.
	DefaultBaseURL = "https://music.youtube.com/youtubei/v1"
	// DefaultClientName is the InnerTube client name for YouTube Music web.
	DefaultClientName = "WEB_REMIX"
	// DefaultClientVersion is the InnerTube WEB_REMIX client version sent in requests.
	DefaultClientVersion = "1.20240101.01.00"
	// DefaultSearchLimit is used when Search is given limit <= 0.
	DefaultSearchLimit = 10
	// MaxSearchLimit caps how many search hits Search returns for a query.
	MaxSearchLimit = 50
	// DefaultPlaylistLimit caps how many tracks a playlist import returns.
	DefaultPlaylistLimit = 100
	// DefaultSearchTTL caches identical search queries in memory.
	DefaultSearchTTL = 10 * time.Minute
	// DefaultTrackTTL caches track metadata keyed by video ID in memory.
	DefaultTrackTTL = 1 * time.Hour
	// DefaultTimeout bounds each InnerTube HTTP request.
	DefaultTimeout = 10 * time.Second
	// DefaultMaxResponseBytes caps InnerTube JSON payloads so oversized bodies cannot OOM.
	DefaultMaxResponseBytes int64 = 16 << 20
	// DefaultUserAgent is sent on InnerTube HTTP requests.
	DefaultUserAgent = "ytm-wmp/0.1"

	// songsSearchParams filters WEB_REMIX search results to the Songs shelf.
	songsSearchParams = "EgWKAQIIAWoKEAkQBRAKEAMQBA%3D%3D"
	// albumsSearchParams filters WEB_REMIX search results to the Albums shelf.
	albumsSearchParams = "EgWKAQIYAWoKEAkQBRAKEAMQBA%3D%3D"
	// artistsSearchParams filters WEB_REMIX search results to the Artists shelf.
	artistsSearchParams = "EgWKAQIgAWoKEAkQBRAKEAMQBA%3D%3D"
	// playlistsSearchParams filters WEB_REMIX search results to Playlists.
	playlistsSearchParams = "EgWKAQIoAWoKEAkQBRAKEAMQBA%3D%3D"
	targetThumbDim        = 360
)

var (
	_ catalog.Catalog       = (*Client)(nil)
	_ catalog.RadioProvider = (*Client)(nil)
	_ catalog.TabCatalog    = (*Client)(nil)
)

type cachedTracks struct {
	tracks  []model.Track
	expires time.Time
}

type cachedCollections struct {
	items   []catalog.Collection
	expires time.Time
}

type cachedTrack struct {
	track   model.Track
	expires time.Time
}

// Client implements catalog.Catalog against YouTube Music InnerTube WEB_REMIX.
type Client struct {
	httpClient    *http.Client
	baseURL       string
	clientName    string
	clientVersion string
	hl            string
	gl            string
	userAgent     string
	timeout       time.Duration
	searchTTL     time.Duration
	trackTTL      time.Duration
	playlistLimit int
	maxBytes      int64
	now           func() time.Time

	mu              sync.Mutex
	searchCache     map[string]cachedTracks
	collectionCache map[string]cachedCollections
	trackCache      map[string]cachedTrack
	radioCache      map[string]cachedTracks
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client used for InnerTube requests.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithBaseURL overrides the InnerTube API base URL (used in tests).
func WithBaseURL(rawURL string) Option {
	return func(c *Client) {
		trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
		if trimmed != "" {
			c.baseURL = trimmed
		}
	}
}

// WithClientVersion overrides the InnerTube WEB_REMIX clientVersion string.
func WithClientVersion(version string) Option {
	return func(c *Client) {
		if v := strings.TrimSpace(version); v != "" {
			c.clientVersion = v
		}
	}
}

// WithLocale sets the InnerTube hl/gl locale parameters.
func WithLocale(hl, gl string) Option {
	return func(c *Client) {
		if v := strings.TrimSpace(hl); v != "" {
			c.hl = v
		}
		if v := strings.TrimSpace(gl); v != "" {
			c.gl = v
		}
	}
}

// WithUserAgent sets the HTTP User-Agent header sent on InnerTube requests.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if v := strings.TrimSpace(ua); v != "" {
			c.userAgent = v
		}
	}
}

// WithTimeout sets the per-request HTTP timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.timeout = d
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

// WithPlaylistLimit sets the maximum number of items returned by Playlist.
func WithPlaylistLimit(n int) Option {
	return func(c *Client) {
		if n > 0 {
			c.playlistLimit = n
		}
	}
}

// WithMaxResponseBytes caps the InnerTube HTTP response body size.
func WithMaxResponseBytes(n int64) Option {
	return func(c *Client) {
		if n > 0 {
			c.maxBytes = n
		}
	}
}

// New creates an InnerTube WEB_REMIX catalog Client.
func New(opts ...Option) *Client {
	c := &Client{
		httpClient:      &http.Client{},
		baseURL:         DefaultBaseURL,
		clientName:      DefaultClientName,
		clientVersion:   DefaultClientVersion,
		hl:              "en",
		gl:              "US",
		userAgent:       DefaultUserAgent,
		timeout:         DefaultTimeout,
		searchTTL:       DefaultSearchTTL,
		trackTTL:        DefaultTrackTTL,
		playlistLimit:   DefaultPlaylistLimit,
		maxBytes:        DefaultMaxResponseBytes,
		now:             time.Now,
		searchCache:     make(map[string]cachedTracks),
		collectionCache: make(map[string]cachedCollections),
		trackCache:      make(map[string]cachedTrack),
		radioCache:      make(map[string]cachedTracks),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// Search queries YouTube Music InnerTube `/search` filtered to songs.
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

	var resp searchResponse
	err := c.postJSON(ctx, "search", innerTubeRequest{
		Query:  query,
		Params: songsSearchParams,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("ytmusic search %q: %w", query, err)
	}

	tracks := extractSearchTracks(resp, limit)
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

// Track resolves metadata for a single video ID or watch URL via `/next`.
func (c *Client) Track(ctx context.Context, idOrURL string) (model.Track, error) {
	idOrURL = strings.TrimSpace(idOrURL)
	if idOrURL == "" {
		return model.Track{}, catalog.ErrEmptyQuery
	}

	vid := model.VideoID(idOrURL)
	if vid == "" {
		return model.Track{}, fmt.Errorf("%w: %q", catalog.ErrNotFound, idOrURL)
	}

	if c.trackTTL > 0 {
		now := c.now()
		c.mu.Lock()
		cached, ok := c.trackCache[vid]
		c.mu.Unlock()
		if ok && now.Before(cached.expires) {
			return cached.track, nil
		}
	}

	var resp nextResponse
	err := c.postJSON(ctx, "next", innerTubeRequest{VideoID: vid}, &resp)
	if err != nil {
		return model.Track{}, fmt.Errorf("ytmusic track %q: %w", idOrURL, err)
	}

	tracks := extractNextTracks(resp)
	track, ok := selectTrackForID(tracks, vid)
	if !ok {
		return model.Track{}, fmt.Errorf("%w: %q", catalog.ErrNotFound, idOrURL)
	}

	c.cacheTracks([]model.Track{track})
	return track, nil
}

// Playlist loads tracks for a YouTube Music playlist, album, or browse URL/ID.
func (c *Client) Playlist(ctx context.Context, idOrURL string) ([]model.Track, error) {
	idOrURL = strings.TrimSpace(idOrURL)
	if idOrURL == "" {
		return nil, catalog.ErrEmptyQuery
	}

	browseID, playlistID := parsePlaylistTarget(idOrURL)
	if browseID == "" {
		return nil, fmt.Errorf("%w: %q", catalog.ErrNotFound, idOrURL)
	}

	var resp browseResponse
	err := c.postJSON(ctx, "browse", innerTubeRequest{BrowseID: browseID}, &resp)
	if err != nil {
		return nil, fmt.Errorf("ytmusic playlist %q: %w", idOrURL, err)
	}

	tracks := extractBrowseTracks(resp, browseID)
	if len(tracks) == 0 && playlistID != "" && !strings.HasPrefix(playlistID, "MPREb_") {
		var nextResp nextResponse
		if nextErr := c.postJSON(ctx, "next", innerTubeRequest{PlaylistID: playlistID}, &nextResp); nextErr == nil {
			for _, candidate := range extractNextTracks(nextResp) {
				tracks = append(tracks, candidate.track)
			}
		} else if len(tracks) == 0 {
			return nil, fmt.Errorf("ytmusic playlist %q: %w", idOrURL, nextErr)
		}
	}

	if c.playlistLimit > 0 && len(tracks) > c.playlistLimit {
		tracks = tracks[:c.playlistLimit]
	}
	c.cacheTracks(tracks)
	return tracks, nil
}

// SupportedTabs reports the catalog search tabs supported by InnerTube WEB_REMIX.
func (c *Client) SupportedTabs() []catalog.SearchTab {
	return []catalog.SearchTab{
		catalog.TabSongs,
		catalog.TabPlaylists,
		catalog.TabAlbums,
		catalog.TabArtists,
	}
}

// SearchCollections searches playlists, albums, or artists via InnerTube `/search`.
func (c *Client) SearchCollections(ctx context.Context, tab catalog.SearchTab, query string, limit int) ([]catalog.Collection, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, catalog.ErrEmptyQuery
	}
	if limit <= 0 {
		limit = DefaultSearchLimit
	} else if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}

	var params string
	switch tab {
	case catalog.TabPlaylists:
		params = playlistsSearchParams
	case catalog.TabAlbums:
		params = albumsSearchParams
	case catalog.TabArtists:
		params = artistsSearchParams
	default:
		return nil, fmt.Errorf("unsupported collection search tab %q", tab)
	}

	key := fmt.Sprintf("%s:%d:%s", tab, limit, strings.ToLower(query))
	if c.searchTTL > 0 {
		now := c.now()
		c.mu.Lock()
		cached, ok := c.collectionCache[key]
		c.mu.Unlock()
		if ok && now.Before(cached.expires) {
			return append([]catalog.Collection(nil), cached.items...), nil
		}
	}

	var resp searchResponse
	err := c.postJSON(ctx, "search", innerTubeRequest{
		Query:  query,
		Params: params,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("ytmusic %s search %q: %w", tab, query, err)
	}

	items := extractSearchCollections(resp, tab, limit)
	if c.searchTTL > 0 {
		c.mu.Lock()
		c.collectionCache[key] = cachedCollections{
			items:   append([]catalog.Collection(nil), items...),
			expires: c.now().Add(c.searchTTL),
		}
		c.mu.Unlock()
	}
	return append([]catalog.Collection(nil), items...), nil
}

// BrowseCollection loads track and optional discography details for a playlist,
// album, or artist collection.
func (c *Client) BrowseCollection(ctx context.Context, col catalog.Collection) (catalog.CollectionDetail, error) {
	id := strings.TrimSpace(col.ID)
	if id == "" {
		return catalog.CollectionDetail{}, catalog.ErrEmptyQuery
	}

	if col.Kind == catalog.CollectionArtist || strings.HasPrefix(id, "UC") {
		var resp browseResponse
		if err := c.postJSON(ctx, "browse", innerTubeRequest{BrowseID: id}, &resp); err != nil {
			return catalog.CollectionDetail{}, fmt.Errorf("ytmusic browse artist %q: %w", id, err)
		}
		detail := extractArtistDetail(resp, col)
		if c.playlistLimit > 0 && len(detail.Tracks) > c.playlistLimit {
			detail.Tracks = detail.Tracks[:c.playlistLimit]
		}
		c.cacheTracks(detail.Tracks)
		return detail, nil
	}

	browseID, playlistID := parsePlaylistTarget(id)
	if browseID == "" {
		return catalog.CollectionDetail{}, fmt.Errorf("%w: %q", catalog.ErrNotFound, id)
	}
	var resp browseResponse
	if err := c.postJSON(ctx, "browse", innerTubeRequest{BrowseID: browseID}, &resp); err != nil {
		return catalog.CollectionDetail{}, fmt.Errorf("ytmusic browse %q: %w", id, err)
	}
	tracks := extractBrowseTracks(resp, browseID)
	if len(tracks) == 0 && playlistID != "" && !strings.HasPrefix(playlistID, "MPREb_") {
		var nextResp nextResponse
		if nextErr := c.postJSON(ctx, "next", innerTubeRequest{PlaylistID: playlistID}, &nextResp); nextErr == nil {
			for _, candidate := range extractNextTracks(nextResp) {
				tracks = append(tracks, candidate.track)
			}
		} else {
			return catalog.CollectionDetail{}, fmt.Errorf("ytmusic browse %q: %w", id, nextErr)
		}
	}
	if c.playlistLimit > 0 && len(tracks) > c.playlistLimit {
		tracks = tracks[:c.playlistLimit]
	}
	c.cacheTracks(tracks)

	updated := col
	if hdr := findBrowseHeader(resp); hdr != nil {
		if t := joinRunsText(hdr.Title); t != "" {
			updated.Title = t
		}
		if sub := joinRunsText(hdr.StraplineTextOne); sub != "" && updated.Subtitle == "" {
			updated.Subtitle = sub
		}
		if u := pickBestThumbnailURL(extractThumbnails(hdr.Thumbnail)); u != "" && updated.ThumbnailURL == "" {
			updated.ThumbnailURL = u
		}
	}
	return catalog.CollectionDetail{
		Collection: updated,
		Tracks:     tracks,
	}, nil
}

// Related fetches unauthenticated YouTube Music Up Next / radio tracks seeded
// by trackID via InnerTube `/next` (`RDAMVM<videoId>`), excluding the seed
// track itself.
func (c *Client) Related(ctx context.Context, trackID string) ([]model.Track, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return nil, catalog.ErrEmptyQuery
	}

	vid := model.VideoID(trackID)
	if vid == "" {
		return nil, fmt.Errorf("%w: %q", catalog.ErrNotFound, trackID)
	}

	if c.searchTTL > 0 {
		now := c.now()
		c.mu.Lock()
		cached, ok := c.radioCache[vid]
		c.mu.Unlock()
		if ok && now.Before(cached.expires) {
			return append([]model.Track(nil), cached.tracks...), nil
		}
	}

	var resp nextResponse
	err := c.postJSON(ctx, "next", innerTubeRequest{
		VideoID:     vid,
		PlaylistID:  "RDAMVM" + vid,
		IsAudioOnly: true,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("ytmusic radio %q: %w", trackID, err)
	}

	tracks := filterRelatedTracks(extractNextTracks(resp), vid)
	if len(tracks) == 0 {
		var plainResp nextResponse
		if plainErr := c.postJSON(ctx, "next", innerTubeRequest{
			VideoID:     vid,
			IsAudioOnly: true,
		}, &plainResp); plainErr == nil {
			if ep, ok := extractAutomixEndpoint(plainResp); ok && ep.PlaylistID != "" {
				var mixResp nextResponse
				if mixErr := c.postJSON(ctx, "next", innerTubeRequest{
					VideoID:     vid,
					PlaylistID:  ep.PlaylistID,
					Params:      ep.Params,
					IsAudioOnly: true,
				}, &mixResp); mixErr == nil {
					tracks = filterRelatedTracks(extractNextTracks(mixResp), vid)
				}
			}
		}
	}

	if c.playlistLimit > 0 && len(tracks) > c.playlistLimit {
		tracks = tracks[:c.playlistLimit]
	}
	c.cacheTracks(tracks)
	if c.searchTTL > 0 && len(tracks) > 0 {
		c.mu.Lock()
		c.radioCache[vid] = cachedTracks{
			tracks:  append([]model.Track(nil), tracks...),
			expires: c.now().Add(c.searchTTL),
		}
		c.mu.Unlock()
	}
	return append([]model.Track(nil), tracks...), nil
}

func (c *Client) postJSON(ctx context.Context, endpoint string, reqBody innerTubeRequest, dst any) error {
	reqBody.Context = innerTubeContext{
		Client: innerTubeClient{
			ClientName:    c.clientName,
			ClientVersion: c.clientVersion,
			HL:            c.hl,
			GL:            c.gl,
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}

	reqCtx := ctx
	var cancel context.CancelFunc
	if c.timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	endpointURL := c.baseURL + "/" + strings.TrimLeft(endpoint, "/") + "?prettyPrint=false"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpointURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://music.youtube.com")
	req.Header.Set("Referer", "https://music.youtube.com/")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > c.maxBytes {
		return fmt.Errorf("%w (%d bytes)", catalog.ErrOutputTooLarge, c.maxBytes)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s returned HTTP 404", catalog.ErrNotFound, endpoint)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 256 {
			snippet = snippet[:256] + "..."
		}
		if snippet != "" {
			return fmt.Errorf("%s returned HTTP %d: %s", endpoint, resp.StatusCode, snippet)
		}
		return fmt.Errorf("%s returned HTTP %d", endpoint, resp.StatusCode)
	}

	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decoding %s response: %w", endpoint, err)
	}
	return nil
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

func parsePlaylistTarget(idOrURL string) (browseID, playlistID string) {
	if u, err := url.Parse(idOrURL); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		if list := strings.TrimSpace(u.Query().Get("list")); list != "" {
			return toBrowseID(list), strings.TrimPrefix(list, "VL")
		}
		trimmedPath := strings.TrimRight(u.Path, "/")
		if strings.HasPrefix(trimmedPath, "/browse/") {
			seg := strings.TrimSpace(strings.TrimPrefix(trimmedPath, "/browse/"))
			if seg != "" {
				return seg, strings.TrimPrefix(seg, "VL")
			}
		}
		return "", ""
	}
	return toBrowseID(idOrURL), strings.TrimPrefix(idOrURL, "VL")
}

func toBrowseID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "VL") || strings.HasPrefix(id, "MPREb_") {
		return id
	}
	return "VL" + id
}

// --- InnerTube JSON wire types ---

type innerTubeRequest struct {
	Context     innerTubeContext `json:"context"`
	Query       string           `json:"query,omitempty"`
	Params      string           `json:"params,omitempty"`
	VideoID     string           `json:"videoId,omitempty"`
	BrowseID    string           `json:"browseId,omitempty"`
	PlaylistID  string           `json:"playlistId,omitempty"`
	IsAudioOnly bool             `json:"isAudioOnly,omitempty"`
}

type innerTubeContext struct {
	Client innerTubeClient `json:"client"`
}

type innerTubeClient struct {
	ClientName    string `json:"clientName"`
	ClientVersion string `json:"clientVersion"`
	HL            string `json:"hl,omitempty"`
	GL            string `json:"gl,omitempty"`
}

type searchResponse struct {
	Contents struct {
		TabbedSearchResultsRenderer *struct {
			Tabs []tabHolder `json:"tabs"`
		} `json:"tabbedSearchResultsRenderer"`
		SectionListRenderer *sectionListRenderer `json:"sectionListRenderer"`
	} `json:"contents"`
}

type browseResponse struct {
	Header   *browseHeader `json:"header"`
	Contents struct {
		TwoColumnBrowseResultsRenderer *struct {
			Tabs              []tabHolder `json:"tabs"`
			SecondaryContents *struct {
				SectionListRenderer *sectionListRenderer `json:"sectionListRenderer"`
			} `json:"secondaryContents"`
		} `json:"twoColumnBrowseResultsRenderer"`
		SingleColumnBrowseResultsRenderer *struct {
			Tabs []tabHolder `json:"tabs"`
		} `json:"singleColumnBrowseResultsRenderer"`
	} `json:"contents"`
}

type nextResponse struct {
	Contents struct {
		SingleColumnMusicWatchNextResultsRenderer *struct {
			TabbedRenderer *struct {
				WatchNextTabbedResultsRenderer *struct {
					Tabs []tabHolder `json:"tabs"`
				} `json:"watchNextTabbedResultsRenderer"`
			} `json:"tabbedRenderer"`
			Playlist *struct {
				PlaylistPanelRenderer *playlistPanelRenderer `json:"playlistPanelRenderer"`
			} `json:"playlist"`
		} `json:"singleColumnMusicWatchNextResultsRenderer"`
	} `json:"contents"`
}

type tabHolder struct {
	TabRenderer *struct {
		Content *struct {
			SectionListRenderer *sectionListRenderer `json:"sectionListRenderer"`
			MusicQueueRenderer  *struct {
				Content *struct {
					PlaylistPanelRenderer *playlistPanelRenderer `json:"playlistPanelRenderer"`
				} `json:"content"`
			} `json:"musicQueueRenderer"`
		} `json:"content"`
	} `json:"tabRenderer"`
}

type sectionListRenderer struct {
	Contents []sectionItem `json:"contents"`
}

type sectionItem struct {
	MusicShelfRenderer              *musicShelfRenderer         `json:"musicShelfRenderer"`
	MusicPlaylistShelfRenderer      *musicShelfRenderer         `json:"musicPlaylistShelfRenderer"`
	MusicCarouselShelfRenderer      *musicCarouselShelfRenderer `json:"musicCarouselShelfRenderer"`
	MusicCardShelfRenderer          *musicCardShelfRenderer     `json:"musicCardShelfRenderer"`
	MusicResponsiveHeaderRenderer   *responsiveHeader           `json:"musicResponsiveHeaderRenderer"`
	MusicEditablePlaylistDetailHdr  *editablePlaylistHeader     `json:"musicEditablePlaylistDetailHeaderRenderer"`
	MusicDetailHeaderRenderer       *responsiveHeader           `json:"musicDetailHeaderRenderer"`
	ItemSectionRenderer             *sectionListRenderer        `json:"itemSectionRenderer"`
	MusicResponsiveListItemRenderer *responsiveListItem         `json:"musicResponsiveListItemRenderer"`
}

type browseHeader struct {
	MusicResponsiveHeaderRenderer             *responsiveHeader       `json:"musicResponsiveHeaderRenderer"`
	MusicEditablePlaylistDetailHeaderRenderer *editablePlaylistHeader `json:"musicEditablePlaylistDetailHeaderRenderer"`
	MusicDetailHeaderRenderer                 *responsiveHeader       `json:"musicDetailHeaderRenderer"`
	MusicImmersiveHeaderRenderer              *responsiveHeader       `json:"musicImmersiveHeaderRenderer"`
	MusicVisualHeaderRenderer                 *responsiveHeader       `json:"musicVisualHeaderRenderer"`
}

type musicCarouselShelfRenderer struct {
	Header *struct {
		MusicCarouselShelfBasicHeaderRenderer *struct {
			Title *textRuns `json:"title"`
		} `json:"musicCarouselShelfBasicHeaderRenderer"`
	} `json:"header"`
	Contents []carouselItem `json:"contents"`
}

type carouselItem struct {
	MusicTwoRowItemRenderer         *twoRowItem         `json:"musicTwoRowItemRenderer"`
	MusicResponsiveListItemRenderer *responsiveListItem `json:"musicResponsiveListItemRenderer"`
}

type twoRowItem struct {
	Title              *textRuns           `json:"title"`
	Subtitle           *textRuns           `json:"subtitle"`
	ThumbnailRenderer  *thumbnailHolder    `json:"thumbnailRenderer"`
	NavigationEndpoint *navigationEndpoint `json:"navigationEndpoint"`
}

type editablePlaylistHeader struct {
	Header *struct {
		MusicResponsiveHeaderRenderer *responsiveHeader `json:"musicResponsiveHeaderRenderer"`
	} `json:"header"`
}

type responsiveHeader struct {
	Title            *textRuns        `json:"title"`
	StraplineTextOne *textRuns        `json:"straplineTextOne"`
	Subtitle         *textRuns        `json:"subtitle"`
	Thumbnail        *thumbnailHolder `json:"thumbnail"`
}

type musicShelfRenderer struct {
	Contents []shelfItem `json:"contents"`
}

type musicCardShelfRenderer struct {
	Title            *textRuns           `json:"title"`
	Subtitle         *textRuns           `json:"subtitle"`
	Thumbnail        *thumbnailHolder    `json:"thumbnail"`
	OnTap            *navigationEndpoint `json:"onTap"`
	ThumbnailOverlay *overlayHolder      `json:"thumbnailOverlay"`
	Contents         []shelfItem         `json:"contents"`
}

type shelfItem struct {
	MusicResponsiveListItemRenderer *responsiveListItem `json:"musicResponsiveListItemRenderer"`
}

type responsiveListItem struct {
	PlaylistItemData   *playlistItemData   `json:"playlistItemData"`
	NavigationEndpoint *navigationEndpoint `json:"navigationEndpoint"`
	Overlay            *overlayHolder      `json:"overlay"`
	Thumbnail          *thumbnailHolder    `json:"thumbnail"`
	FlexColumns        []flexColumnHolder  `json:"flexColumns"`
	FixedColumns       []fixedColumnHolder `json:"fixedColumns"`
}

type playlistItemData struct {
	VideoID string `json:"videoId"`
}

type overlayHolder struct {
	MusicItemThumbnailOverlayRenderer *struct {
		Content *struct {
			MusicPlayButtonRenderer *struct {
				PlayNavigationEndpoint *navigationEndpoint `json:"playNavigationEndpoint"`
			} `json:"musicPlayButtonRenderer"`
		} `json:"content"`
	} `json:"musicItemThumbnailOverlayRenderer"`
}

type flexColumnHolder struct {
	MusicResponsiveListItemFlexColumnRenderer *columnTextRenderer `json:"musicResponsiveListItemFlexColumnRenderer"`
}

type fixedColumnHolder struct {
	MusicResponsiveListItemFixedColumnRenderer *columnTextRenderer `json:"musicResponsiveListItemFixedColumnRenderer"`
}

type columnTextRenderer struct {
	Text *textRuns `json:"text"`
}

type playlistPanelRenderer struct {
	Contents []playlistPanelItem `json:"contents"`
}

type playlistPanelItem struct {
	PlaylistPanelVideoRenderer        *playlistPanelVideo `json:"playlistPanelVideoRenderer"`
	PlaylistPanelVideoWrapperRenderer *struct {
		PrimaryRenderer *struct {
			PlaylistPanelVideoRenderer *playlistPanelVideo `json:"playlistPanelVideoRenderer"`
		} `json:"primaryRenderer"`
	} `json:"playlistPanelVideoWrapperRenderer"`
	AutomixPreviewVideoRenderer *struct {
		Content *struct {
			AutomixPlaylistVideoRenderer *struct {
				NavigationEndpoint *struct {
					WatchPlaylistEndpoint *watchPlaylistEndpoint `json:"watchPlaylistEndpoint"`
				} `json:"navigationEndpoint"`
			} `json:"automixPlaylistVideoRenderer"`
		} `json:"content"`
	} `json:"automixPreviewVideoRenderer"`
}

type watchPlaylistEndpoint struct {
	PlaylistID string `json:"playlistId"`
	Params     string `json:"params"`
}

type playlistPanelVideo struct {
	VideoID            string              `json:"videoId"`
	Title              *textRuns           `json:"title"`
	ShortBylineText    *textRuns           `json:"shortBylineText"`
	LongBylineText     *textRuns           `json:"longBylineText"`
	LengthText         *textRuns           `json:"lengthText"`
	Thumbnail          *thumbnailList      `json:"thumbnail"`
	NavigationEndpoint *navigationEndpoint `json:"navigationEndpoint"`
	Selected           bool                `json:"selected"`
}

type textRuns struct {
	Runs []textRun `json:"runs"`
}

type textRun struct {
	Text               string              `json:"text"`
	NavigationEndpoint *navigationEndpoint `json:"navigationEndpoint"`
}

type navigationEndpoint struct {
	WatchEndpoint *struct {
		VideoID string `json:"videoId"`
	} `json:"watchEndpoint"`
	BrowseEndpoint *struct {
		BrowseID                              string `json:"browseId"`
		BrowseEndpointContextSupportedConfigs *struct {
			BrowseEndpointContextMusicConfig *struct {
				PageType string `json:"pageType"`
			} `json:"browseEndpointContextMusicConfig"`
		} `json:"browseEndpointContextSupportedConfigs"`
	} `json:"browseEndpoint"`
}

type thumbnailHolder struct {
	MusicThumbnailRenderer *struct {
		Thumbnail *thumbnailList `json:"thumbnail"`
	} `json:"musicThumbnailRenderer"`
	CroppedSquareThumbnailRenderer *struct {
		Thumbnail *thumbnailList `json:"thumbnail"`
	} `json:"croppedSquareThumbnailRenderer"`
}

type thumbnailList struct {
	Thumbnails []thumbnailItem `json:"thumbnails"`
}

type thumbnailItem struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// --- Track extraction helpers ---

func extractSearchTracks(resp searchResponse, limit int) []model.Track {
	var sections []sectionItem
	if resp.Contents.TabbedSearchResultsRenderer != nil {
		for _, tab := range resp.Contents.TabbedSearchResultsRenderer.Tabs {
			if tab.TabRenderer == nil || tab.TabRenderer.Content == nil || tab.TabRenderer.Content.SectionListRenderer == nil {
				continue
			}
			sections = append(sections, tab.TabRenderer.Content.SectionListRenderer.Contents...)
		}
	}
	if resp.Contents.SectionListRenderer != nil {
		sections = append(sections, resp.Contents.SectionListRenderer.Contents...)
	}

	tracks := make([]model.Track, 0, limit)
	seen := make(map[string]struct{}, limit)
	addTrack := func(tr model.Track, ok bool) bool {
		if !ok || tr.ID == "" {
			return false
		}
		if _, dup := seen[tr.ID]; dup {
			return false
		}
		seen[tr.ID] = struct{}{}
		tracks = append(tracks, tr)
		return limit > 0 && len(tracks) >= limit
	}

	for _, sec := range sections {
		if sec.MusicCardShelfRenderer != nil {
			if addTrack(cardToTrack(sec.MusicCardShelfRenderer)) {
				return tracks
			}
			for _, item := range sec.MusicCardShelfRenderer.Contents {
				if item.MusicResponsiveListItemRenderer != nil {
					if addTrack(listItemToTrack(item.MusicResponsiveListItemRenderer, nil)) {
						return tracks
					}
				}
			}
		}
		if sec.MusicShelfRenderer != nil {
			for _, item := range sec.MusicShelfRenderer.Contents {
				if item.MusicResponsiveListItemRenderer != nil {
					if addTrack(listItemToTrack(item.MusicResponsiveListItemRenderer, nil)) {
						return tracks
					}
				}
			}
		}
		if sec.MusicResponsiveListItemRenderer != nil {
			if addTrack(listItemToTrack(sec.MusicResponsiveListItemRenderer, nil)) {
				return tracks
			}
		}
		if sec.ItemSectionRenderer != nil {
			for _, sub := range sec.ItemSectionRenderer.Contents {
				if sub.MusicResponsiveListItemRenderer != nil {
					if addTrack(listItemToTrack(sub.MusicResponsiveListItemRenderer, nil)) {
						return tracks
					}
				}
				if sub.MusicShelfRenderer != nil {
					for _, item := range sub.MusicShelfRenderer.Contents {
						if item.MusicResponsiveListItemRenderer != nil {
							if addTrack(listItemToTrack(item.MusicResponsiveListItemRenderer, nil)) {
								return tracks
							}
						}
					}
				}
			}
		}
	}
	return tracks
}

type nextCandidate struct {
	track    model.Track
	selected bool
}

func nextPanels(resp nextResponse) []*playlistPanelRenderer {
	var panels []*playlistPanelRenderer
	sc := resp.Contents.SingleColumnMusicWatchNextResultsRenderer
	if sc == nil {
		return nil
	}
	if sc.TabbedRenderer != nil && sc.TabbedRenderer.WatchNextTabbedResultsRenderer != nil {
		for _, tab := range sc.TabbedRenderer.WatchNextTabbedResultsRenderer.Tabs {
			if tab.TabRenderer == nil || tab.TabRenderer.Content == nil || tab.TabRenderer.Content.MusicQueueRenderer == nil {
				continue
			}
			mq := tab.TabRenderer.Content.MusicQueueRenderer
			if mq.Content != nil && mq.Content.PlaylistPanelRenderer != nil {
				panels = append(panels, mq.Content.PlaylistPanelRenderer)
			}
		}
	}
	if sc.Playlist != nil && sc.Playlist.PlaylistPanelRenderer != nil {
		panels = append(panels, sc.Playlist.PlaylistPanelRenderer)
	}
	return panels
}

func extractNextTracks(resp nextResponse) []nextCandidate {
	var out []nextCandidate
	for _, panel := range nextPanels(resp) {
		for _, item := range panel.Contents {
			vidRenderer := item.PlaylistPanelVideoRenderer
			if vidRenderer == nil && item.PlaylistPanelVideoWrapperRenderer != nil && item.PlaylistPanelVideoWrapperRenderer.PrimaryRenderer != nil {
				vidRenderer = item.PlaylistPanelVideoWrapperRenderer.PrimaryRenderer.PlaylistPanelVideoRenderer
			}
			if vidRenderer == nil {
				continue
			}
			if tr, ok := panelVideoToTrack(vidRenderer); ok {
				out = append(out, nextCandidate{track: tr, selected: vidRenderer.Selected})
			}
		}
	}
	return out
}

func extractAutomixEndpoint(resp nextResponse) (watchPlaylistEndpoint, bool) {
	for _, panel := range nextPanels(resp) {
		for _, item := range panel.Contents {
			if item.AutomixPreviewVideoRenderer == nil ||
				item.AutomixPreviewVideoRenderer.Content == nil ||
				item.AutomixPreviewVideoRenderer.Content.AutomixPlaylistVideoRenderer == nil ||
				item.AutomixPreviewVideoRenderer.Content.AutomixPlaylistVideoRenderer.NavigationEndpoint == nil ||
				item.AutomixPreviewVideoRenderer.Content.AutomixPlaylistVideoRenderer.NavigationEndpoint.WatchPlaylistEndpoint == nil {
				continue
			}
			ep := *item.AutomixPreviewVideoRenderer.Content.AutomixPlaylistVideoRenderer.NavigationEndpoint.WatchPlaylistEndpoint
			if ep.PlaylistID != "" {
				return ep, true
			}
		}
	}
	return watchPlaylistEndpoint{}, false
}

func filterRelatedTracks(candidates []nextCandidate, seedID string) []model.Track {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]model.Track, 0, len(candidates))
	seen := map[string]struct{}{}
	if seedID != "" {
		seen[seedID] = struct{}{}
	}
	for _, c := range candidates {
		id := model.NormalizeTrackID(c.track.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		c.track.ID = id
		out = append(out, c.track)
	}
	return out
}

func selectTrackForID(candidates []nextCandidate, targetID string) (model.Track, bool) {
	if len(candidates) == 0 {
		return model.Track{}, false
	}
	for _, c := range candidates {
		if c.track.ID == targetID {
			return c.track, true
		}
	}
	for _, c := range candidates {
		if c.selected {
			return c.track, true
		}
	}
	return candidates[0].track, true
}

type browseDefaults struct {
	album  string
	artist string
	thumbs []thumbnailItem
}

func extractBrowseTracks(resp browseResponse, browseID string) []model.Track {
	defaults := extractBrowseDefaults(resp, browseID)
	isAlbum := strings.HasPrefix(browseID, "MPREb_")

	var shelves []*musicShelfRenderer
	collectFromSections := func(secList *sectionListRenderer) {
		if secList == nil {
			return
		}
		for _, sec := range secList.Contents {
			if sec.MusicPlaylistShelfRenderer != nil {
				shelves = append(shelves, sec.MusicPlaylistShelfRenderer)
			}
			if sec.MusicShelfRenderer != nil {
				shelves = append(shelves, sec.MusicShelfRenderer)
			}
			if sec.ItemSectionRenderer != nil {
				for _, sub := range sec.ItemSectionRenderer.Contents {
					if sub.MusicPlaylistShelfRenderer != nil {
						shelves = append(shelves, sub.MusicPlaylistShelfRenderer)
					}
					if sub.MusicShelfRenderer != nil {
						shelves = append(shelves, sub.MusicShelfRenderer)
					}
				}
			}
		}
	}

	if resp.Contents.TwoColumnBrowseResultsRenderer != nil {
		if resp.Contents.TwoColumnBrowseResultsRenderer.SecondaryContents != nil {
			collectFromSections(resp.Contents.TwoColumnBrowseResultsRenderer.SecondaryContents.SectionListRenderer)
		}
		for _, tab := range resp.Contents.TwoColumnBrowseResultsRenderer.Tabs {
			if tab.TabRenderer != nil && tab.TabRenderer.Content != nil {
				collectFromSections(tab.TabRenderer.Content.SectionListRenderer)
			}
		}
	}
	if resp.Contents.SingleColumnBrowseResultsRenderer != nil {
		for _, tab := range resp.Contents.SingleColumnBrowseResultsRenderer.Tabs {
			if tab.TabRenderer != nil && tab.TabRenderer.Content != nil {
				collectFromSections(tab.TabRenderer.Content.SectionListRenderer)
			}
		}
	}

	var tracks []model.Track
	for _, shelf := range shelves {
		for _, item := range shelf.Contents {
			if item.MusicResponsiveListItemRenderer == nil {
				continue
			}
			var fallbackThumbs []thumbnailItem
			if isAlbum {
				fallbackThumbs = defaults.thumbs
			}
			tr, ok := listItemToTrack(item.MusicResponsiveListItemRenderer, fallbackThumbs)
			if !ok {
				continue
			}
			if tr.Artist == "" && defaults.artist != "" {
				tr.Artist = defaults.artist
			}
			if tr.Album == "" && isAlbum && defaults.album != "" {
				tr.Album = defaults.album
			}
			tracks = append(tracks, tr)
		}
	}
	return tracks
}

func extractBrowseDefaults(resp browseResponse, browseID string) browseDefaults {
	hdr := findBrowseHeader(resp)
	if hdr == nil {
		return browseDefaults{}
	}

	var out browseDefaults
	if strings.HasPrefix(browseID, "MPREb_") {
		out.album = joinRunsText(hdr.Title)
	}
	out.artist = joinRunsText(hdr.StraplineTextOne)
	if out.artist == "" && hdr.Subtitle != nil {
		meta := parseRunsMetadata(hdr.Subtitle.Runs, false)
		out.artist = meta.artist
	}
	out.thumbs = extractThumbnails(hdr.Thumbnail)
	return out
}

func findBrowseHeader(resp browseResponse) *responsiveHeader {
	if resp.Header != nil {
		switch {
		case resp.Header.MusicResponsiveHeaderRenderer != nil:
			return resp.Header.MusicResponsiveHeaderRenderer
		case resp.Header.MusicEditablePlaylistDetailHeaderRenderer != nil && resp.Header.MusicEditablePlaylistDetailHeaderRenderer.Header != nil:
			return resp.Header.MusicEditablePlaylistDetailHeaderRenderer.Header.MusicResponsiveHeaderRenderer
		case resp.Header.MusicDetailHeaderRenderer != nil:
			return resp.Header.MusicDetailHeaderRenderer
		case resp.Header.MusicImmersiveHeaderRenderer != nil:
			return resp.Header.MusicImmersiveHeaderRenderer
		case resp.Header.MusicVisualHeaderRenderer != nil:
			return resp.Header.MusicVisualHeaderRenderer
		}
	}
	if resp.Contents.TwoColumnBrowseResultsRenderer != nil {
		for _, tab := range resp.Contents.TwoColumnBrowseResultsRenderer.Tabs {
			if tab.TabRenderer == nil || tab.TabRenderer.Content == nil || tab.TabRenderer.Content.SectionListRenderer == nil {
				continue
			}
			for _, sec := range tab.TabRenderer.Content.SectionListRenderer.Contents {
				switch {
				case sec.MusicResponsiveHeaderRenderer != nil:
					return sec.MusicResponsiveHeaderRenderer
				case sec.MusicEditablePlaylistDetailHdr != nil && sec.MusicEditablePlaylistDetailHdr.Header != nil:
					return sec.MusicEditablePlaylistDetailHdr.Header.MusicResponsiveHeaderRenderer
				case sec.MusicDetailHeaderRenderer != nil:
					return sec.MusicDetailHeaderRenderer
				}
			}
		}
	}
	return nil
}

func extractSearchCollections(resp searchResponse, tab catalog.SearchTab, limit int) []catalog.Collection {
	var sections []sectionItem
	if resp.Contents.TabbedSearchResultsRenderer != nil {
		for _, t := range resp.Contents.TabbedSearchResultsRenderer.Tabs {
			if t.TabRenderer == nil || t.TabRenderer.Content == nil || t.TabRenderer.Content.SectionListRenderer == nil {
				continue
			}
			sections = append(sections, t.TabRenderer.Content.SectionListRenderer.Contents...)
		}
	}
	if resp.Contents.SectionListRenderer != nil {
		sections = append(sections, resp.Contents.SectionListRenderer.Contents...)
	}

	items := make([]catalog.Collection, 0, limit)
	seen := make(map[string]struct{}, limit)
	addCol := func(c catalog.Collection, ok bool) bool {
		if !ok || c.ID == "" {
			return false
		}
		if _, dup := seen[c.ID]; dup {
			return false
		}
		seen[c.ID] = struct{}{}
		items = append(items, c)
		return limit > 0 && len(items) >= limit
	}

	for _, sec := range sections {
		if sec.MusicCardShelfRenderer != nil {
			if addCol(cardToCollection(sec.MusicCardShelfRenderer, tab)) {
				return items
			}
			for _, item := range sec.MusicCardShelfRenderer.Contents {
				if item.MusicResponsiveListItemRenderer != nil {
					if addCol(listItemToCollection(item.MusicResponsiveListItemRenderer, tab)) {
						return items
					}
				}
			}
		}
		if sec.MusicShelfRenderer != nil {
			for _, item := range sec.MusicShelfRenderer.Contents {
				if item.MusicResponsiveListItemRenderer != nil {
					if addCol(listItemToCollection(item.MusicResponsiveListItemRenderer, tab)) {
						return items
					}
				}
			}
		}
		if sec.MusicResponsiveListItemRenderer != nil {
			if addCol(listItemToCollection(sec.MusicResponsiveListItemRenderer, tab)) {
				return items
			}
		}
		if sec.ItemSectionRenderer != nil {
			for _, sub := range sec.ItemSectionRenderer.Contents {
				if sub.MusicResponsiveListItemRenderer != nil {
					if addCol(listItemToCollection(sub.MusicResponsiveListItemRenderer, tab)) {
						return items
					}
				}
				if sub.MusicShelfRenderer != nil {
					for _, item := range sub.MusicShelfRenderer.Contents {
						if item.MusicResponsiveListItemRenderer != nil {
							if addCol(listItemToCollection(item.MusicResponsiveListItemRenderer, tab)) {
								return items
							}
						}
					}
				}
			}
		}
	}
	return items
}

func extractArtistDetail(resp browseResponse, col catalog.Collection) catalog.CollectionDetail {
	updated := col
	updated.Kind = catalog.CollectionArtist
	if hdr := findBrowseHeader(resp); hdr != nil {
		if t := joinRunsText(hdr.Title); t != "" {
			updated.Title = t
		}
		if sub := joinRunsText(hdr.Subtitle); sub != "" && updated.Subtitle == "" {
			updated.Subtitle = sub
		}
		if u := pickBestThumbnailURL(extractThumbnails(hdr.Thumbnail)); u != "" && updated.ThumbnailURL == "" {
			updated.ThumbnailURL = u
		}
	}

	var sections []sectionItem
	collectSections := func(sl *sectionListRenderer) {
		if sl != nil {
			sections = append(sections, sl.Contents...)
		}
	}
	if resp.Contents.SingleColumnBrowseResultsRenderer != nil {
		for _, t := range resp.Contents.SingleColumnBrowseResultsRenderer.Tabs {
			if t.TabRenderer != nil && t.TabRenderer.Content != nil {
				collectSections(t.TabRenderer.Content.SectionListRenderer)
			}
		}
	}
	if resp.Contents.TwoColumnBrowseResultsRenderer != nil {
		if resp.Contents.TwoColumnBrowseResultsRenderer.SecondaryContents != nil {
			collectSections(resp.Contents.TwoColumnBrowseResultsRenderer.SecondaryContents.SectionListRenderer)
		}
		for _, t := range resp.Contents.TwoColumnBrowseResultsRenderer.Tabs {
			if t.TabRenderer != nil && t.TabRenderer.Content != nil {
				collectSections(t.TabRenderer.Content.SectionListRenderer)
			}
		}
	}

	var tracks []model.Track
	var albums []catalog.Collection
	seenTracks := make(map[string]struct{})
	seenAlbums := make(map[string]struct{})

	for _, sec := range sections {
		if sec.MusicShelfRenderer != nil {
			for _, item := range sec.MusicShelfRenderer.Contents {
				if item.MusicResponsiveListItemRenderer == nil {
					continue
				}
				if tr, ok := listItemToTrack(item.MusicResponsiveListItemRenderer, nil); ok {
					if _, dup := seenTracks[tr.ID]; !dup {
						seenTracks[tr.ID] = struct{}{}
						if tr.Artist == "" && updated.Title != "" {
							tr.Artist = updated.Title
						}
						tracks = append(tracks, tr)
					}
				}
			}
		}
		if sec.MusicCarouselShelfRenderer != nil {
			for _, item := range sec.MusicCarouselShelfRenderer.Contents {
				if item.MusicTwoRowItemRenderer != nil {
					if al, ok := twoRowToCollection(item.MusicTwoRowItemRenderer); ok {
						if _, dup := seenAlbums[al.ID]; !dup {
							seenAlbums[al.ID] = struct{}{}
							albums = append(albums, al)
						}
					}
				} else if item.MusicResponsiveListItemRenderer != nil {
					if tr, ok := listItemToTrack(item.MusicResponsiveListItemRenderer, nil); ok {
						if _, dup := seenTracks[tr.ID]; !dup {
							seenTracks[tr.ID] = struct{}{}
							tracks = append(tracks, tr)
						}
					}
				}
			}
		}
	}

	return catalog.CollectionDetail{
		Collection: updated,
		Tracks:     tracks,
		Albums:     albums,
	}
}

func cardToCollection(card *musicCardShelfRenderer, tab catalog.SearchTab) (catalog.Collection, bool) {
	if card == nil || card.OnTap == nil || card.OnTap.BrowseEndpoint == nil {
		return catalog.Collection{}, false
	}
	browseID := strings.TrimSpace(card.OnTap.BrowseEndpoint.BrowseID)
	if browseID == "" {
		return catalog.Collection{}, false
	}
	pageType := ""
	if card.OnTap.BrowseEndpoint.BrowseEndpointContextSupportedConfigs != nil &&
		card.OnTap.BrowseEndpoint.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig != nil {
		pageType = card.OnTap.BrowseEndpoint.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig.PageType
	}
	kind, ok := collectionKindFor(tab, pageType, browseID)
	if !ok {
		return catalog.Collection{}, false
	}
	title := joinRunsText(card.Title)
	if title == "" {
		title = browseID
	}
	return catalog.Collection{
		ID:           browseID,
		Kind:         kind,
		Title:        title,
		Subtitle:     joinRunsText(card.Subtitle),
		ThumbnailURL: pickBestThumbnailURL(extractThumbnails(card.Thumbnail)),
	}, true
}

func listItemToCollection(item *responsiveListItem, tab catalog.SearchTab) (catalog.Collection, bool) {
	if item == nil {
		return catalog.Collection{}, false
	}
	browseID, pageType := extractListItemBrowse(item)
	if browseID == "" {
		return catalog.Collection{}, false
	}
	kind, ok := collectionKindFor(tab, pageType, browseID)
	if !ok {
		return catalog.Collection{}, false
	}
	var title string
	if len(item.FlexColumns) > 0 && item.FlexColumns[0].MusicResponsiveListItemFlexColumnRenderer != nil {
		title = joinRunsText(item.FlexColumns[0].MusicResponsiveListItemFlexColumnRenderer.Text)
	}
	if title == "" {
		title = browseID
	}
	var subParts []string
	for i := 1; i < len(item.FlexColumns); i++ {
		col := item.FlexColumns[i].MusicResponsiveListItemFlexColumnRenderer
		if col == nil {
			continue
		}
		if s := joinRunsText(col.Text); s != "" {
			subParts = append(subParts, s)
		}
	}
	return catalog.Collection{
		ID:           browseID,
		Kind:         kind,
		Title:        title,
		Subtitle:     strings.Join(subParts, " • "),
		ThumbnailURL: pickBestThumbnailURL(extractThumbnails(item.Thumbnail)),
	}, true
}

func twoRowToCollection(item *twoRowItem) (catalog.Collection, bool) {
	if item == nil || item.NavigationEndpoint == nil || item.NavigationEndpoint.BrowseEndpoint == nil {
		return catalog.Collection{}, false
	}
	be := item.NavigationEndpoint.BrowseEndpoint
	browseID := strings.TrimSpace(be.BrowseID)
	if browseID == "" {
		return catalog.Collection{}, false
	}
	pageType := ""
	if be.BrowseEndpointContextSupportedConfigs != nil &&
		be.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig != nil {
		pageType = be.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig.PageType
	}
	kind, ok := collectionKindFor("", pageType, browseID)
	if !ok {
		return catalog.Collection{}, false
	}
	title := joinRunsText(item.Title)
	if title == "" {
		title = browseID
	}
	return catalog.Collection{
		ID:           browseID,
		Kind:         kind,
		Title:        title,
		Subtitle:     joinRunsText(item.Subtitle),
		ThumbnailURL: pickBestThumbnailURL(extractThumbnails(item.ThumbnailRenderer)),
	}, true
}

func extractListItemBrowse(item *responsiveListItem) (browseID, pageType string) {
	if item.NavigationEndpoint != nil && item.NavigationEndpoint.BrowseEndpoint != nil {
		be := item.NavigationEndpoint.BrowseEndpoint
		if be.BrowseEndpointContextSupportedConfigs != nil &&
			be.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig != nil {
			pageType = be.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig.PageType
		}
		return strings.TrimSpace(be.BrowseID), pageType
	}
	for _, col := range item.FlexColumns {
		if col.MusicResponsiveListItemFlexColumnRenderer == nil || col.MusicResponsiveListItemFlexColumnRenderer.Text == nil {
			continue
		}
		for _, r := range col.MusicResponsiveListItemFlexColumnRenderer.Text.Runs {
			pt, bid := runBrowseTarget(r)
			if bid != "" {
				return strings.TrimSpace(bid), pt
			}
		}
	}
	return "", ""
}

func collectionKindFor(tab catalog.SearchTab, pageType, browseID string) (catalog.CollectionKind, bool) {
	switch pageType {
	case "MUSIC_PAGE_TYPE_PLAYLIST":
		return catalog.CollectionPlaylist, true
	case "MUSIC_PAGE_TYPE_ALBUM":
		return catalog.CollectionAlbum, true
	case "MUSIC_PAGE_TYPE_ARTIST", "MUSIC_PAGE_TYPE_USER_CHANNEL":
		return catalog.CollectionArtist, true
	}
	switch {
	case strings.HasPrefix(browseID, "MPREb_"):
		return catalog.CollectionAlbum, true
	case strings.HasPrefix(browseID, "UC"):
		return catalog.CollectionArtist, true
	case strings.HasPrefix(browseID, "VL"), strings.HasPrefix(browseID, "PL"), strings.HasPrefix(browseID, "RD"):
		return catalog.CollectionPlaylist, true
	}
	switch tab {
	case catalog.TabPlaylists:
		return catalog.CollectionPlaylist, true
	case catalog.TabAlbums:
		return catalog.CollectionAlbum, true
	case catalog.TabArtists:
		return catalog.CollectionArtist, true
	}
	return "", false
}

func cardToTrack(card *musicCardShelfRenderer) (model.Track, bool) {
	if card == nil {
		return model.Track{}, false
	}
	var id string
	if card.OnTap != nil && card.OnTap.WatchEndpoint != nil {
		id = model.VideoID(card.OnTap.WatchEndpoint.VideoID)
	}
	if id == "" {
		id = watchVideoIDFromRuns(card.Title)
	}
	if id == "" && card.ThumbnailOverlay != nil {
		id = watchVideoIDFromOverlay(card.ThumbnailOverlay)
	}
	if id == "" {
		return model.Track{}, false
	}

	title := joinRunsText(card.Title)
	if title == "" {
		title = id
	}
	var meta parsedMeta
	if card.Subtitle != nil {
		meta = parseRunsMetadata(card.Subtitle.Runs, true)
	}

	return model.Track{
		ID:           id,
		Title:        title,
		Artist:       meta.artist,
		Album:        meta.album,
		Duration:     meta.duration,
		ThumbnailURL: pickThumbnail(extractThumbnails(card.Thumbnail), nil, id),
	}, true
}

func listItemToTrack(item *responsiveListItem, fallbackThumbs []thumbnailItem) (model.Track, bool) {
	if item == nil {
		return model.Track{}, false
	}

	id := extractListItemVideoID(item)
	if id == "" {
		return model.Track{}, false
	}

	var title string
	if len(item.FlexColumns) > 0 && item.FlexColumns[0].MusicResponsiveListItemFlexColumnRenderer != nil {
		title = joinRunsText(item.FlexColumns[0].MusicResponsiveListItemFlexColumnRenderer.Text)
	}
	if title == "" {
		title = id
	}

	var meta parsedMeta
	for _, col := range item.FixedColumns {
		if col.MusicResponsiveListItemFixedColumnRenderer == nil {
			continue
		}
		if d, ok := parseDurationText(joinRunsText(col.MusicResponsiveListItemFixedColumnRenderer.Text)); ok {
			meta.duration = d
			break
		}
	}

	for i := 1; i < len(item.FlexColumns); i++ {
		col := item.FlexColumns[i].MusicResponsiveListItemFlexColumnRenderer
		if col == nil || col.Text == nil {
			continue
		}
		colMeta := parseRunsMetadata(col.Text.Runs, true)
		if meta.artist == "" && colMeta.artist != "" {
			meta.artist = colMeta.artist
		}
		if meta.album == "" && colMeta.album != "" {
			meta.album = colMeta.album
		}
		if meta.duration == 0 && colMeta.duration > 0 {
			meta.duration = colMeta.duration
		}
	}

	return model.Track{
		ID:           id,
		Title:        title,
		Artist:       meta.artist,
		Album:        meta.album,
		Duration:     meta.duration,
		ThumbnailURL: pickThumbnail(extractThumbnails(item.Thumbnail), fallbackThumbs, id),
	}, true
}

func panelVideoToTrack(v *playlistPanelVideo) (model.Track, bool) {
	if v == nil {
		return model.Track{}, false
	}
	id := model.VideoID(v.VideoID)
	if id == "" && v.NavigationEndpoint != nil && v.NavigationEndpoint.WatchEndpoint != nil {
		id = model.VideoID(v.NavigationEndpoint.WatchEndpoint.VideoID)
	}
	if id == "" {
		return model.Track{}, false
	}

	title := joinRunsText(v.Title)
	if title == "" {
		title = id
	}

	var meta parsedMeta
	if v.LongBylineText != nil {
		meta = parseRunsMetadata(v.LongBylineText.Runs, true)
	}
	if shortArtist := joinRunsText(v.ShortBylineText); shortArtist != "" {
		meta.artist = shortArtist
	}
	if d, ok := parseDurationText(joinRunsText(v.LengthText)); ok {
		meta.duration = d
	}

	var thumbs []thumbnailItem
	if v.Thumbnail != nil {
		thumbs = v.Thumbnail.Thumbnails
	}

	return model.Track{
		ID:           id,
		Title:        title,
		Artist:       meta.artist,
		Album:        meta.album,
		Duration:     meta.duration,
		ThumbnailURL: pickThumbnail(thumbs, nil, id),
	}, true
}

func extractListItemVideoID(item *responsiveListItem) string {
	if item.PlaylistItemData != nil {
		if id := model.VideoID(item.PlaylistItemData.VideoID); id != "" {
			return id
		}
	}
	if id := watchVideoIDFromOverlay(item.Overlay); id != "" {
		return id
	}
	if item.NavigationEndpoint != nil && item.NavigationEndpoint.WatchEndpoint != nil {
		if id := model.VideoID(item.NavigationEndpoint.WatchEndpoint.VideoID); id != "" {
			return id
		}
	}
	for _, col := range item.FlexColumns {
		if col.MusicResponsiveListItemFlexColumnRenderer == nil {
			continue
		}
		if id := watchVideoIDFromRuns(col.MusicResponsiveListItemFlexColumnRenderer.Text); id != "" {
			return id
		}
	}
	return ""
}

func watchVideoIDFromOverlay(o *overlayHolder) string {
	if o == nil || o.MusicItemThumbnailOverlayRenderer == nil ||
		o.MusicItemThumbnailOverlayRenderer.Content == nil ||
		o.MusicItemThumbnailOverlayRenderer.Content.MusicPlayButtonRenderer == nil {
		return ""
	}
	ep := o.MusicItemThumbnailOverlayRenderer.Content.MusicPlayButtonRenderer.PlayNavigationEndpoint
	if ep != nil && ep.WatchEndpoint != nil {
		return model.VideoID(ep.WatchEndpoint.VideoID)
	}
	return ""
}

func watchVideoIDFromRuns(tr *textRuns) string {
	if tr == nil {
		return ""
	}
	for _, r := range tr.Runs {
		if r.NavigationEndpoint != nil && r.NavigationEndpoint.WatchEndpoint != nil {
			if id := model.VideoID(r.NavigationEndpoint.WatchEndpoint.VideoID); id != "" {
				return id
			}
		}
	}
	return ""
}

type parsedMeta struct {
	artist   string
	album    string
	duration time.Duration
}

type runSegment struct {
	text       string
	hasArtist  bool
	hasAlbum   bool
	isFallback bool
}

func parseRunsMetadata(runs []textRun, allowFallback bool) parsedMeta {
	segments := splitRunSegments(runs)
	var meta parsedMeta
	var fallbackCandidates []string

	for _, seg := range segments {
		if seg.text == "" {
			continue
		}
		if d, ok := parseDurationText(seg.text); ok {
			if meta.duration == 0 {
				meta.duration = d
			}
			continue
		}
		if seg.hasArtist {
			if meta.artist == "" {
				meta.artist = seg.text
			}
			continue
		}
		if seg.hasAlbum {
			if meta.album == "" {
				meta.album = seg.text
			}
			continue
		}
		if allowFallback && seg.isFallback && !isTypeLabel(seg.text) && !isMetricSegment(seg.text) {
			fallbackCandidates = append(fallbackCandidates, seg.text)
		}
	}

	if allowFallback {
		for _, cand := range fallbackCandidates {
			if meta.artist == "" {
				meta.artist = cand
				continue
			}
			if meta.album == "" && !isYearSegment(cand) {
				meta.album = cand
			}
		}
	}
	return meta
}

func splitRunSegments(runs []textRun) []runSegment {
	var segments []runSegment
	var cur strings.Builder
	var hasArtist, hasAlbum, hasBrowse bool

	flush := func() {
		text := strings.TrimSpace(cur.String())
		if text != "" {
			segments = append(segments, runSegment{
				text:       text,
				hasArtist:  hasArtist,
				hasAlbum:   hasAlbum,
				isFallback: !hasBrowse,
			})
		}
		cur.Reset()
		hasArtist, hasAlbum, hasBrowse = false, false, false
	}

	for _, r := range runs {
		pageType, browseID := runBrowseTarget(r)
		if pageType != "" || browseID != "" {
			hasBrowse = true
		}
		switch {
		case pageType == "MUSIC_PAGE_TYPE_ARTIST" || pageType == "MUSIC_PAGE_TYPE_USER_CHANNEL" || strings.HasPrefix(browseID, "UC"):
			hasArtist = true
		case pageType == "MUSIC_PAGE_TYPE_ALBUM" || strings.HasPrefix(browseID, "MPREb_"):
			hasAlbum = true
		}

		parts := splitSeparators(r.Text)
		for i, part := range parts {
			if i > 0 {
				flush()
			}
			cur.WriteString(part)
		}
	}
	flush()
	return segments
}

func splitSeparators(s string) []string {
	// Split on InnerTube's middle-dot / bullet separators (" • " or " · ").
	if !strings.ContainsAny(s, "•·") {
		return []string{s}
	}
	var out []string
	start := 0
	for i, r := range s {
		if r == '•' || r == '·' {
			out = append(out, s[start:i])
			start = i + len(string(r))
		}
	}
	out = append(out, s[start:])
	return out
}

func runBrowseTarget(r textRun) (pageType, browseID string) {
	if r.NavigationEndpoint == nil || r.NavigationEndpoint.BrowseEndpoint == nil {
		return "", ""
	}
	be := r.NavigationEndpoint.BrowseEndpoint
	browseID = be.BrowseID
	if be.BrowseEndpointContextSupportedConfigs != nil &&
		be.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig != nil {
		pageType = be.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig.PageType
	}
	return pageType, browseID
}

func isTypeLabel(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "song", "video", "single", "ep", "album", "playlist", "episode", "podcast", "live":
		return true
	default:
		return false
	}
}

func isMetricSegment(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, suffix := range []string{" plays", " play", " views", " view", " likes", " like", " subscribers"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func isYearSegment(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s[0] == '1' || s[0] == '2'
}

func parseDurationText(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, false
	}
	vals := make([]int, len(parts))
	for i, p := range parts {
		if p == "" {
			return 0, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, false
		}
		if i > 0 && n >= 60 {
			return 0, false
		}
		vals[i] = n
	}

	var totalSec int
	if len(vals) == 2 {
		totalSec = vals[0]*60 + vals[1]
	} else {
		totalSec = vals[0]*3600 + vals[1]*60 + vals[2]
	}
	return time.Duration(totalSec) * time.Second, true
}

func joinRunsText(tr *textRuns) string {
	if tr == nil || len(tr.Runs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range tr.Runs {
		b.WriteString(r.Text)
	}
	return strings.TrimSpace(b.String())
}

func extractThumbnails(h *thumbnailHolder) []thumbnailItem {
	if h == nil {
		return nil
	}
	if h.MusicThumbnailRenderer != nil && h.MusicThumbnailRenderer.Thumbnail != nil {
		return h.MusicThumbnailRenderer.Thumbnail.Thumbnails
	}
	if h.CroppedSquareThumbnailRenderer != nil && h.CroppedSquareThumbnailRenderer.Thumbnail != nil {
		return h.CroppedSquareThumbnailRenderer.Thumbnail.Thumbnails
	}
	return nil
}

func pickThumbnail(thumbs, fallbackThumbs []thumbnailItem, id string) string {
	if u := pickBestThumbnailURL(thumbs); u != "" {
		return u
	}
	if u := pickBestThumbnailURL(fallbackThumbs); u != "" {
		return u
	}
	return model.DefaultThumbnailURL(id)
}

func pickBestThumbnailURL(thumbs []thumbnailItem) string {
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
	return lastHTTP
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
