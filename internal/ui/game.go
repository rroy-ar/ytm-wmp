package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"ytm-wmp/internal/artwork"
	"ytm-wmp/internal/catalog"
	"ytm-wmp/internal/model"
	"ytm-wmp/internal/playback"
	"ytm-wmp/internal/skin"
	"ytm-wmp/internal/ui/skinview"
)

const (
	seekStep   = 5 * time.Second
	volumeStep = 0.1
)

type panelKind int

const (
	panelMain panelKind = iota
	panelEQ
	panelPlaylist
)

// PlaybackController drives playback via commands, events, and atomic snapshots.
type PlaybackController interface {
	Run(ctx context.Context)
	Send(cmd playback.Command) bool
	Events() <-chan playback.Event
	Snapshot() playback.Snapshot
}

// AudioLifecycle initializes and closes the audio device on UI lifecycle
// boundaries; playback control goes through PlaybackController.
type AudioLifecycle interface {
	Init() error
	Close()
}

// ArtworkSource fetches and caches resized track thumbnails off the UI thread.
type ArtworkSource interface {
	Get(ctx context.Context, videoID, thumbnailURL string) (artwork.Bundle, error)
	LookupMemory(videoID string) (artwork.Bundle, bool)
}

// GameOption customizes Game wiring for catalog search, artwork, and rack persistence.
type GameOption func(*Game)

// WithCatalog enables catalog search, playlist import, and track metadata enrichment.
func WithCatalog(cat catalog.Catalog) GameOption {
	return func(g *Game) { g.cat = cat }
}

// WithYouTubeCatalog sets the regular YouTube (yt-dlp) catalog used for `yt:`
// / `ytdlp:` prefixed searches in the browser panel.
func WithYouTubeCatalog(cat catalog.Catalog) GameOption {
	return func(g *Game) { g.ytCat = cat }
}

// WithArtwork enables asynchronous thumbnail fetching and display.
func WithArtwork(art ArtworkSource) GameOption {
	return func(g *Game) { g.art = art }
}

// WithInitialQuery seeds the browser search box and runs an initial lookup.
func WithInitialQuery(q string) GameOption {
	return func(g *Game) {
		g.searchQuery = strings.TrimSpace(q)
		if g.searchQuery != "" {
			g.browserMode = true
		}
	}
}

// WithSearchDebounce overrides the typing debounce window used by the catalog searcher.
func WithSearchDebounce(d time.Duration) GameOption {
	return func(g *Game) {
		g.searchDebounce = d
		g.hasSearchDebounce = true
	}
}

// WithClipboard overrides the clipboard reader used for browser search and M3U paste.
func WithClipboard(fn ClipboardFunc) GameOption {
	return func(g *Game) { g.clipboard = fn }
}

// WithWindowResizer registers the OS window resize callback invoked when EQ/PL panels toggle.
func WithWindowResizer(fn func(w, h int)) GameOption {
	return func(g *Game) { g.windowResizer = fn }
}

// WithEQPath configures where EQ band/preamp state is atomically persisted.
func WithEQPath(path string) GameOption {
	return func(g *Game) { g.eqPath = path }
}

// WithPlaylistPath configures the default M3U save/load file path.
func WithPlaylistPath(path string) GameOption {
	return func(g *Game) { g.plM3UPath = path }
}

// WithSkinName records the loaded skin label for the debug overlay.
func WithSkinName(name string) GameOption {
	return func(g *Game) { g.skinName = name }
}

// WithSkinPath records the loaded skin path so the skin picker opens in its directory.
func WithSkinPath(path string) GameOption {
	return func(g *Game) {
		g.skinPath = path
		if g.skinName == "" && path != "" {
			g.skinName = filepath.Base(path)
		}
	}
}

// WithSkinPicker overrides the OS file chooser used by the main window skin button.
func WithSkinPicker(fn SkinPickerFunc) GameOption {
	return func(g *Game) { g.skinPicker = fn }
}

// WithIME enables Ebitengine's platform IME Composer (used when running inside
// ebiten.RunGame; disabled in headless unit tests).
func WithIME(enabled bool) GameOption {
	return func(g *Game) { g.imeEnabled = enabled }
}

// WithToolStatus seeds initial yt-dlp and ffmpeg dependency statuses.
func WithToolStatus(ytdlp, ffmpeg ToolStatus) GameOption {
	return func(g *Game) {
		g.ytdlpStatus = ytdlp
		g.ffmpegStatus = ffmpeg
	}
}

// WithToolChecker registers the async dependency verification callback.
func WithToolChecker(fn ToolCheckFunc) GameOption {
	return func(g *Game) { g.toolChecker = fn }
}

