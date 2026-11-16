package ui

import (
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

var (
	colPanelBg      = color.RGBA{R: 0x0c, G: 0x13, B: 0x1b, A: 0xff}
	colHeaderBg     = color.RGBA{R: 0x15, G: 0x22, B: 0x30, A: 0xff}
	colSurfaceBg    = color.RGBA{R: 0x10, G: 0x1a, B: 0x24, A: 0xff}
	colRowAltBg     = color.RGBA{R: 0x0e, G: 0x17, B: 0x20, A: 0xff}
	colRowSelected  = color.RGBA{R: 0x1b, G: 0x3c, B: 0x59, A: 0xff}
	colButtonBg     = color.RGBA{R: 0x1a, G: 0x2a, B: 0x3a, A: 0xff}
	colButtonActive = color.RGBA{R: 0x23, G: 0x45, B: 0x60, A: 0xff}
	colBorder       = color.RGBA{R: 0x2d, G: 0x45, B: 0x5c, A: 0xff}
	colFocusBorder  = color.RGBA{R: 0x38, G: 0xc0, B: 0x70, A: 0xff}
	colModalBg      = color.RGBA{R: 0x06, G: 0x0a, B: 0x10, A: 0xe8}
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
	searchDebounce    time.Duration
	hasSearchDebounce bool
	searchQuery       string
	searchFocused     bool
	searchStatus      string
	results           []model.Track
	selectedResult    int
	scrollOffset      int
	showArtModal      bool

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
		g.searcher = catalog.NewSearcher(g.cat, catalog.WithDebounce(debounce))
		if strings.TrimSpace(g.searchQuery) != "" {
			g.searchStatus = "Searching..."
			g.searcher.Submit(g.searchQuery)
		}
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
	if wheelY != 0 && len(g.results) > VisibleResultRows {
		cx, cy := ebiten.CursorPosition()
		if image.Pt(cx, cy).In(ResultListBounds) {
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
			if res.Query == "" {
				g.results = nil
				g.selectedResult = 0
				g.scrollOffset = 0
				g.searchStatus = "Type query or paste URL"
				continue
			}
			g.results = append([]model.Track(nil), res.Tracks...)
			g.selectedResult = 0
			g.scrollOffset = 0
			if res.IsPlaylist {
				g.searchStatus = fmt.Sprintf("Playlist: %d tracks", len(res.Tracks))
			} else {
				g.searchStatus = fmt.Sprintf("%d results", len(res.Tracks))
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
	if g.art == nil || len(g.results) == 0 {
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
	g.searchFocused = true
	g.showArtModal = false
}

func (g *Game) blurSearch() {
	g.searchFocused = false
}

func (g *Game) typeSearchText(s string) {
	if s == "" {
		return
	}
	g.typeSearchRunes([]rune(s))
}

func (g *Game) typeSearchRunes(chars []rune) {
	added := false
	for _, r := range chars {
		if r >= 0x20 && r != 0x7f {
			g.searchQuery += string(r)
			added = true
		}
	}
	if added {
		g.onSearchTextChanged()
	}
}

func (g *Game) backspaceSearch() {
	if g.searchQuery == "" {
		return
	}
	_, size := utf8.DecodeLastRuneInString(g.searchQuery)
	if size <= 0 {
		return
	}
	g.searchQuery = g.searchQuery[:len(g.searchQuery)-size]
	g.onSearchTextChanged()
}

func (g *Game) clearSearch() {
	g.searchQuery = ""
	g.searchFocused = true
	g.onSearchTextChanged()
}

func (g *Game) submitSearch() {
	g.searchFocused = false
	if g.searcher == nil {
		return
	}
	if strings.TrimSpace(g.searchQuery) == "" {
		g.searcher.SetQuery("")
		g.searchStatus = "Type query or paste URL"
		return
	}
	g.searchStatus = "Searching..."
	g.searcher.Submit(g.searchQuery)
}

func (g *Game) onSearchTextChanged() {
	if g.searcher == nil {
		return
	}
	if strings.TrimSpace(g.searchQuery) == "" {
		g.searcher.SetQuery("")
		g.searchStatus = "Type query or paste URL"
		return
	}
	g.searchStatus = "Typing..."
	g.searcher.SetQuery(g.searchQuery)
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
	if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
		g.moveResultSelection(-1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
		g.moveResultSelection(1)
	}
	if chars := ebiten.AppendInputChars(nil); len(chars) > 0 {
		g.typeSearchRunes(chars)
	}
	if dur := inpututil.KeyPressDuration(ebiten.KeyBackspace); dur == 1 || (dur > 20 && dur%3 == 0) {
		g.backspaceSearch()
	}
}

func (g *Game) onBrowserClick(id string) bool {
	if g.showArtModal {
		if id == WidgetNowPlayingArt {
			g.showArtModal = false
		}
		return true
	}

	switch id {
	case WidgetEject, WidgetSearchInput:
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
		if g.selectedResult >= 0 && g.selectedResult < len(g.results) {
			g.playResultIndex(g.selectedResult)
		}
		return true
	case WidgetBrowserQueue:
		if g.ctrl != nil && g.selectedResult >= 0 && g.selectedResult < len(g.results) {
			tr := g.results[g.selectedResult]
			g.ctrl.Send(playback.Enqueue{Tracks: []model.Track{tr}})
			g.searchStatus = "Queued: " + truncateRunes(trackDisplayTitle(tr), 18)
		}
		return true
	case WidgetBrowserImportAll:
		if g.ctrl != nil && len(g.results) > 0 {
			g.ctrl.Send(playback.SetQueue{Tracks: g.results, Index: 0, AutoPlay: true})
			g.errText = ""
			g.searchStatus = fmt.Sprintf("Playing %d tracks", len(g.results))
		}
		return true
	}

	for rowIdx, rowWidget := range resultRowWidgets {
		if id == rowWidget {
			idx := g.scrollOffset + rowIdx
			if idx >= 0 && idx < len(g.results) {
				if g.selectedResult == idx {
					g.playResultIndex(idx)
				} else {
					g.selectedResult = idx
				}
			}
			return true
		}
	}
	return false
}

func (g *Game) playResultIndex(idx int) {
	if g.ctrl == nil || idx < 0 || idx >= len(g.results) {
		return
	}
	g.errText = ""
	g.ctrl.Send(playback.SetQueue{
		Tracks:   g.results,
		Index:    idx,
		AutoPlay: true,
	})
}

func (g *Game) moveResultSelection(delta int) {
	if len(g.results) == 0 {
		return
	}
	next := g.selectedResult + delta
	if next < 0 {
		next = 0
	} else if next >= len(g.results) {
		next = len(g.results) - 1
	}
	g.selectedResult = next
	if g.selectedResult < g.scrollOffset {
		g.scrollOffset = g.selectedResult
	} else if g.selectedResult >= g.scrollOffset+VisibleResultRows {
		g.scrollOffset = g.selectedResult - VisibleResultRows + 1
	}
	g.requestVisibleArtwork()
}

func (g *Game) scrollResults(delta int) {
	if len(g.results) <= VisibleResultRows {
		g.scrollOffset = 0
		return
	}
	maxOffset := len(g.results) - VisibleResultRows
	g.scrollOffset += delta
	if g.scrollOffset < 0 {
		g.scrollOffset = 0
	} else if g.scrollOffset > maxOffset {
		g.scrollOffset = maxOffset
	}
	g.requestVisibleArtwork()
}

func (g *Game) drawBrowser(screen *ebiten.Image) {
	vector.DrawFilledRect(screen, 0, MainWindowHeight, WindowWidth, BrowserHeight, colPanelBg, false)
	vector.DrawFilledRect(screen, 0, MainWindowHeight, WindowWidth, 12, colHeaderBg, false)
	vector.StrokeRect(screen, 0.5, MainWindowHeight+0.5, WindowWidth-1, BrowserHeight-1, 1, colBorder, false)

	ebitenutil.DebugPrintAt(screen, "CATALOG", 6, MainWindowHeight)
	ebitenutil.DebugPrintAt(screen, truncateRunes(g.searchStatus, 28), 72, MainWindowHeight)

	snap := g.snap

	// Now-Playing strip.
	vector.DrawFilledRect(screen, 4, 129, 267, 29, colSurfaceBg, false)
	g.drawScaledImage(screen, g.artworkListImage(snap.Track), NowPlayingArtBounds)
	vector.StrokeRect(screen, float32(NowPlayingArtBounds.Min.X)-0.5, float32(NowPlayingArtBounds.Min.Y)-0.5, float32(NowPlayingArtBounds.Dx()+1), float32(NowPlayingArtBounds.Dy()+1), 1, colBorder, false)

	title := trackDisplayTitle(snap.Track)
	if title == "" {
		title = "No track selected"
	}
	artist := snap.Track.Artist
	if artist == "" {
		artist = snap.State.String()
	}
	infoLine := fmt.Sprintf("%s [%s/%s]", truncateRunes(artist, 22), formatClock(snap.Position), formatClock(snap.Duration))
	ebitenutil.DebugPrintAt(screen, truncateRunes(title, 38), 36, 129)
	ebitenutil.DebugPrintAt(screen, truncateRunes(infoLine, 38), 36, 142)

	// Search input + action buttons.
	inputBorder := colBorder
	if g.searchFocused {
		inputBorder = colFocusBorder
	}
	vector.DrawFilledRect(screen, float32(SearchInputBounds.Min.X), float32(SearchInputBounds.Min.Y), float32(SearchInputBounds.Dx()), float32(SearchInputBounds.Dy()), colSurfaceBg, false)
	vector.StrokeRect(screen, float32(SearchInputBounds.Min.X)+0.5, float32(SearchInputBounds.Min.Y)+0.5, float32(SearchInputBounds.Dx()-1), float32(SearchInputBounds.Dy()-1), 1, inputBorder, false)

	inputLabel := "Search or playlist URL (/)"
	if g.searchQuery != "" || g.searchFocused {
		inputLabel = tailRunes(g.searchQuery, 29)
		if g.searchFocused {
			inputLabel += "_"
		}
	}
	ebitenutil.DebugPrintAt(screen, inputLabel, SearchInputBounds.Min.X+3, SearchInputBounds.Min.Y+1)

	g.drawButtonRect(screen, SearchSubmitBounds, WidgetSearchSubmit, "GO")
	g.drawButtonRect(screen, SearchClearBounds, WidgetSearchClear, "CLR")

	// Results list.
	vector.DrawFilledRect(screen, float32(ResultListBounds.Min.X), float32(ResultListBounds.Min.Y), float32(ResultListBounds.Dx()), float32(ResultListBounds.Dy()), colSurfaceBg, false)
	vector.StrokeRect(screen, float32(ResultListBounds.Min.X)+0.5, float32(ResultListBounds.Min.Y)+0.5, float32(ResultListBounds.Dx()-1), float32(ResultListBounds.Dy()-1), 1, colBorder, false)

	if len(g.results) == 0 {
		ebitenutil.DebugPrintAt(screen, "Press / or click search box", 16, 200)
		ebitenutil.DebugPrintAt(screen, "to search YTM or paste URL", 16, 216)
	} else {
		for row := 0; row < VisibleResultRows; row++ {
			idx := g.scrollOffset + row
			if idx >= len(g.results) {
				break
			}
			tr := g.results[idx]
			rowY := ResultListBounds.Min.Y + row*ResultRowHeight

			rowBg := colSurfaceBg
			if idx == g.selectedResult {
				rowBg = colRowSelected
			} else if row%2 == 1 {
				rowBg = colRowAltBg
			}
			vector.DrawFilledRect(screen, float32(ResultListBounds.Min.X+1), float32(rowY), float32(ResultListBounds.Dx()-2), ResultRowHeight, rowBg, false)

			thumbRect := image.Rect(9, rowY+2, 29, rowY+22)
			g.drawScaledImage(screen, g.artworkListImage(tr), thumbRect)

			ebitenutil.DebugPrintAt(screen, truncateRunes(trackDisplayTitle(tr), 38), 33, rowY)
			sub := fmt.Sprintf("%s %s", truncateRunes(tr.Artist, 28), formatClock(tr.Duration))
			ebitenutil.DebugPrintAt(screen, truncateRunes(sub, 38), 33, rowY+11)
		}
	}

	// Bottom bar.
	g.drawButtonRect(screen, BrowserPlayBounds, WidgetBrowserPlay, "PLAY")
	g.drawButtonRect(screen, BrowserQueueBounds, WidgetBrowserQueue, "QUEUE")
	g.drawButtonRect(screen, BrowserImportAllBounds, WidgetBrowserImportAll, "ALL->Q")

	queueSummary := fmt.Sprintf("Q:%d/%d", snap.Index+1, len(snap.Queue))
	if len(snap.Queue) == 0 {
		queueSummary = "Q:0/0"
	}
	ebitenutil.DebugPrintAt(screen, queueSummary, 192, 327)

	if g.showArtModal {
		g.drawBrowserArtworkModal(screen, snap)
	}
}

func (g *Game) drawBrowserArtworkModal(screen *ebiten.Image, snap playback.Snapshot) {
	vector.DrawFilledRect(screen, 0, MainWindowHeight, WindowWidth, BrowserHeight, colModalBg, false)
	g.drawScaledImage(screen, g.artworkNowPlayingImage(snap.Track), ArtModalBounds)
	vector.StrokeRect(screen, float32(ArtModalBounds.Min.X)-0.5, float32(ArtModalBounds.Min.Y)-0.5, float32(ArtModalBounds.Dx()+1), float32(ArtModalBounds.Dy()+1), 1, colFocusBorder, false)
	ebitenutil.DebugPrintAt(screen, truncateRunes(trackDisplayTitle(snap.Track), 38), 16, 328)
}

func (g *Game) drawButtonRect(screen *ebiten.Image, rect image.Rectangle, widgetID, label string) {
	bg := colButtonBg
	if g.pointer.Active() == widgetID && g.pointer.Pressed() {
		bg = colButtonActive
	}
	vector.DrawFilledRect(screen, float32(rect.Min.X), float32(rect.Min.Y), float32(rect.Dx()), float32(rect.Dy()), bg, false)
	vector.StrokeRect(screen, float32(rect.Min.X)+0.5, float32(rect.Min.Y)+0.5, float32(rect.Dx()-1), float32(rect.Dy()-1), 1, colBorder, false)
	ebitenutil.DebugPrintAt(screen, label, rect.Min.X+4, rect.Min.Y+1)
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
