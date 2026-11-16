package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"ytm-wmp/internal/skin"
	"ytm-wmp/internal/ui/skinview"
)

// ToolStatus reports startup or re-checked external dependency state.
type ToolStatus struct {
	Name         string
	Configured   string
	ResolvedPath string
	Version      string
	OK           bool
	Error        string
	Hint         string
}

// ToolCheckFunc verifies a named tool (yt-dlp or ffmpeg) at overridePath or PATH.
type ToolCheckFunc func(ctx context.Context, name, overridePath string) ToolStatus

// ArtworkAdmin exposes runtime artwork disk cache inspection and management.
type ArtworkAdmin interface {
	CacheDir() string
	SetCacheDir(dir string)
	DiskStats() (files int, totalBytes int64, err error)
	PruneDisk(maxBytes int64) error
	ClearDisk() error
}

var (
	bufferPresetsMs = []int{0, 100, 200, 350, 500}
	cacheMaxPresets = []int{64, 128, 256, 512, 0}
)

type settingsEditTarget int

const (
	editTargetNone settingsEditTarget = iota
	editTargetCacheDir
	editTargetYTDLP
	editTargetFFmpeg
)

type cacheStatsResult struct {
	files int
	bytes int64
	msg   string
}

type settingsState struct {
	defaultSkinPath string
	configPath      string
	scale           int
	bufferMs        int
	cacheDir        string
	cacheMaxMB      int
	cacheFiles      int
	cacheBytes      int64
	cacheStatsCh    chan cacheStatsResult

	ytdlpStatus  ToolStatus
	ffmpegStatus ToolStatus
	toolChecker  ToolCheckFunc
	toolCheckCh  chan ToolStatus
	toolChecking bool
	toolApplier  func(ytdlpPath, ffmpegPath string)

	bufferApplier func(time.Duration)

	editTarget settingsEditTarget
	editField  textField
}

func (g *Game) initSettings() {
	if g.scale != 2 {
		g.scale = 1
	}
	if g.cacheMaxMB == 0 && g.configPath == "" {
		g.cacheMaxMB = 256
	}
	if g.cacheDir == "" {
		if admin, ok := g.art.(ArtworkAdmin); ok {
			g.cacheDir = admin.CacheDir()
		}
	}
	if g.ytdlpStatus.Name == "" {
		g.ytdlpStatus = ToolStatus{Name: "yt-dlp", OK: true}
	}
	if g.ffmpegStatus.Name == "" {
		g.ffmpegStatus = ToolStatus{Name: "ffmpeg", OK: true}
	}
	g.cacheStatsCh = make(chan cacheStatsResult, 4)
	g.toolCheckCh = make(chan ToolStatus, 4)
	g.refreshCacheStatsAsync("")
	g.syncDependencyToasts()
}

func (g *Game) openSettings() {
	g.hidePL = false
	g.browserMode = true
	g.showSettings = true
	g.showArtModal = false
	g.plMenu = plMenuNone
	g.focusedPanel = panelPlaylist
	g.blurSearch()
	g.syncWindowSize()
	g.refreshCacheStatsAsync("")
}

func (g *Game) syncDependencyToasts() {
	var missing []string
	if !g.ytdlpStatus.OK {
		missing = append(missing, "yt-dlp")
	}
	if !g.ffmpegStatus.OK {
		missing = append(missing, "ffmpeg")
	}
	if len(missing) == 0 {
		g.clearToastSource("deps")
		return
	}
	msg := fmt.Sprintf("Missing tool: %s. Configure path in Settings or install on PATH.", strings.Join(missing, ", "))
	g.pushActionToast("deps", toastError, msg, "SETTINGS", "open_settings", 0)
}

func (g *Game) refreshCacheStatsAsync(toastMsg string) {
	admin, ok := g.art.(ArtworkAdmin)
	if !ok || g.cacheStatsCh == nil {
		return
	}
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		files, bytes, _ := admin.DiskStats()
		select {
		case g.cacheStatsCh <- cacheStatsResult{files: files, bytes: bytes, msg: toastMsg}:
		case <-g.ctx.Done():
		}
	}()
}