// WithToolApplier registers the callback invoked when tool binary paths update.
func WithToolApplier(fn func(ytdlpPath, ffmpegPath string)) GameOption {
	return func(g *Game) { g.toolApplier = fn }
}

// WithBufferApplier registers the callback invoked when audio buffer size changes.
func WithBufferApplier(fn func(time.Duration)) GameOption {
	return func(g *Game) { g.bufferApplier = fn }
}

// WithDefaultSkinPath sets the bundled dev-skin path used by Settings reset.
func WithDefaultSkinPath(path string) GameOption {
	return func(g *Game) { g.defaultSkinPath = path }
}

type skinLoadResult struct {
	path string
	skin *skin.Skin
	err  error
}

// Game is the Ebitengine application: a docked Winamp rack (Main, EQ,
// Playlist/Browser) wired to the playback actor.
type Game struct {
	ctx    context.Context
	cancel context.CancelFunc

	skin           *skin.Skin
	skinName       string
	skinPath       string
	skinPicker     SkinPickerFunc
	skinPickActive bool
	skinLoadCh     chan skinLoadResult
	ctrl           PlaybackController
	sink           AudioLifecycle
	cat            catalog.Catalog
	ytCat          catalog.Catalog
	art            ArtworkSource

	view          *skinview.View
	textFace      text.Face
	pointer       Pointer
	snap          playback.Snapshot
	hasSnap       bool
	debug         bool
	audioInit     bool
	imeEnabled    bool
	quitRequested bool

	windowResizer    func(w, h int)
	lastCanvasHeight int

	showEQ       bool
	hidePL       bool
	browserMode  bool
	showSettings bool
	focusedPanel panelKind

	tick            uint64
	timeRemaining   bool
	balance         float64
	dragSlider      string
	seekPreviewFrac float64
	transientMsg    string
	transientUntil  time.Time

	errText string

	browserPanel
	playlistPanel
	eqPanel
	settingsState
	toastState

	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewGame wires the UI to a loaded skin, playback controller, audio sink, and
// optional catalog/artwork services. It starts the playback actor under a
// derived context canceled by Close.
func NewGame(parent context.Context, s *skin.Skin, ctrl PlaybackController, sink AudioLifecycle, opts ...GameOption) *Game {
	ctx, cancel := context.WithCancel(parent)
	g := &Game{
		ctx:        ctx,
		cancel:     cancel,
		skin:       s,
		ctrl:       ctrl,
		sink:       sink,
		skinLoadCh: make(chan skinLoadResult, 1),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	g.textFace = initUITextFace()
	g.initBrowser()
	g.initPlaylist()
	g.initEQ()
	g.initSettings()
	g.lastCanvasHeight = g.canvasHeight()

	if ctrl != nil {
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			ctrl.Run(ctx)
		}()
	}
	return g
}

func (g *Game) canvasHeight() int {
	h := MainWindowHeight
	if g.showEQ {
		h += EQWindowHeight
	}
	if !g.hidePL {
		h += PlaylistHeight
	}
	return h
}

func (g *Game) eqTop() int {
	return MainWindowHeight
}

func (g *Game) plTop() int {
	if g.showEQ {
		return MainWindowHeight + EQWindowHeight
	}
	return MainWindowHeight
}

func (g *Game) browserDeltaY() int {
	return g.plTop() - MainWindowHeight
}

func (g *Game) syncWindowSize() {
	h := g.canvasHeight()
	if h == g.lastCanvasHeight {
		return
	}
	g.lastCanvasHeight = h
	if g.windowResizer != nil {
		scale := g.scale
		if scale != 2 {
			scale = 1
		}
		g.windowResizer(WindowWidth*scale, h*scale)
	}
}

func (g *Game) setTransientStatus(msg string) {
	g.transientMsg = msg
	g.transientUntil = time.Now().Add(1500 * time.Millisecond)
}

func (g *Game) activeWidgets() []Widget {
	widgets := make([]Widget, 0, 64)
	widgets = append(widgets, mainWidgets...)
	widgets = append(widgets, mainControlWidgets...)
	if g.showEQ {
		widgets = append(widgets, eqWidgetsAt(g.eqTop())...)
	}
	if !g.hidePL {
		if g.browserMode {
			widgets = append(widgets, g.browserWidgetsAt(g.plTop())...)
		} else {
			widgets = append(widgets, playlistWidgetsAt(g.plTop(), g.plMenu)...)
		}
	}
	widgets = append(widgets, g.toastWidgets()...)
	return widgets
}

// Update advances input, drains playback/catalog events, and enforces the fixed
// timestep. It returns ebiten.Termination when the parent context is canceled or
// the close button is clicked.
func (g *Game) Update() error {
	if g.ctx != nil && g.ctx.Err() != nil {
		return ebiten.Termination
	}
	if g.quitRequested {
		return ebiten.Termination
	}

	g.tick++

	// The audio context and GPU textures must be created after Ebitengine has
	// started, so both happen on the first tick rather than in main.
	if !g.audioInit {
		g.audioInit = true
		if g.sink != nil {
			if err := g.sink.Init(); err != nil {
				g.setError(fmt.Sprintf("opening audio device: %v", err))
			}
		}
	}
	if g.view == nil && g.skin != nil {
		g.view = skinview.New(g.skin)
	}
	if g.textFace == nil {
		g.textFace = initUITextFace()
	}

	g.drainEvents()
	g.drainSkinLoads()
	g.drainSettingsResults()
	g.expireToasts(time.Now())
	if g.ctrl != nil {
		g.snap = g.ctrl.Snapshot()
		g.hasSnap = true
	}
	g.updateBrowser()

	cursorX, cursorY := ebiten.CursorPosition()
	mousePressed := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	wasMousePressed := g.pointer.Pressed()
	dy := g.browserDeltaY()
	if mousePressed && !wasMousePressed {
		pt := image.Pt(cursorX, cursorY)
		g.updatePanelFocus(pt)
		if g.searchFocused && !pt.In(offsetRect(SearchInputBounds, dy)) && !pt.In(EjectButtonBounds) {
			g.searchFocused = false
		}
		if g.showArtModal && !pt.In(offsetRect(NowPlayingArtBounds, dy)) {
			g.showArtModal = false
		}
	}
	if !g.showArtModal && !g.showSettings && g.browserMode && !g.hidePL &&
		len(g.detailStack) == 0 && g.activeTab != browserTabQueue &&
		(inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonMiddle) || inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight)) &&
		image.Pt(cursorX, cursorY).In(offsetRect(SearchInputBounds, dy)) {
		g.pasteSearch()
	}

	widgets := g.activeWidgets()
	clicked := g.pointer.Update(cursorX, cursorY, mousePressed, widgets)

	g.updateMainSliders(cursorX, mousePressed)
	if g.showEQ {
		g.updateEQDrag(g.eqTop(), cursorY, mousePressed)
	}
	if !g.hidePL && !g.browserMode {
		g.updatePlaylist(g.plTop(), cursorX, cursorY, mousePressed)
	}

	if clicked != "" {
		g.onClick(clicked)
	} else if !mousePressed && wasMousePressed && g.plMenu != plMenuNone {
		g.plMenu = plMenuNone
	}

	g.updateSearchIME()
	g.handleKeys()
	g.maybeSaveEQ()

	if inpututil.IsKeyJustPressed(ebiten.KeyF1) {
		g.toggleDebug()
	}
	return nil
}

