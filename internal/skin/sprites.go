package skin

import (
	"fmt"
	"image"
	"math"
	"unicode"
)

// Sprite names match Webamp's skinSprites.ts (captbaritone/webamp, MIT License;
// see THIRD_PARTY_NOTICES.md) where possible so coordinates map directly to
// classic Winamp 2.x sheets.
const (
	MainBackground = "MAIN_WINDOW_BACKGROUND"

	TitlebarActive        = "MAIN_TITLE_BAR_SELECTED"
	TitlebarIdle          = "MAIN_TITLE_BAR"
	TitlebarEasterActive  = "MAIN_EASTER_EGG_TITLE_BAR_SELECTED"
	TitlebarEasterIdle    = "MAIN_EASTER_EGG_TITLE_BAR"
	MainOptionsButton     = "MAIN_OPTIONS_BUTTON"
	MainOptionsActive     = "MAIN_OPTIONS_BUTTON_DEPRESSED"
	MainMinimizeButton    = "MAIN_MINIMIZE_BUTTON"
	MainMinimizeActive    = "MAIN_MINIMIZE_BUTTON_DEPRESSED"
	MainShadeButton       = "MAIN_SHADE_BUTTON"
	MainShadeActive       = "MAIN_SHADE_BUTTON_DEPRESSED"
	MainCloseButton       = "MAIN_CLOSE_BUTTON"
	MainCloseActive       = "MAIN_CLOSE_BUTTON_DEPRESSED"
	MainClutterBar        = "MAIN_CLUTTER_BAR_BACKGROUND"
	MainClutterDisabled   = "MAIN_CLUTTER_BAR_BACKGROUND_DISABLED"
	MainClutterOSelected  = "MAIN_CLUTTER_BAR_BUTTON_O_SELECTED"
	MainClutterASelected  = "MAIN_CLUTTER_BAR_BUTTON_A_SELECTED"
	MainClutterISelected  = "MAIN_CLUTTER_BAR_BUTTON_I_SELECTED"
	MainClutterDSelected  = "MAIN_CLUTTER_BAR_BUTTON_D_SELECTED"
	MainClutterVSelected  = "MAIN_CLUTTER_BAR_BUTTON_V_SELECTED"
	MainShadeBg           = "MAIN_SHADE_BACKGROUND"
	MainShadeBgSelected   = "MAIN_SHADE_BACKGROUND_SELECTED"
	MainShadeBtnSelected  = "MAIN_SHADE_BUTTON_SELECTED"
	MainShadeBtnDepressed = "MAIN_SHADE_BUTTON_SELECTED_DEPRESSED"
	MainShadePosBg        = "MAIN_SHADE_POSITION_BACKGROUND"
	MainShadePosThumb     = "MAIN_SHADE_POSITION_THUMB"
	MainShadePosThumbL    = "MAIN_SHADE_POSITION_THUMB_LEFT"
	MainShadePosThumbR    = "MAIN_SHADE_POSITION_THUMB_RIGHT"

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

	MainShuffleButton                 = "MAIN_SHUFFLE_BUTTON"
	MainShuffleButtonDepressed        = "MAIN_SHUFFLE_BUTTON_DEPRESSED"
	MainShuffleButtonSelected         = "MAIN_SHUFFLE_BUTTON_SELECTED"
	MainShuffleButtonSelectedDepress  = "MAIN_SHUFFLE_BUTTON_SELECTED_DEPRESSED"
	MainRepeatButton                  = "MAIN_REPEAT_BUTTON"
	MainRepeatButtonDepressed         = "MAIN_REPEAT_BUTTON_DEPRESSED"
	MainRepeatButtonSelected          = "MAIN_REPEAT_BUTTON_SELECTED"
	MainRepeatButtonSelectedDepressed = "MAIN_REPEAT_BUTTON_SELECTED_DEPRESSED"
	MainEQButton                      = "MAIN_EQ_BUTTON"
	MainEQButtonSelected              = "MAIN_EQ_BUTTON_SELECTED"
	MainEQButtonDepressed             = "MAIN_EQ_BUTTON_DEPRESSED"
	MainEQButtonDepressedSelected     = "MAIN_EQ_BUTTON_DEPRESSED_SELECTED"
	MainPlaylistButton                = "MAIN_PLAYLIST_BUTTON"
	MainPlaylistButtonSelected        = "MAIN_PLAYLIST_BUTTON_SELECTED"
	MainPlaylistButtonDepressed       = "MAIN_PLAYLIST_BUTTON_DEPRESSED"
	MainPlaylistButtonDepressedSelect = "MAIN_PLAYLIST_BUTTON_DEPRESSED_SELECTED"

	MainPositionBackground    = "MAIN_POSITION_SLIDER_BACKGROUND"
	MainPositionThumb         = "MAIN_POSITION_SLIDER_THUMB"
	MainPositionThumbSelected = "MAIN_POSITION_SLIDER_THUMB_SELECTED"

	MainVolumeBackground    = "MAIN_VOLUME_BACKGROUND"
	MainVolumeThumb         = "MAIN_VOLUME_THUMB"
	MainVolumeThumbSelected = "MAIN_VOLUME_THUMB_SELECTED"

	MainBalanceBackground  = "MAIN_BALANCE_BACKGROUND"
	MainBalanceThumb       = "MAIN_BALANCE_THUMB"
	MainBalanceThumbActive = "MAIN_BALANCE_THUMB_ACTIVE"

	MainPlayingIndicator    = "MAIN_PLAYING_INDICATOR"
	MainPausedIndicator     = "MAIN_PAUSED_INDICATOR"
	MainStoppedIndicator    = "MAIN_STOPPED_INDICATOR"
	MainNotWorkingIndicator = "MAIN_NOT_WORKING_INDICATOR"
	MainWorkingIndicator    = "MAIN_WORKING_INDICATOR"

	MainStereo         = "MAIN_STEREO"
	MainStereoSelected = "MAIN_STEREO_SELECTED"
	MainMono           = "MAIN_MONO"
	MainMonoSelected   = "MAIN_MONO_SELECTED"

	NoMinusSign   = "NO_MINUS_SIGN"
	MinusSign     = "MINUS_SIGN"
	NoMinusSignEx = "NO_MINUS_SIGN_EX"
	MinusSignEx   = "MINUS_SIGN_EX"

	PlaylistTopTile            = "PLAYLIST_TOP_TILE"
	PlaylistTopLeftCorner      = "PLAYLIST_TOP_LEFT_CORNER"
	PlaylistTitleBar           = "PLAYLIST_TITLE_BAR"
	PlaylistTopRightCorner     = "PLAYLIST_TOP_RIGHT_CORNER"
	PlaylistTopTileSelected    = "PLAYLIST_TOP_TILE_SELECTED"
	PlaylistTopLeftSelected    = "PLAYLIST_TOP_LEFT_SELECTED"
	PlaylistTitleBarSelected   = "PLAYLIST_TITLE_BAR_SELECTED"
	PlaylistTopRightSelected   = "PLAYLIST_TOP_RIGHT_CORNER_SELECTED"
	PlaylistLeftTile           = "PLAYLIST_LEFT_TILE"
	PlaylistRightTile          = "PLAYLIST_RIGHT_TILE"
	PlaylistBottomTile         = "PLAYLIST_BOTTOM_TILE"
	PlaylistBottomLeftCorner   = "PLAYLIST_BOTTOM_LEFT_CORNER"
	PlaylistBottomRightCorner  = "PLAYLIST_BOTTOM_RIGHT_CORNER"
	PlaylistVisualizerBg       = "PLAYLIST_VISUALIZER_BACKGROUND"
	PlaylistShadeBg            = "PLAYLIST_SHADE_BACKGROUND"
	PlaylistShadeBgLeft        = "PLAYLIST_SHADE_BACKGROUND_LEFT"
	PlaylistShadeBgRight       = "PLAYLIST_SHADE_BACKGROUND_RIGHT"
	PlaylistShadeBgRightSelect = "PLAYLIST_SHADE_BACKGROUND_RIGHT_SELECTED"
	PlaylistScrollHandle       = "PLAYLIST_SCROLL_HANDLE"
	PlaylistScrollHandleSelect = "PLAYLIST_SCROLL_HANDLE_SELECTED"
	PlaylistAddURL             = "PLAYLIST_ADD_URL"
	PlaylistAddURLSelected     = "PLAYLIST_ADD_URL_SELECTED"
	PlaylistAddDir             = "PLAYLIST_ADD_DIR"
	PlaylistAddDirSelected     = "PLAYLIST_ADD_DIR_SELECTED"
	PlaylistAddFile            = "PLAYLIST_ADD_FILE"
	PlaylistAddFileSelected    = "PLAYLIST_ADD_FILE_SELECTED"
	PlaylistRemoveAll          = "PLAYLIST_REMOVE_ALL"
	PlaylistRemoveAllSelected  = "PLAYLIST_REMOVE_ALL_SELECTED"
	PlaylistCrop               = "PLAYLIST_CROP"
	PlaylistCropSelected       = "PLAYLIST_CROP_SELECTED"
	PlaylistRemoveSel          = "PLAYLIST_REMOVE_SELECTED"
	PlaylistRemoveSelSelected  = "PLAYLIST_REMOVE_SELECTED_SELECTED"
	PlaylistRemoveMisc         = "PLAYLIST_REMOVE_MISC"
	PlaylistRemoveMiscSelected = "PLAYLIST_REMOVE_MISC_SELECTED"
	PlaylistInvertSel          = "PLAYLIST_INVERT_SELECTION"
	PlaylistInvertSelSelected  = "PLAYLIST_INVERT_SELECTION_SELECTED"
	PlaylistSelectZero         = "PLAYLIST_SELECT_ZERO"
	PlaylistSelectZeroSelected = "PLAYLIST_SELECT_ZERO_SELECTED"
	PlaylistSelectAll          = "PLAYLIST_SELECT_ALL"
	PlaylistSelectAllSelected  = "PLAYLIST_SELECT_ALL_SELECTED"
	PlaylistSortList           = "PLAYLIST_SORT_LIST"
	PlaylistSortListSelected   = "PLAYLIST_SORT_LIST_SELECTED"
	PlaylistFileInfo           = "PLAYLIST_FILE_INFO"
	PlaylistFileInfoSelected   = "PLAYLIST_FILE_INFO_SELECTED"
	PlaylistMiscOptions        = "PLAYLIST_MISC_OPTIONS"
	PlaylistMiscOptionsSelect  = "PLAYLIST_MISC_OPTIONS_SELECTED"
	PlaylistNewList            = "PLAYLIST_NEW_LIST"
	PlaylistNewListSelected    = "PLAYLIST_NEW_LIST_SELECTED"
	PlaylistSaveList           = "PLAYLIST_SAVE_LIST"
	PlaylistSaveListSelected   = "PLAYLIST_SAVE_LIST_SELECTED"
	PlaylistLoadList           = "PLAYLIST_LOAD_LIST"
	PlaylistLoadListSelected   = "PLAYLIST_LOAD_LIST_SELECTED"
	PlaylistAddMenuBar         = "PLAYLIST_ADD_MENU_BAR"
	PlaylistRemoveMenuBar      = "PLAYLIST_REMOVE_MENU_BAR"
	PlaylistSelectMenuBar      = "PLAYLIST_SELECT_MENU_BAR"
	PlaylistMiscMenuBar        = "PLAYLIST_MISC_MENU_BAR"
	PlaylistListBar            = "PLAYLIST_LIST_BAR"
	PlaylistCloseSelected      = "PLAYLIST_CLOSE_SELECTED"
	PlaylistCollapseSelected   = "PLAYLIST_COLLAPSE_SELECTED"
	PlaylistExpandSelected     = "PLAYLIST_EXPAND_SELECTED"

	EQWindowBackground         = "EQ_WINDOW_BACKGROUND"
	EQTitleBar                 = "EQ_TITLE_BAR"
	EQTitleBarSelected         = "EQ_TITLE_BAR_SELECTED"
	EQSliderBackground         = "EQ_SLIDER_BACKGROUND"
	EQSliderThumb              = "EQ_SLIDER_THUMB"
	EQSliderThumbSelected      = "EQ_SLIDER_THUMB_SELECTED"
	EQCloseButton              = "EQ_CLOSE_BUTTON"
	EQCloseButtonActive        = "EQ_CLOSE_BUTTON_ACTIVE"
	EQMaximizeFallback         = "EQ_MAXIMIZE_BUTTON_ACTIVE_FALLBACK"
	EQOnButton                 = "EQ_ON_BUTTON"
	EQOnButtonDepressed        = "EQ_ON_BUTTON_DEPRESSED"
	EQOnButtonSelected         = "EQ_ON_BUTTON_SELECTED"
	EQOnButtonSelectedDepress  = "EQ_ON_BUTTON_SELECTED_DEPRESSED"
	EQAutoButton               = "EQ_AUTO_BUTTON"
	EQAutoButtonDepressed      = "EQ_AUTO_BUTTON_DEPRESSED"
	EQAutoButtonSelected       = "EQ_AUTO_BUTTON_SELECTED"
	EQAutoButtonSelectedDepres = "EQ_AUTO_BUTTON_SELECTED_DEPRESSED"
	EQGraphBackground          = "EQ_GRAPH_BACKGROUND"
	EQGraphLineColors          = "EQ_GRAPH_LINE_COLORS"
	EQPresetsButton            = "EQ_PRESETS_BUTTON"
	EQPresetsButtonSelected    = "EQ_PRESETS_BUTTON_SELECTED"
	EQPreampLine               = "EQ_PREAMP_LINE"

	EQShadeBgSelected      = "EQ_SHADE_BACKGROUND_SELECTED"
	EQShadeBg              = "EQ_SHADE_BACKGROUND"
	EQShadeVolSliderLeft   = "EQ_SHADE_VOLUME_SLIDER_LEFT"
	EQShadeVolSliderCenter = "EQ_SHADE_VOLUME_SLIDER_CENTER"
	EQShadeVolSliderRight  = "EQ_SHADE_VOLUME_SLIDER_RIGHT"
	EQShadeBalSliderLeft   = "EQ_SHADE_BALANCE_SLIDER_LEFT"
	EQShadeBalSliderCenter = "EQ_SHADE_BALANCE_SLIDER_CENTER"
	EQShadeBalSliderRight  = "EQ_SHADE_BALANCE_SLIDER_RIGHT"
	EQMaximizeButtonActive = "EQ_MAXIMIZE_BUTTON_ACTIVE"
	EQMinimizeButtonActive = "EQ_MINIMIZE_BUTTON_ACTIVE"
	EQShadeCloseButton     = "EQ_SHADE_CLOSE_BUTTON"
	EQShadeCloseActive     = "EQ_SHADE_CLOSE_BUTTON_ACTIVE"

	GenTopLeftSelected       = "GEN_TOP_LEFT_SELECTED"
	GenTopLeftEndSelected    = "GEN_TOP_LEFT_END_SELECTED"
	GenTopCenterFillSelected = "GEN_TOP_CENTER_FILL_SELECTED"
	GenTopRightEndSelected   = "GEN_TOP_RIGHT_END_SELECTED"
	GenTopLRFillSelected     = "GEN_TOP_LEFT_RIGHT_FILL_SELECTED"
	GenTopRightSelected      = "GEN_TOP_RIGHT_SELECTED"
	GenTopLeft               = "GEN_TOP_LEFT"
	GenTopLeftEnd            = "GEN_TOP_LEFT_END"
	GenTopCenterFill         = "GEN_TOP_CENTER_FILL"
	GenTopRightEnd           = "GEN_TOP_RIGHT_END"
	GenTopLRFill             = "GEN_TOP_LEFT_RIGHT_FILL"
	GenTopRight              = "GEN_TOP_RIGHT"
	GenBottomLeft            = "GEN_BOTTOM_LEFT"
	GenBottomRight           = "GEN_BOTTOM_RIGHT"
	GenBottomFill            = "GEN_BOTTOM_FILL"
	GenMiddleLeft            = "GEN_MIDDLE_LEFT"
	GenMiddleLeftBottom      = "GEN_MIDDLE_LEFT_BOTTOM"
	GenMiddleRight           = "GEN_MIDDLE_RIGHT"
	GenMiddleRightBottom     = "GEN_MIDDLE_RIGHT_BOTTOM"
	GenCloseSelected         = "GEN_CLOSE_SELECTED"
)

