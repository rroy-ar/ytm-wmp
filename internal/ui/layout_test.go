package ui

import (
	"image"
	"testing"

	"ytm-wmp/internal/skin"
)

func TestLayoutIsFixedLogicalCanvas(t *testing.T) {
	g := &Game{}
	if width, height := g.Layout(1920, 1080); width != WindowWidth || height != WindowHeight {
		t.Fatalf("Layout = %dx%d, want %dx%d", width, height, WindowWidth, WindowHeight)
	}
}

func TestTransportWidgetsMatchButtonPlacements(t *testing.T) {
	want := map[string]image.Rectangle{
		WidgetPrevious: image.Rect(16, 88, 39, 106),
		WidgetPlay:     image.Rect(39, 88, 62, 106),
		WidgetPause:    image.Rect(62, 88, 85, 106),
		WidgetStop:     image.Rect(85, 88, 108, 106),
		WidgetNext:     image.Rect(108, 88, 131, 106),
	}
	if len(mainWidgets) != len(want) {
		t.Fatalf("len(mainWidgets) = %d, want %d", len(mainWidgets), len(want))
	}
	for _, widget := range mainWidgets {
		if got := widget.Bounds; got != want[widget.ID] {
			t.Errorf("%s bounds = %v, want %v", widget.ID, got, want[widget.ID])
		}
	}
}

func TestAllPlacementsReferenceKnownSprites(t *testing.T) {
	for _, p := range mainPlacements {
		def, ok := skin.Definition(p.Sprite)
		if !ok {
			t.Errorf("placement %q has no sprite definition", p.Sprite)
			continue
		}
		if def.Src.Dx() <= 0 || def.Src.Dy() <= 0 {
			t.Errorf("placement %q has empty source rect %v", p.Sprite, def.Src)
		}
		if p.At.X < 0 || p.At.Y < 0 || p.At.X+def.Src.Dx() > WindowWidth || p.At.Y+def.Src.Dy() > WindowHeight {
			t.Errorf("placement %q at %v with size %v is outside the %dx%d window",
				p.Sprite, p.At, def.Src.Size(), WindowWidth, WindowHeight)
		}
	}
}

func TestWidgetsStayInsideWindow(t *testing.T) {
	for _, widget := range mainWidgets {
		if !widget.Bounds.In(image.Rect(0, 0, WindowWidth, WindowHeight)) {
			t.Errorf("widget %q bounds %v are outside the window", widget.ID, widget.Bounds)
		}
	}
}
