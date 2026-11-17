package ui

import (
	"encoding/json"
	"image"
	"image/color"
	"math"
	"os"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/rroy-ar/ytm-wmp/internal/skin"
	"github.com/rroy-ar/ytm-wmp/internal/store"
)

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

	eqPath     string
	eqDirty    bool
	eqSaveAt   time.Time
	eqDragBand int // -1 none, 0..9 band, 10 preamp
}

func (g *Game) initEQ() {
	g.eqPreamp = 50
	for i := range g.eqBands {
		g.eqBands[i] = 50
	}
	g.eqDragBand = -1
	g.loadEQ()
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
			g.markEQDirty()
		}
	} else if g.eqDragBand >= 0 && g.eqDragBand < 10 {
		if math.Abs(g.eqBands[g.eqDragBand]-val) > 0.1 {
			g.eqBands[g.eqDragBand] = val
			g.setTransientStatus("EQ BAND")
			g.markEQDirty()
		}
	}
}

func (g *Game) onEQClick(id string) bool {
	switch id {
	case WidgetEQClose:
		g.showEQ = false
		g.syncWindowSize()
		g.markConfigDirty()
		return true
	case WidgetEQOn:
		g.eqOn = !g.eqOn
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
		g.markEQDirty()
		return true
	case WidgetEQPlus12:
		for i := range g.eqBands {
			g.eqBands[i] = 100
		}
		g.markEQDirty()
		return true
	case WidgetEQZero:
		for i := range g.eqBands {
			g.eqBands[i] = 50
		}
		g.markEQDirty()
		return true
	case WidgetEQMinus12:
		for i := range g.eqBands {
			g.eqBands[i] = 0
		}
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
	g.drawSprite(screen, skin.EQGraphBackground, image.Pt(x, y))

	preampY := y + int(math.Round((1.0-clampEQVal(g.eqPreamp)/100.0)*18.0))
	g.drawSprite(screen, skin.EQPreampLine, image.Pt(x, preampY))

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