func (g *Game) drainSettingsResults() {
	if g.cacheStatsCh != nil {
		for {
			select {
			case res := <-g.cacheStatsCh:
				g.cacheFiles = res.files
				g.cacheBytes = res.bytes
				if res.msg != "" {
					g.pushToast(toastInfo, res.msg, defaultInfoToastTTL)
				}
			default:
				goto checkTools
			}
		}
	}
checkTools:
	if g.toolCheckCh != nil {
		for {
			select {
			case st := <-g.toolCheckCh:
				switch st.Name {
				case "yt-dlp":
					g.ytdlpStatus = st
				case "ffmpeg":
					g.ffmpegStatus = st
				}
				g.toolChecking = false
				g.applyToolPaths()
				g.syncDependencyToasts()
				if st.OK {
					g.pushToast(toastInfo, fmt.Sprintf("%s ready (%s)", st.Name, st.Version), defaultInfoToastTTL)
				} else {
					g.pushActionToast("deps", toastError, st.Error, "SETTINGS", "open_settings", 0)
				}
			default:
				return
			}
		}
	}
}

func (g *Game) applyToolPaths() {
	if g.toolApplier == nil {
		return
	}
	yt := g.ytdlpStatus.ResolvedPath
	if yt == "" {
		yt = g.ytdlpStatus.Configured
	}
	ff := g.ffmpegStatus.ResolvedPath
	if ff == "" {
		ff = g.ffmpegStatus.Configured
	}
	g.toolApplier(yt, ff)
}

func (g *Game) startToolCheckAsync(name, overridePath string) {
	if g.toolChecker == nil || g.toolCheckCh == nil {
		return
	}
	g.toolChecking = true
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		st := g.toolChecker(g.ctx, name, overridePath)
		st.Configured = overridePath
		select {
		case g.toolCheckCh <- st:
		case <-g.ctx.Done():
		}
	}()
}

func (g *Game) recheckAllTools() {
	if g.toolChecker == nil {
		return
	}
	g.startToolCheckAsync("yt-dlp", g.ytdlpStatus.Configured)
	g.startToolCheckAsync("ffmpeg", g.ffmpegStatus.Configured)
}

func (g *Game) setScale(scale int) {
	if scale != 2 {
		scale = 1
	}
	if g.scale == scale {
		return
	}
	g.scale = scale
	if g.windowResizer != nil {
		g.windowResizer(WindowWidth*g.scale, g.canvasHeight()*g.scale)
	}
	g.markConfigDirty()
	g.pushToast(toastInfo, fmt.Sprintf("Display scale: %dX", g.scale), defaultInfoToastTTL)
}

func (g *Game) cycleBufferMs() {
	next := bufferPresetsMs[0]
	for i, v := range bufferPresetsMs {
		if v == g.bufferMs {
			next = bufferPresetsMs[(i+1)%len(bufferPresetsMs)]
			break
		}
	}
	g.bufferMs = next
	if g.bufferApplier != nil {
		g.bufferApplier(time.Duration(g.bufferMs) * time.Millisecond)
	}
	g.markConfigDirty()
	if g.bufferMs == 0 {
		g.pushToast(toastInfo, "Audio buffer: Default", defaultInfoToastTTL)
	} else {
		g.pushToast(toastInfo, fmt.Sprintf("Audio buffer: %dms", g.bufferMs), defaultInfoToastTTL)
	}
}

func (g *Game) cycleCacheMaxMB() {
	next := cacheMaxPresets[0]
	for i, v := range cacheMaxPresets {
		if v == g.cacheMaxMB {
			next = cacheMaxPresets[(i+1)%len(cacheMaxPresets)]
			break
		}
	}
	g.cacheMaxMB = next
	g.markConfigDirty()
}