func (g *Game) updatePanelFocus(pt image.Point) {
	switch {
	case pt.Y < MainWindowHeight:
		g.focusedPanel = panelMain
	case g.showEQ && pt.Y < g.plTop():
		g.focusedPanel = panelEQ
	case !g.hidePL:
		g.focusedPanel = panelPlaylist
	default:
		g.focusedPanel = panelMain
	}
}

func (g *Game) updateMainSliders(cursorX int, pressed bool) {
	if !pressed {
		if g.dragSlider == WidgetMainPosition && g.ctrl != nil && g.snap.Duration > 0 && g.snap.State != playback.StateIdle {
			target := time.Duration(g.seekPreviewFrac * float64(g.snap.Duration))
			g.ctrl.Send(playback.Seek{Position: target})
		}
		g.dragSlider = ""
		return
	}

	if g.dragSlider == "" {
		switch g.pointer.Active() {
		case WidgetMainVolume, WidgetMainBalance, WidgetMainPosition:
			g.dragSlider = g.pointer.Active()
		}
	}

	switch g.dragSlider {
	case WidgetMainVolume:
		vol := sliderRatioX(cursorX, MainVolumeBounds, 14)
		g.setTransientStatus(fmt.Sprintf("VOLUME: %d%%", int(math.Round(vol*100))))
		if g.ctrl != nil && math.Abs(vol-g.snap.Volume) > 0.005 {
			g.ctrl.Send(playback.SetVolume{Volume: vol})
		}
	case WidgetMainBalance:
		ratio := sliderRatioX(cursorX, MainBalanceBounds, 14)
		bal := ratio*2.0 - 1.0
		if math.Abs(bal) < 0.06 {
			bal = 0
		}
		g.balance = bal
		switch {
		case bal == 0:
			g.setTransientStatus("BALANCE: CENTER")
		case bal < 0:
			g.setTransientStatus(fmt.Sprintf("BALANCE: %d%% LEFT", int(math.Round(-bal*100))))
		default:
			g.setTransientStatus(fmt.Sprintf("BALANCE: %d%% RIGHT", int(math.Round(bal*100))))
		}
	case WidgetMainPosition:
		if g.snap.Duration > 0 && g.snap.State != playback.StateIdle {
			g.seekPreviewFrac = sliderRatioX(cursorX, MainPositionBounds, 29)
			target := time.Duration(g.seekPreviewFrac * float64(g.snap.Duration))
			g.setTransientStatus(fmt.Sprintf("SEEK TO: %s / %s", formatClock(target), formatClock(g.snap.Duration)))
		}
	}
}

