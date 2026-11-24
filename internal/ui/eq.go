package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/rroy-ar/ytm-wmp/internal/audio"
	"github.com/rroy-ar/ytm-wmp/internal/playback"
	"github.com/rroy-ar/ytm-wmp/internal/skin"
	"github.com/rroy-ar/ytm-wmp/internal/store"
)

type eqSink interface {
	SetEQ(audio.EQSettings)
}

type eqPreset struct {
	Name   string
	Preamp float64
	Bands  [10]float64
}

var eqPresets = []eqPreset{
	{Name: "FLAT", Preamp: 50, Bands: [10]float64{50, 50, 50, 50, 50, 50, 50, 50, 50, 50}},
	{Name: "ROCK", Preamp: 50, Bands: [10]float64{72, 64, 52, 42, 46, 56, 68, 74, 74, 72}},
	{Name: "POP", Preamp: 50, Bands: [10]float64{44, 58, 66, 68, 60, 48, 44, 46, 52, 56}},
	{Name: "ELECTRONIC", Preamp: 50, Bands: [10]float64{68, 64, 54, 46, 50, 58, 62, 68, 70, 72}},
	{Name: "BASS BOOST", Preamp: 50, Bands: [10]float64{78, 74, 66, 56, 50, 50, 50, 50, 50, 50}},
	{Name: "CLASSICAL", Preamp: 50, Bands: [10]float64{50, 50, 50, 50, 50, 50, 42, 42, 42, 36}},
}

var eqBandXOffsets = [10]int{78, 96, 114, 132, 150, 168, 186, 204, 222, 240}

type eqStateDisk struct {
	Version int         `json:"version"`
	On      bool        `json:"on"`
	Auto    bool        `json:"auto"`
	Preamp  float64     `json:"preamp"`
	Bands   [10]float64 `json:"bands"`
}

type eqPanel struct {
	eqOn        bool
	eqAuto      bool
	eqPreamp    float64
	eqBands     [10]float64
	eqPresetIdx int

	eqPath      string
	eqDirty     bool
	eqSaveAt    time.Time
	eqDragBand  int // -1 none, 0..9 band, 10 preamp
	eqShadeDrag string
}

func (g *Game) initEQ() {
	g.eqPreamp = 50
	for i := range g.eqBands {
		g.eqBands[i] = 50
	}
	g.eqDragBand = -1
	g.loadEQ()
	g.applyEQ()
}

func (g *Game) currentEQSettings() audio.EQSettings {
	var s audio.EQSettings
	s.Enabled = g.eqOn
	s.PreampDB = audio.EQSliderToDB(g.eqPreamp)
	for i, v := range g.eqBands {
		s.BandsDB[i] = audio.EQSliderToDB(v)
	}
	return s
}

func (g *Game) applyEQ() {
	if s, ok := g.sink.(eqSink); ok && s != nil {
		s.SetEQ(g.currentEQSettings())
	}
}

func (g *Game) loadEQ() {
	if g.eqPath == "" {
		return
	}
	data, err := os.ReadFile(g.eqPath)
	if err != nil {
		return
	}
	var st eqStateDisk
	if err := json.Unmarshal(data, &st); err != nil {
		return
	}
	g.eqOn = st.On
	g.eqAuto = st.Auto
	g.eqPreamp = clampEQVal(st.Preamp)
	for i := range g.eqBands {
		g.eqBands[i] = clampEQVal(st.Bands[i])
	}
}

func (g *Game) markEQDirty() {
	g.eqDirty = true
	g.eqSaveAt = time.Now().Add(500 * time.Millisecond)
}

func (g *Game) maybeSaveEQ() {
	if !g.eqDirty || g.eqPath == "" || time.Now().Before(g.eqSaveAt) {
		return
	}
	g.saveEQNow()
}