const (
	charCellWidth  = 5
	charCellHeight = 6
)

// Sprite is a named rectangle inside one bitmap sheet.
type Sprite struct {
	Sheet string
	Src   image.Rectangle
}

// fontLookup maps runes in text.bmp to (row, col) cells, ported from Webamp's FONT_LOOKUP.
var fontLookup = map[rune][2]int{
	'a': {0, 0}, 'b': {0, 1}, 'c': {0, 2}, 'd': {0, 3}, 'e': {0, 4},
	'f': {0, 5}, 'g': {0, 6}, 'h': {0, 7}, 'i': {0, 8}, 'j': {0, 9},
	'k': {0, 10}, 'l': {0, 11}, 'm': {0, 12}, 'n': {0, 13}, 'o': {0, 14},
	'p': {0, 15}, 'q': {0, 16}, 'r': {0, 17}, 's': {0, 18}, 't': {0, 19},
	'u': {0, 20}, 'v': {0, 21}, 'w': {0, 22}, 'x': {0, 23}, 'y': {0, 24},
	'z': {0, 25}, '"': {0, 26}, '@': {0, 27}, ' ': {0, 30},

	'0': {1, 0}, '1': {1, 1}, '2': {1, 2}, '3': {1, 3}, '4': {1, 4},
	'5': {1, 5}, '6': {1, 6}, '7': {1, 7}, '8': {1, 8}, '9': {1, 9},
	'…': {1, 10}, '.': {1, 11}, ':': {1, 12}, '(': {1, 13}, ')': {1, 14},
	'-': {1, 15}, '\'': {1, 16}, '!': {1, 17}, '_': {1, 18}, '+': {1, 19},
	'\\': {1, 20}, '/': {1, 21}, '[': {1, 22}, ']': {1, 23}, '^': {1, 24},
	'&': {1, 25}, '%': {1, 26}, ',': {1, 27}, '=': {1, 28}, '$': {1, 29},
	'#': {1, 30},

	'Å': {2, 0}, 'Ö': {2, 1}, 'Ä': {2, 2}, '?': {2, 3}, '*': {2, 4},
	'<': {1, 22}, '>': {1, 23}, '{': {1, 22}, '}': {1, 23},
}

