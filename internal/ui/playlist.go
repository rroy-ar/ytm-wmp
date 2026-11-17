package ui

import (
	"fmt"
	"image"
	"os"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/rroy-ar/ytm-wmp/internal/m3u"
	"github.com/rroy-ar/ytm-wmp/internal/playback"
	"github.com/rroy-ar/ytm-wmp/internal/skin"
	"github.com/rroy-ar/ytm-wmp/internal/store"
)

type playlistMenuKind int

const (
	plMenuNone playlistMenuKind = iota
	plMenuAdd
	plMenuRem
	plMenuSel
	plMenuMisc
	plMenuList
)

type plMenuEntry struct {
	normal   string
	selected string
	action   string
}

var playlistMenus = map[playlistMenuKind]struct {
	x       int
	bar     string
	entries []plMenuEntry
}{
	plMenuAdd: {
		x:   14,
		bar: skin.PlaylistAddMenuBar,
		entries: []plMenuEntry{
			{normal: skin.PlaylistAddFile, selected: skin.PlaylistAddFileSelected, action: "add.paste_m3u"},
			{normal: skin.PlaylistAddURL, selected: skin.PlaylistAddURLSelected, action: "add.search"},
		},
	},
	plMenuRem: {
		x:   43,
		bar: skin.PlaylistRemoveMenuBar,
		entries: []plMenuEntry{
			{normal: skin.PlaylistRemoveSel, selected: skin.PlaylistRemoveSelSelected, action: "rem.selected"},
			{normal: skin.PlaylistCrop, selected: skin.PlaylistCropSelected, action: "rem.crop"},
			{normal: skin.PlaylistRemoveAll, selected: skin.PlaylistRemoveAllSelected, action: "rem.all"},
		},
	},
	plMenuSel: {
		x:   72,
		bar: skin.PlaylistSelectMenuBar,
		entries: []plMenuEntry{
			{normal: skin.PlaylistSelectAll, selected: skin.PlaylistSelectAllSelected, action: "sel.all"},
			{normal: skin.PlaylistSelectZero, selected: skin.PlaylistSelectZeroSelected, action: "sel.zero"},
			{normal: skin.PlaylistInvertSel, selected: skin.PlaylistInvertSelSelected, action: "sel.invert"},
		},
	},
	plMenuMisc: {
		x:   101,
		bar: skin.PlaylistMiscMenuBar,
		entries: []plMenuEntry{
			{normal: skin.PlaylistFileInfo, selected: skin.PlaylistFileInfoSelected, action: "misc.search"},
			{normal: skin.PlaylistSortList, selected: skin.PlaylistSortListSelected, action: "misc.sort"},
		},
	},
	plMenuList: {
		x:   231,
		bar: skin.PlaylistListBar,
		entries: []plMenuEntry{
			{normal: skin.PlaylistLoadList, selected: skin.PlaylistLoadListSelected, action: "list.load"},
			{normal: skin.PlaylistSaveList, selected: skin.PlaylistSaveListSelected, action: "list.save"},
			{normal: skin.PlaylistNewList, selected: skin.PlaylistNewListSelected, action: "list.new"},
		},
	},
}

type playlistPanel struct {
	plSelected    map[int]bool
	plAnchor      int
	plScroll      int
	plDoubleClick doubleClickTracker
	plMenu        playlistMenuKind
	plScrollDrag  bool
	plM3UPath     string
	plStatus      string
	plStatusUntil time.Time
}

func (g *Game) initPlaylist() {
	g.plSelected = make(map[int]bool)
	g.plAnchor = -1
}

func (g *Game) setPlaylistStatus(msg string) {
	g.plStatus = msg
	g.plStatusUntil = time.Now().Add(3 * time.Second)
}

