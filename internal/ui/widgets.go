package ui

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/goregular"

	"ytm-wmp/internal/playback"
	"ytm-wmp/internal/skin"
	"ytm-wmp/internal/ui/skinview"
)

const (
	doubleClickWindow = 400 * time.Millisecond
	marqueeStepFrames = 3
	marqueeSeparator  = "  ***  "
	marqueeMaxChars   = 30
)

type doubleClickTracker struct {
	lastIdx  int
	lastTime time.Time
}

func (d *doubleClickTracker) Observe(idx int, now time.Time) bool {
	isDouble := idx == d.lastIdx && !d.lastTime.IsZero() && now.Sub(d.lastTime) <= doubleClickWindow
	if isDouble {
		d.lastIdx = -1
		d.lastTime = time.Time{}
		return true
	}
	d.lastIdx = idx
	d.lastTime = now
	return false
}

func buttonSprite(normal, depressed string, isPressed bool) string {
	if isPressed {
		return depressed
	}
	return normal
}

func toggleSprite(normal, depressed, selected, selectedDepressed string, isSelected, isPressed bool) string {
	switch {
	case isSelected && isPressed:
		return selectedDepressed
	case isSelected:
		return selected
	case isPressed:
		return depressed
	default:
		return normal
	}
}

func sliderRatioX(cursorX int, bounds image.Rectangle, thumbW int) float64 {
	travel := bounds.Dx() - thumbW
	if travel <= 0 {
		return 0
	}
	rel := float64(cursorX-bounds.Min.X-thumbW/2) / float64(travel)
	if rel < 0 {
		return 0
	}
	if rel > 1 {
		return 1
	}
	return rel
}

func sliderRatioY(cursorY int, bounds image.Rectangle, thumbH int) float64 {
	travel := bounds.Dy() - thumbH
	if travel <= 0 {
		return 0
	}
	rel := float64(cursorY-bounds.Min.Y-thumbH/2) / float64(travel)
	if rel < 0 {
		return 0
	}
	if rel > 1 {
		return 1
	}
	return rel
}

func initUITextFace() text.Face {
	src, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		return nil
	}
	return &text.GoTextFace{
		Source: src,
		Size:   9,
	}
}

func drawTTFText(dst *ebiten.Image, face text.Face, s string, x, y int, col color.Color) {
	if face == nil || s == "" {
		return
	}
	op := &text.DrawOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.ScaleWithColor(col)
	text.Draw(dst, s, face, op)
}

func measureTTFWidth(face text.Face, s string) int {
	if face == nil || s == "" {
		return len(s) * 5
	}
	w, _ := text.Measure(s, face, 0)
	return int(math.Ceil(w))
}

func ellipsizeTTF(face text.Face, s string, maxWidth float64) string {
	if face == nil || s == "" || maxWidth <= 0 {
		return ""
	}
	w, _ := text.Measure(s, face, 0)
	if w <= maxWidth {
		return s
	}
	ellipsis := "..."
	ew, _ := text.Measure(ellipsis, face, 0)
	if ew >= maxWidth {
		return ""
	}
	runes := []rune(s)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		cw, _ := text.Measure(string(runes[:mid]), face, 0)
		if cw+ew <= maxWidth {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(runes[:lo]) + ellipsis
}

// drawBitmapString draws 5x6 text.bmp glyphs clipped to dstRect, shifted left by scrollPx.
func drawBitmapString(dst *ebiten.Image, view *skinview.View, face text.Face, s string, dstRect image.Rectangle, scrollPx int, fallbackCol color.Color) {
	if dst == nil || s == "" {
		return
	}
	clipped, ok := dst.SubImage(dstRect).(*ebiten.Image)
	if !ok {
		return
	}
	if view == nil || !view.HasSprite(skin.CharacterSpriteName('a')) {
		drawTTFText(clipped, face, s, dstRect.Min.X-scrollPx, dstRect.Min.Y-2, fallbackCol)
		return
	}

	x := dstRect.Min.X - scrollPx
	y := dstRect.Min.Y
	for _, r := range s {
		if x >= dstRect.Max.X {
			break
		}
		if x+5 > dstRect.Min.X {
			glyph, ok := view.Glyph(r)
			if ok {
				op := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest}
				op.GeoM.Translate(float64(x), float64(y))
				clipped.DrawImage(glyph, op)
			}
		}
		x += 5
	}
}

func drawMarquee(dst *ebiten.Image, view *skinview.View, face text.Face, msg string, tick uint64) {
	if msg == "" {
		return
	}
	clipRect := image.Rect(111, 27, 265, 33)
	runes := []rune(msg)
	if len(runes) <= marqueeMaxChars {
		drawBitmapString(dst, view, face, msg, clipRect, 0, color.RGBA{0xc4, 0xcf, 0xde, 0xff})
		return
	}
	looped := msg + marqueeSeparator + msg
	loopWidth := (len(runes) + len([]rune(marqueeSeparator))) * 5
	scrollPx := 0
	if loopWidth > 0 {
		scrollPx = int(tick/marqueeStepFrames) % loopWidth
	}
	drawBitmapString(dst, view, face, looped, clipRect, scrollPx, color.RGBA{0xc4, 0xcf, 0xde, 0xff})
}

func drawTimeDisplay(dst *ebiten.Image, view *skinview.View, pos, dur time.Duration, state playback.State, remaining bool, blinkOff bool) {
	if view == nil || state == playback.StateIdle {
		return
	}
	if state == playback.StatePaused && blinkOff {
		return
	}

	display := pos
	showMinus := false
	if remaining && dur > 0 {
		rem := dur - pos
		if rem < 0 {
			rem = 0
		}
		display = rem
		showMinus = true
	}
	if display < 0 {
		display = 0
	}

	totalSecs := int(math.Floor(display.Seconds()))
	mins := (totalSecs / 60) % 100
	secs := totalSecs % 60
	useEx := view.HasNumbersEx()

	if showMinus {
		minusName := skin.MinusSpriteName(useEx)
		if view.HasSprite(minusName) {
			op := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest}
			if useEx {
				op.GeoM.Translate(39, 26)
			} else {
				op.GeoM.Translate(38, 32)
			}
			dst.DrawImage(view.Sprite(minusName), op)
		}
	}

	digits := [4]struct {
		val int
		x   int
	}{
		{mins / 10, 48},
		{mins % 10, 60},
		{secs / 10, 78},
		{secs % 10, 90},
	}
	for _, d := range digits {
		name := skin.NumberSpriteName(d.val, useEx)
		op := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest}
		op.GeoM.Translate(float64(d.x), 26)
		dst.DrawImage(view.Sprite(name), op)
	}
}