func (g *Game) pruneArtworkCacheAsync() {
	admin, ok := g.art.(ArtworkAdmin)
	if !ok {
		return
	}
	maxBytes := int64(g.cacheMaxMB) * 1024 * 1024
	if maxBytes <= 0 {
		g.pushToast(toastInfo, "Cache cap is unlimited", defaultInfoToastTTL)
		return
	}
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		_ = admin.PruneDisk(maxBytes)
		files, bytes, _ := admin.DiskStats()
		select {
		case g.cacheStatsCh <- cacheStatsResult{files: files, bytes: bytes, msg: "Artwork cache pruned"}:
		case <-g.ctx.Done():
		}
	}()
}

func (g *Game) clearArtworkCacheAsync() {
	admin, ok := g.art.(ArtworkAdmin)
	if !ok {
		return
	}
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		_ = admin.ClearDisk()
		files, bytes, _ := admin.DiskStats()
		select {
		case g.cacheStatsCh <- cacheStatsResult{files: files, bytes: bytes, msg: "Artwork cache cleared"}:
		case <-g.ctx.Done():
		}
	}()
}

func (g *Game) resetToDevSkin() {
	path := g.defaultSkinPath
	if path == "" {
		path = filepath.Join("assets", "dev-skin")
	}
	loaded, err := skin.Open(path)
	if err != nil {
		g.setError(err.Error())
		return
	}
	if g.view != nil {
		g.view.Deallocate()
	}
	g.skin = loaded
	g.skinPath = path
	g.skinName = filepath.Base(path)
	g.view = skinview.New(loaded)
	g.clearError()
	g.markConfigDirty()
	g.setTransientStatus("SKIN: " + strings.ToUpper(g.skinName))
	g.pushToast(toastInfo, "Restored default dev skin", defaultInfoToastTTL)
}

func (g *Game) startSettingsEdit(target settingsEditTarget, initial string) {
	g.editTarget = target
	g.editField.SetText(initial)
}

func (g *Game) applySettingsEdit() {
	val := strings.TrimSpace(g.editField.Text())
	switch g.editTarget {
	case editTargetCacheDir:
		g.cacheDir = val
		if admin, ok := g.art.(ArtworkAdmin); ok {
			admin.SetCacheDir(val)
		}
		g.markConfigDirty()
		g.refreshCacheStatsAsync("Updated artwork cache dir")
	case editTargetYTDLP:
		g.ytdlpStatus.Configured = val
		g.markConfigDirty()
		g.startToolCheckAsync("yt-dlp", val)
	case editTargetFFmpeg:
		g.ffmpegStatus.Configured = val
		g.markConfigDirty()
		g.startToolCheckAsync("ffmpeg", val)
	}
	g.editTarget = editTargetNone
}

func (g *Game) pasteSettingsEdit() {
	if g.clipboard == nil {
		return
	}
	raw, err := g.clipboard(g.ctx)
	if err != nil {
		return
	}
	clean := sanitizePasteText(raw)
	if clean != "" {
		g.editField.SetText(clean)
	}
}

func (g *Game) handleSettingsKeys() bool {
	if !g.showSettings || g.editTarget == editTargetNone {
		return false
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		g.editTarget = editTargetNone
		return true
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyKPEnter) {
		g.applySettingsEdit()
		return true
	}
	if isPasteJustPressed() {
		g.pasteSettingsEdit()
		return true
	}
	if isClearLineJustPressed() {
		g.editField.Clear()
		return true
	}
	if dur := inpututil.KeyPressDuration(ebiten.KeyLeft); dur == 1 || (dur > 20 && dur%3 == 0) {
		g.editField.MoveLeft()
	}
	if dur := inpututil.KeyPressDuration(ebiten.KeyRight); dur == 1 || (dur > 20 && dur%3 == 0) {
		g.editField.MoveRight()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyHome) {
		g.editField.Home()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnd) {
		g.editField.End()
	}
	ctrlWithoutAlt := ebiten.IsKeyPressed(ebiten.KeyControl) && !ebiten.IsKeyPressed(ebiten.KeyAlt)
	if !ctrlWithoutAlt && !ebiten.IsKeyPressed(ebiten.KeyMeta) {
		if chars := ebiten.AppendInputChars(nil); len(chars) > 0 {
			g.editField.Insert(string(chars))
		}
	}
	if dur := inpututil.KeyPressDuration(ebiten.KeyBackspace); dur == 1 || (dur > 20 && dur%3 == 0) {
		g.editField.Backspace()
	}
	if dur := inpututil.KeyPressDuration(ebiten.KeyDelete); dur == 1 || (dur > 20 && dur%3 == 0) {
		g.editField.Delete()
	}
	return true
}

