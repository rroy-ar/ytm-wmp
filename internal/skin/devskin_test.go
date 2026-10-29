package skin

import (
	"image"
	"path/filepath"
	"testing"
)

// TestDevSkinLoads guards the asset that a bare `go run ./cmd/ytm-wmp` uses.
// It also catches accidental PNG/JPEG conversions, wrong dimensions, and
// renamed sheets before they turn into a blank window.
func TestDevSkinLoads(t *testing.T) {
	path := filepath.Join("..", "..", "assets", "dev-skin")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open(dev skin) = %v", err)
	}

	for _, name := range []string{MainBackground, TitlebarActive, TitlebarIdle, PlayButton, PlayButtonActive, PauseButton, PauseButtonActive} {
		if _, _, ok := s.Lookup(name); !ok {
			t.Errorf("dev skin is missing sprite %q", name)
		}
	}

	if img, _ := s.Sheet("main.bmp"); img.Bounds().Size() != image.Pt(275, 116) {
		t.Errorf("main.bmp size = %v, want 275x116", img.Bounds().Size())
	}
	if img, _ := s.Sheet("cbuttons.bmp"); img.Bounds().Size() != image.Pt(136, 36) {
		t.Errorf("cbuttons.bmp size = %v, want 136x36", img.Bounds().Size())
	}
	if img, _ := s.Sheet("titlebar.bmp"); img.Bounds().Size() != image.Pt(312, 87) {
		t.Errorf("titlebar.bmp size = %v, want 312x87", img.Bounds().Size())
	}
}
