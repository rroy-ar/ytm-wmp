package ui

import (
	"image"
	"testing"
)

var testWidgets = []Widget{
	{ID: "back", Bounds: image.Rect(0, 0, 20, 20)},
	{ID: "front", Bounds: image.Rect(10, 10, 30, 30)},
}

func TestClickCompletesOnRelease(t *testing.T) {
	var p Pointer

	if got := p.Update(5, 5, true, testWidgets); got != "" {
		t.Fatalf("press clicked %q, want no click until release", got)
	}
	if got := p.Active(); got != "back" {
		t.Fatalf("active = %q, want back", got)
	}
	if got := p.Update(5, 5, false, testWidgets); got != "back" {
		t.Fatalf("release clicked %q, want back", got)
	}
	if got := p.Active(); got != "" {
		t.Fatalf("active after release = %q, want empty", got)
	}
}

func TestReleaseOutsideCancelsClick(t *testing.T) {
	var p Pointer

	p.Update(5, 5, true, testWidgets)
	if got := p.Update(100, 100, false, testWidgets); got != "" {
		t.Fatalf("release outside clicked %q, want nothing", got)
	}
}

func TestDraggingBackInsideStillClicks(t *testing.T) {
	var p Pointer

	p.Update(5, 5, true, testWidgets)
	p.Update(100, 100, true, testWidgets)
	if got := p.Update(6, 6, false, testWidgets); got != "back" {
		t.Fatalf("release after dragging back clicked %q, want back", got)
	}
}

func TestTopmostWidgetWins(t *testing.T) {
	var p Pointer

	p.Update(15, 15, false, testWidgets)
	if got := p.Hot(); got != "front" {
		t.Fatalf("hot = %q, want front (later widget is on top)", got)
	}

	p.Update(15, 15, true, testWidgets)
	if got := p.Active(); got != "front" {
		t.Fatalf("active = %q, want front", got)
	}
}

func TestPressStartedOutsideDoesNotClick(t *testing.T) {
	var p Pointer

	p.Update(100, 100, true, testWidgets) // first observation: button already down
	p.Update(5, 5, true, testWidgets)     // dragged into a widget
	if got := p.Update(5, 5, false, testWidgets); got != "" {
		t.Fatalf("release clicked %q, want nothing after a press started outside", got)
	}
}

func TestHotTracksPointerAndClearsOutside(t *testing.T) {
	var p Pointer

	p.Update(5, 5, false, testWidgets)
	if got := p.Hot(); got != "back" {
		t.Fatalf("hot = %q, want back", got)
	}
	p.Update(50, 50, false, testWidgets)
	if got := p.Hot(); got != "" {
		t.Fatalf("hot outside = %q, want empty", got)
	}
}