// spriteDefs is the canonical classic Winamp 2.x sprite table, ported from
// Webamp's MIT-licensed skinSprites.ts.
var spriteDefs = func() map[string]Sprite {
	m := map[string]Sprite{
		MainBackground: {"main.bmp", image.Rect(0, 0, 275, 116)},

		TitlebarActive:        {"titlebar.bmp", image.Rect(27, 0, 302, 14)},
		TitlebarIdle:          {"titlebar.bmp", image.Rect(27, 15, 302, 29)},
		TitlebarEasterActive:  {"titlebar.bmp", image.Rect(27, 57, 302, 71)},
		TitlebarEasterIdle:    {"titlebar.bmp", image.Rect(27, 72, 302, 86)},
		MainOptionsButton:     {"titlebar.bmp", image.Rect(0, 0, 9, 9)},
		MainOptionsActive:     {"titlebar.bmp", image.Rect(0, 9, 9, 18)},
		MainMinimizeButton:    {"titlebar.bmp", image.Rect(9, 0, 18, 9)},
		MainMinimizeActive:    {"titlebar.bmp", image.Rect(9, 9, 18, 18)},
		MainShadeButton:       {"titlebar.bmp", image.Rect(0, 18, 9, 27)},
		MainShadeActive:       {"titlebar.bmp", image.Rect(9, 18, 18, 27)},
		MainCloseButton:       {"titlebar.bmp", image.Rect(18, 0, 27, 9)},
		MainCloseActive:       {"titlebar.bmp", image.Rect(18, 9, 27, 18)},
		MainClutterBar:        {"titlebar.bmp", image.Rect(304, 0, 312, 43)},
		MainClutterDisabled:   {"titlebar.bmp", image.Rect(312, 0, 320, 43)},
		MainClutterOSelected:  {"titlebar.bmp", image.Rect(304, 47, 312, 55)},
		MainClutterASelected:  {"titlebar.bmp", image.Rect(312, 55, 320, 62)},
		MainClutterISelected:  {"titlebar.bmp", image.Rect(320, 62, 328, 69)},
		MainClutterDSelected:  {"titlebar.bmp", image.Rect(328, 69, 336, 77)},
		MainClutterVSelected:  {"titlebar.bmp", image.Rect(336, 77, 344, 84)},
		MainShadeBg:           {"titlebar.bmp", image.Rect(27, 42, 302, 56)},
		MainShadeBgSelected:   {"titlebar.bmp", image.Rect(27, 29, 302, 43)},
		MainShadeBtnSelected:  {"titlebar.bmp", image.Rect(0, 27, 9, 36)},
		MainShadeBtnDepressed: {"titlebar.bmp", image.Rect(9, 27, 18, 36)},
		MainShadePosBg:        {"titlebar.bmp", image.Rect(0, 36, 17, 43)},
		MainShadePosThumb:     {"titlebar.bmp", image.Rect(20, 36, 23, 43)},
		MainShadePosThumbL:    {"titlebar.bmp", image.Rect(17, 36, 20, 43)},
		MainShadePosThumbR:    {"titlebar.bmp", image.Rect(23, 36, 26, 43)},

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

		MainShuffleButton:                 {"shufrep.bmp", image.Rect(28, 0, 75, 15)},
		MainShuffleButtonDepressed:        {"shufrep.bmp", image.Rect(28, 15, 75, 30)},
		MainShuffleButtonSelected:         {"shufrep.bmp", image.Rect(28, 30, 75, 45)},
		MainShuffleButtonSelectedDepress:  {"shufrep.bmp", image.Rect(28, 45, 75, 60)},
		MainRepeatButton:                  {"shufrep.bmp", image.Rect(0, 0, 28, 15)},
		MainRepeatButtonDepressed:         {"shufrep.bmp", image.Rect(0, 15, 28, 30)},
		MainRepeatButtonSelected:          {"shufrep.bmp", image.Rect(0, 30, 28, 45)},
		MainRepeatButtonSelectedDepressed: {"shufrep.bmp", image.Rect(0, 45, 28, 60)},
		MainEQButton:                      {"shufrep.bmp", image.Rect(0, 61, 23, 73)},
		MainEQButtonSelected:              {"shufrep.bmp", image.Rect(0, 73, 23, 85)},
		MainEQButtonDepressed:             {"shufrep.bmp", image.Rect(46, 61, 69, 73)},
		MainEQButtonDepressedSelected:     {"shufrep.bmp", image.Rect(46, 73, 69, 85)},
		MainPlaylistButton:                {"shufrep.bmp", image.Rect(23, 61, 46, 73)},
		MainPlaylistButtonSelected:        {"shufrep.bmp", image.Rect(23, 73, 46, 85)},
		MainPlaylistButtonDepressed:       {"shufrep.bmp", image.Rect(69, 61, 92, 73)},
		MainPlaylistButtonDepressedSelect: {"shufrep.bmp", image.Rect(69, 73, 92, 85)},

		MainPositionBackground:    {"posbar.bmp", image.Rect(0, 0, 248, 10)},
		MainPositionThumb:         {"posbar.bmp", image.Rect(248, 0, 277, 10)},
		MainPositionThumbSelected: {"posbar.bmp", image.Rect(278, 0, 307, 10)},

		MainVolumeBackground:    {"volume.bmp", image.Rect(0, 0, 68, 420)},
		MainVolumeThumb:         {"volume.bmp", image.Rect(15, 422, 29, 433)},
		MainVolumeThumbSelected: {"volume.bmp", image.Rect(0, 422, 14, 433)},

		MainBalanceBackground:  {"balance.bmp", image.Rect(9, 0, 47, 420)},
		MainBalanceThumb:       {"balance.bmp", image.Rect(15, 422, 29, 433)},
		MainBalanceThumbActive: {"balance.bmp", image.Rect(0, 422, 14, 433)},

		MainPlayingIndicator:    {"playpaus.bmp", image.Rect(0, 0, 9, 9)},
		MainPausedIndicator:     {"playpaus.bmp", image.Rect(9, 0, 18, 9)},
		MainStoppedIndicator:    {"playpaus.bmp", image.Rect(18, 0, 27, 9)},
		MainNotWorkingIndicator: {"playpaus.bmp", image.Rect(36, 0, 45, 9)},
		MainWorkingIndicator:    {"playpaus.bmp", image.Rect(39, 0, 48, 9)},

		MainStereo:         {"monoster.bmp", image.Rect(0, 12, 29, 24)},
		MainStereoSelected: {"monoster.bmp", image.Rect(0, 0, 29, 12)},
		MainMono:           {"monoster.bmp", image.Rect(29, 12, 56, 24)},
		MainMonoSelected:   {"monoster.bmp", image.Rect(29, 0, 56, 12)},

		NoMinusSign:   {"numbers.bmp", image.Rect(9, 6, 14, 7)},
		MinusSign:     {"numbers.bmp", image.Rect(20, 6, 25, 7)},
		NoMinusSignEx: {"nums_ex.bmp", image.Rect(90, 0, 99, 13)},
		MinusSignEx:   {"nums_ex.bmp", image.Rect(99, 0, 108, 13)},

		PlaylistTopTile:            {"pledit.bmp", image.Rect(127, 21, 152, 41)},
		PlaylistTopLeftCorner:      {"pledit.bmp", image.Rect(0, 21, 25, 41)},
		PlaylistTitleBar:           {"pledit.bmp", image.Rect(26, 21, 126, 41)},
		PlaylistTopRightCorner:     {"pledit.bmp", image.Rect(153, 21, 178, 41)},
		PlaylistTopTileSelected:    {"pledit.bmp", image.Rect(127, 0, 152, 20)},
		PlaylistTopLeftSelected:    {"pledit.bmp", image.Rect(0, 0, 25, 20)},
		PlaylistTitleBarSelected:   {"pledit.bmp", image.Rect(26, 0, 126, 20)},
		PlaylistTopRightSelected:   {"pledit.bmp", image.Rect(153, 0, 178, 20)},
		PlaylistLeftTile:           {"pledit.bmp", image.Rect(0, 42, 12, 71)},
		PlaylistRightTile:          {"pledit.bmp", image.Rect(31, 42, 51, 71)},
		PlaylistBottomTile:         {"pledit.bmp", image.Rect(179, 0, 204, 38)},
		PlaylistBottomLeftCorner:   {"pledit.bmp", image.Rect(0, 72, 125, 110)},
		PlaylistBottomRightCorner:  {"pledit.bmp", image.Rect(126, 72, 276, 110)},
		PlaylistVisualizerBg:       {"pledit.bmp", image.Rect(205, 0, 280, 38)},
		PlaylistShadeBg:            {"pledit.bmp", image.Rect(72, 57, 97, 71)},
		PlaylistShadeBgLeft:        {"pledit.bmp", image.Rect(72, 42, 97, 56)},
		PlaylistShadeBgRight:       {"pledit.bmp", image.Rect(99, 57, 149, 71)},
		PlaylistShadeBgRightSelect: {"pledit.bmp", image.Rect(99, 42, 149, 56)},
		PlaylistScrollHandle:       {"pledit.bmp", image.Rect(52, 53, 60, 71)},
		PlaylistScrollHandleSelect: {"pledit.bmp", image.Rect(61, 53, 69, 71)},
		PlaylistAddURL:             {"pledit.bmp", image.Rect(0, 111, 22, 129)},
		PlaylistAddURLSelected:     {"pledit.bmp", image.Rect(23, 111, 45, 129)},
		PlaylistAddDir:             {"pledit.bmp", image.Rect(0, 130, 22, 148)},
		PlaylistAddDirSelected:     {"pledit.bmp", image.Rect(23, 130, 45, 148)},
		PlaylistAddFile:            {"pledit.bmp", image.Rect(0, 149, 22, 167)},
		PlaylistAddFileSelected:    {"pledit.bmp", image.Rect(23, 149, 45, 167)},
		PlaylistRemoveAll:          {"pledit.bmp", image.Rect(54, 111, 76, 129)},
		PlaylistRemoveAllSelected:  {"pledit.bmp", image.Rect(77, 111, 99, 129)},
		PlaylistCrop:               {"pledit.bmp", image.Rect(54, 130, 76, 148)},
		PlaylistCropSelected:       {"pledit.bmp", image.Rect(77, 130, 99, 148)},
		PlaylistRemoveSel:          {"pledit.bmp", image.Rect(54, 149, 76, 167)},
		PlaylistRemoveSelSelected:  {"pledit.bmp", image.Rect(77, 149, 99, 167)},
		PlaylistRemoveMisc:         {"pledit.bmp", image.Rect(54, 168, 76, 186)},
		PlaylistRemoveMiscSelected: {"pledit.bmp", image.Rect(77, 168, 99, 186)},
		PlaylistInvertSel:          {"pledit.bmp", image.Rect(104, 111, 126, 129)},
		PlaylistInvertSelSelected:  {"pledit.bmp", image.Rect(127, 111, 149, 129)},
		PlaylistSelectZero:         {"pledit.bmp", image.Rect(104, 130, 126, 148)},
		PlaylistSelectZeroSelected: {"pledit.bmp", image.Rect(127, 130, 149, 148)},
		PlaylistSelectAll:          {"pledit.bmp", image.Rect(104, 149, 126, 167)},
		PlaylistSelectAllSelected:  {"pledit.bmp", image.Rect(127, 149, 149, 167)},
		PlaylistSortList:           {"pledit.bmp", image.Rect(154, 111, 176, 129)},
		PlaylistSortListSelected:   {"pledit.bmp", image.Rect(177, 111, 199, 129)},
		PlaylistFileInfo:           {"pledit.bmp", image.Rect(154, 130, 176, 148)},
		PlaylistFileInfoSelected:   {"pledit.bmp", image.Rect(177, 130, 199, 148)},
		PlaylistMiscOptions:        {"pledit.bmp", image.Rect(154, 149, 176, 167)},
		PlaylistMiscOptionsSelect:  {"pledit.bmp", image.Rect(177, 149, 199, 167)},
		PlaylistNewList:            {"pledit.bmp", image.Rect(204, 111, 226, 129)},
		PlaylistNewListSelected:    {"pledit.bmp", image.Rect(227, 111, 249, 129)},
		PlaylistSaveList:           {"pledit.bmp", image.Rect(204, 130, 226, 148)},
		PlaylistSaveListSelected:   {"pledit.bmp", image.Rect(227, 130, 249, 148)},
		PlaylistLoadList:           {"pledit.bmp", image.Rect(204, 149, 226, 167)},
		PlaylistLoadListSelected:   {"pledit.bmp", image.Rect(227, 149, 249, 167)},
		PlaylistAddMenuBar:         {"pledit.bmp", image.Rect(48, 111, 51, 165)},
		PlaylistRemoveMenuBar:      {"pledit.bmp", image.Rect(100, 111, 103, 183)},
		PlaylistSelectMenuBar:      {"pledit.bmp", image.Rect(150, 111, 153, 165)},
		PlaylistMiscMenuBar:        {"pledit.bmp", image.Rect(200, 111, 203, 165)},
		PlaylistListBar:            {"pledit.bmp", image.Rect(250, 111, 253, 165)},
		PlaylistCloseSelected:      {"pledit.bmp", image.Rect(52, 42, 61, 51)},
		PlaylistCollapseSelected:   {"pledit.bmp", image.Rect(62, 42, 71, 51)},
		PlaylistExpandSelected:     {"pledit.bmp", image.Rect(150, 42, 159, 51)},

		EQWindowBackground:         {"eqmain.bmp", image.Rect(0, 0, 275, 116)},
		EQTitleBar:                 {"eqmain.bmp", image.Rect(0, 149, 275, 163)},
		EQTitleBarSelected:         {"eqmain.bmp", image.Rect(0, 134, 275, 148)},
		EQSliderBackground:         {"eqmain.bmp", image.Rect(13, 164, 222, 293)},
		EQSliderThumb:              {"eqmain.bmp", image.Rect(0, 164, 11, 175)},
		EQSliderThumbSelected:      {"eqmain.bmp", image.Rect(0, 176, 11, 187)},
		EQCloseButton:              {"eqmain.bmp", image.Rect(0, 116, 9, 125)},
		EQCloseButtonActive:        {"eqmain.bmp", image.Rect(0, 125, 9, 134)},
		EQMaximizeFallback:         {"eqmain.bmp", image.Rect(254, 152, 263, 161)},
		EQOnButton:                 {"eqmain.bmp", image.Rect(10, 119, 36, 131)},
		EQOnButtonDepressed:        {"eqmain.bmp", image.Rect(128, 119, 154, 131)},
		EQOnButtonSelected:         {"eqmain.bmp", image.Rect(69, 119, 95, 131)},
		EQOnButtonSelectedDepress:  {"eqmain.bmp", image.Rect(187, 119, 213, 131)},
		EQAutoButton:               {"eqmain.bmp", image.Rect(36, 119, 68, 131)},
		EQAutoButtonDepressed:      {"eqmain.bmp", image.Rect(154, 119, 186, 131)},
		EQAutoButtonSelected:       {"eqmain.bmp", image.Rect(95, 119, 127, 131)},
		EQAutoButtonSelectedDepres: {"eqmain.bmp", image.Rect(213, 119, 245, 131)},
		EQGraphBackground:          {"eqmain.bmp", image.Rect(0, 294, 113, 313)},
		EQGraphLineColors:          {"eqmain.bmp", image.Rect(115, 294, 116, 313)},
		EQPresetsButton:            {"eqmain.bmp", image.Rect(224, 164, 268, 176)},
		EQPresetsButtonSelected:    {"eqmain.bmp", image.Rect(224, 176, 268, 188)},
		EQPreampLine:               {"eqmain.bmp", image.Rect(0, 314, 113, 315)},

		EQShadeBgSelected:      {"eq_ex.bmp", image.Rect(0, 0, 275, 14)},
		EQShadeBg:              {"eq_ex.bmp", image.Rect(0, 15, 275, 29)},
		EQShadeVolSliderLeft:   {"eq_ex.bmp", image.Rect(1, 30, 4, 37)},
		EQShadeVolSliderCenter: {"eq_ex.bmp", image.Rect(4, 30, 7, 37)},
		EQShadeVolSliderRight:  {"eq_ex.bmp", image.Rect(7, 30, 10, 37)},
		EQShadeBalSliderLeft:   {"eq_ex.bmp", image.Rect(11, 30, 14, 37)},
		EQShadeBalSliderCenter: {"eq_ex.bmp", image.Rect(14, 30, 17, 37)},
		EQShadeBalSliderRight:  {"eq_ex.bmp", image.Rect(17, 30, 20, 37)},
		EQMaximizeButtonActive: {"eq_ex.bmp", image.Rect(1, 38, 10, 47)},
		EQMinimizeButtonActive: {"eq_ex.bmp", image.Rect(1, 47, 10, 56)},
		EQShadeCloseButton:     {"eq_ex.bmp", image.Rect(11, 38, 20, 47)},
		EQShadeCloseActive:     {"eq_ex.bmp", image.Rect(11, 47, 20, 56)},

		GenTopLeftSelected:       {"gen.bmp", image.Rect(0, 0, 25, 20)},
		GenTopLeftEndSelected:    {"gen.bmp", image.Rect(26, 0, 51, 20)},
		GenTopCenterFillSelected: {"gen.bmp", image.Rect(52, 0, 77, 20)},
		GenTopRightEndSelected:   {"gen.bmp", image.Rect(78, 0, 103, 20)},
		GenTopLRFillSelected:     {"gen.bmp", image.Rect(104, 0, 129, 20)},
		GenTopRightSelected:      {"gen.bmp", image.Rect(130, 0, 155, 20)},
		GenTopLeft:               {"gen.bmp", image.Rect(0, 21, 25, 41)},
		GenTopLeftEnd:            {"gen.bmp", image.Rect(26, 21, 51, 41)},
		GenTopCenterFill:         {"gen.bmp", image.Rect(52, 21, 77, 41)},
		GenTopRightEnd:           {"gen.bmp", image.Rect(78, 21, 103, 41)},
		GenTopLRFill:             {"gen.bmp", image.Rect(104, 21, 129, 41)},
		GenTopRight:              {"gen.bmp", image.Rect(130, 21, 155, 41)},
		GenBottomLeft:            {"gen.bmp", image.Rect(0, 42, 125, 56)},
		GenBottomRight:           {"gen.bmp", image.Rect(0, 57, 125, 71)},
		GenBottomFill:            {"gen.bmp", image.Rect(127, 72, 152, 86)},
		GenMiddleLeft:            {"gen.bmp", image.Rect(127, 42, 138, 71)},
		GenMiddleLeftBottom:      {"gen.bmp", image.Rect(158, 42, 169, 66)},
		GenMiddleRight:           {"gen.bmp", image.Rect(139, 42, 147, 71)},
		GenMiddleRightBottom:     {"gen.bmp", image.Rect(170, 42, 178, 66)},
		GenCloseSelected:         {"gen.bmp", image.Rect(148, 42, 157, 51)},
	}

	for d := 0; d <= 9; d++ {
		x := d * 9
		m[fmt.Sprintf("DIGIT_%d", d)] = Sprite{Sheet: "numbers.bmp", Src: image.Rect(x, 0, x+9, 13)}
		m[fmt.Sprintf("DIGIT_%d_EX", d)] = Sprite{Sheet: "nums_ex.bmp", Src: image.Rect(x, 0, x+9, 13)}
	}
	for r, pos := range fontLookup {
		x := pos[1] * charCellWidth
		y := pos[0] * charCellHeight
		m[CharacterSpriteName(r)] = Sprite{
			Sheet: "text.bmp",
			Src:   image.Rect(x, y, x+charCellWidth, y+charCellHeight),
		}
	}
	return m
}()

