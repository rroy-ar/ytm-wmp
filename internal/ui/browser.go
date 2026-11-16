package ui

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"ytm-wmp/internal/artwork"
	"ytm-wmp/internal/catalog"
	"ytm-wmp/internal/model"
	"ytm-wmp/internal/playback"
)

const (
	maxArtTextureCache  = 64
	maxBrowserWorkers   = 4
	enrichRetryCooldown = 30 * time.Second
)

type browserTab int

const (
	browserTabSongs browserTab = iota
	browserTabPlaylists
	browserTabAlbums
	browserTabArtists
	browserTabQueue
)

func (t browserTab) catalogTab() catalog.SearchTab {
	switch t {
	case browserTabPlaylists:
		return catalog.TabPlaylists
	case browserTabAlbums:
		return catalog.TabAlbums
	case browserTabArtists:
		return catalog.TabArtists
	default:
		return catalog.TabSongs
	}
}

type detailFrame struct {
	detail     catalog.CollectionDetail
	showAlbums bool
	selected   int
	scroll     int
}

var (
	colPanelBg       = color.RGBA{R: 0x0c, G: 0x13, B: 0x1b, A: 0xff}
	colHeaderBg      = color.RGBA{R: 0x15, G: 0x22, B: 0x30, A: 0xff}
	colSurfaceBg     = color.RGBA{R: 0x10, G: 0x1a, B: 0x24, A: 0xff}
	colRowAltBg      = color.RGBA{R: 0x0e, G: 0x17, B: 0x20, A: 0xff}
	colRowSelected   = color.RGBA{R: 0x1b, G: 0x3c, B: 0x59, A: 0xff}
	colRowPlaying    = color.RGBA{R: 0x16, G: 0x33, B: 0x29, A: 0xff}
	colButtonBg      = color.RGBA{R: 0x1a, G: 0x2a, B: 0x3a, A: 0xff}
	colButtonActive  = color.RGBA{R: 0x23, G: 0x45, B: 0x60, A: 0xff}
	colButtonMuted   = color.RGBA{R: 0x12, G: 0x1b, B: 0x24, A: 0xff}
	colBorder        = color.RGBA{R: 0x2d, G: 0x45, B: 0x5c, A: 0xff}
	colFocusBorder   = color.RGBA{R: 0x38, G: 0xc0, B: 0x70, A: 0xff}
	colPreeditAccent = color.RGBA{R: 0xe6, G: 0xb8, B: 0x4c, A: 0xff}
	colModalBg       = color.RGBA{R: 0x06, G: 0x0a, B: 0x10, A: 0xe8}
)

type artTextures struct {
	list          *ebiten.Image
	nowPlaying    *ebiten.Image
	nowPlayingSrc image.Image
}

type artFetchResult struct {
	videoID string
	url     string
	bundle  artwork.Bundle
	err     error
}

type enrichFetchResult struct {
	videoID string
	track   model.Track
	err     error
}

type browserPanel struct {
	searcher          *catalog.Searcher
	clipboard         ClipboardFunc
	searchDebounce    time.Duration
	hasSearchDebounce bool

	searchField     textField
	searchQuery     string
	searchFocused   bool
	searchStatus    string
	imeHandled      bool
	imeCommitted    bool
	activeTab       browserTab
	lastSearchedTab browserTab

	results        []model.Track
	collections    []catalog.Collection
	selectedResult int
	scrollOffset   int

	detailStack []detailFrame

	queueSelected int
	queueScroll   int

	showArtModal bool

	artPlaceholder *ebiten.Image
	artResults     chan artFetchResult
	enrichResults  chan enrichFetchResult
	artTex         map[string]artTextures
	artOrder       []string
	artPending     map[string]bool
	artFailed      map[string]string
	knownThumbURL  map[string]string
	enriched       map[string]bool
	enrichPending  map[string]bool
	enrichFailed   map[string]time.Time
	sem            chan struct{}
}

func (g *Game) initBrowser() {
	g.searchStatus = "Type query or paste URL"
	if g.clipboard == nil {
		g.clipboard = readSystemClipboard
	}
	g.searchField.SetText(g.searchQuery)
	g.artResults = make(chan artFetchResult, 32)
	g.enrichResults = make(chan enrichFetchResult, 32)
	g.sem = make(chan struct{}, maxBrowserWorkers)
	g.artTex = make(map[string]artTextures)
	g.artPending = make(map[string]bool)
	g.artFailed = make(map[string]string)
	g.knownThumbURL = make(map[string]string)
	g.enriched = make(map[string]bool)
	g.enrichPending = make(map[string]bool)
	g.enrichFailed = make(map[string]time.Time)

	if g.cat != nil {
		debounce := catalog.DefaultDebounce
		if g.hasSearchDebounce {
			debounce = g.searchDebounce
		}
		searchOpts := []catalog.SearcherOption{catalog.WithDebounce(debounce)}
		if g.ytCat != nil {
			searchOpts = append(searchOpts, catalog.WithYouTubeCatalog(g.ytCat))
		}
		g.searcher = catalog.NewSearcher(g.cat, searchOpts...)
		if strings.TrimSpace(g.searchQuery) != "" {
			g.searchStatus = "Searching..."
			g.lastSearchedTab = g.activeTab
			g.searcher.SubmitForTab(g.searchQuery, g.activeTab.catalogTab())
		}
	}
}

func (g *Game) supportsBrowserTab(tab browserTab) bool {
	if tab == browserTabSongs || tab == browserTabQueue {
		return true
	}
	return catalog.SupportsTab(g.cat, tab.catalogTab())
}

func (g *Game) syncSearchFieldFromQuery() {
	if g.searchField.Text() != g.searchQuery {
		g.searchField.SetText(g.searchQuery)
	}
}

func (g *Game) updateSearchIME() {
	g.syncSearchFieldFromQuery()
	if !g.imeEnabled {
		g.imeHandled = false
		g.imeCommitted = false
		return
	}
	active := g.searchFocused && g.browserMode && !g.hidePL && !g.showSettings && len(g.detailStack) == 0 && g.activeTab != browserTabQueue
	dy := g.browserDeltaY()
	bounds := offsetRect(SearchInputBounds, dy)
	handled, committed, changed, ended := g.searchField.UpdateIME(active, bounds)
	g.imeHandled = handled
	g.imeCommitted = committed
	if changed {
		g.searchQuery = g.searchField.Text()
		g.onSearchTextChanged()
	}
	if ended {
		g.blurSearch()
	}
}