func playlistWidgetsAt(topY int, activeMenu playlistMenuKind) []Widget {
	widgets := []Widget{
		{ID: WidgetPlaylistClose, Bounds: image.Rect(264, topY+3, 273, topY+12)},
		{ID: WidgetPlaylistScrollbar, Bounds: image.Rect(255, topY+20, 275, topY+194)},
		{ID: WidgetPlaylistAdd, Bounds: image.Rect(14, topY+202, 36, topY+220)},
		{ID: WidgetPlaylistRem, Bounds: image.Rect(43, topY+202, 65, topY+220)},
		{ID: WidgetPlaylistSel, Bounds: image.Rect(72, topY+202, 94, topY+220)},
		{ID: WidgetPlaylistMisc, Bounds: image.Rect(101, topY+202, 123, topY+220)},
		{ID: WidgetPlaylistList, Bounds: image.Rect(231, topY+202, 253, topY+220)},
		{ID: WidgetPlaylistMiniPrev, Bounds: image.Rect(128, topY+216, 138, topY+226)},
		{ID: WidgetPlaylistMiniPlay, Bounds: image.Rect(138, topY+216, 148, topY+226)},
		{ID: WidgetPlaylistMiniPause, Bounds: image.Rect(148, topY+216, 158, topY+226)},
		{ID: WidgetPlaylistMiniStop, Bounds: image.Rect(158, topY+216, 168, topY+226)},
		{ID: WidgetPlaylistMiniNext, Bounds: image.Rect(168, topY+216, 178, topY+226)},
		{ID: WidgetPlaylistMiniEject, Bounds: image.Rect(178, topY+216, 188, topY+226)},
	}
	for row := 0; row < VisiblePlaylistRows; row++ {
		y := topY + 20 + row*PlaylistRowHeight
		widgets = append(widgets, Widget{
			ID:     playlistRowWidgetIDs[row],
			Bounds: image.Rect(12, y, 255, y+PlaylistRowHeight),
		})
	}
	if activeMenu != plMenuNone {
		if def, ok := playlistMenus[activeMenu]; ok {
			menuIDs := [3]string{WidgetPLMenuItem0, WidgetPLMenuItem1, WidgetPLMenuItem2}
			for i := range def.entries {
				if i >= len(menuIDs) {
					break
				}
				iy := topY + 202 - (i+1)*18
				widgets = append(widgets, Widget{
					ID:     menuIDs[i],
					Bounds: image.Rect(def.x, iy, def.x+22, iy+18),
				})
			}
		}
	}
	return widgets
}

func (g *Game) updatePlaylist(topY, cx, cy int, pressed bool) {
	g.clampPlaylistScroll()

	// Prune out-of-range selection indices when the queue shrinks.
	qLen := len(g.snap.Queue)
	for idx := range g.plSelected {
		if idx < 0 || idx >= qLen {
			delete(g.plSelected, idx)
		}
	}

	if !pressed {
		g.plScrollDrag = false
	} else {
		if !g.plScrollDrag && g.pointer.Active() == WidgetPlaylistScrollbar {
			g.plScrollDrag = true
		}
		if g.plScrollDrag {
			maxScroll := g.maxPlaylistScroll()
			if maxScroll > 0 {
				track := image.Rect(255, topY+20, 275, topY+194)
				ratio := sliderRatioY(cy, track, 18)
				g.plScroll = int(ratio*float64(maxScroll) + 0.5)
				g.clampPlaylistScroll()
			}
		}
	}

	_, wheelY := ebiten.Wheel()
	if wheelY != 0 && len(g.snap.Queue) > VisiblePlaylistRows {
		listArea := image.Rect(12, topY+20, 255, topY+194)
		if image.Pt(cx, cy).In(listArea) {
			if wheelY > 0 {
				g.scrollPlaylist(-1)
			} else {
				g.scrollPlaylist(1)
			}
		}
	}
}

func (g *Game) handlePlaylistKeys() {
	if g.focusedPanel != panelPlaylist || g.browserMode || g.hidePL {
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDelete) {
		g.removeSelectedPlaylistTracks()
		return
	}
	if isCtrlOrMetaPressed() && inpututil.IsKeyJustPressed(ebiten.KeyA) {
		g.selectAllPlaylistTracks()
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) && g.plMenu != plMenuNone {
		g.plMenu = plMenuNone
	}
}

