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
	{Sprite: skin.EjectButton, At: image.Pt(136, 89)},
}

// mainWidgets follows the classic main-window transport coordinates.
var mainWidgets = []Widget{
	{ID: WidgetPrevious, Bounds: image.Rect(16, 88, 39, 106)},
	{ID: WidgetPlay, Bounds: image.Rect(39, 88, 62, 106)},
	{ID: WidgetPause, Bounds: image.Rect(62, 88, 85, 106)},
	{ID: WidgetStop, Bounds: image.Rect(85, 88, 108, 106)},
	{ID: WidgetNext, Bounds: image.Rect(108, 88, 131, 106)},
	{ID: WidgetEject, Bounds: image.Rect(136, 89, 158, 105)},
}

var resultRowWidgets = []string{
	WidgetResultRow0,
	WidgetResultRow1,
	WidgetResultRow2,
	WidgetResultRow3,
	WidgetResultRow4,
	WidgetResultRow5,
}

var browserWidgets = []Widget{
	{ID: WidgetNowPlayingArt, Bounds: image.Rect(6, 130, 32, 156)},
	{ID: WidgetSearchInput, Bounds: image.Rect(6, 160, 198, 178)},
	{ID: WidgetSearchSubmit, Bounds: image.Rect(200, 160, 234, 178)},
	{ID: WidgetSearchClear, Bounds: image.Rect(236, 160, 269, 178)},
	{ID: WidgetResultRow0, Bounds: image.Rect(6, 180, 269, 204)},
	{ID: WidgetResultRow1, Bounds: image.Rect(6, 204, 269, 228)},
	{ID: WidgetResultRow2, Bounds: image.Rect(6, 228, 269, 252)},
	{ID: WidgetResultRow3, Bounds: image.Rect(6, 252, 269, 276)},
	{ID: WidgetResultRow4, Bounds: image.Rect(6, 276, 269, 300)},
	{ID: WidgetResultRow5, Bounds: image.Rect(6, 300, 269, 324)},
	{ID: WidgetBrowserPlay, Bounds: image.Rect(6, 326, 54, 344)},
	{ID: WidgetBrowserQueue, Bounds: image.Rect(57, 326, 112, 344)},
	{ID: WidgetBrowserImportAll, Bounds: image.Rect(115, 326, 185, 344)},
}

var allWidgets = append(append([]Widget(nil), mainWidgets...), browserWidgets...)