func (g *Game) updateBrowser() {
	if g.artPlaceholder == nil {
		g.artPlaceholder = ebiten.NewImageFromImage(artwork.Placeholder(artwork.ListSize))
	}

	g.drainSearchResults()
	g.drainEnrichments()

	if g.snap.Track.ID != "" {
		g.maybeEnrichTrack(g.snap.Track)
		g.requestArtwork(g.snap.Track)
		if g.showArtModal {
			g.ensureNowPlayingTexture(g.snap.Track)
		}
	}
	g.enrichQueue()

	g.drainArtwork()

	_, wheelY := ebiten.Wheel()
	if wheelY != 0 && g.browserMode && !g.hidePL && !g.showSettings {
		cx, cy := ebiten.CursorPosition()
		if image.Pt(cx, cy).In(offsetRect(ResultListBounds, g.browserDeltaY())) {
			if wheelY > 0 {
				g.scrollResults(-1)
			} else {
				g.scrollResults(1)
			}
		}
	}
}

func (g *Game) drainSearchResults() {
	if g.searcher == nil {
		return
	}
	ch := g.searcher.Results()
	for {
		select {
		case res := <-ch:
			if res.Err != nil {
				g.searchStatus = fmt.Sprintf("Err: %v", res.Err)
				continue
			}
			if res.Detail != nil {
				g.detailStack = append(g.detailStack, detailFrame{detail: *res.Detail})
				col := res.Detail.Collection
				if col.Kind == catalog.CollectionArtist {
					g.searchStatus = fmt.Sprintf("%s (%d trk, %d alb)", truncateRunes(col.Title, 14), len(res.Detail.Tracks), len(res.Detail.Albums))
				} else {
					g.searchStatus = fmt.Sprintf("%s: %d tracks", truncateRunes(col.Title, 14), len(res.Detail.Tracks))
				}
				g.requestVisibleArtwork()
				continue
			}
			if res.Query == "" {
				g.results = nil
				g.collections = nil
				g.selectedResult = 0
				g.scrollOffset = 0
				g.searchStatus = "Type query or paste URL"
				continue
			}
			g.detailStack = nil
			if res.IsPlaylist || res.Tab == "" || res.Tab == catalog.TabSongs {
				g.results = append([]model.Track(nil), res.Tracks...)
				g.collections = nil
				g.selectedResult = 0
				g.scrollOffset = 0
				switch {
				case res.IsPlaylist:
					g.searchStatus = fmt.Sprintf("Playlist: %d tracks", len(res.Tracks))
				case res.IsYouTube:
					g.searchStatus = fmt.Sprintf("YT: %d results", len(res.Tracks))
				default:
					g.searchStatus = fmt.Sprintf("%d results", len(res.Tracks))
				}
			} else {
				g.collections = append([]catalog.Collection(nil), res.Collections...)
				g.results = nil
				g.selectedResult = 0
				g.scrollOffset = 0
				g.searchStatus = fmt.Sprintf("%d %s", len(res.Collections), res.Tab)
			}
			g.requestVisibleArtwork()
		default:
			return
		}
	}
}

func (g *Game) drainEnrichments() {
	for {
		select {
		case res := <-g.enrichResults:
			delete(g.enrichPending, res.videoID)
			if res.err != nil {
				g.enrichFailed[res.videoID] = time.Now()
				continue
			}
			delete(g.enrichFailed, res.videoID)
			g.enriched[res.videoID] = true
			g.requestArtwork(res.track)
		default:
			return
		}
	}
}

func (g *Game) drainArtwork() {
	for {
		select {
		case res := <-g.artResults:
			delete(g.artPending, res.videoID)
			if res.err != nil {
				g.artFailed[res.videoID] = res.url
				slog.Debug("artwork fetch failed", "video_id", res.videoID, "err", res.err)
				if known := g.knownThumbURL[res.videoID]; known != "" && known != res.url {
					g.requestArtwork(model.Track{ID: res.videoID, ThumbnailURL: known})
				}
				continue
			}
			delete(g.artFailed, res.videoID)
			g.storeArtTextures(res.videoID, res.bundle)
		default:
			return
		}
	}
}

func (g *Game) storeArtTextures(videoID string, b artwork.Bundle) {
	if b.List == nil && b.NowPlaying == nil {
		return
	}
	var tex artTextures
	if b.List != nil {
		tex.list = ebiten.NewImageFromImage(b.List)
	}
	tex.nowPlayingSrc = b.NowPlaying
	if g.showArtModal && videoID == model.NormalizeTrackID(g.snap.Track.ID) && tex.nowPlayingSrc != nil {
		tex.nowPlaying = ebiten.NewImageFromImage(tex.nowPlayingSrc)
		tex.nowPlayingSrc = nil
	}

	if old, exists := g.artTex[videoID]; exists {
		deallocateArtTextures(old)
	} else {
		g.artOrder = append(g.artOrder, videoID)
		for len(g.artOrder) > maxArtTextureCache {
			oldest := g.artOrder[0]
			g.artOrder = g.artOrder[1:]
			if evicted, ok := g.artTex[oldest]; ok {
				deallocateArtTextures(evicted)
				delete(g.artTex, oldest)
			}
		}
	}
	g.artTex[videoID] = tex
}

func (g *Game) ensureNowPlayingTexture(tr model.Track) {
	vid := model.NormalizeTrackID(tr.ID)
	if vid == "" {
		return
	}
	tex, ok := g.artTex[vid]
	if ok {
		if tex.nowPlaying != nil {
			return
		}
		if tex.nowPlayingSrc != nil {
			tex.nowPlaying = ebiten.NewImageFromImage(tex.nowPlayingSrc)
			tex.nowPlayingSrc = nil
			g.artTex[vid] = tex
			return
		}
	}
	if g.art == nil {
		return
	}
	if b, memOK := g.art.LookupMemory(vid); memOK {
		if !ok {
			g.storeArtTextures(vid, b)
			tex, ok = g.artTex[vid]
		}
		if ok && tex.nowPlaying == nil && b.NowPlaying != nil {
			tex.nowPlaying = ebiten.NewImageFromImage(b.NowPlaying)
			tex.nowPlayingSrc = nil
			g.artTex[vid] = tex
		}
	}
}

func deallocateArtTextures(tex artTextures) {
	if tex.list != nil {
		tex.list.Deallocate()
	}
	if tex.nowPlaying != nil {
		tex.nowPlaying.Deallocate()
	}
}

func (g *Game) enrichQueue() {
	if g.cat == nil || g.ctrl == nil || len(g.snap.Queue) == 0 {
		return
	}
	maxInFlight := cap(g.sem)
	if maxInFlight <= 0 {
		maxInFlight = maxBrowserWorkers
	}
	for _, tr := range g.snap.Queue {
		if len(g.enrichPending) >= maxInFlight {
			return
		}
		g.maybeEnrichTrack(tr)
	}
}

