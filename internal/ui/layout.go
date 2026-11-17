package ui

import (
	"image"

	"github.com/rroy-ar/ytm-wmp/internal/catalog"
	"github.com/rroy-ar/ytm-wmp/internal/skin"
)

// Fixed logical dimensions in 1x skin coordinates.
// Scaling, if any, happens only at the draw boundary.
const (
	MainWindowWidth  = 275
	MainWindowHeight = 116
	EQWindowHeight   = 116
	BrowserHeight    = 232
	PlaylistHeight   = 232

	WindowWidth  = MainWindowWidth
	WindowHeight = MainWindowHeight + BrowserHeight

	VisibleResultRows   = 6
	ResultRowHeight     = 22
	VisiblePlaylistRows = 13
	PlaylistRowHeight   = 13
)

// Widget IDs used by the game loop.
const (
	WidgetPrevious = "previous"
	WidgetPlay     = "play"
	WidgetPause    = "pause"
	WidgetStop     = "stop"
	WidgetNext     = "next"
	WidgetEject    = "eject"

	WidgetMainClose    = "main.close"
	WidgetMainTime     = "main.time"
	WidgetMainVolume   = "main.volume"
	WidgetMainBalance  = "main.balance"
	WidgetMainEQ       = "main.eq"
	WidgetMainPL       = "main.pl"
	WidgetMainPosition = "main.position"
	WidgetMainShuffle  = "main.shuffle"
	WidgetMainRepeat   = "main.repeat"
	WidgetMainSkin     = "main.skin"

	WidgetEQClose   = "eq.close"
	WidgetEQOn      = "eq.on"
	WidgetEQAuto    = "eq.auto"
	WidgetEQPresets = "eq.presets"
	WidgetEQPlus12  = "eq.plus12"
	WidgetEQZero    = "eq.zero"
	WidgetEQMinus12 = "eq.minus12"
	WidgetEQPreamp  = "eq.preamp"

	WidgetPlaylistClose     = "playlist.close"
	WidgetPlaylistScrollbar = "playlist.scrollbar"
	WidgetPlaylistAdd       = "playlist.add"
	WidgetPlaylistRem       = "playlist.rem"
	WidgetPlaylistSel       = "playlist.sel"
	WidgetPlaylistMisc      = "playlist.misc"
	WidgetPlaylistList      = "playlist.list"
	WidgetPlaylistMiniPrev  = "playlist.mini.prev"
	WidgetPlaylistMiniPlay  = "playlist.mini.play"
	WidgetPlaylistMiniPause = "playlist.mini.pause"
	WidgetPlaylistMiniStop  = "playlist.mini.stop"
	WidgetPlaylistMiniNext  = "playlist.mini.next"
	WidgetPlaylistMiniEject = "playlist.mini.eject"

	WidgetPLMenuItem0 = "playlist.menu.0"
	WidgetPLMenuItem1 = "playlist.menu.1"
	WidgetPLMenuItem2 = "playlist.menu.2"

	WidgetNowPlayingArt      = "browser.nowplaying.art"
	WidgetBrowserSettings    = "browser.settings"
	WidgetBrowserModePL      = "browser.mode_playlist"
	WidgetBrowserTabSongs    = "browser.tab.songs"
	WidgetBrowserTabPlaylist = "browser.tab.playlists"
	WidgetBrowserTabAlbums   = "browser.tab.albums"
	WidgetBrowserTabArtists  = "browser.tab.artists"
	WidgetBrowserTabQueue    = "browser.tab.queue"
	WidgetDetailBack         = "browser.detail.back"
	WidgetDetailModeToggle   = "browser.detail.mode"
	WidgetQueueMoveUp        = "browser.queue.up"
	WidgetQueueMoveDown      = "browser.queue.down"
	WidgetQueueRemove        = "browser.queue.rem"
	WidgetQueueClear         = "browser.queue.clr"
	WidgetSearchInput        = "browser.search.input"
	WidgetSearchSubmit       = "browser.search.submit"
	WidgetSearchClear        = "browser.search.clear"
	WidgetResultRow0         = "browser.result.0"
	WidgetResultRow1         = "browser.result.1"
	WidgetResultRow2         = "browser.result.2"
	WidgetResultRow3         = "browser.result.3"
	WidgetResultRow4         = "browser.result.4"
	WidgetResultRow5         = "browser.result.5"
	WidgetBrowserPlay        = "browser.play"
	WidgetBrowserQueue       = "browser.queue"
	WidgetBrowserImportAll   = "browser.import_all"
	WidgetBrowserAutoplay    = "browser.autoplay"

	WidgetToastDismiss = "toast.dismiss"
	WidgetToastAction  = "toast.action"

	WidgetSettingsSkinPick     = "settings.skin.pick"
	WidgetSettingsSkinDev      = "settings.skin.dev"
	WidgetSettingsScale        = "settings.scale"
	WidgetSettingsBuffer       = "settings.buffer"
	WidgetSettingsCacheEdit    = "settings.cache.edit"
	WidgetSettingsCacheMax     = "settings.cache.max"
	WidgetSettingsCachePrune   = "settings.cache.prune"
	WidgetSettingsCacheClear   = "settings.cache.clear"
	WidgetSettingsYTDLPEdit    = "settings.ytdlp.edit"
	WidgetSettingsYTDLPClr     = "settings.ytdlp.clr"
	WidgetSettingsFFmpegEdit   = "settings.ffmpeg.edit"
	WidgetSettingsFFmpegClr    = "settings.ffmpeg.clr"
	WidgetSettingsRecheck      = "settings.recheck"
	WidgetSettingsClose        = "settings.close"
	WidgetSettingsInlineInput  = "settings.inline.input"
	WidgetSettingsInlineApply  = "settings.inline.apply"
	WidgetSettingsInlinePaste  = "settings.inline.paste"
	WidgetSettingsInlineCancel = "settings.inline.cancel"
)

