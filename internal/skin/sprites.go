package skin

import "image"

// Sprite names match Webamp's skinSprites.ts where possible. Keeping the same
// names makes it straightforward to extend the table in Phase 4 without
// translating coordinate references.
const (
	MainBackground = "MAIN_WINDOW_BACKGROUND"
	TitlebarActive = "MAIN_TITLE_BAR_SELECTED"
	TitlebarIdle   = "MAIN_TITLE_BAR"

	PreviousButton       = "MAIN_PREVIOUS_BUTTON"
	PreviousButtonActive = "MAIN_PREVIOUS_BUTTON_ACTIVE"
	PlayButton           = "MAIN_PLAY_BUTTON"
	PlayButtonActive     = "MAIN_PLAY_BUTTON_ACTIVE"
	PauseButton          = "MAIN_PAUSE_BUTTON"
	PauseButtonActive    = "MAIN_PAUSE_BUTTON_ACTIVE"
	StopButton           = "MAIN_STOP_BUTTON"
	StopButtonActive     = "MAIN_STOP_BUTTON_ACTIVE"
	NextButton           = "MAIN_NEXT_BUTTON"
	NextButtonActive     = "MAIN_NEXT_BUTTON_ACTIVE"
	EjectButton          = "MAIN_EJECT_BUTTON"
	EjectButtonActive    = "MAIN_EJECT_BUTTON_ACTIVE"
)

// Sprite is a named rectangle inside one bitmap sheet.
type Sprite struct {
	Sheet string
	Src   image.Rectangle
}

// spriteDefs is the Phase 1 subset of the classic main-window sprite table.
// Coordinates come from Webamp's MIT-licensed canonical table, which matches
// what Winamp 2.x and real .wsz files expect.
var spriteDefs = map[string]Sprite{
	MainBackground: {"main.bmp", image.Rect(0, 0, 275, 116)},
	TitlebarActive: {"titlebar.bmp", image.Rect(27, 0, 302, 14)},
	TitlebarIdle:   {"titlebar.bmp", image.Rect(27, 15, 302, 29)},

	PreviousButton:       {"cbuttons.bmp", image.Rect(0, 0, 23, 18)},
	PreviousButtonActive: {"cbuttons.bmp", image.Rect(0, 18, 23, 36)},
	PlayButton:           {"cbuttons.bmp", image.Rect(23, 0, 46, 18)},
	PlayButtonActive:     {"cbuttons.bmp", image.Rect(23, 18, 46, 36)},
	PauseButton:          {"cbuttons.bmp", image.Rect(46, 0, 69, 18)},
	PauseButtonActive:    {"cbuttons.bmp", image.Rect(46, 18, 69, 36)},
	StopButton:           {"cbuttons.bmp", image.Rect(69, 0, 92, 18)},
	StopButtonActive:     {"cbuttons.bmp", image.Rect(69, 18, 92, 36)},
	NextButton:           {"cbuttons.bmp", image.Rect(92, 0, 115, 18)},
	NextButtonActive:     {"cbuttons.bmp", image.Rect(92, 18, 114, 36)},
	EjectButton:          {"cbuttons.bmp", image.Rect(114, 0, 136, 16)},
	EjectButtonActive:    {"cbuttons.bmp", image.Rect(114, 16, 136, 32)},
}

// Definition returns the source rectangle for a sprite name.
func Definition(name string) (Sprite, bool) {
	def, ok := spriteDefs[name]
	return def, ok
}

// Definitions returns a copy of the sprite table, useful for debug overlays
// and tests that should not mutate the package state.
func Definitions() map[string]Sprite {
	out := make(map[string]Sprite, len(spriteDefs))
	for name, def := range spriteDefs {
		out[name] = def
	}
	return out
}