func (g *Game) maybeEnrichTrack(tr model.Track) {
	if g.cat == nil || g.ctrl == nil {
		return
	}
	vid := model.NormalizeTrackID(tr.ID)
	if vid == "" {
		return
	}
	if tr.Title != "" && tr.Duration > 0 && tr.ThumbnailURL != "" {
		return
	}
	if g.enriched[vid] || g.enrichPending[vid] {
		return
	}
	if failedAt, failed := g.enrichFailed[vid]; failed && time.Since(failedAt) < enrichRetryCooldown {
		return
	}
	g.enrichPending[vid] = true

	g.wg.Add(1)
	go func(id string) {
		defer g.wg.Done()
		if g.sem != nil {
			select {
			case g.sem <- struct{}{}:
				defer func() { <-g.sem }()
			case <-g.ctx.Done():
				return
			}
		}
		meta, err := g.cat.Track(g.ctx, id)
		if err != nil {
			slog.Debug("track metadata lookup failed", "track_id", id, "err", err)
		} else {
			if meta.ID == "" {
				meta.ID = id
			}
			g.ctrl.Send(playback.UpdateTrack{Track: meta})
		}
		select {
		case g.enrichResults <- enrichFetchResult{videoID: id, track: meta, err: err}:
		case <-g.ctx.Done():
		}
	}(vid)
}

func (g *Game) requestVisibleArtwork() {
	if g.art == nil {
		return
	}
	if len(g.detailStack) > 0 {
		cur := &g.detailStack[len(g.detailStack)-1]
		if cur.showAlbums {
			end := cur.scroll + VisibleResultRows + 2
			if end > len(cur.detail.Albums) {
				end = len(cur.detail.Albums)
			}
			for i := cur.scroll; i < end; i++ {
				al := cur.detail.Albums[i]
				g.requestArtwork(model.Track{ID: al.ID, ThumbnailURL: al.ThumbnailURL})
			}
			return
		}
		end := cur.scroll + VisibleResultRows + 2
		if end > len(cur.detail.Tracks) {
			end = len(cur.detail.Tracks)
		}
		for i := cur.scroll; i < end; i++ {
			g.requestArtwork(cur.detail.Tracks[i])
		}
		return
	}

	if g.activeTab == browserTabQueue {
		end := g.queueScroll + VisibleResultRows + 2
		if end > len(g.snap.Queue) {
			end = len(g.snap.Queue)
		}
		for i := g.queueScroll; i < end; i++ {
			g.requestArtwork(g.snap.Queue[i])
		}
		return
	}

	if len(g.collections) > 0 {
		end := g.scrollOffset + VisibleResultRows + 2
		if end > len(g.collections) {
			end = len(g.collections)
		}
		for i := g.scrollOffset; i < end; i++ {
			c := g.collections[i]
			g.requestArtwork(model.Track{ID: c.ID, ThumbnailURL: c.ThumbnailURL})
		}
		return
	}

	if len(g.results) == 0 {
		return
	}
	end := g.scrollOffset + VisibleResultRows + 2
	if end > len(g.results) {
		end = len(g.results)
	}
	for i := g.scrollOffset; i < end; i++ {
		g.requestArtwork(g.results[i])
	}
}

func (g *Game) requestArtwork(tr model.Track) {
	if g.art == nil {
		return
	}
	vid := model.NormalizeTrackID(tr.ID)
	if vid == "" {
		return
	}
	thumbURL := strings.TrimSpace(tr.ThumbnailURL)
	if thumbURL != "" {
		g.knownThumbURL[vid] = thumbURL
	} else {
		thumbURL = g.knownThumbURL[vid]
	}
	if _, ok := g.artTex[vid]; ok {
		return
	}
	if b, ok := g.art.LookupMemory(vid); ok {
		g.artPending[vid] = true
		select {
		case g.artResults <- artFetchResult{videoID: vid, url: thumbURL, bundle: b}:
		default:
			delete(g.artPending, vid)
		}
		return
	}
	if g.artPending[vid] {
		return
	}
	if failedURL, failed := g.artFailed[vid]; failed && failedURL == thumbURL {
		return
	}

	g.artPending[vid] = true
	g.wg.Add(1)
	go func(id, rawURL string) {
		defer g.wg.Done()
		if g.sem != nil {
			select {
			case g.sem <- struct{}{}:
				defer func() { <-g.sem }()
			case <-g.ctx.Done():
				return
			}
		}
		b, err := g.art.Get(g.ctx, id, rawURL)
		select {
		case g.artResults <- artFetchResult{videoID: id, url: rawURL, bundle: b, err: err}:
		case <-g.ctx.Done():
		}
	}(vid, thumbURL)
}

func (g *Game) focusSearch() {
	g.hidePL = false
	g.browserMode = true
	g.showSettings = false
	g.plMenu = plMenuNone
	g.focusedPanel = panelPlaylist
	if g.activeTab == browserTabQueue {
		g.activeTab = browserTabSongs
	}
	g.detailStack = nil
	g.searchFocused = true
	g.showArtModal = false
	g.syncSearchFieldFromQuery()
	g.syncWindowSize()
}

func (g *Game) blurSearch() {
	g.searchField.ConfirmIME()
	g.searchFocused = false
}

func (g *Game) typeSearchText(s string) {
	if s == "" {
		return
	}
	g.syncSearchFieldFromQuery()
	if g.searchField.Insert(s) {
		g.searchQuery = g.searchField.Text()
		g.onSearchTextChanged()
	}
}

func (g *Game) typeSearchRunes(chars []rune) {
	if len(chars) == 0 {
		return
	}
	g.typeSearchText(string(chars))
}

func (g *Game) backspaceSearch() {
	g.syncSearchFieldFromQuery()
	if g.searchField.Backspace() {
		g.searchQuery = g.searchField.Text()
		g.onSearchTextChanged()
	}
}

func (g *Game) deleteSearch() {
	g.syncSearchFieldFromQuery()
	if g.searchField.Delete() {
		g.searchQuery = g.searchField.Text()
		g.onSearchTextChanged()
	}
}

func (g *Game) clearSearch() {
	g.searchField.Clear()
	g.searchQuery = ""
	g.searchFocused = true
	g.onSearchTextChanged()
}

func (g *Game) pasteSearch() {
	g.focusSearch()
	if g.clipboard == nil {
		return
	}
	raw, err := g.clipboard(g.ctx)
	if err != nil {
		if errors.Is(err, errNoClipboardTool) {
			g.searchStatus = "Paste err: install xclip/wl-paste"
		} else {
			g.searchStatus = truncateRunes("Err: "+err.Error(), 28)
		}
		return
	}
	clean := sanitizePasteText(raw)
	if clean == "" {
		if g.searchQuery == "" {
			g.searchStatus = "Clipboard empty"
		}
		return
	}
	g.syncSearchFieldFromQuery()
	if isURLPaste(clean) {
		g.searchField.SetText(clean)
	} else {
		g.searchField.Insert(clean)
	}
	g.searchQuery = g.searchField.Text()
	g.onSearchTextChanged()
}

