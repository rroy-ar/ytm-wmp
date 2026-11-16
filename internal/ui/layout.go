package ui

import (
	"image"

	"ytm-wmp/internal/skin"
)

// Fixed logical dimensions in 1x skin coordinates.
// Scaling, if any, happens only at the draw boundary.
const (
	MainWindowWidth  = 275
	MainWindowHeight = 116
	BrowserHeight    = 232

	WindowWidth  = MainWindowWidth
	WindowHeight = MainWindowHeight + BrowserHeight

	VisibleResultRows = 6
	ResultRowHeight   = 24
)

// Widget IDs used by the game loop.
const (
	WidgetPrevious = "previous"
	WidgetPlay     = "play"
	WidgetPause    = "pause"
	WidgetStop     = "stop"
	WidgetNext     = "next"
	WidgetEject    = "eject"

	WidgetNowPlayingArt    = "browser.nowplaying.art"
	WidgetSearchInput      = "browser.search.input"
	WidgetSearchSubmit     = "browser.search.submit"
	WidgetSearchClear      = "browser.search.clear"
	WidgetResultRow0       = "browser.result.0"
	WidgetResultRow1       = "browser.result.1"
	WidgetResultRow2       = "browser.result.2"
	WidgetResultRow3       = "browser.result.3"
	WidgetResultRow4       = "browser.result.4"
	WidgetResultRow5       = "browser.result.5"
	WidgetBrowserPlay      = "browser.play"
	WidgetBrowserQueue     = "browser.queue"
	WidgetBrowserImportAll = "browser.import_all"
)

// Named bounds in 1x logical coordinates shared between hit testing, input
// focus checks, and rendering.
var (
	EjectButtonBounds      = image.Rect(136, 89, 158, 105)
	NowPlayingArtBounds    = image.Rect(6, 130, 32, 156)
	SearchInputBounds      = image.Rect(6, 160, 198, 178)
	SearchSubmitBounds     = image.Rect(200, 160, 234, 178)
	SearchClearBounds      = image.Rect(236, 160, 269, 178)
	ResultListBounds       = image.Rect(6, 180, 269, 324)
	BrowserPlayBounds      = image.Rect(6, 326, 54, 344)
	BrowserQueueBounds     = image.Rect(57, 326, 112, 344)
	BrowserImportAllBounds = image.Rect(115, 326, 185, 344)
	ArtModalBounds         = image.Rect(38, 124, 238, 324)
)

// placement is a sprite name and its destination in logical coordinates.
type placement struct {
	Sprite string
	At     image.Point
}

// mainPlacements is drawn back-to-front.
var mainPlacements = []placement{
	{Sprite: skin.MainBackground, At: image.Pt(0, 0)},
	{Sprite: skin.TitlebarActive, At: image.Pt(0, 0)},
	{Sprite: skin.PreviousButton, At: image.Pt(16, 88)},
	{Sprite: skin.PlayButton, At: image.Pt(39, 88)},
	{Sprite: skin.PauseButton, At: image.Pt(62, 88)},
	{Sprite: skin.StopButton, At: image.Pt(85, 88)},
	{Sprite: skin.NextButton, At: image.Pt(108, 88)},
	{Sprite: skin.EjectButton, At: EjectButtonBounds.Min},
}

// mainWidgets follows the classic main-window transport coordinates.
var mainWidgets = []Widget{
	{ID: WidgetPrevious, Bounds: image.Rect(16, 88, 39, 106)},
	{ID: WidgetPlay, Bounds: image.Rect(39, 88, 62, 106)},
	{ID: WidgetPause, Bounds: image.Rect(62, 88, 85, 106)},
	{ID: WidgetStop, Bounds: image.Rect(85, 88, 108, 106)},
	{ID: WidgetNext, Bounds: image.Rect(108, 88, 131, 106)},
	{ID: WidgetEject, Bounds: EjectButtonBounds},
}

var resultRowWidgets = []string{
	WidgetResultRow0,
	WidgetResultRow1,
	WidgetResultRow2,
	WidgetResultRow3,
	WidgetResultRow4,
	WidgetResultRow5,
}

func resultRowBounds(row int) image.Rectangle {
	y := ResultListBounds.Min.Y + row*ResultRowHeight
	return image.Rect(ResultListBounds.Min.X, y, ResultListBounds.Max.X, y+ResultRowHeight)
}

var browserWidgets = []Widget{
	{ID: WidgetNowPlayingArt, Bounds: NowPlayingArtBounds},
	{ID: WidgetSearchInput, Bounds: SearchInputBounds},
	{ID: WidgetSearchSubmit, Bounds: SearchSubmitBounds},
	{ID: WidgetSearchClear, Bounds: SearchClearBounds},
	{ID: WidgetResultRow0, Bounds: resultRowBounds(0)},
	{ID: WidgetResultRow1, Bounds: resultRowBounds(1)},
	{ID: WidgetResultRow2, Bounds: resultRowBounds(2)},
	{ID: WidgetResultRow3, Bounds: resultRowBounds(3)},
	{ID: WidgetResultRow4, Bounds: resultRowBounds(4)},
	{ID: WidgetResultRow5, Bounds: resultRowBounds(5)},
	{ID: WidgetBrowserPlay, Bounds: BrowserPlayBounds},
	{ID: WidgetBrowserQueue, Bounds: BrowserQueueBounds},
	{ID: WidgetBrowserImportAll, Bounds: BrowserImportAllBounds},
}

var allWidgets = append(append([]Widget(nil), mainWidgets...), browserWidgets...)
