package ui

import (
	"image"

	"ytm-wmp/internal/skin"
)

// Fixed logical size of the Phase 1 main window. All skin coordinates stay in
// this 1x space; scaling, if any, happens only at the draw boundary.
const (
	WindowWidth  = 275
	WindowHeight = 116
)

// Widget IDs used by the game loop.
const (
	WidgetPlay  = "play"
	WidgetPause = "pause"
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
	{Sprite: skin.PlayButton, At: image.Pt(39, 88)},
	{Sprite: skin.PauseButton, At: image.Pt(62, 88)},
}

// mainWidgets follows the classic main-window transport coordinates.
var mainWidgets = []Widget{
	{ID: WidgetPlay, Bounds: image.Rect(39, 88, 62, 106)},
	{ID: WidgetPause, Bounds: image.Rect(62, 88, 85, 106)},
}