// Draw renders the main window, optional EQ window, and Playlist/Browser panel.
// It never does I/O, resolves URLs, or allocates textures.
func (g *Game) Draw(screen *ebiten.Image) {
	if !g.hasSnap && g.ctrl != nil {
		g.snap = g.ctrl.Snapshot()
		g.hasSnap = true
	}
	screen.Fill(color.RGBA{R: 0x0c, G: 0x0f, B: 0x14, A: 0xff})

	if g.view == nil {
		ebitenutil.DebugPrintAt(screen, "skin view not ready", 8, 8)
	} else {
		g.drawMainWindow(screen)
		if g.showEQ {
			g.drawEQ(screen, g.eqTop())
		}
	}

	if !g.hidePL {
		if g.browserMode || g.view == nil {
			g.drawBrowser(screen)
		} else {
			g.drawPlaylist(screen, g.plTop())
		}
	}

	if _, ok := g.activeToast(); ok {
		g.drawToast(screen)
	}
	if g.debug {
		g.drawDebug(screen)
	}
}

func (g *Game) drawMainWindow(screen *ebiten.Image) {
	snap := g.snap
	g.drawSprite(screen, skin.MainBackground, image.Pt(0, 0))

	titleName := skin.TitlebarIdle
	if g.focusedPanel == panelMain && ebiten.IsFocused() {
		titleName = skin.TitlebarActive
	}
	g.drawSprite(screen, titleName, image.Pt(0, 0))

	if g.pointer.Active() == WidgetMainClose && g.pointer.Pressed() && g.view.HasSprite(skin.MainCloseActive) {
		g.drawSprite(screen, skin.MainCloseActive, MainCloseBounds.Min)
	} else if g.view.HasSprite(skin.MainCloseButton) {
		g.drawSprite(screen, skin.MainCloseButton, MainCloseBounds.Min)
	}

	if g.view.HasSprite(skin.MainClutterBar) {
		g.drawSprite(screen, skin.MainClutterBar, image.Pt(10, 22))
	}

	// Play/pause/stop & working indicators.
	switch snap.State {
	case playback.StatePlaying, playback.StateResolving, playback.StateSeeking:
		g.drawSprite(screen, skin.MainPlayingIndicator, image.Pt(26, 28))
		if g.view.HasSprite(skin.MainWorkingIndicator) {
			g.drawSprite(screen, skin.MainWorkingIndicator, image.Pt(24, 28))
		}
	case playback.StatePaused:
		g.drawSprite(screen, skin.MainPausedIndicator, image.Pt(26, 28))
	default:
		g.drawSprite(screen, skin.MainStoppedIndicator, image.Pt(26, 28))
	}

	// Time display (blinks at ~1Hz when paused).
	blinkOff := (g.tick/30)%2 == 1
	drawTimeDisplay(screen, g.view, snap.Position, snap.Duration, snap.State, g.timeRemaining, blinkOff)

	// Marquee text.
	drawMarquee(screen, g.view, g.textFace, g.marqueeText(snap), g.tick)

	// Bitrate / sample rate + stereo/mono indicators.
	if snap.State != playback.StateIdle {
		drawBitmapString(screen, g.view, g.textFace, "128", image.Rect(111, 43, 126, 49), 0, color.RGBA{0x38, 0xc0, 0x70, 0xff})
		drawBitmapString(screen, g.view, g.textFace, "48", image.Rect(156, 43, 166, 49), 0, color.RGBA{0x38, 0xc0, 0x70, 0xff})
	}
	g.drawSprite(screen, skin.MainMono, image.Pt(212, 41))
	if snap.State == playback.StatePlaying || snap.State == playback.StatePaused || snap.State == playback.StateSeeking {
		g.drawSprite(screen, skin.MainStereoSelected, image.Pt(239, 41))
	} else {
		g.drawSprite(screen, skin.MainStereo, image.Pt(239, 41))
	}

	// Volume slider (107, 57).
	opVol := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest}
	opVol.GeoM.Translate(float64(MainVolumeBounds.Min.X), float64(MainVolumeBounds.Min.Y))
	screen.DrawImage(g.view.VolumeFrame(snap.Volume), opVol)
	volThumbX := MainVolumeBounds.Min.X + int(math.Round(clampUnit(snap.Volume)*float64(65-14)))
	volThumb := buttonSprite(skin.MainVolumeThumb, skin.MainVolumeThumbSelected, g.dragSlider == WidgetMainVolume)
	g.drawSprite(screen, volThumb, image.Pt(volThumbX, MainVolumeBounds.Min.Y+1))

	// Balance slider (177, 57).
	opBal := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest}
	opBal.GeoM.Translate(float64(MainBalanceBounds.Min.X), float64(MainBalanceBounds.Min.Y))
	screen.DrawImage(g.view.BalanceFrame(g.balance), opBal)
	balRatio := (g.balance + 1.0) / 2.0
	balThumbX := MainBalanceBounds.Min.X + int(math.Round(clampUnit(balRatio)*float64(MainBalanceBounds.Dx()-14)))
	balThumb := buttonSprite(skin.MainBalanceThumb, skin.MainBalanceThumbActive, g.dragSlider == WidgetMainBalance)
	g.drawSprite(screen, balThumb, image.Pt(balThumbX, MainBalanceBounds.Min.Y+1))

	// EQ / PL toggles.
	eqName := toggleSprite(
		skin.MainEQButton, skin.MainEQButtonDepressed,
		skin.MainEQButtonSelected, skin.MainEQButtonDepressedSelected,
		g.showEQ, g.pointer.Active() == WidgetMainEQ && g.pointer.Pressed(),
	)
	g.drawSprite(screen, eqName, MainEQToggleBounds.Min)

	plName := toggleSprite(
		skin.MainPlaylistButton, skin.MainPlaylistButtonDepressed,
		skin.MainPlaylistButtonSelected, skin.MainPlaylistButtonDepressedSelect,
		!g.hidePL, g.pointer.Active() == WidgetMainPL && g.pointer.Pressed(),
	)
	g.drawSprite(screen, plName, MainPLToggleBounds.Min)

	// Position / seek bar (16, 72).
	g.drawSprite(screen, skin.MainPositionBackground, MainPositionBounds.Min)
	if snap.State != playback.StateIdle && snap.Duration > 0 {
		frac := float64(snap.Position) / float64(snap.Duration)
		if g.dragSlider == WidgetMainPosition {
			frac = g.seekPreviewFrac
		}
		frac = clampUnit(frac)
		posX := MainPositionBounds.Min.X + int(math.Round(frac*float64(MainPositionBounds.Dx()-29)))
		posThumb := buttonSprite(skin.MainPositionThumb, skin.MainPositionThumbSelected, g.dragSlider == WidgetMainPosition)
		g.drawSprite(screen, posThumb, image.Pt(posX, MainPositionBounds.Min.Y))
	}

	// Transport buttons + Eject.
	for _, p := range mainPlacements[2:] {
		name := g.spriteName(p.Sprite)
		g.drawSprite(screen, name, p.At)
	}

	// Shuffle & Repeat toggles.
	shufName := toggleSprite(
		skin.MainShuffleButton, skin.MainShuffleButtonDepressed,
		skin.MainShuffleButtonSelected, skin.MainShuffleButtonSelectedDepress,
		snap.Shuffle, g.pointer.Active() == WidgetMainShuffle && g.pointer.Pressed(),
	)
	g.drawSprite(screen, shufName, MainShuffleBounds.Min)

	repName := toggleSprite(
		skin.MainRepeatButton, skin.MainRepeatButtonDepressed,
		skin.MainRepeatButtonSelected, skin.MainRepeatButtonSelectedDepressed,
		snap.Repeat != playback.RepeatOff, g.pointer.Active() == WidgetMainRepeat && g.pointer.Pressed(),
	)
	g.drawSprite(screen, repName, MainRepeatBounds.Min)

	if g.pointer.Active() == WidgetMainSkin && g.pointer.Pressed() {
		vector.DrawFilledRect(
			screen,
			float32(MainSkinBounds.Min.X),
			float32(MainSkinBounds.Min.Y),
			float32(MainSkinBounds.Dx()),
			float32(MainSkinBounds.Dy()),
			color.RGBA{A: 0x48},
			false,
		)
	}
}