func isURLPaste(s string) bool {
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func isCtrlOrMetaPressed() bool {
	return ebiten.IsKeyPressed(ebiten.KeyControl) || ebiten.IsKeyPressed(ebiten.KeyMeta)
}

func isShiftPressed() bool {
	return ebiten.IsKeyPressed(ebiten.KeyShift)
}

func isPasteJustPressed() bool {
	return (isCtrlOrMetaPressed() && inpututil.IsKeyJustPressed(ebiten.KeyV)) ||
		(isShiftPressed() && inpututil.IsKeyJustPressed(ebiten.KeyInsert))
}

func isClearLineJustPressed() bool {
	return isCtrlOrMetaPressed() &&
		(inpututil.IsKeyJustPressed(ebiten.KeyU) || inpututil.IsKeyJustPressed(ebiten.KeyBackspace))
}

func (g *Game) submitSearch() {
	g.blurSearch()
	g.syncSearchFieldFromQuery()
	g.markConfigDirty()
	if g.searcher == nil {
		return
	}
	if strings.TrimSpace(g.searchQuery) == "" {
		g.searcher.SetQueryForTab("", g.activeTab.catalogTab())
		g.searchStatus = "Type query or paste URL"
		return
	}
	g.detailStack = nil
	g.lastSearchedTab = g.activeTab
	g.searchStatus = "Searching..."
	g.searcher.SubmitForTab(g.searchQuery, g.activeTab.catalogTab())
}

func (g *Game) onSearchTextChanged() {
	g.markConfigDirty()
	if g.searcher == nil {
		return
	}
	if strings.TrimSpace(g.searchQuery) == "" {
		g.searcher.SetQueryForTab("", g.activeTab.catalogTab())
		g.searchStatus = "Type query or paste URL"
		return
	}
	g.detailStack = nil
	g.lastSearchedTab = g.activeTab
	g.searchStatus = "Typing..."
	g.searcher.SetQueryForTab(g.searchQuery, g.activeTab.catalogTab())
}

func (g *Game) handleSearchKeys() {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		g.blurSearch()
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyKPEnter) {
		g.submitSearch()
		return
	}
	if isPasteJustPressed() {
		g.pasteSearch()
		return
	}
	if isClearLineJustPressed() {
		g.clearSearch()
		return
	}
	g.syncSearchFieldFromQuery()
	if !g.imeHandled {
		if dur := inpututil.KeyPressDuration(ebiten.KeyLeft); dur == 1 || (dur > 20 && dur%3 == 0) {
			g.searchField.MoveLeft()
		}
		if dur := inpututil.KeyPressDuration(ebiten.KeyRight); dur == 1 || (dur > 20 && dur%3 == 0) {
			g.searchField.MoveRight()
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyHome) {
			g.searchField.Home()
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEnd) {
			g.searchField.End()
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
			g.moveResultSelection(-1)
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
			g.moveResultSelection(1)
		}
		ctrlWithoutAlt := ebiten.IsKeyPressed(ebiten.KeyControl) && !ebiten.IsKeyPressed(ebiten.KeyAlt)
		if !ctrlWithoutAlt && !ebiten.IsKeyPressed(ebiten.KeyMeta) && !g.imeCommitted {
			if chars := ebiten.AppendInputChars(nil); len(chars) > 0 {
				g.typeSearchRunes(chars)
			}
		}
		if dur := inpututil.KeyPressDuration(ebiten.KeyBackspace); dur == 1 || (dur > 20 && dur%3 == 0) {
			g.backspaceSearch()
		}
		if dur := inpututil.KeyPressDuration(ebiten.KeyDelete); dur == 1 || (dur > 20 && dur%3 == 0) {
			g.deleteSearch()
		}
	}
}

func (g *Game) switchBrowserTab(tab browserTab) {
	if tab != browserTabSongs && tab != browserTabQueue && !g.supportsBrowserTab(tab) {
		g.searchStatus = "Tab needs InnerTube"
		return
	}
	g.showSettings = false
	g.detailStack = nil
	g.activeTab = tab
	if tab == browserTabQueue {
		g.blurSearch()
		g.clampQueueBrowserScroll()
		g.requestVisibleArtwork()
		return
	}
	if strings.TrimSpace(g.searchQuery) != "" && tab != g.lastSearchedTab && g.searcher != nil {
		g.lastSearchedTab = tab
		g.searchStatus = "Searching..."
		g.searcher.SubmitForTab(g.searchQuery, tab.catalogTab())
	} else {
		g.requestVisibleArtwork()
	}
}

func (g *Game) openCollection(col catalog.Collection) {
	if g.searcher == nil || col.ID == "" {
		return
	}
	g.blurSearch()
	g.searchStatus = "Loading..."
	g.searcher.Browse(col)
}

func (g *Game) popDetail() {
	if len(g.detailStack) == 0 {
		return
	}
	g.detailStack = g.detailStack[:len(g.detailStack)-1]
	if len(g.detailStack) > 0 {
		cur := g.detailStack[len(g.detailStack)-1]
		g.searchStatus = fmt.Sprintf("%s: %d tracks", truncateRunes(cur.detail.Collection.Title, 14), len(cur.detail.Tracks))
	} else {
		g.searchStatus = fmt.Sprintf("%d %s", len(g.collections), g.activeTab.catalogTab())
	}
	g.requestVisibleArtwork()
}

func (g *Game) onBrowserClick(id string) bool {
	if g.showArtModal {
		if id == WidgetNowPlayingArt {
			g.showArtModal = false
		}
		return true
	}

	switch id {
	case WidgetEject:
		g.focusSearch()
		return true
	case WidgetBrowserSettings:
		g.showSettings = !g.showSettings
		g.blurSearch()
		return true
	case WidgetBrowserModePL:
		g.showSettings = false
		g.browserMode = false
		g.blurSearch()
		g.markConfigDirty()
		return true
	}

	if g.showSettings {
		return g.onSettingsClick(id)
	}

	switch id {
	case WidgetBrowserTabSongs:
		g.switchBrowserTab(browserTabSongs)
		return true
	case WidgetBrowserTabPlaylist:
		g.switchBrowserTab(browserTabPlaylists)
		return true
	case WidgetBrowserTabAlbums:
		g.switchBrowserTab(browserTabAlbums)
		return true
	case WidgetBrowserTabArtists:
		g.switchBrowserTab(browserTabArtists)
		return true
	case WidgetBrowserTabQueue:
		g.switchBrowserTab(browserTabQueue)
		return true
	case WidgetDetailBack:
		g.popDetail()
		return true
	case WidgetDetailModeToggle:
		if len(g.detailStack) > 0 {
			cur := &g.detailStack[len(g.detailStack)-1]
			cur.showAlbums = !cur.showAlbums
			cur.selected = 0
			cur.scroll = 0
			g.requestVisibleArtwork()
		}
		return true
	case WidgetQueueMoveUp:
		if g.ctrl != nil && g.queueSelected > 0 && g.queueSelected < len(g.snap.Queue) {
			g.ctrl.Send(playback.MoveTrack{From: g.queueSelected, To: g.queueSelected - 1})
			g.queueSelected--
		}
		return true
	case WidgetQueueMoveDown:
		if g.ctrl != nil && g.queueSelected >= 0 && g.queueSelected+1 < len(g.snap.Queue) {
			g.ctrl.Send(playback.MoveTrack{From: g.queueSelected, To: g.queueSelected + 1})
			g.queueSelected++
		}
		return true
	case WidgetQueueRemove:
		if g.ctrl != nil && g.queueSelected >= 0 && g.queueSelected < len(g.snap.Queue) {
			g.ctrl.Send(playback.RemoveIndices{Indices: []int{g.queueSelected}})
			if g.queueSelected >= len(g.snap.Queue)-1 && g.queueSelected > 0 {
				g.queueSelected--
			}
		}
		return true
	case WidgetQueueClear:
		if g.ctrl != nil {
			g.ctrl.Send(playback.SetQueue{Tracks: nil, Index: -1})
			g.queueSelected = 0
			g.queueScroll = 0
		}
		return true
	case WidgetSearchInput:
		g.focusSearch()
		return true
	case WidgetSearchSubmit:
		g.submitSearch()
		return true
	case WidgetSearchClear:
		g.clearSearch()
		return true
	case WidgetNowPlayingArt:
		g.showArtModal = !g.showArtModal
		if g.showArtModal {
			g.ensureNowPlayingTexture(g.snap.Track)
		}
		return true
	case WidgetBrowserPlay:
		g.onBrowserPrimaryAction()
		return true
	case WidgetBrowserQueue:
		g.onBrowserQueueAction()
		return true
	case WidgetBrowserImportAll:
		g.onBrowserImportAllAction()
		return true
	case WidgetBrowserAutoplay:
		if g.ctrl != nil {
			g.ctrl.Send(playback.SetAutoplay{Enabled: !g.snap.Autoplay})
			g.markConfigDirty()
		}
		return true
	}

	for rowIdx, rowWidget := range resultRowWidgets {
		if id == rowWidget {
			g.onBrowserRowClick(rowIdx)
			return true
		}
	}
	return false
}