func (g *Game) saveEQNow() {
	if !g.eqDirty || g.eqPath == "" {
		return
	}
	g.eqDirty = false
	st := eqStateDisk{
		Version: 1,
		On:      g.eqOn,
		Auto:    g.eqAuto,
		Preamp:  g.eqPreamp,
		Bands:   g.eqBands,
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	if err := store.AtomicWriteFile(g.eqPath, data); err != nil {
		return
	}
}

func clampEQVal(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func eqWidgetsAt(topY int) []Widget {
	widgets := []Widget{
		{ID: WidgetEQShade, Bounds: image.Rect(254, topY+3, 263, topY+12)},
		{ID: WidgetEQClose, Bounds: image.Rect(264, topY+3, 273, topY+12)},
		{ID: WidgetEQOn, Bounds: image.Rect(14, topY+18, 40, topY+30)},
		{ID: WidgetEQAuto, Bounds: image.Rect(40, topY+18, 72, topY+30)},
		{ID: WidgetEQPresets, Bounds: image.Rect(217, topY+18, 261, topY+30)},
		{ID: WidgetEQPlus12, Bounds: image.Rect(45, topY+36, 67, topY+44)},
		{ID: WidgetEQZero, Bounds: image.Rect(45, topY+64, 67, topY+72)},
		{ID: WidgetEQMinus12, Bounds: image.Rect(45, topY+95, 67, topY+103)},
		{ID: WidgetEQPreamp, Bounds: image.Rect(21, topY+38, 35, topY+101)},
	}
	for i, x := range eqBandXOffsets {
		widgets = append(widgets, Widget{
			ID:     eqBandWidgetIDs[i],
			Bounds: image.Rect(x, topY+38, x+14, topY+101),
		})
	}
	return widgets
}

func eqShadeWidgetsAt(topY int) []Widget {
	return []Widget{
		{ID: WidgetEQShadeVol, Bounds: image.Rect(61, topY+4, 158, topY+11)},
		{ID: WidgetEQShadeBal, Bounds: image.Rect(164, topY+4, 206, topY+11)},
		{ID: WidgetEQShade, Bounds: image.Rect(254, topY+3, 263, topY+12)},
		{ID: WidgetEQClose, Bounds: image.Rect(264, topY+3, 273, topY+12)},
	}
}

func (g *Game) updateEQShadeDrag(cx int, pressed bool) {
	if !pressed {
		g.eqShadeDrag = ""
		return
	}
	if g.eqShadeDrag == "" {
		switch g.pointer.Active() {
		case WidgetEQShadeVol, WidgetEQShadeBal:
			g.eqShadeDrag = g.pointer.Active()
		}
	}
	switch g.eqShadeDrag {
	case WidgetEQShadeVol:
		vol := sliderRatioX(cx, image.Rect(61, 0, 158, 14), 3)
		g.setTransientStatus(fmt.Sprintf("VOLUME: %d%%", int(math.Round(vol*100))))
		if g.ctrl != nil && math.Abs(vol-g.snap.Volume) > 0.005 {
			g.ctrl.Send(playback.SetVolume{Volume: vol})
			g.markConfigDirty()
		}
	case WidgetEQShadeBal:
		ratio := sliderRatioX(cx, image.Rect(164, 0, 206, 14), 3)
		bal := ratio*2.0 - 1.0
		if math.Abs(bal) < 0.06 {
			bal = 0
		}
		if math.Abs(g.balance-bal) > 0.005 {
			g.markConfigDirty()
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
	}
}

func (g *Game) updateEQDrag(topY, cy int, pressed bool) {
	if !pressed {
		g.eqDragBand = -1
		return
	}
	if g.eqDragBand < 0 {
		active := g.pointer.Active()
		switch active {
		case WidgetEQPreamp:
			g.eqDragBand = 10
		default:
			for i, id := range eqBandWidgetIDs {
				if active == id {
					g.eqDragBand = i
					break
				}
			}
		}
	}
	if g.eqDragBand < 0 {
		return
	}
	sliderRect := image.Rect(0, topY+38, 14, topY+100)
	ratio := sliderRatioY(cy, sliderRect, 11)
	val := (1.0 - ratio) * 100.0
	if g.eqDragBand == 10 {
		if math.Abs(g.eqPreamp-val) > 0.1 {
			g.eqPreamp = val
			g.setTransientStatus("EQ PREAMP")
			g.applyEQ()
			g.markEQDirty()
		}
	} else if g.eqDragBand >= 0 && g.eqDragBand < 10 {
		if math.Abs(g.eqBands[g.eqDragBand]-val) > 0.1 {
			g.eqBands[g.eqDragBand] = val
			g.setTransientStatus("EQ BAND")
			g.applyEQ()
			g.markEQDirty()
		}
	}
}

func (g *Game) onEQClick(id string) bool {
	switch id {
	case WidgetEQShade:
		g.shadeEQ = !g.shadeEQ
		g.eqDragBand = -1
		g.eqShadeDrag = ""
		g.syncWindowSize()
		g.markConfigDirty()
		return true
	case WidgetEQClose:
		g.showEQ = false
		g.syncWindowSize()
		g.markConfigDirty()
		return true
	case WidgetEQOn:
		g.eqOn = !g.eqOn
		g.applyEQ()
		g.markEQDirty()
		return true
	case WidgetEQAuto:
		g.eqAuto = !g.eqAuto
		g.markEQDirty()
		return true
	case WidgetEQPresets:
		g.eqPresetIdx = (g.eqPresetIdx + 1) % len(eqPresets)
		p := eqPresets[g.eqPresetIdx]
		g.eqPreamp = p.Preamp
		g.eqBands = p.Bands
		g.setTransientStatus("EQ: " + p.Name)
		g.applyEQ()
		g.markEQDirty()
		return true
	case WidgetEQPlus12:
		for i := range g.eqBands {
			g.eqBands[i] = 100
		}
		g.applyEQ()
		g.markEQDirty()
		return true
	case WidgetEQZero:
		for i := range g.eqBands {
			g.eqBands[i] = 50
		}
		g.applyEQ()
		g.markEQDirty()
		return true
	case WidgetEQMinus12:
		for i := range g.eqBands {
			g.eqBands[i] = 0
		}
		g.applyEQ()
		g.markEQDirty()
		return true
	}
	return false
}

func (g *Game) drawEQ(screen *ebiten.Image, topY int) {
	if g.view == nil {
		return
	}
	g.drawSprite(screen, skin.EQWindowBackground, image.Pt(0, topY))

	titleSprite := skin.EQTitleBar
	if g.focusedPanel == panelEQ && ebiten.IsFocused() {
		titleSprite = skin.EQTitleBarSelected
	}
	g.drawSprite(screen, titleSprite, image.Pt(0, topY))

	if g.pointer.Active() == WidgetEQShade && g.pointer.Pressed() {
		if g.view.HasSprite(skin.EQMinimizeButtonActive) {
			g.drawSprite(screen, skin.EQMinimizeButtonActive, image.Pt(254, topY+3))
		} else {
			vector.DrawFilledRect(screen, 254, float32(topY+3), 9, 9, color.RGBA{A: 0x48}, false)
		}
	}

	closePressed := g.pointer.Active() == WidgetEQClose && g.pointer.Pressed()
	closeSprite := buttonSprite(skin.EQCloseButton, skin.EQCloseButtonActive, closePressed)
	g.drawSprite(screen, closeSprite, image.Pt(264, topY+3))

	onSprite := toggleSprite(
		skin.EQOnButton, skin.EQOnButtonDepressed,
		skin.EQOnButtonSelected, skin.EQOnButtonSelectedDepress,
		g.eqOn, g.pointer.Active() == WidgetEQOn && g.pointer.Pressed(),
	)
	g.drawSprite(screen, onSprite, image.Pt(14, topY+18))

	autoSprite := toggleSprite(
		skin.EQAutoButton, skin.EQAutoButtonDepressed,
		skin.EQAutoButtonSelected, skin.EQAutoButtonSelectedDepres,
		g.eqAuto, g.pointer.Active() == WidgetEQAuto && g.pointer.Pressed(),
	)
	g.drawSprite(screen, autoSprite, image.Pt(40, topY+18))

	presetSprite := buttonSprite(
		skin.EQPresetsButton, skin.EQPresetsButtonSelected,
		g.pointer.Active() == WidgetEQPresets && g.pointer.Pressed(),
	)
	g.drawSprite(screen, presetSprite, image.Pt(217, topY+18))

	g.drawEQGraph(screen, 86, topY+17)

	g.drawEQSlider(screen, 21, topY+38, g.eqPreamp, g.eqDragBand == 10)
	for i, x := range eqBandXOffsets {
		g.drawEQSlider(screen, x, topY+38, g.eqBands[i], g.eqDragBand == i)
	}
}

func (g *Game) drawEQSlider(screen *ebiten.Image, x, y int, val float64, active bool) {
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest}
	op.GeoM.Translate(float64(x), float64(y))
	screen.DrawImage(g.view.EQFrame(val), op)

	thumbSprite := skin.EQSliderThumb
	if active {
		thumbSprite = skin.EQSliderThumbSelected
	}
	thumbY := y + int(math.Round((1.0-clampEQVal(val)/100.0)*51.0))
	g.drawSprite(screen, thumbSprite, image.Pt(x+1, thumbY))
}

func (g *Game) drawEQGraph(screen *ebiten.Image, x, y int) {
	// Classic 275x294 eqmain.bmp sheets omit the y=294..314 graph section
	// intentionally when the skin has no EQ graph (e.g. Bento Classified).
	if !g.view.HasSprite(skin.EQGraphBackground) && !g.view.HasSprite(skin.EQGraphLineColors) {
		return
	}
	if g.view.HasSprite(skin.EQGraphBackground) {
		g.drawSprite(screen, skin.EQGraphBackground, image.Pt(x, y))
	}

	if g.view.HasSprite(skin.EQPreampLine) {
		preampY := y + int(math.Round((1.0-clampEQVal(g.eqPreamp)/100.0)*18.0))
		g.drawSprite(screen, skin.EQPreampLine, image.Pt(x, preampY))
	}

	var prevX, prevY float32
	for i, val := range g.eqBands {
		px := float32(x) + float32(i)*112.0/9.0
		row := int(math.Round((1.0 - clampEQVal(val)/100.0) * 18.0))
		py := float32(y + row)
		if i > 0 {
			col := g.view.EQGraphColor(row)
			vector.StrokeLine(screen, prevX+0.5, prevY+0.5, px+0.5, py+0.5, 1, col, false)
		}
		prevX, prevY = px, py
	}
	// Highlight band anchor dots for readability.
	for i, val := range g.eqBands {
		px := float32(x) + float32(i)*112.0/9.0
		row := int(math.Round((1.0 - clampEQVal(val)/100.0) * 18.0))
		py := float32(y + row)
		col := g.view.EQGraphColor(row)
		vector.DrawFilledRect(screen, px, py, 1.5, 1.5, color.RGBA{R: col.R, G: col.G, B: col.B, A: 0xff}, false)
	}
}

func (g *Game) drawEQShade(screen *ebiten.Image, topY int) {
	if g.view == nil {
		return
	}
	focused := g.focusedPanel == panelEQ && ebiten.IsFocused()
	volFrac := clampUnit(g.snap.Volume)
	volThumbX := 61 + int(math.Round(volFrac*float64(97-3)))
	balFrac := clampUnit((g.balance + 1.0) / 2.0)
	balThumbX := 164 + int(math.Round(balFrac*float64(42-3)))

	if g.view.HasSprite(skin.EQShadeBg) && g.view.HasSprite(skin.EQShadeBgSelected) {
		bgSprite := skin.EQShadeBg
		if focused {
			bgSprite = skin.EQShadeBgSelected
		}
		g.drawSprite(screen, bgSprite, image.Pt(0, topY))

		volSprite := skin.EQShadeVolSliderCenter
		switch {
		case volFrac < 0.33 && g.view.HasSprite(skin.EQShadeVolSliderLeft):
			volSprite = skin.EQShadeVolSliderLeft
		case volFrac > 0.67 && g.view.HasSprite(skin.EQShadeVolSliderRight):
			volSprite = skin.EQShadeVolSliderRight
		}
		if g.view.HasSprite(volSprite) {
			g.drawSprite(screen, volSprite, image.Pt(volThumbX, topY+4))
		}

		balSprite := skin.EQShadeBalSliderCenter
		switch {
		case g.balance < -0.15 && g.view.HasSprite(skin.EQShadeBalSliderLeft):
			balSprite = skin.EQShadeBalSliderLeft
		case g.balance > 0.15 && g.view.HasSprite(skin.EQShadeBalSliderRight):
			balSprite = skin.EQShadeBalSliderRight
		}
		if g.view.HasSprite(balSprite) {
			g.drawSprite(screen, balSprite, image.Pt(balThumbX, topY+4))
		}

		if g.pointer.Active() == WidgetEQShade && g.pointer.Pressed() && g.view.HasSprite(skin.EQMaximizeButtonActive) {
			g.drawSprite(screen, skin.EQMaximizeButtonActive, image.Pt(254, topY+3))
		}
		closePressed := g.pointer.Active() == WidgetEQClose && g.pointer.Pressed()
		if closePressed && g.view.HasSprite(skin.EQShadeCloseActive) {
			g.drawSprite(screen, skin.EQShadeCloseActive, image.Pt(264, topY+3))
		} else if !closePressed && g.view.HasSprite(skin.EQShadeCloseButton) {
			g.drawSprite(screen, skin.EQShadeCloseButton, image.Pt(264, topY+3))
		}
		return
	}

	// Fallback for classic skins that omit eq_ex.bmp.
	titleSprite := skin.EQTitleBar
	if focused {
		titleSprite = skin.EQTitleBarSelected
	}
	g.drawSprite(screen, titleSprite, image.Pt(0, topY))

	trackCol := color.RGBA{R: 0x0c, G: 0x0f, B: 0x14, A: 0xff}
	thumbCol := color.RGBA{R: 0x38, G: 0xc0, B: 0x70, A: 0xff}
	vector.DrawFilledRect(screen, 61, float32(topY+4), 97, 7, trackCol, false)
	vector.DrawFilledRect(screen, float32(volThumbX), float32(topY+4), 3, 7, thumbCol, false)
	vector.DrawFilledRect(screen, 164, float32(topY+4), 42, 7, trackCol, false)
	vector.DrawFilledRect(screen, float32(balThumbX), float32(topY+4), 3, 7, thumbCol, false)

	if g.pointer.Active() == WidgetEQShade && g.pointer.Pressed() {
		if g.view.HasSprite(skin.EQMaximizeFallback) {
			g.drawSprite(screen, skin.EQMaximizeFallback, image.Pt(254, topY+3))
		} else {
			vector.DrawFilledRect(screen, 254, float32(topY+3), 9, 9, color.RGBA{A: 0x48}, false)
		}
	}
	closePressed := g.pointer.Active() == WidgetEQClose && g.pointer.Pressed()
	closeSprite := buttonSprite(skin.EQCloseButton, skin.EQCloseButtonActive, closePressed)
	g.drawSprite(screen, closeSprite, image.Pt(264, topY+3))
}