func (g *Game) marqueeText(snap playback.Snapshot) string {
	if g.transientMsg != "" && time.Now().Before(g.transientUntil) {
		return g.transientMsg
	}
	title := trackDisplayTitle(snap.Track)
	if title == "" {
		return "YTM-WMP CLASSIC PLAYER"
	}
	if snap.Track.Artist != "" && snap.Track.Title != "" {
		title = snap.Track.Artist + " - " + snap.Track.Title
	}
	if snap.Duration > 0 {
		return fmt.Sprintf("%s (%s)", title, formatClock(snap.Duration))
	}
	return title
}

func clampUnit(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Layout keeps a fixed 275px logical width and adapts logical height to the
// open rack panels (Main, optional EQ, optional Playlist/Browser).
func (g *Game) Layout(_, _ int) (int, int) {
	return WindowWidth, g.canvasHeight()
}

// Close cancels the playback actor, searcher, and background workers, waits for
// them to stop, flushes EQ state, then releases the audio device.
func (g *Game) Close() {
	g.closeOnce.Do(func() {
		g.saveEQNow()
		if g.searcher != nil {
			g.searcher.Close()
		}
		if g.cancel != nil {
			g.cancel()
		}
		g.wg.Wait()
		if g.sink != nil {
			g.sink.Close()
		}
		for k, tex := range g.artTex {
			deallocateArtTextures(tex)
			delete(g.artTex, k)
		}
		g.artOrder = nil
		if g.artPlaceholder != nil {
			g.artPlaceholder.Deallocate()
			g.artPlaceholder = nil
		}
		if g.view != nil {
			g.view.Deallocate()
			g.view = nil
		}
	})
}

func (g *Game) onClick(id string) {
	if g.onToastClick(id) {
		return
	}
	if g.onEQClick(id) {
		return
	}
	if g.onPlaylistClick(id) {
		return
	}
	if g.onBrowserClick(id) {
		return
	}
	switch id {
	case WidgetMainClose:
		g.quitRequested = true
		return
	case WidgetMainTime:
		g.timeRemaining = !g.timeRemaining
		return
	case WidgetMainEQ:
		g.showEQ = !g.showEQ
		g.syncWindowSize()
		return
	case WidgetMainPL:
		g.hidePL = !g.hidePL
		g.plMenu = plMenuNone
		g.syncWindowSize()
		return
	case WidgetMainSkin:
		g.clearError()
		g.openSkinPicker()
		return
	}

	if g.ctrl == nil {
		return
	}
	slog.Debug("ui button clicked", "widget", id)
	switch id {
	case WidgetPrevious:
		g.clearError()
		g.ctrl.Send(playback.Prev{})
	case WidgetPlay:
		g.clearError()
		g.ctrl.Send(playback.Play{})
	case WidgetPause:
		g.ctrl.Send(playback.Pause{})
	case WidgetStop:
		g.clearError()
		g.ctrl.Send(playback.Stop{})
	case WidgetNext:
		g.clearError()
		g.ctrl.Send(playback.Next{})
	case WidgetMainShuffle:
		next := !g.snap.Shuffle
		g.ctrl.Send(playback.SetShuffle{Enabled: next})
		if next {
			g.setTransientStatus("SHUFFLE: ON")
		} else {
			g.setTransientStatus("SHUFFLE: OFF")
		}
	case WidgetMainRepeat:
		next := (g.snap.Repeat + 1) % 3
		g.ctrl.Send(playback.SetRepeat{Mode: next})
		g.setTransientStatus("REPEAT: " + strings.ToUpper(next.String()))
	}
}

func (g *Game) handleKeys() {
	if g.handleSettingsKeys() {
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyF2) {
		if g.showSettings {
			g.showSettings = false
		} else {
			g.openSettings()
		}
		return
	}
	if !g.showArtModal && isPasteJustPressed() {
		if g.focusedPanel == panelPlaylist && !g.browserMode && !g.hidePL && !g.searchFocused {
			g.importM3UFromClipboard()
			return
		}
		g.pasteSearch()
		return
	}
	if !g.searchFocused && inpututil.IsKeyJustPressed(ebiten.KeySlash) {
		g.focusSearch()
		return
	}
	if g.searchFocused {
		g.handleSearchKeys()
		return
	}
	if g.showArtModal && inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		g.showArtModal = false
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) && g.dismissActiveToast() {
		return
	}
	g.handlePlaylistKeys()

	if g.ctrl == nil {
		return
	}
	snap := g.snap

	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		slog.Debug("ui key", "key", "space", "state", snap.State.String())
		switch snap.State {
		case playback.StatePlaying:
			g.ctrl.Send(playback.Pause{})
		case playback.StatePaused:
			g.ctrl.Send(playback.Resume{})
		default:
			g.clearError()
			g.ctrl.Send(playback.Play{})
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyS) {
		slog.Debug("ui key", "key", "s")
		g.clearError()
		g.ctrl.Send(playback.Stop{})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyZ) {
		slog.Debug("ui key", "key", "z")
		g.clearError()
		g.ctrl.Send(playback.Prev{})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyB) {
		slog.Debug("ui key", "key", "b")
		g.clearError()
		g.ctrl.Send(playback.Next{})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyLeft) {
		target := snap.Position - seekStep
		if target < 0 {
			target = 0
		}
		slog.Debug("ui key", "key", "left", "seek", target)
		g.ctrl.Send(playback.Seek{Position: target})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyRight) {
		target := snap.Position + seekStep
		slog.Debug("ui key", "key", "right", "seek", target)
		g.ctrl.Send(playback.Seek{Position: target})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
		g.ctrl.Send(playback.SetVolume{Volume: snap.Volume + volumeStep})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
		g.ctrl.Send(playback.SetVolume{Volume: snap.Volume - volumeStep})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyR) {
		next := (snap.Repeat + 1) % 3
		g.ctrl.Send(playback.SetRepeat{Mode: next})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyH) {
		g.ctrl.Send(playback.SetShuffle{Enabled: !snap.Shuffle})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyA) {
		g.ctrl.Send(playback.SetAutoplay{Enabled: !snap.Autoplay})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyE) {
		g.showEQ = !g.showEQ
		g.syncWindowSize()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyP) {
		g.hidePL = !g.hidePL
		g.syncWindowSize()
	}
}