func (g *Game) onBrowserRowClick(rowIdx int) {
	if len(g.detailStack) > 0 {
		cur := &g.detailStack[len(g.detailStack)-1]
		idx := cur.scroll + rowIdx
		if cur.showAlbums {
			if idx >= 0 && idx < len(cur.detail.Albums) {
				if cur.selected == idx {
					g.openCollection(cur.detail.Albums[idx])
				} else {
					cur.selected = idx
				}
			}
			return
		}
		if idx >= 0 && idx < len(cur.detail.Tracks) {
			if cur.selected == idx {
				g.playTrack(cur.detail.Tracks[idx])
			} else {
				cur.selected = idx
			}
		}
		return
	}

	if g.activeTab == browserTabQueue {
		idx := g.queueScroll + rowIdx
		if idx >= 0 && idx < len(g.snap.Queue) {
			if g.queueSelected == idx && g.ctrl != nil {
				g.errText = ""
				g.ctrl.Send(playback.PlayIndex{Index: idx})
			} else {
				g.queueSelected = idx
			}
		}
		return
	}

	if len(g.collections) > 0 {
		idx := g.scrollOffset + rowIdx
		if idx >= 0 && idx < len(g.collections) {
			if g.selectedResult == idx {
				g.openCollection(g.collections[idx])
			} else {
				g.selectedResult = idx
			}
		}
		return
	}

	idx := g.scrollOffset + rowIdx
	if idx >= 0 && idx < len(g.results) {
		if g.selectedResult == idx {
			g.playResultIndex(idx)
		} else {
			g.selectedResult = idx
		}
	}
}

func (g *Game) onBrowserPrimaryAction() {
	if len(g.detailStack) > 0 {
		cur := &g.detailStack[len(g.detailStack)-1]
		if cur.showAlbums {
			if cur.selected >= 0 && cur.selected < len(cur.detail.Albums) {
				g.openCollection(cur.detail.Albums[cur.selected])
			}
			return
		}
		if cur.selected >= 0 && cur.selected < len(cur.detail.Tracks) {
			g.playTrack(cur.detail.Tracks[cur.selected])
		} else if len(cur.detail.Tracks) > 0 {
			g.onBrowserImportAllAction()
		}
		return
	}
	if g.activeTab == browserTabQueue {
		if g.ctrl != nil && g.queueSelected >= 0 && g.queueSelected < len(g.snap.Queue) {
			g.errText = ""
			g.ctrl.Send(playback.PlayIndex{Index: g.queueSelected})
		}
		return
	}
	if len(g.collections) > 0 {
		if g.selectedResult >= 0 && g.selectedResult < len(g.collections) {
			g.openCollection(g.collections[g.selectedResult])
		}
		return
	}
	if g.selectedResult >= 0 && g.selectedResult < len(g.results) {
		g.playResultIndex(g.selectedResult)
	}
}

func (g *Game) onBrowserQueueAction() {
	if g.ctrl == nil {
		return
	}
	if len(g.detailStack) > 0 {
		cur := &g.detailStack[len(g.detailStack)-1]
		if !cur.showAlbums && cur.selected >= 0 && cur.selected < len(cur.detail.Tracks) {
			tr := cur.detail.Tracks[cur.selected]
			g.ctrl.Send(playback.Enqueue{Tracks: []model.Track{tr}})
			g.searchStatus = "Queued: " + truncateRunes(trackDisplayTitle(tr), 18)
		}
		return
	}
	if g.activeTab == browserTabQueue {
		if g.queueSelected >= 0 && g.queueSelected < len(g.snap.Queue) {
			g.ctrl.Send(playback.RemoveIndices{Indices: []int{g.queueSelected}})
		}
		return
	}
	if len(g.collections) == 0 && g.selectedResult >= 0 && g.selectedResult < len(g.results) {
		tr := g.results[g.selectedResult]
		g.ctrl.Send(playback.Enqueue{Tracks: []model.Track{tr}})
		g.searchStatus = "Queued: " + truncateRunes(trackDisplayTitle(tr), 18)
	}
}

func (g *Game) onBrowserImportAllAction() {
	if g.ctrl == nil {
		return
	}
	if len(g.detailStack) > 0 {
		cur := &g.detailStack[len(g.detailStack)-1]
		if len(cur.detail.Tracks) > 0 {
			g.ctrl.Send(playback.SetQueue{Tracks: cur.detail.Tracks, Index: 0, AutoPlay: true})
			g.errText = ""
			g.searchStatus = fmt.Sprintf("Playing %d tracks", len(cur.detail.Tracks))
		}
		return
	}
	if len(g.results) > 0 {
		g.ctrl.Send(playback.SetQueue{Tracks: g.results, Index: 0, AutoPlay: true})
		g.errText = ""
		g.searchStatus = fmt.Sprintf("Playing %d tracks", len(g.results))
	}
}

func (g *Game) playTrack(tr model.Track) {
	if g.ctrl == nil {
		return
	}
	g.errText = ""
	g.ctrl.Send(playback.SetQueue{
		Tracks:   []model.Track{tr},
		Index:    0,
		AutoPlay: true,
	})
}

func (g *Game) playResultIndex(idx int) {
	if idx < 0 || idx >= len(g.results) {
		return
	}
	g.playTrack(g.results[idx])
}

