package ui

import "image"

// Widget is a named hit rectangle in logical (1x) skin coordinates.
//
// Widgets are passed in draw order: when two rectangles overlap, the later one
// wins. That matches the usual immediate-mode bottom-to-top hit test.
type Widget struct {
	ID     string
	Bounds image.Rectangle
}

// Pointer implements hot/active immediate-mode mouse semantics.
//
// A click is reported on release, not on press. Pressing captures the widget
// under the cursor; moving outside and releasing cancels the click, while
// moving back inside before release still completes it. That feels like a
// button and avoids accidentally triggering on a drag.
type Pointer struct {
	x, y    int
	hot     string
	active  string
	pressed bool
}

// Update advances the pointer state and returns a widget ID when a complete
// press/release pair landed on the same widget.
func (p *Pointer) Update(x, y int, pressed bool, widgets []Widget) (clicked string) {
	wasPressed := p.pressed
	p.x, p.y = x, y
	p.hot = topmostAt(x, y, widgets)

	if pressed && !wasPressed {
		p.active = p.hot
	}
	if !pressed && wasPressed {
		if p.active != "" && p.hot == p.active {
			clicked = p.active
		}
		p.active = ""
	}
	p.pressed = pressed
	return clicked
}

// Hot returns the widget currently under the pointer.
func (p *Pointer) Hot() string { return p.hot }

// Active returns the widget captured by the current press.
func (p *Pointer) Active() string { return p.active }

// Pressed reports whether the mouse button is currently down.
func (p *Pointer) Pressed() bool { return p.pressed }

// Position returns the last logical cursor position.
func (p *Pointer) Position() image.Point { return image.Pt(p.x, p.y) }

// topmostAt returns the last matching widget, or "" when none match.
func topmostAt(x, y int, widgets []Widget) string {
	for i := len(widgets) - 1; i >= 0; i-- {
		if image.Pt(x, y).In(widgets[i].Bounds) {
			return widgets[i].ID
		}
	}
	return ""
}