func (g *Game) drainEvents() {
	if g.ctrl == nil {
		return
	}
	events := g.ctrl.Events()
	if events == nil {
		return
	}
	for {
		select {
		case ev := <-events:
			switch e := ev.(type) {
			case playback.PlaybackError:
				g.setError(e.Message)
			case playback.StateChanged:
				if e.To == playback.StateResolving || e.To == playback.StatePlaying || e.To == playback.StateIdle {
					g.clearError()
				}
			case playback.TrackChanged:
				if vid := model.NormalizeTrackID(e.Track.ID); vid != "" {
					delete(g.enrichFailed, vid)
				}
				g.maybeEnrichTrack(e.Track)
				g.requestArtwork(e.Track)
			case playback.QueueChanged:
				clear(g.enrichFailed)
			}
		default:
			return
		}
	}
}

func (g *Game) setError(msg string) {
	g.errText = msg
	g.pushActionToast("error", toastError, msg, "", "", 0)
	slog.Error("playback failed", "err", msg)
}

func (g *Game) clearError() {
	g.errText = ""
	g.clearToastSource("error")
}

func (g *Game) markConfigDirty() {}

func (g *Game) openSkinPicker() {
	if g.skinPickActive {
		return
	}
	picker := g.skinPicker
	if picker == nil {
		picker = pickSkinFile
	}
	startDir := defaultSkinStartDir(g.skinPath)
	g.skinPickActive = true
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		path, err := picker(g.ctx, startDir)
		if g.ctx.Err() != nil {
			return
		}
		if err != nil {
			g.sendSkinLoad(skinLoadResult{err: err})
			return
		}
		path = strings.TrimSpace(path)
		if path == "" {
			g.sendSkinLoad(skinLoadResult{})
			return
		}
		loaded, err := skin.Open(path)
		if g.ctx.Err() != nil {
			return
		}
		g.sendSkinLoad(skinLoadResult{path: path, skin: loaded, err: err})
	}()
}

