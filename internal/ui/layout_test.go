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
		WidgetEject:    image.Rect(136, 89, 158, 105),
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
		if p.At.X < 0 || p.At.Y < 0 || p.At.X+def.Src.Dx() > MainWindowWidth || p.At.Y+def.Src.Dy() > MainWindowHeight {
			t.Errorf("placement %q at %v with size %v is outside the %dx%d main window",
				p.Sprite, p.At, def.Src.Size(), MainWindowWidth, MainWindowHeight)
		}
	}
}

func TestWidgetsStayInsideWindowAndDoNotOverlap(t *testing.T) {
	mainArea := image.Rect(0, 0, MainWindowWidth, MainWindowHeight)
	for _, widget := range mainWidgets {
		if !widget.Bounds.In(mainArea) {
			t.Errorf("main widget %q bounds %v are outside main window", widget.ID, widget.Bounds)
		}
	}

	browserArea := image.Rect(0, MainWindowHeight, WindowWidth, WindowHeight)
	for i, widget := range browserWidgets {
		if !widget.Bounds.In(browserArea) {
			t.Errorf("browser widget %q bounds %v are outside browser panel %v", widget.ID, widget.Bounds, browserArea)
		}
		for j := i + 1; j < len(browserWidgets); j++ {
			if widget.Bounds.Overlaps(browserWidgets[j].Bounds) {
				t.Errorf("browser widgets %q and %q overlap", widget.ID, browserWidgets[j].ID)
			}
		}
	}
}
