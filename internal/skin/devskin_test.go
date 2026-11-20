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

	for _, name := range []string{
		MainBackground, TitlebarActive, TitlebarIdle,
		MainShadeButton, MainShadeActive, MainShadeBg, MainShadeBgSelected,
		MainShadeBtnSelected, MainShadeBtnDepressed, MainShadePosBg, MainShadePosThumb,
		MainClutterBar, MainClutterDSelected,
		PlayButton, PlayButtonActive, PauseButton, PauseButtonActive,
		MainShuffleButton, MainRepeatButton, MainEQButton, MainPlaylistButton,
		MainPositionBackground, MainPositionThumb,
		MainVolumeBackground, MainVolumeThumb,
		MainBalanceBackground, MainBalanceThumb,
		MainPlayingIndicator, MainStereoSelected,
		PlaylistTopLeftCorner, PlaylistBottomLeftCorner, PlaylistBottomRightCorner,
		PlaylistShadeBg, PlaylistShadeBgLeft, PlaylistShadeBgRight, PlaylistShadeBgRightSelect,
		PlaylistCollapseSelected, PlaylistExpandSelected,
		EQWindowBackground, EQTitleBarSelected, EQSliderBackground, EQGraphBackground,
		EQShadeBg, EQShadeBgSelected, EQShadeVolSliderCenter, EQShadeBalSliderCenter,
		EQMaximizeButtonActive, EQMinimizeButtonActive, EQShadeCloseButton, EQShadeCloseActive,
	} {
		if _, _, ok := s.Lookup(name); !ok {
			t.Errorf("dev skin is missing sprite %q", name)
		}
	}

	wantSizes := map[string]image.Point{
		"main.bmp":     image.Pt(275, 116),
		"cbuttons.bmp": image.Pt(136, 36),
		"titlebar.bmp": image.Pt(344, 87),
		"shufrep.bmp":  image.Pt(92, 85),
		"posbar.bmp":   image.Pt(307, 10),
		"volume.bmp":   image.Pt(68, 433),
		"balance.bmp":  image.Pt(68, 433),
		"playpaus.bmp": image.Pt(48, 9),
		"monoster.bmp": image.Pt(56, 24),
		"numbers.bmp":  image.Pt(90, 13),
		"text.bmp":     image.Pt(155, 18),
		"pledit.bmp":   image.Pt(280, 186),
		"eqmain.bmp":   image.Pt(275, 315),
		"eq_ex.bmp":    image.Pt(275, 56),
	}
	for sheet, want := range wantSizes {
		img, ok := s.Sheet(sheet)
		if !ok {
			t.Errorf("dev skin missing sheet %q", sheet)
			continue
		}
		if got := img.Bounds().Size(); got != want {
			t.Errorf("%s size = %v, want %v", sheet, got, want)
		}
	}
}