// CharacterSpriteName returns the canonical Webamp sprite name for a character glyph.
func CharacterSpriteName(r rune) string {
	return fmt.Sprintf("CHARACTER_%d", r)
}

// FontGlyphRect returns the 5x6 source rectangle inside text.bmp for r.
// ASCII uppercase letters fold to lowercase; unsupported runes return false.
func FontGlyphRect(r rune) (image.Rectangle, bool) {
	if r >= 'A' && r <= 'Z' {
		r = unicode.ToLower(r)
	}
	pos, ok := fontLookup[r]
	if !ok {
		return image.Rectangle{}, false
	}
	x := pos[1] * charCellWidth
	y := pos[0] * charCellHeight
	return image.Rect(x, y, x+charCellWidth, y+charCellHeight), true
}

// FontGlyphRunes returns every rune mapped in text.bmp.
func FontGlyphRunes() []rune {
	out := make([]rune, 0, len(fontLookup))
	for r := range fontLookup {
		out = append(out, r)
	}
	return out
}

// NumberSpriteName returns the sprite name for a single digit 0..9.
func NumberSpriteName(digit int, extended bool) string {
	if digit < 0 {
		digit = 0
	} else if digit > 9 {
		digit = 9
	}
	if extended {
		return fmt.Sprintf("DIGIT_%d_EX", digit)
	}
	return fmt.Sprintf("DIGIT_%d", digit)
}