func (g *Game) sendSkinLoad(res skinLoadResult) {
	if g.skinLoadCh == nil {
		return
	}
	select {
	case g.skinLoadCh <- res:
	case <-g.ctx.Done():
	}
}

func (g *Game) drainSkinLoads() {
	if g.skinLoadCh == nil {
		return
	}
	for {
		select {
		case res := <-g.skinLoadCh:
			g.skinPickActive = false
			if res.err != nil {
				g.setError(res.err.Error())
				continue
			}
			if res.skin == nil {
				continue
			}
			if g.view != nil {
				g.view.Deallocate()
			}
			g.skin = res.skin
			g.skinPath = res.path
			g.skinName = filepath.Base(res.path)
			g.view = skinview.New(res.skin)
			g.clearError()
			g.markConfigDirty()
			g.setTransientStatus("SKIN: " + strings.ToUpper(g.skinName))
			g.pushToast(toastInfo, "Skin: "+g.skinName, defaultInfoToastTTL)
			slog.Info("skin reloaded", "path", res.path, "sheets", res.skin.SheetNames())
		default:
			return
		}
	}
}

func (g *Game) toggleDebug() {
	g.debug = !g.debug
}

func (g *Game) spriteName(name string) string {
	switch name {
	case skin.TitlebarActive:
		if g.focusedPanel != panelMain || !ebiten.IsFocused() {
			return skin.TitlebarIdle
		}
	case skin.PreviousButton:
		if g.pointer.Active() == WidgetPrevious && g.pointer.Pressed() {
			return skin.PreviousButtonActive
		}
	case skin.PlayButton:
		if g.pointer.Active() == WidgetPlay && g.pointer.Pressed() {
			return skin.PlayButtonActive
		}
	case skin.PauseButton:
		if g.pointer.Active() == WidgetPause && g.pointer.Pressed() {
			return skin.PauseButtonActive
		}
	case skin.StopButton:
		if g.pointer.Active() == WidgetStop && g.pointer.Pressed() {
			return skin.StopButtonActive
		}
	case skin.NextButton:
		if g.pointer.Active() == WidgetNext && g.pointer.Pressed() {
			return skin.NextButtonActive
		}
	case skin.EjectButton:
		if g.pointer.Active() == WidgetEject && g.pointer.Pressed() {
			return skin.EjectButtonActive
		}
	}
	return name
}