func (g *Game) moveResultSelection(delta int) {
	if len(g.detailStack) > 0 {
		cur := &g.detailStack[len(g.detailStack)-1]
		count := len(cur.detail.Tracks)
		if cur.showAlbums {
			count = len(cur.detail.Albums)
		}
		if count == 0 {
			return
		}
		cur.selected, cur.scroll = stepSelection(cur.selected, cur.scroll, count, delta)
		g.requestVisibleArtwork()
		return
	}
	if g.activeTab == browserTabQueue {
		if len(g.snap.Queue) == 0 {
			return
		}
		g.queueSelected, g.queueScroll = stepSelection(g.queueSelected, g.queueScroll, len(g.snap.Queue), delta)
		g.requestVisibleArtwork()
		return
	}
	count := len(g.results)
	if len(g.collections) > 0 {
		count = len(g.collections)
	}
	if count == 0 {
		return
	}
	g.selectedResult, g.scrollOffset = stepSelection(g.selectedResult, g.scrollOffset, count, delta)
	g.requestVisibleArtwork()
}

func stepSelection(selected, scroll, count, delta int) (int, int) {
	next := selected + delta
	if next < 0 {
		next = 0
	} else if next >= count {
		next = count - 1
	}
	if next < scroll {
		scroll = next
	} else if next >= scroll+VisibleResultRows {
		scroll = next - VisibleResultRows + 1
	}
	return next, scroll
}

func (g *Game) scrollResults(delta int) {
	if len(g.detailStack) > 0 {
		cur := &g.detailStack[len(g.detailStack)-1]
		count := len(cur.detail.Tracks)
		if cur.showAlbums {
			count = len(cur.detail.Albums)
		}
		cur.scroll = clampScroll(cur.scroll+delta, count)
		g.requestVisibleArtwork()
		return
	}
	if g.activeTab == browserTabQueue {
		g.queueScroll = clampScroll(g.queueScroll+delta, len(g.snap.Queue))
		g.requestVisibleArtwork()
		return
	}
	count := len(g.results)
	if len(g.collections) > 0 {
		count = len(g.collections)
	}
	g.scrollOffset = clampScroll(g.scrollOffset+delta, count)
	g.requestVisibleArtwork()
}

func (g *Game) clampQueueBrowserScroll() {
	g.queueScroll = clampScroll(g.queueScroll, len(g.snap.Queue))
	if g.queueSelected >= len(g.snap.Queue) && len(g.snap.Queue) > 0 {
		g.queueSelected = len(g.snap.Queue) - 1
	}
}

func clampScroll(offset, count int) int {
	if count <= VisibleResultRows || offset < 0 {
		return 0
	}
	maxOffset := count - VisibleResultRows
	if offset > maxOffset {
		return maxOffset
	}
	return offset
}

func (g *Game) drawBrowser(screen *ebiten.Image) {
	dy := g.browserDeltaY()
	topY := MainWindowHeight + dy
	vector.DrawFilledRect(screen, 0, float32(topY), WindowWidth, BrowserHeight, colPanelBg, false)
	vector.DrawFilledRect(screen, 0, float32(topY), WindowWidth, 12, colHeaderBg, false)
	vector.StrokeRect(screen, 0.5, float32(topY)+0.5, WindowWidth-1, BrowserHeight-1, 1, colBorder, false)

	headerLabel := "CATALOG"
	headerStatus := g.searchStatus
	if g.showSettings {
		headerLabel = "SETTINGS"
		headerStatus = "Config & tools"
	}
	ebitenutil.DebugPrintAt(screen, headerLabel, 6, topY)
	ebitenutil.DebugPrintAt(screen, truncateRunes(headerStatus, 20), 58, topY)
	g.drawToggleButtonRect(screen, offsetRect(BrowserSettingsBounds, dy), WidgetBrowserSettings, "SET", g.showSettings)
	g.drawButtonRect(screen, offsetRect(BrowserModePLBounds, dy), WidgetBrowserModePL, "PL")

	if g.showSettings {
		g.drawSettings(screen, dy)
		return
	}

	snap := g.snap

	// Compact Now-Playing strip.
	artRect := offsetRect(NowPlayingArtBounds, dy)
	vector.DrawFilledRect(screen, 4, float32(129+dy), 267, 24, colSurfaceBg, false)
	g.drawScaledImage(screen, g.artworkListImage(snap.Track), artRect)
	vector.StrokeRect(screen, float32(artRect.Min.X)-0.5, float32(artRect.Min.Y)-0.5, float32(artRect.Dx()+1), float32(artRect.Dy()+1), 1, colBorder, false)

	title := trackDisplayTitle(snap.Track)
	if title == "" {
		title = "No track selected"
	}
	artist := snap.Track.Artist
	if artist == "" {
		artist = snap.State.String()
	}
	infoLine := fmt.Sprintf("%s [%s/%s]", truncateRunes(artist, 22), formatClock(snap.Position), formatClock(snap.Duration))
	ebitenutil.DebugPrintAt(screen, truncateRunes(title, 39), 32, 128+dy)
	ebitenutil.DebugPrintAt(screen, truncateRunes(infoLine, 39), 32, 139+dy)

	// Browser tabs.
	g.drawBrowserTabs(screen, dy)

	// Subheader bar: Detail header, Queue header, or Search input.
	switch {
	case len(g.detailStack) > 0:
		cur := g.detailStack[len(g.detailStack)-1]
		g.drawButtonRect(screen, offsetRect(DetailBackBounds, dy), WidgetDetailBack, "<BACK")
		detailTitle := cur.detail.Collection.Title
		if detailTitle == "" {
			detailTitle = cur.detail.Collection.ID
		}
		maxChars := 34
		if cur.detail.Collection.Kind == catalog.CollectionArtist && len(cur.detail.Albums) > 0 {
			maxChars = 25
			toggleLabel := "ALBUMS"
			if cur.showAlbums {
				toggleLabel = "SONGS"
			}
			g.drawButtonRect(screen, offsetRect(DetailModeBounds, dy), WidgetDetailModeToggle, toggleLabel)
		}
		ebitenutil.DebugPrintAt(screen, truncateRunes(detailTitle, maxChars), 52, 171+dy)
	case g.activeTab == browserTabQueue:
		ebitenutil.DebugPrintAt(screen, fmt.Sprintf("QUEUE (%d tracks)", len(snap.Queue)), 8, 171+dy)
		g.drawButtonRect(screen, offsetRect(QueueMoveUpBounds, dy), WidgetQueueMoveUp, "UP")
		g.drawButtonRect(screen, offsetRect(QueueMoveDownBounds, dy), WidgetQueueMoveDown, "DN")
		g.drawButtonRect(screen, offsetRect(QueueRemoveBounds, dy), WidgetQueueRemove, "REM")
		g.drawButtonRect(screen, offsetRect(QueueClearBounds, dy), WidgetQueueClear, "CLR")
	default:
		g.drawSearchBar(screen, dy)
	}

	// List area.
	listRect := offsetRect(ResultListBounds, dy)
	vector.DrawFilledRect(screen, float32(listRect.Min.X), float32(listRect.Min.Y), float32(listRect.Dx()), float32(listRect.Dy()), colSurfaceBg, false)
	vector.StrokeRect(screen, float32(listRect.Min.X)+0.5, float32(listRect.Min.Y)+0.5, float32(listRect.Dx()-1), float32(listRect.Dy()-1), 1, colBorder, false)

	switch {
	case len(g.detailStack) > 0:
		cur := g.detailStack[len(g.detailStack)-1]
		if cur.showAlbums {
			g.drawCollectionRows(screen, listRect, cur.detail.Albums, cur.scroll, cur.selected, "No albums found")
		} else {
			g.drawTrackRows(screen, listRect, cur.detail.Tracks, cur.scroll, cur.selected, -1, "No tracks in collection")
		}
	case g.activeTab == browserTabQueue:
		g.drawTrackRows(screen, listRect, snap.Queue, g.queueScroll, g.queueSelected, snap.Index, "Queue is empty")
	case len(g.collections) > 0:
		g.drawCollectionRows(screen, listRect, g.collections, g.scrollOffset, g.selectedResult, "No results")
	case len(g.results) == 0:
		if g.activeTab == browserTabSongs {
			ebitenutil.DebugPrintAt(screen, "Press / or click search box", 16, 206+dy)
			ebitenutil.DebugPrintAt(screen, "to search YTM (yt: for YouTube)", 16, 222+dy)
		} else {
			ebitenutil.DebugPrintAt(screen, "Search above to browse "+string(g.activeTab.catalogTab()), 16, 206+dy)
		}
	default:
		g.drawTrackRows(screen, listRect, g.results, g.scrollOffset, g.selectedResult, -1, "")
	}

	// Bottom action bar.
	playLabel := "PLAY"
	if len(g.detailStack) == 0 && len(g.collections) > 0 && g.activeTab != browserTabQueue {
		playLabel = "OPEN"
	}
	g.drawButtonRect(screen, offsetRect(BrowserPlayBounds, dy), WidgetBrowserPlay, playLabel)
	qBtnLabel := "QUEUE"
	if g.activeTab == browserTabQueue && len(g.detailStack) == 0 {
		qBtnLabel = "REM"
	}
	g.drawButtonRect(screen, offsetRect(BrowserQueueBounds, dy), WidgetBrowserQueue, qBtnLabel)
	g.drawButtonRect(screen, offsetRect(BrowserImportAllBounds, dy), WidgetBrowserImportAll, "ALL->Q")
	autoLabel := "AUTO:OF"
	if snap.Autoplay {
		autoLabel = "AUTO:ON"
	}
	g.drawToggleButtonRect(screen, offsetRect(BrowserAutoplayBounds, dy), WidgetBrowserAutoplay, autoLabel, snap.Autoplay)

	queueSummary := fmt.Sprintf("Q:%d/%d", snap.Index+1, len(snap.Queue))
	if len(snap.Queue) == 0 {
		queueSummary = "Q:0/0"
	}
	ebitenutil.DebugPrintAt(screen, queueSummary, 190, 327+dy)

	if g.showArtModal {
		g.drawBrowserArtworkModal(screen, snap, dy)
	}
}