var eqBandWidgetIDs = [10]string{
	"eq.band.0", "eq.band.1", "eq.band.2", "eq.band.3", "eq.band.4",
	"eq.band.5", "eq.band.6", "eq.band.7", "eq.band.8", "eq.band.9",
}

var playlistRowWidgetIDs = [VisiblePlaylistRows]string{
	"playlist.row.0", "playlist.row.1", "playlist.row.2", "playlist.row.3",
	"playlist.row.4", "playlist.row.5", "playlist.row.6", "playlist.row.7",
	"playlist.row.8", "playlist.row.9", "playlist.row.10", "playlist.row.11",
	"playlist.row.12",
}

// Named bounds in 1x logical coordinates shared between hit testing, input
// focus checks, and rendering.
var (
	EjectButtonBounds  = image.Rect(136, 89, 158, 105)
	MainCloseBounds    = image.Rect(264, 3, 273, 12)
	MainTimeBounds     = image.Rect(39, 26, 98, 39)
	MainMarqueeBounds  = image.Rect(111, 24, 265, 36)
	MainVolumeBounds   = image.Rect(107, 57, 175, 70)
	MainBalanceBounds  = image.Rect(177, 57, 215, 70)
	MainEQToggleBounds = image.Rect(219, 58, 242, 70)
	MainPLToggleBounds = image.Rect(242, 58, 265, 70)
	MainPositionBounds = image.Rect(16, 72, 264, 82)
	MainShuffleBounds  = image.Rect(164, 89, 210, 104)
	MainRepeatBounds   = image.Rect(210, 89, 238, 104)
	MainSkinBounds     = image.Rect(252, 90, 266, 104)

	NowPlayingArtBounds    = image.Rect(6, 130, 28, 152)
	BrowserSettingsBounds  = image.Rect(194, 117, 226, 128)
	BrowserModePLBounds    = image.Rect(228, 117, 272, 128)
	BrowserTabSongsBounds  = image.Rect(6, 155, 56, 169)
	BrowserTabPLBounds     = image.Rect(58, 155, 112, 169)
	BrowserTabAlbumsBounds = image.Rect(114, 155, 166, 169)
	BrowserTabArtistBounds = image.Rect(168, 155, 222, 169)
	BrowserTabQueueBounds  = image.Rect(224, 155, 269, 169)
	SearchInputBounds      = image.Rect(6, 171, 198, 187)
	SearchSubmitBounds     = image.Rect(200, 171, 234, 187)
	SearchClearBounds      = image.Rect(236, 171, 269, 187)
	DetailBackBounds       = image.Rect(6, 171, 48, 187)
	DetailModeBounds       = image.Rect(216, 171, 269, 187)
	QueueMoveUpBounds      = image.Rect(160, 171, 182, 187)
	QueueMoveDownBounds    = image.Rect(184, 171, 206, 187)
	QueueRemoveBounds      = image.Rect(208, 171, 237, 187)
	QueueClearBounds       = image.Rect(239, 171, 269, 187)
	ResultListBounds       = image.Rect(6, 189, 269, 321)
	BrowserPlayBounds      = image.Rect(6, 326, 40, 344)
	BrowserQueueBounds     = image.Rect(43, 326, 83, 344)
	BrowserImportAllBounds = image.Rect(86, 326, 132, 344)
	BrowserAutoplayBounds  = image.Rect(135, 326, 185, 344)
	ArtModalBounds         = image.Rect(38, 124, 238, 324)

	SettingsSkinPickBounds     = image.Rect(190, 132, 230, 147)
	SettingsSkinDevBounds      = image.Rect(233, 132, 269, 147)
	SettingsScaleBounds        = image.Rect(6, 166, 126, 183)
	SettingsBufferBounds       = image.Rect(132, 166, 269, 183)
	SettingsCacheEditBounds    = image.Rect(228, 189, 269, 204)
	SettingsCacheMaxBounds     = image.Rect(6, 219, 120, 236)
	SettingsCachePruneBounds   = image.Rect(124, 219, 194, 236)
	SettingsCacheClearBounds   = image.Rect(198, 219, 269, 236)
	SettingsYTDLPEditBounds    = image.Rect(192, 242, 232, 257)
	SettingsYTDLPClrBounds     = image.Rect(235, 242, 269, 257)
	SettingsFFmpegEditBounds   = image.Rect(192, 279, 232, 294)
	SettingsFFmpegClrBounds    = image.Rect(235, 279, 269, 294)
	SettingsRecheckBounds      = image.Rect(6, 322, 118, 342)
	SettingsCloseBounds        = image.Rect(210, 322, 269, 342)
	SettingsInlineInputBounds  = image.Rect(6, 298, 164, 315)
	SettingsInlineApplyBounds  = image.Rect(166, 298, 202, 315)
	SettingsInlinePasteBounds  = image.Rect(204, 298, 240, 315)
	SettingsInlineCancelBounds = image.Rect(242, 298, 269, 315)
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

var mainControlWidgets = []Widget{
	{ID: WidgetMainClose, Bounds: MainCloseBounds},
	{ID: WidgetMainTime, Bounds: MainTimeBounds},
	{ID: WidgetMainVolume, Bounds: MainVolumeBounds},
	{ID: WidgetMainBalance, Bounds: MainBalanceBounds},
	{ID: WidgetMainEQ, Bounds: MainEQToggleBounds},
	{ID: WidgetMainPL, Bounds: MainPLToggleBounds},
	{ID: WidgetMainPosition, Bounds: MainPositionBounds},
	{ID: WidgetMainShuffle, Bounds: MainShuffleBounds},
	{ID: WidgetMainRepeat, Bounds: MainRepeatBounds},
	{ID: WidgetMainSkin, Bounds: MainSkinBounds},
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
	{ID: WidgetBrowserSettings, Bounds: BrowserSettingsBounds},
	{ID: WidgetBrowserTabSongs, Bounds: BrowserTabSongsBounds},
	{ID: WidgetBrowserTabPlaylist, Bounds: BrowserTabPLBounds},
	{ID: WidgetBrowserTabAlbums, Bounds: BrowserTabAlbumsBounds},
	{ID: WidgetBrowserTabArtists, Bounds: BrowserTabArtistBounds},
	{ID: WidgetBrowserTabQueue, Bounds: BrowserTabQueueBounds},
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
	{ID: WidgetBrowserAutoplay, Bounds: BrowserAutoplayBounds},
}

var allWidgets = append(append([]Widget(nil), mainWidgets...), browserWidgets...)

func offsetRect(r image.Rectangle, dy int) image.Rectangle {
	if dy == 0 {
		return r
	}
	return r.Add(image.Pt(0, dy))
}

func (g *Game) browserWidgetsAt(plTop int) []Widget {
	dy := plTop - MainWindowHeight
	out := make([]Widget, 0, len(browserWidgets)+4)
	out = append(out,
		Widget{ID: WidgetBrowserSettings, Bounds: offsetRect(BrowserSettingsBounds, dy)},
		Widget{ID: WidgetBrowserModePL, Bounds: offsetRect(BrowserModePLBounds, dy)},
	)
	if g.showSettings {
		out = append(out, settingsWidgetsAt(dy)...)
		return out
	}

	out = append(out,
		Widget{ID: WidgetNowPlayingArt, Bounds: offsetRect(NowPlayingArtBounds, dy)},
		Widget{ID: WidgetBrowserTabSongs, Bounds: offsetRect(BrowserTabSongsBounds, dy)},
		Widget{ID: WidgetBrowserTabPlaylist, Bounds: offsetRect(BrowserTabPLBounds, dy)},
		Widget{ID: WidgetBrowserTabAlbums, Bounds: offsetRect(BrowserTabAlbumsBounds, dy)},
		Widget{ID: WidgetBrowserTabArtists, Bounds: offsetRect(BrowserTabArtistBounds, dy)},
		Widget{ID: WidgetBrowserTabQueue, Bounds: offsetRect(BrowserTabQueueBounds, dy)},
	)

	switch {
	case len(g.detailStack) > 0:
		out = append(out, Widget{ID: WidgetDetailBack, Bounds: offsetRect(DetailBackBounds, dy)})
		cur := g.detailStack[len(g.detailStack)-1]
		if cur.detail.Collection.Kind == catalog.CollectionArtist && len(cur.detail.Albums) > 0 {
			out = append(out, Widget{ID: WidgetDetailModeToggle, Bounds: offsetRect(DetailModeBounds, dy)})
		}
	case g.activeTab == browserTabQueue:
		out = append(out,
			Widget{ID: WidgetQueueMoveUp, Bounds: offsetRect(QueueMoveUpBounds, dy)},
			Widget{ID: WidgetQueueMoveDown, Bounds: offsetRect(QueueMoveDownBounds, dy)},
			Widget{ID: WidgetQueueRemove, Bounds: offsetRect(QueueRemoveBounds, dy)},
			Widget{ID: WidgetQueueClear, Bounds: offsetRect(QueueClearBounds, dy)},
		)
	default:
		out = append(out,
			Widget{ID: WidgetSearchInput, Bounds: offsetRect(SearchInputBounds, dy)},
			Widget{ID: WidgetSearchSubmit, Bounds: offsetRect(SearchSubmitBounds, dy)},
			Widget{ID: WidgetSearchClear, Bounds: offsetRect(SearchClearBounds, dy)},
		)
	}

	for i, rw := range resultRowWidgets {
		out = append(out, Widget{ID: rw, Bounds: offsetRect(resultRowBounds(i), dy)})
	}
	out = append(out,
		Widget{ID: WidgetBrowserPlay, Bounds: offsetRect(BrowserPlayBounds, dy)},
		Widget{ID: WidgetBrowserQueue, Bounds: offsetRect(BrowserQueueBounds, dy)},
		Widget{ID: WidgetBrowserImportAll, Bounds: offsetRect(BrowserImportAllBounds, dy)},
		Widget{ID: WidgetBrowserAutoplay, Bounds: offsetRect(BrowserAutoplayBounds, dy)},
	)
	return out
}
