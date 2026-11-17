package ui

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/rroy-ar/ytm-wmp/internal/audio"
)

const (
	visWidth  = 76
	visHeight = 16
	visBars   = audio.VisBarCount
)

type visMode int

const (
	visModeSpectrum visMode = iota
	visModeOscilloscope
	visModeOff
	visModeCount
)

type visSource interface {
	ReadVisSamples(dst []float32) int
}

type visState struct {
	visMode      visMode
	visSampleBuf [audio.VisFFTSize]float32
	visRawBars   [visBars]float64
	visBars      [visBars]float64
	visPeaks     [visBars]float64
	visPeakVel   [visBars]float64
	visScope     [visWidth]float64
}

func (g *Game) cycleVisMode() {
	g.visMode = (g.visMode + 1) % visModeCount
	switch g.visMode {
	case visModeSpectrum:
		g.setTransientStatus("VIS: SPECTRUM")
	case visModeOscilloscope:
		g.setTransientStatus("VIS: OSCILLOSCOPE")
	case visModeOff:
		g.setTransientStatus("VIS: OFF")
	}
}

func (g *Game) updateVisualizer(playing bool) {
	if g.visMode == visModeOff {
		g.decayVisualizer()
		return
	}

	var samples []float32
	if playing {
		if vs, ok := g.sink.(visSource); ok && vs != nil {
			n := vs.ReadVisSamples(g.visSampleBuf[:])
			if n > 0 {
				samples = g.visSampleBuf[:n]
			}
		}
	}

	if len(samples) == 0 {
		g.decayVisualizer()
		return
	}

	switch g.visMode {
	case visModeSpectrum:
		audio.ComputeSpectrum(samples, g.visRawBars[:])
		for i := 0; i < visBars; i++ {
			target := g.visRawBars[i]
			decayed := g.visBars[i] * 0.80
			if target > decayed {
				g.visBars[i] = target
			} else {
				g.visBars[i] = decayed
			}
			if g.visBars[i] >= g.visPeaks[i] {
				g.visPeaks[i] = g.visBars[i]
				g.visPeakVel[i] = 0
			} else {
				g.visPeakVel[i] += 0.0035
				g.visPeaks[i] -= g.visPeakVel[i]
				if g.visPeaks[i] < 0 {
					g.visPeaks[i] = 0
				}
			}
		}
	case visModeOscilloscope:
		window := len(samples)
		if window > visWidth*4 {
			samples = samples[window-visWidth*4:]
			window = len(samples)
		}
		for x := 0; x < visWidth; x++ {
			idx := (x * window) / visWidth
			if idx >= window {
				idx = window - 1
			}
			v := float64(samples[idx])
			if v > 1 {
				v = 1
			} else if v < -1 {
				v = -1
			}
			g.visScope[x] = v
		}
	}
}

func (g *Game) decayVisualizer() {
	for i := 0; i < visBars; i++ {
		g.visBars[i] *= 0.78
		if g.visBars[i] < 0.01 {
			g.visBars[i] = 0
		}
		if g.visPeaks[i] > 0 {
			g.visPeakVel[i] += 0.004
			g.visPeaks[i] -= g.visPeakVel[i]
			if g.visPeaks[i] < 0 {
				g.visPeaks[i] = 0
			}
		}
	}
	for x := 0; x < visWidth; x++ {
		g.visScope[x] *= 0.75
		if math.Abs(g.visScope[x]) < 0.01 {
			g.visScope[x] = 0
		}
	}
}

func (g *Game) drawVisualizer(screen *ebiten.Image) {
	if g.view == nil {
		return
	}
	colors := g.view.VisColors()
	x0 := float32(MainVisBounds.Min.X)
	y0 := float32(MainVisBounds.Min.Y)

	// Fill visualizer background (viscolor[0]).
	vector.DrawFilledRect(screen, x0, y0, visWidth, visHeight, colors[0], false)

	switch g.visMode {
	case visModeSpectrum:
		for i := 0; i < visBars; i++ {
			bx := float32(MainVisBounds.Min.X + i*4)
			lit := int(math.Round(g.visBars[i] * float64(visHeight)))
			if lit > visHeight {
				lit = visHeight
			}
			for r := 0; r < lit; r++ {
				rowY := float32(MainVisBounds.Max.Y - 1 - r)
				// viscolor[17] is bottom (quiet), viscolor[2] is top (loud).
				vector.DrawFilledRect(screen, bx, rowY, 3, 1, colors[17-r], false)
			}
			if g.visPeaks[i] > 0.02 {
				peakRow := int(math.Round(g.visPeaks[i] * float64(visHeight-1)))
				if peakRow >= visHeight {
					peakRow = visHeight - 1
				}
				peakY := float32(MainVisBounds.Max.Y - 1 - peakRow)
				vector.DrawFilledRect(screen, bx, peakY, 3, 1, colors[23], false)
			}
		}
	case visModeOscilloscope:
		centerY := y0 + float32(visHeight)*0.5
		prevX := x0 + 0.5
		prevY := clampVisY(centerY - float32(g.visScope[0])*7.0)
		for x := 1; x < visWidth; x++ {
			cx := x0 + float32(x) + 0.5
			cy := clampVisY(centerY - float32(g.visScope[x])*7.0)
			amp := math.Abs(g.visScope[x])
			cIdx := 18 + int(math.Min(4, amp*4.5))
			vector.StrokeLine(screen, prevX, prevY, cx, cy, 1, colors[cIdx], false)
			prevX, prevY = cx, cy
		}
	}
}

func clampVisY(y float32) float32 {
	minY := float32(MainVisBounds.Min.Y) + 0.5
	maxY := float32(MainVisBounds.Max.Y) - 0.5
	if y < minY {
		return minY
	}
	if y > maxY {
		return maxY
	}
	return y
}