func (g *Game) drawBrowserTabs(screen *ebiten.Image, dy int) {
	tabs := []struct {
		tab    browserTab
		id     string
		bounds image.Rectangle
		label  string
	}{
		{browserTabSongs, WidgetBrowserTabSongs, BrowserTabSongsBounds, "SONGS"},
		{browserTabPlaylists, WidgetBrowserTabPlaylist, BrowserTabPLBounds, "PLAYLST"},
		{browserTabAlbums, WidgetBrowserTabAlbums, BrowserTabAlbumsBounds, "ALBUMS"},
		{browserTabArtists, WidgetBrowserTabArtists, BrowserTabArtistBounds, "ARTISTS"},
		{browserTabQueue, WidgetBrowserTabQueue, BrowserTabQueueBounds, "QUEUE"},
	}
	for _, tb := range tabs {
		r := offsetRect(tb.bounds, dy)
		supported := g.supportsBrowserTab(tb.tab)
		active := g.activeTab == tb.tab && len(g.detailStack) == 0
		if !supported {
			vector.DrawFilledRect(screen, float32(r.Min.X), float32(r.Min.Y), float32(r.Dx()), float32(r.Dy()), colButtonMuted, false)
			vector.StrokeRect(screen, float32(r.Min.X)+0.5, float32(r.Min.Y)+0.5, float32(r.Dx()-1), float32(r.Dy()-1), 1, colBorder, false)
			ebitenutil.DebugPrintAt(screen, tb.label, r.Min.X+4, r.Min.Y-1)
			continue
		}
		g.drawToggleButtonRect(screen, r, tb.id, tb.label, active)
	}
}

func (g *Game) drawSearchBar(screen *ebiten.Image, dy int) {
	g.syncSearchFieldFromQuery()
	inputRect := offsetRect(SearchInputBounds, dy)
	inputBorder := colBorder
	if g.searchFocused {
		inputBorder = colFocusBorder
	}
	vector.DrawFilledRect(screen, float32(inputRect.Min.X), float32(inputRect.Min.Y), float32(inputRect.Dx()), float32(inputRect.Dy()), colSurfaceBg, false)
	vector.StrokeRect(screen, float32(inputRect.Min.X)+0.5, float32(inputRect.Min.Y)+0.5, float32(inputRect.Dx()-1), float32(inputRect.Dy()-1), 1, inputBorder, false)

	if g.searchField.Text() == "" && g.searchField.Preedit() == "" && !g.searchFocused {
		ebitenutil.DebugPrintAt(screen, "Search or playlist URL (/)", inputRect.Min.X+3, inputRect.Min.Y)
	} else {
		vis, caretCol, preStart, preEnd := g.searchField.DisplayWindow(30)
		textX := inputRect.Min.X + 3
		textY := inputRect.Min.Y
		ebitenutil.DebugPrintAt(screen, vis, textX, textY)
		if preStart >= 0 && preEnd > preStart {
			px := float32(textX + preStart*6)
			pw := float32((preEnd - preStart) * 6)
			vector.DrawFilledRect(screen, px, float32(inputRect.Max.Y-3), pw, 1, colPreeditAccent, false)
		}
		if g.searchFocused && (g.tick/20)%2 == 0 {
			cx := float32(textX + caretCol*6)
			if cx > float32(inputRect.Max.X-3) {
				cx = float32(inputRect.Max.X - 3)
			}
			vector.DrawFilledRect(screen, cx, float32(inputRect.Min.Y+2), 1, float32(inputRect.Dy()-4), colFocusBorder, false)
		}
	}

	g.drawButtonRect(screen, offsetRect(SearchSubmitBounds, dy), WidgetSearchSubmit, "GO")
	g.drawButtonRect(screen, offsetRect(SearchClearBounds, dy), WidgetSearchClear, "CLR")
}

