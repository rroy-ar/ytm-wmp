package ui

import (
	"image"
	"image/color"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

type toastKind int

const (
	toastInfo toastKind = iota
	toastWarn
	toastError
)

const (
	defaultInfoToastTTL = 4 * time.Second
	defaultWarnToastTTL = 6 * time.Second
)

type toastItem struct {
	source      string
	kind        toastKind
	message     string
	actionLabel string
	actionID    string
	expiresAt   time.Time
}

type toastState struct {
	toasts []toastItem
}

func (g *Game) pushToast(kind toastKind, message string, ttl time.Duration) {
	g.pushActionToast("", kind, message, "", "", ttl)
}

func (g *Game) pushActionToast(source string, kind toastKind, message, actionLabel, actionID string, ttl time.Duration) {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return
	}
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	item := toastItem{
		source:      source,
		kind:        kind,
		message:     msg,
		actionLabel: actionLabel,
		actionID:    actionID,
		expiresAt:   exp,
	}
	if source != "" {
		for i := range g.toasts {
			if g.toasts[i].source == source {
				g.toasts[i] = item
				return
			}
		}
	}
	const maxToasts = 4
	if len(g.toasts) >= maxToasts {
		g.toasts = append(g.toasts[1:], item)
		return
	}
	g.toasts = append(g.toasts, item)
}

func (g *Game) clearToastSource(source string) {
	if source == "" || len(g.toasts) == 0 {
		return
	}
	n := 0
	for _, t := range g.toasts {
		if t.source != source {
			g.toasts[n] = t
			n++
		}
	}
	g.toasts = g.toasts[:n]
}

func (g *Game) expireToasts(now time.Time) {
	if len(g.toasts) == 0 {
		return
	}
	n := 0
	for _, t := range g.toasts {
		if t.expiresAt.IsZero() || now.Before(t.expiresAt) {
			g.toasts[n] = t
			n++
		}
	}
	g.toasts = g.toasts[:n]
}

func (g *Game) activeToast() (toastItem, bool) {
	if len(g.toasts) > 0 {
		return g.toasts[len(g.toasts)-1], true
	}
	if g.errText != "" {
		return toastItem{source: "error", kind: toastError, message: g.errText}, true
	}
	return toastItem{}, false
}

func (g *Game) dismissActiveToast() bool {
	if len(g.toasts) == 0 && g.errText == "" {
		return false
	}
	if len(g.toasts) > 0 {
		top := g.toasts[len(g.toasts)-1]
		g.toasts = g.toasts[:len(g.toasts)-1]
		if top.source == "error" {
			g.errText = ""
		}
		return true
	}
	g.errText = ""
	return true
}

func (g *Game) toastLayout(t toastItem) (box, dismissRect, actionRect image.Rectangle, lines []string) {
	const maxLines = 4
	lines = wrapText(t.message, 38)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = strings.TrimSuffix(lines[maxLines-1], " ") + "..."
	}
	hasAction := t.actionLabel != "" && t.actionID != ""
	height := 10 + len(lines)*13
	if hasAction {
		height += 18
	}
	y := 17
	if mainH := g.mainHeight(); g.canvasHeight() > mainH {
		y = mainH + 3
	}
	box = image.Rect(5, y, 270, y+height)
	dismissRect = image.Rect(box.Max.X-16, box.Min.Y+2, box.Max.X-2, box.Min.Y+15)
	if hasAction {
		actionRect = image.Rect(box.Min.X+6, box.Max.Y-18, box.Min.X+76, box.Max.Y-3)
	}
	return box, dismissRect, actionRect, lines
}

func (g *Game) toastWidgets() []Widget {
	t, ok := g.activeToast()
	if !ok {
		return nil
	}
	_, dismissRect, actionRect, _ := g.toastLayout(t)
	widgets := []Widget{{ID: WidgetToastDismiss, Bounds: dismissRect}}
	if !actionRect.Empty() {
		widgets = append(widgets, Widget{ID: WidgetToastAction, Bounds: actionRect})
	}
	return widgets
}

func (g *Game) onToastClick(id string) bool {
	switch id {
	case WidgetToastDismiss:
		g.dismissActiveToast()
		return true
	case WidgetToastAction:
		t, ok := g.activeToast()
		if !ok {
			return false
		}
		switch t.actionID {
		case "open_settings":
			g.dismissActiveToast()
			g.openSettings()
		}
		return true
	}
	return false
}

func (g *Game) drawToast(screen *ebiten.Image) {
	t, ok := g.activeToast()
	if !ok {
		return
	}
	box, dismissRect, actionRect, lines := g.toastLayout(t)

	bg := color.RGBA{R: 0x10, G: 0x26, B: 0x36, A: 0xeb}
	border := color.RGBA{R: 0x38, G: 0xc0, B: 0x70, A: 0xff}
	switch t.kind {
	case toastWarn:
		bg = color.RGBA{R: 0x3b, G: 0x2a, B: 0x08, A: 0xeb}
		border = color.RGBA{R: 0xe6, G: 0xb8, B: 0x4c, A: 0xff}
	case toastError:
		bg = color.RGBA{R: 0x50, G: 0x08, B: 0x08, A: 0xeb}
		border = color.RGBA{R: 0xff, G: 0x90, B: 0x90, A: 0xff}
	}

	vector.DrawFilledRect(screen, float32(box.Min.X), float32(box.Min.Y), float32(box.Dx()), float32(box.Dy()), bg, false)
	vector.StrokeRect(screen, float32(box.Min.X)+0.5, float32(box.Min.Y)+0.5, float32(box.Dx()-1), float32(box.Dy()-1), 1, border, false)

	for i, line := range lines {
		ebitenutil.DebugPrintAt(screen, line, box.Min.X+5, box.Min.Y+3+i*13)
	}

	// Dismiss [X] button.
	vector.DrawFilledRect(screen, float32(dismissRect.Min.X), float32(dismissRect.Min.Y), float32(dismissRect.Dx()), float32(dismissRect.Dy()), colButtonBg, false)
	vector.StrokeRect(screen, float32(dismissRect.Min.X)+0.5, float32(dismissRect.Min.Y)+0.5, float32(dismissRect.Dx()-1), float32(dismissRect.Dy()-1), 1, border, false)
	ebitenutil.DebugPrintAt(screen, "X", dismissRect.Min.X+4, dismissRect.Min.Y-1)

	if !actionRect.Empty() {
		g.drawButtonRect(screen, actionRect, WidgetToastAction, t.actionLabel)
	}
}