func settingsWidgetsAt(dy int) []Widget {
	return []Widget{
		{ID: WidgetSettingsSkinPick, Bounds: offsetRect(SettingsSkinPickBounds, dy)},
		{ID: WidgetSettingsSkinDev, Bounds: offsetRect(SettingsSkinDevBounds, dy)},
		{ID: WidgetSettingsScale, Bounds: offsetRect(SettingsScaleBounds, dy)},
		{ID: WidgetSettingsBuffer, Bounds: offsetRect(SettingsBufferBounds, dy)},
		{ID: WidgetSettingsCacheEdit, Bounds: offsetRect(SettingsCacheEditBounds, dy)},
		{ID: WidgetSettingsCacheMax, Bounds: offsetRect(SettingsCacheMaxBounds, dy)},
		{ID: WidgetSettingsCachePrune, Bounds: offsetRect(SettingsCachePruneBounds, dy)},
		{ID: WidgetSettingsCacheClear, Bounds: offsetRect(SettingsCacheClearBounds, dy)},
		{ID: WidgetSettingsYTDLPEdit, Bounds: offsetRect(SettingsYTDLPEditBounds, dy)},
		{ID: WidgetSettingsYTDLPClr, Bounds: offsetRect(SettingsYTDLPClrBounds, dy)},
		{ID: WidgetSettingsFFmpegEdit, Bounds: offsetRect(SettingsFFmpegEditBounds, dy)},
		{ID: WidgetSettingsFFmpegClr, Bounds: offsetRect(SettingsFFmpegClrBounds, dy)},
		{ID: WidgetSettingsInlineInput, Bounds: offsetRect(SettingsInlineInputBounds, dy)},
		{ID: WidgetSettingsInlineApply, Bounds: offsetRect(SettingsInlineApplyBounds, dy)},
		{ID: WidgetSettingsInlinePaste, Bounds: offsetRect(SettingsInlinePasteBounds, dy)},
		{ID: WidgetSettingsInlineCancel, Bounds: offsetRect(SettingsInlineCancelBounds, dy)},
		{ID: WidgetSettingsRecheck, Bounds: offsetRect(SettingsRecheckBounds, dy)},
		{ID: WidgetSettingsClose, Bounds: offsetRect(SettingsCloseBounds, dy)},
	}
}

func (g *Game) onSettingsClick(id string) bool {
	switch id {
	case WidgetSettingsSkinPick:
		g.clearError()
		g.openSkinPicker()
		return true
	case WidgetSettingsSkinDev:
		g.resetToDevSkin()
		return true
	case WidgetSettingsScale:
		if g.scale == 2 {
			g.setScale(1)
		} else {
			g.setScale(2)
		}
		return true
	case WidgetSettingsBuffer:
		g.cycleBufferMs()
		return true
	case WidgetSettingsCacheEdit:
		g.startSettingsEdit(editTargetCacheDir, g.cacheDir)
		return true
	case WidgetSettingsCacheMax:
		g.cycleCacheMaxMB()
		return true
	case WidgetSettingsCachePrune:
		g.pruneArtworkCacheAsync()
		return true
	case WidgetSettingsCacheClear:
		g.clearArtworkCacheAsync()
		return true
	case WidgetSettingsYTDLPEdit:
		g.startSettingsEdit(editTargetYTDLP, g.ytdlpStatus.Configured)
		return true
	case WidgetSettingsYTDLPClr:
		g.ytdlpStatus.Configured = ""
		g.markConfigDirty()
		g.startToolCheckAsync("yt-dlp", "")
		return true
	case WidgetSettingsFFmpegEdit:
		g.startSettingsEdit(editTargetFFmpeg, g.ffmpegStatus.Configured)
		return true
	case WidgetSettingsFFmpegClr:
		g.ffmpegStatus.Configured = ""
		g.markConfigDirty()
		g.startToolCheckAsync("ffmpeg", "")
		return true
	case WidgetSettingsInlineApply:
		g.applySettingsEdit()
		return true
	case WidgetSettingsInlinePaste:
		g.pasteSettingsEdit()
		return true
	case WidgetSettingsInlineCancel:
		g.editTarget = editTargetNone
		return true
	case WidgetSettingsRecheck:
		g.recheckAllTools()
		return true
	case WidgetSettingsClose:
		g.showSettings = false
		g.editTarget = editTargetNone
		return true
	}
	return false
}