func (g *Game) maxPlaylistScroll() int {
	if len(g.snap.Queue) <= VisiblePlaylistRows {
		return 0
	}
	return len(g.snap.Queue) - VisiblePlaylistRows
}

func (g *Game) clampPlaylistScroll() {
	maxScroll := g.maxPlaylistScroll()
	if g.plScroll < 0 {
		g.plScroll = 0
	} else if g.plScroll > maxScroll {
		g.plScroll = maxScroll
	}
}

func (g *Game) scrollPlaylist(delta int) {
	g.plScroll += delta
	g.clampPlaylistScroll()
}

func (g *Game) onPlaylistClick(id string) bool {
	if g.plMenu != plMenuNone {
		if g.handlePlaylistMenuClick(id) {
			return true
		}
	}

	switch id {
	case WidgetPlaylistClose:
		g.plMenu = plMenuNone
		g.hidePL = true
		g.syncWindowSize()
		g.markConfigDirty()
		return true
	case WidgetPlaylistAdd:
		g.togglePlaylistMenu(plMenuAdd)
		return true
	case WidgetPlaylistRem:
		g.togglePlaylistMenu(plMenuRem)
		return true
	case WidgetPlaylistSel:
		g.togglePlaylistMenu(plMenuSel)
		return true
	case WidgetPlaylistMisc:
		g.togglePlaylistMenu(plMenuMisc)
		return true
	case WidgetPlaylistList:
		g.togglePlaylistMenu(plMenuList)
		return true
	case WidgetPlaylistMiniPrev:
		if g.ctrl != nil {
			g.ctrl.Send(playback.Prev{})
		}
		return true
	case WidgetPlaylistMiniPlay:
		if g.ctrl != nil {
			g.errText = ""
			g.ctrl.Send(playback.Play{})
		}
		return true
	case WidgetPlaylistMiniPause:
		if g.ctrl != nil {
			g.ctrl.Send(playback.Pause{})
		}
		return true
	case WidgetPlaylistMiniStop:
		if g.ctrl != nil {
			g.errText = ""
			g.ctrl.Send(playback.Stop{})
		}
		return true
	case WidgetPlaylistMiniNext:
		if g.ctrl != nil {
			g.ctrl.Send(playback.Next{})
		}
		return true
	case WidgetPlaylistMiniEject:
		g.focusSearch()
		return true
	}

	for row, rowID := range playlistRowWidgetIDs {
		if id == rowID {
			idx := g.plScroll + row
			if idx >= 0 && idx < len(g.snap.Queue) {
				g.onPlaylistRowClick(idx)
			}
			return true
		}
	}
	return false
}

func (g *Game) togglePlaylistMenu(kind playlistMenuKind) {
	if g.plMenu == kind {
		g.plMenu = plMenuNone
		return
	}
	g.plMenu = kind
}

func (g *Game) handlePlaylistMenuClick(id string) bool {
	def, ok := playlistMenus[g.plMenu]
	if !ok {
		g.plMenu = plMenuNone
		return false
	}
	menuIDs := [3]string{WidgetPLMenuItem0, WidgetPLMenuItem1, WidgetPLMenuItem2}
	for i, entry := range def.entries {
		if i < len(menuIDs) && id == menuIDs[i] {
			g.plMenu = plMenuNone
			g.runPlaylistAction(entry.action)
			return true
		}
	}
	// Clicking a bottom menu toggle switches menus; clicking elsewhere closes it.
	g.plMenu = plMenuNone
	return false
}

