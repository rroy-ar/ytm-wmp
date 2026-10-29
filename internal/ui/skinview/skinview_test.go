package skinview

import "testing"

func TestPlaceholderSizeUsesSpriteDefinition(t *testing.T) {
	width, height := placeholderSize("MAIN_WINDOW_BACKGROUND")
	if width != 275 || height != 116 {
		t.Fatalf("placeholderSize(main) = %dx%d, want 275x116", width, height)
	}

	width, height = placeholderSize("NOT_A_SPRITE")
	if width != 16 || height != 16 {
		t.Fatalf("placeholderSize(unknown) = %dx%d, want 16x16", width, height)
	}
}