func (g *Game) drawTrackRows(screen *ebiten.Image, listRect image.Rectangle, tracks []model.Track, scroll, selected, playingIdx int, emptyMsg string) {
	if len(tracks) == 0 {
		if emptyMsg != "" {
			ebitenutil.DebugPrintAt(screen, emptyMsg, listRect.Min.X+10, listRect.Min.Y+16)
		}
		return
	}
	for row := 0; row < VisibleResultRows; row++ {
		idx := scroll + row
		if idx >= len(tracks) {
			break
		}
		tr := tracks[idx]
		rowY := listRect.Min.Y + row*ResultRowHeight

		rowBg := colSurfaceBg
		if idx == selected {
			rowBg = colRowSelected
		} else if idx == playingIdx {
			rowBg = colRowPlaying
		} else if row%2 == 1 {
			rowBg = colRowAltBg
		}
		vector.DrawFilledRect(screen, float32(listRect.Min.X+1), float32(rowY), float32(listRect.Dx()-2), ResultRowHeight, rowBg, false)

		thumbRect := image.Rect(9, rowY+2, 27, rowY+20)
		g.drawScaledImage(screen, g.artworkListImage(tr), thumbRect)

		title := trackDisplayTitle(tr)
		if idx == playingIdx {
			title = "> " + title
		}
		ebitenutil.DebugPrintAt(screen, truncateRunes(title, 38), 31, rowY-1)
		sub := fmt.Sprintf("%s %s", truncateRunes(tr.Artist, 28), formatClock(tr.Duration))
		ebitenutil.DebugPrintAt(screen, truncateRunes(sub, 38), 31, rowY+9)
	}
}

func (g *Game) drawCollectionRows(screen *ebiten.Image, listRect image.Rectangle, items []catalog.Collection, scroll, selected int, emptyMsg string) {
	if len(items) == 0 {
		if emptyMsg != "" {
			ebitenutil.DebugPrintAt(screen, emptyMsg, listRect.Min.X+10, listRect.Min.Y+16)
		}
		return
	}
	for row := 0; row < VisibleResultRows; row++ {
		idx := scroll + row
		if idx >= len(items) {
			break
		}
		col := items[idx]
		rowY := listRect.Min.Y + row*ResultRowHeight

		rowBg := colSurfaceBg
		if idx == selected {
			rowBg = colRowSelected
		} else if row%2 == 1 {
			rowBg = colRowAltBg
		}
		vector.DrawFilledRect(screen, float32(listRect.Min.X+1), float32(rowY), float32(listRect.Dx()-2), ResultRowHeight, rowBg, false)

		thumbRect := image.Rect(9, rowY+2, 27, rowY+20)
		g.drawScaledImage(screen, g.artworkListImage(model.Track{ID: col.ID, ThumbnailURL: col.ThumbnailURL}), thumbRect)

		badge := "[PL]"
		switch col.Kind {
		case catalog.CollectionAlbum:
			badge = "[AL]"
		case catalog.CollectionArtist:
			badge = "[AR]"
		}
		ebitenutil.DebugPrintAt(screen, truncateRunes(badge+" "+col.Title, 38), 31, rowY-1)
		sub := col.Subtitle
		if sub == "" {
			sub = string(col.Kind)
		}
		ebitenutil.DebugPrintAt(screen, truncateRunes(sub, 38), 31, rowY+9)
	}
}

func (g *Game) drawBrowserArtworkModal(screen *ebiten.Image, snap playback.Snapshot, dy int) {
	vector.DrawFilledRect(screen, 0, float32(MainWindowHeight+dy), WindowWidth, BrowserHeight, colModalBg, false)
	modalRect := offsetRect(ArtModalBounds, dy)
	g.drawScaledImage(screen, g.artworkNowPlayingImage(snap.Track), modalRect)
	vector.StrokeRect(screen, float32(modalRect.Min.X)-0.5, float32(modalRect.Min.Y)-0.5, float32(modalRect.Dx()+1), float32(modalRect.Dy()+1), 1, colFocusBorder, false)
	ebitenutil.DebugPrintAt(screen, truncateRunes(trackDisplayTitle(snap.Track), 38), 16, 328+dy)
}

func (g *Game) drawButtonRect(screen *ebiten.Image, rect image.Rectangle, widgetID, label string) {
	g.drawToggleButtonRect(screen, rect, widgetID, label, false)
}

func (g *Game) drawToggleButtonRect(screen *ebiten.Image, rect image.Rectangle, widgetID, label string, active bool) {
	bg := colButtonBg
	border := colBorder
	if active {
		bg = colButtonActive
		border = colFocusBorder
	}
	if g.pointer.Active() == widgetID && g.pointer.Pressed() {
		bg = colButtonActive
	}
	vector.DrawFilledRect(screen, float32(rect.Min.X), float32(rect.Min.Y), float32(rect.Dx()), float32(rect.Dy()), bg, false)
	vector.StrokeRect(screen, float32(rect.Min.X)+0.5, float32(rect.Min.Y)+0.5, float32(rect.Dx()-1), float32(rect.Dy()-1), 1, border, false)
	ebitenutil.DebugPrintAt(screen, label, rect.Min.X+4, rect.Min.Y)
}

func (g *Game) drawScaledImage(dst, src *ebiten.Image, rect image.Rectangle) {
	if src == nil {
		vector.DrawFilledRect(dst, float32(rect.Min.X), float32(rect.Min.Y), float32(rect.Dx()), float32(rect.Dy()), colHeaderBg, false)
		return
	}
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw <= 0 || sh <= 0 {
		return
	}
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(float64(rect.Dx())/float64(sw), float64(rect.Dy())/float64(sh))
	op.GeoM.Translate(float64(rect.Min.X), float64(rect.Min.Y))
	dst.DrawImage(src, op)
}

func (g *Game) artworkListImage(tr model.Track) *ebiten.Image {
	vid := model.NormalizeTrackID(tr.ID)
	if vid != "" {
		if tex, ok := g.artTex[vid]; ok && tex.list != nil {
			return tex.list
		}
	}
	return g.artPlaceholder
}

func (g *Game) artworkNowPlayingImage(tr model.Track) *ebiten.Image {
	vid := model.NormalizeTrackID(tr.ID)
	if vid != "" {
		if tex, ok := g.artTex[vid]; ok {
			if tex.nowPlaying != nil {
				return tex.nowPlaying
			}
			if tex.list != nil {
				return tex.list
			}
		}
	}
	return g.artPlaceholder
}

func trackDisplayTitle(tr model.Track) string {
	if tr.Title != "" {
		return tr.Title
	}
	return tr.ID
}

func formatClock(d time.Duration) string {
	if d <= 0 {
		return "--:--"
	}
	total := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

func tailRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[len(r)-max:])
}

var _ = utf8.RuneCountInString