func (g *Game) runPlaylistAction(action string) {
	switch action {
	case "add.search", "misc.search":
		g.focusSearch()
	case "add.paste_m3u":
		g.importM3UFromClipboard()
	case "rem.selected":
		g.removeSelectedPlaylistTracks()
	case "rem.crop":
		g.cropSelectedPlaylistTracks()
	case "rem.all", "list.new":
		g.clearPlaylist()
	case "sel.all":
		g.selectAllPlaylistTracks()
	case "sel.zero":
		clear(g.plSelected)
		g.plAnchor = -1
	case "sel.invert":
		for i := range g.snap.Queue {
			if g.plSelected[i] {
				delete(g.plSelected, i)
			} else {
				g.plSelected[i] = true
			}
		}
	case "misc.sort":
		if g.ctrl != nil {
			g.ctrl.Send(playback.SortQueue{})
			g.setPlaylistStatus("SORTED")
		}
	case "list.save":
		g.savePlaylistM3U()
	case "list.load":
		g.loadPlaylistM3U()
	}
}

func (g *Game) onPlaylistRowClick(idx int) {
	g.onPlaylistRowClickAt(idx, time.Now(), isCtrlOrMetaPressed(), isShiftPressed())
}

func (g *Game) onPlaylistRowClickAt(idx int, now time.Time, ctrlHeld, shiftHeld bool) {
	if idx < 0 || idx >= len(g.snap.Queue) {
		return
	}
	if shiftHeld && g.plAnchor >= 0 && g.plAnchor < len(g.snap.Queue) {
		if !ctrlHeld {
			clear(g.plSelected)
		}
		lo, hi := g.plAnchor, idx
		if lo > hi {
			lo, hi = hi, lo
		}
		for i := lo; i <= hi; i++ {
			g.plSelected[i] = true
		}
		return
	}
	if ctrlHeld {
		if g.plSelected[idx] {
			delete(g.plSelected, idx)
		} else {
			g.plSelected[idx] = true
		}
		g.plAnchor = idx
		return
	}

	clear(g.plSelected)
	g.plSelected[idx] = true
	g.plAnchor = idx
	if g.plDoubleClick.Observe(idx, now) && g.ctrl != nil {
		g.errText = ""
		g.ctrl.Send(playback.PlayIndex{Index: idx})
	}
}

func (g *Game) selectedPlaylistIndices() []int {
	if len(g.plSelected) == 0 {
		return nil
	}
	out := make([]int, 0, len(g.plSelected))
	for i := range g.snap.Queue {
		if g.plSelected[i] {
			out = append(out, i)
		}
	}
	return out
}

func (g *Game) removeSelectedPlaylistTracks() {
	if g.ctrl == nil {
		return
	}
	indices := g.selectedPlaylistIndices()
	if len(indices) == 0 {
		return
	}
	clear(g.plSelected)
	g.plAnchor = -1
	g.ctrl.Send(playback.RemoveIndices{Indices: indices})
}

func (g *Game) cropSelectedPlaylistTracks() {
	if g.ctrl == nil || len(g.plSelected) == 0 {
		return
	}
	var toRemove []int
	for i := range g.snap.Queue {
		if !g.plSelected[i] {
			toRemove = append(toRemove, i)
		}
	}
	if len(toRemove) == 0 {
		return
	}
	clear(g.plSelected)
	g.plAnchor = -1
	g.ctrl.Send(playback.RemoveIndices{Indices: toRemove})
}

func (g *Game) clearPlaylist() {
	if g.ctrl == nil {
		return
	}
	clear(g.plSelected)
	g.plAnchor = -1
	g.plScroll = 0
	g.ctrl.Send(playback.SetQueue{Tracks: nil, Index: -1})
}

func (g *Game) selectAllPlaylistTracks() {
	for i := range g.snap.Queue {
		g.plSelected[i] = true
	}
}

func (g *Game) savePlaylistM3U() {
	if g.plM3UPath == "" {
		g.setPlaylistStatus("NO M3U PATH")
		return
	}
	content := m3u.Format(g.snap.Queue)
	if err := store.AtomicWriteFile(g.plM3UPath, []byte(content)); err != nil {
		g.setPlaylistStatus("SAVE ERR")
		return
	}
	g.setPlaylistStatus(fmt.Sprintf("SAVED %d", len(g.snap.Queue)))
}