func (g *Game) drawSettings(screen *ebiten.Image, dy int) {
	// 1. Skin section.
	vector.DrawFilledRect(screen, 4, float32(130+dy), 267, 32, colSurfaceBg, false)
	skinLabel := g.skinName
	if skinLabel == "" {
		skinLabel = "dev-skin"
	}
	ebitenutil.DebugPrintAt(screen, "SKIN: "+truncateRunes(skinLabel, 22), 8, 132+dy)
	ebitenutil.DebugPrintAt(screen, truncateRunes(g.skinPath, 42), 8, 146+dy)
	g.drawButtonRect(screen, offsetRect(SettingsSkinPickBounds, dy), WidgetSettingsSkinPick, "PICK")
	g.drawButtonRect(screen, offsetRect(SettingsSkinDevBounds, dy), WidgetSettingsSkinDev, "DEV")

	// 2. Scale & Audio Buffer.
	scaleLabel := fmt.Sprintf("SCALE: %dX", g.scale)
	g.drawToggleButtonRect(screen, offsetRect(SettingsScaleBounds, dy), WidgetSettingsScale, scaleLabel, g.scale == 2)
	bufLabel := "BUFFER: DEFAULT"
	if g.bufferMs > 0 {
		bufLabel = fmt.Sprintf("BUFFER: %dms", g.bufferMs)
	}
	g.drawToggleButtonRect(screen, offsetRect(SettingsBufferBounds, dy), WidgetSettingsBuffer, bufLabel, g.bufferMs > 0)

	// 3. Artwork Cache.
	vector.DrawFilledRect(screen, 4, float32(187+dy), 267, 51, colSurfaceBg, false)
	dirText := g.cacheDir
	if dirText == "" {
		dirText = "(disabled)"
	}
	ebitenutil.DebugPrintAt(screen, "CACHE: "+truncateRunes(dirText, 27), 8, 189+dy)
	usedMB := float64(g.cacheBytes) / (1024 * 1024)
	ebitenutil.DebugPrintAt(screen, fmt.Sprintf("USED: %d files (%.1f MB)", g.cacheFiles, usedMB), 8, 203+dy)
	g.drawButtonRect(screen, offsetRect(SettingsCacheEditBounds, dy), WidgetSettingsCacheEdit, "EDIT")
	maxLabel := "MAX: UNLIM"
	if g.cacheMaxMB > 0 {
		maxLabel = fmt.Sprintf("MAX: %dMB", g.cacheMaxMB)
	}
	g.drawButtonRect(screen, offsetRect(SettingsCacheMaxBounds, dy), WidgetSettingsCacheMax, maxLabel)
	g.drawButtonRect(screen, offsetRect(SettingsCachePruneBounds, dy), WidgetSettingsCachePrune, "PRUNE")
	g.drawButtonRect(screen, offsetRect(SettingsCacheClearBounds, dy), WidgetSettingsCacheClear, "CLEAR")

	// 4. Dependencies (yt-dlp & ffmpeg).
	g.drawToolStatusRow(screen, 240+dy, g.ytdlpStatus, offsetRect(SettingsYTDLPEditBounds, dy), WidgetSettingsYTDLPEdit, offsetRect(SettingsYTDLPClrBounds, dy), WidgetSettingsYTDLPClr)
	g.drawToolStatusRow(screen, 277+dy, g.ffmpegStatus, offsetRect(SettingsFFmpegEditBounds, dy), WidgetSettingsFFmpegEdit, offsetRect(SettingsFFmpegClrBounds, dy), WidgetSettingsFFmpegClr)

	// Inline path editor overlay when editing cache/yt-dlp/ffmpeg.
	if g.editTarget != editTargetNone {
		inRect := offsetRect(SettingsInlineInputBounds, dy)
		vector.DrawFilledRect(screen, 4, float32(inRect.Min.Y-2), 267, 21, colHeaderBg, false)
		vector.DrawFilledRect(screen, float32(inRect.Min.X), float32(inRect.Min.Y), float32(inRect.Dx()), float32(inRect.Dy()), colSurfaceBg, false)
		vector.StrokeRect(screen, float32(inRect.Min.X)+0.5, float32(inRect.Min.Y)+0.5, float32(inRect.Dx()-1), float32(inRect.Dy()-1), 1, colFocusBorder, false)
		vis, caretCol, _, _ := g.editField.DisplayWindow(24)
		ebitenutil.DebugPrintAt(screen, vis, inRect.Min.X+3, inRect.Min.Y)
		if (g.tick/20)%2 == 0 {
			cx := float32(inRect.Min.X + 3 + caretCol*6)
			if cx > float32(inRect.Max.X-3) {
				cx = float32(inRect.Max.X - 3)
			}
			vector.DrawFilledRect(screen, cx, float32(inRect.Min.Y+2), 1, float32(inRect.Dy()-4), colFocusBorder, false)
		}
		g.drawButtonRect(screen, offsetRect(SettingsInlineApplyBounds, dy), WidgetSettingsInlineApply, "OK")
		g.drawButtonRect(screen, offsetRect(SettingsInlinePasteBounds, dy), WidgetSettingsInlinePaste, "PST")
		g.drawButtonRect(screen, offsetRect(SettingsInlineCancelBounds, dy), WidgetSettingsInlineCancel, "ESC")
	}

	// 5. Bottom actions.
	recheckLabel := "RECHECK TOOLS"
	if g.toolChecking {
		recheckLabel = "CHECKING..."
	}
	g.drawButtonRect(screen, offsetRect(SettingsRecheckBounds, dy), WidgetSettingsRecheck, recheckLabel)
	g.drawButtonRect(screen, offsetRect(SettingsCloseBounds, dy), WidgetSettingsClose, "DONE")
}