// MinusSpriteName returns the minus-sign sprite name for numbers.bmp or nums_ex.bmp.
func MinusSpriteName(extended bool) string {
	if extended {
		return MinusSignEx
	}
	return MinusSign
}

// VolumeFrameRect returns the 68x13 frame inside volume.bmp for volume in [0, 1].
func VolumeFrameRect(volume float64) image.Rectangle {
	v := clamp01(volume)
	frame := int(math.Round(v * 27))
	if frame < 0 {
		frame = 0
	} else if frame > 27 {
		frame = 27
	}
	y := frame * 15
	return image.Rect(0, y, 68, y+13)
}

// BalanceFrameRect returns the 38x13 frame inside balance.bmp for balance in [-1, 1].
func BalanceFrameRect(balance float64) image.Rectangle {
	b := math.Abs(balance)
	if b > 1 {
		b = 1
	}
	frame := int(math.Floor(b * 27))
	if frame < 0 {
		frame = 0
	} else if frame > 27 {
		frame = 27
	}
	y := frame * 15
	return image.Rect(9, y, 47, y+13)
}

// EQSliderFrameRect returns the 14x63 frame inside eqmain.bmp for slider value in [0, 100].
func EQSliderFrameRect(value float64) image.Rectangle {
	if value < 0 {
		value = 0
	} else if value > 100 {
		value = 100
	}
	frame := int(math.Round((value / 100.0) * 27))
	if frame < 0 {
		frame = 0
	} else if frame > 27 {
		frame = 27
	}
	col := frame % 14
	row := frame / 14
	x := 13 + col*15
	y := 164 + row*65
	return image.Rect(x, y, x+14, y+63)
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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