func (g *Game) drawSprite(dst *ebiten.Image, name string, at image.Point) {
	img := g.view.Sprite(name)
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest}
	op.GeoM.Translate(float64(at.X), float64(at.Y))
	dst.DrawImage(img, op)

	if g.view.Missing(name) {
		// A silent skip makes coordinate bugs nearly impossible to find.
		ebitenutil.DebugPrintAt(dst, name, at.X+1, at.Y+1)
	}
}

func (g *Game) drawDebug(screen *ebiten.Image) {
	cyan := color.RGBA{R: 0x00, G: 0xff, B: 0xff, A: 0xff}
	magenta := color.RGBA{R: 0xff, G: 0x00, B: 0xff, A: 0xff}

	for _, p := range mainPlacements {
		name := g.spriteName(p.Sprite)
		def, ok := skin.Definition(name)
		if !ok {
			continue
		}
		width, height := def.Src.Dx(), def.Src.Dy()
		vector.StrokeRect(screen, float32(p.At.X)+0.5, float32(p.At.Y)+0.5, float32(width), float32(height), 1, cyan, false)
	}
	for _, widget := range g.activeWidgets() {
		b := widget.Bounds
		vector.StrokeRect(screen, float32(b.Min.X)+0.5, float32(b.Min.Y)+0.5, float32(b.Dx()), float32(b.Dy()), 1, magenta, false)
	}

	snap := g.snap
	missingCount := 0
	if g.view != nil {
		missingCount = len(g.view.MissingNames())
	}

	// Keep the report to six short lines: the built-in debug font does not
	// wrap, and the window is only 275 logical pixels wide.
	var report strings.Builder
	fmt.Fprintf(&report, "FPS %.0f TPS %.0f state=%s eq=%v pl=%v\n", ebiten.ActualFPS(), ebiten.ActualTPS(), snap.State, g.showEQ, !g.hidePL)
	fmt.Fprintf(&report, "q=%d/%d pos=%s/%s vol=%.0f%%\n", snap.Index+1, len(snap.Queue), snap.Position.Round(time.Second), snap.Duration.Round(time.Second), snap.Volume*100)
	fmt.Fprintf(&report, "shuf=%v rep=%s auto=%v id=%s\n", snap.Shuffle, snap.Repeat, snap.Autoplay, snap.Track.ID)
	fmt.Fprintf(&report, "skin=%s miss=%d\n", truncateRunes(g.skinName, 18), missingCount)
	fmt.Fprintf(&report, "hot=%q active=%q cur=%v\n", g.pointer.Hot(), g.pointer.Active(), g.pointer.Position())

	lines := wrapText(report.String(), 43)
	if len(lines) > 6 {
		lines = lines[:6]
	}
	height := float32(10 + len(lines)*14)
	vector.DrawFilledRect(screen, 3, 3, 269, height, color.RGBA{A: 0xe0}, false)
	for i, line := range lines {
		ebitenutil.DebugPrintAt(screen, line, 5, 5+i*14)
	}
}

func wrapText(text string, limit int) []string {
	if limit <= 0 {
		limit = 1
	}

	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		var line []rune
		for _, r := range paragraph {
			if len(line) == limit {
				lines = append(lines, string(line))
				line = line[:0]
			}
			line = append(line, r)
		}
		if len(line) > 0 {
			lines = append(lines, string(line))
		}
	}
	return lines
}