func (g *Game) loadPlaylistM3U() {
	if g.plM3UPath != "" {
		if data, err := os.ReadFile(g.plM3UPath); err == nil {
			tracks, parseErr := m3u.Parse(strings.NewReader(string(data)))
			if parseErr == nil && len(tracks) > 0 && g.ctrl != nil {
				g.ctrl.Send(playback.SetQueue{Tracks: tracks, Index: 0})
				g.setPlaylistStatus(fmt.Sprintf("LOADED %d", len(tracks)))
				return
			}
		}
	}
	g.importM3UFromClipboard()
}

func (g *Game) importM3UFromClipboard() {
	if g.clipboard == nil {
		g.setPlaylistStatus("NO CLIPBOARD")
		return
	}
	raw, err := g.clipboard(g.ctx)
	if err != nil {
		g.setPlaylistStatus("PASTE ERR")
		return
	}
	tracks, parseErr := m3u.Parse(strings.NewReader(raw))
	if parseErr != nil || len(tracks) == 0 {
		g.setPlaylistStatus("NO M3U/URLS")
		return
	}
	if g.ctrl != nil {
		g.ctrl.Send(playback.Enqueue{Tracks: tracks})
		g.setPlaylistStatus(fmt.Sprintf("+%d TRACKS", len(tracks)))
	}
}

func (g *Game) drawPlaylist(screen *ebiten.Image, topY int) {
	if g.view == nil {
		return
	}
	style := g.view.PlaylistStyle()
	focused := g.focusedPanel == panelPlaylist && ebiten.IsFocused()

	// 1. Top titlebar (275x20).
	topLeft := skin.PlaylistTopLeftCorner
	topTile := skin.PlaylistTopTile
	titleBar := skin.PlaylistTitleBar
	topRight := skin.PlaylistTopRightCorner
	if focused {
		topLeft = skin.PlaylistTopLeftSelected
		topTile = skin.PlaylistTopTileSelected
		titleBar = skin.PlaylistTitleBarSelected
		topRight = skin.PlaylistTopRightSelected
	}
	g.drawSprite(screen, topLeft, image.Pt(0, topY))
	for x := 25; x < 250; x += 25 {
		g.drawSprite(screen, topTile, image.Pt(x, topY))
	}
	g.drawSprite(screen, titleBar, image.Pt(87, topY))
	g.drawSprite(screen, topRight, image.Pt(250, topY))

	if g.pointer.Active() == WidgetPlaylistClose && g.pointer.Pressed() && g.view.HasSprite(skin.PlaylistCloseSelected) {
		g.drawSprite(screen, skin.PlaylistCloseSelected, image.Pt(264, topY+3))
	}

	// 2. Middle left/right borders + track list background (y = topY+20 .. topY+194).
	midH := PlaylistHeight - 20 - 38 // 174
	midTop := topY + 20
	vector.DrawFilledRect(screen, 12, float32(midTop), 243, float32(midH), style.NormalBG, false)

	for y := 0; y < midH; y += 29 {
		g.drawSpriteClippedY(screen, skin.PlaylistLeftTile, image.Pt(0, midTop+y), topY+194)
		g.drawSpriteClippedY(screen, skin.PlaylistRightTile, image.Pt(255, midTop+y), topY+194)
	}

	// Scrollbar thumb (8x18 at x=260).
	thumbY := midTop
	if maxScroll := g.maxPlaylistScroll(); maxScroll > 0 {
		travel := midH - 18
		thumbY = midTop + (g.plScroll*travel)/maxScroll
	}
	handleSprite := skin.PlaylistScrollHandle
	if g.plScrollDrag || (g.pointer.Active() == WidgetPlaylistScrollbar && g.pointer.Pressed()) {
		handleSprite = skin.PlaylistScrollHandleSelect
	}
	g.drawSprite(screen, handleSprite, image.Pt(260, thumbY))

	// Virtualized rows (13 rows x 13px).
	for row := 0; row < VisiblePlaylistRows; row++ {
		idx := g.plScroll + row
		if idx >= len(g.snap.Queue) {
			break
		}
		tr := g.snap.Queue[idx]
		ry := midTop + 2 + row*PlaylistRowHeight

		if g.plSelected[idx] {
			vector.DrawFilledRect(screen, 13, float32(ry), 241, float32(PlaylistRowHeight), style.SelectedBG, false)
		}
		fg := style.Normal
		if idx == g.snap.Index {
			fg = style.Current
		}

		durStr := ""
		if tr.Duration > 0 {
			durStr = formatClock(tr.Duration)
		}
		titleMaxW := float64(236)
		if durStr != "" {
			durW := measureTTFWidth(g.textFace, durStr)
			titleMaxW = float64(236 - durW - 6)
			drawTTFText(screen, g.textFace, durStr, 253-durW, ry+1, fg)
		}

		label := trackDisplayTitle(tr)
		if tr.Artist != "" && tr.Title != "" {
			label = tr.Artist + " - " + tr.Title
		}
		line := fmt.Sprintf("%d. %s", idx+1, label)
		if g.textFace != nil {
			line = ellipsizeTTF(g.textFace, line, titleMaxW)
			drawTTFText(screen, g.textFace, line, 15, ry+1, fg)
		}
	}

	// 3. Bottom bar (275x38 at topY+194).
	botY := topY + 194
	g.drawSprite(screen, skin.PlaylistBottomLeftCorner, image.Pt(0, botY))
	g.drawSprite(screen, skin.PlaylistBottomRightCorner, image.Pt(125, botY))

	// Running time / status inside bottom-right LCD area.
	summary := g.playlistSummaryText()
	if g.plStatus != "" && time.Now().Before(g.plStatusUntil) {
		summary = g.plStatus
	}
	drawTTFText(screen, g.textFace, summary, 134, botY+10, style.Normal)

	miniTime := formatClock(g.snap.Position)
	if g.snap.State == playback.StateIdle {
		miniTime = "--:--"
	}
	drawTTFText(screen, g.textFace, miniTime, 193, botY+22, style.Current)

	// 4. Active popup menu above bottom buttons.
	if g.plMenu != plMenuNone {
		g.drawPlaylistPopupMenu(screen, topY)
	}
}