func (g *Game) drawToolStatusRow(screen *ebiten.Image, y int, st ToolStatus, editRect image.Rectangle, editID string, clrRect image.Rectangle, clrID string) {
	vector.DrawFilledRect(screen, 4, float32(y), 267, 35, colSurfaceBg, false)
	accent := colFocusBorder
	statusText := "OK"
	if st.Version != "" {
		statusText = "OK " + truncateRunes(st.Version, 16)
	}
	if !st.OK {
		accent = color.RGBA{R: 0xff, G: 0x70, B: 0x70, A: 0xff}
		statusText = "MISSING"
	}
	vector.DrawFilledRect(screen, 4, float32(y), 2, 35, accent, false)
	ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%s: %s", st.Name, statusText), 9, y+1)

	pathInfo := st.Configured
	if pathInfo == "" {
		pathInfo = st.ResolvedPath
	}
	if pathInfo == "" {
		if st.Hint != "" {
			pathInfo = st.Hint
		} else {
			pathInfo = "(PATH lookup)"
		}
	}
	ebitenutil.DebugPrintAt(screen, truncateRunes(pathInfo, 42), 9, y+16)
	g.drawButtonRect(screen, editRect, editID, "EDIT")
	g.drawButtonRect(screen, clrRect, clrID, "CLR")
}