func (g *Game) drawPlaylistPopupMenu(screen *ebiten.Image, topY int) {
	def, ok := playlistMenus[g.plMenu]
	if !ok {
		return
	}
	menuIDs := [3]string{WidgetPLMenuItem0, WidgetPLMenuItem1, WidgetPLMenuItem2}
	barH := len(def.entries) * 18
	barTop := topY + 202 - barH
	g.drawSpriteClippedY(screen, def.bar, image.Pt(def.x-3, barTop), topY+202)

	for i, entry := range def.entries {
		iy := topY + 202 - (i+1)*18
		sp := entry.normal
		if i < len(menuIDs) && (g.pointer.Hot() == menuIDs[i] || g.pointer.Active() == menuIDs[i]) {
			sp = entry.selected
		}
		g.drawSprite(screen, sp, image.Pt(def.x, iy))
	}
}

func (g *Game) drawSpriteClippedY(dst *ebiten.Image, name string, at image.Point, maxY int) {
	if at.Y >= maxY {
		return
	}
	img := g.view.Sprite(name)
	h := img.Bounds().Dy()
	if at.Y+h > maxY {
		clipped, ok := img.SubImage(image.Rect(
			img.Bounds().Min.X,
			img.Bounds().Min.Y,
			img.Bounds().Max.X,
			img.Bounds().Min.Y+(maxY-at.Y),
		)).(*ebiten.Image)
		if !ok {
			return
		}
		img = clipped
	}
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest}
	op.GeoM.Translate(float64(at.X), float64(at.Y))
	dst.DrawImage(img, op)
}

func (g *Game) playlistSummaryText() string {
	var total time.Duration
	for _, tr := range g.snap.Queue {
		if tr.Duration > 0 {
			total += tr.Duration
		}
	}
	if total > 0 {
		return fmt.Sprintf("%d TRK %s", len(g.snap.Queue), formatClock(total))
	}
	return fmt.Sprintf("%d TRACKS", len(g.snap.Queue))
}
