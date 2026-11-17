package skinview

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"

	"github.com/rroy-ar/ytm-wmp/internal/skin"
)

func TestViewCachesSprites(t *testing.T) {
	s, err := skin.Open(filepath.Join("..", "..", "..", "assets", "dev-skin"))
	if err != nil {
		t.Fatalf("Open(dev skin) = %v", err)
	}
	view := New(s)

	for _, name := range []string{skin.MainBackground, skin.TitlebarActive, skin.PlayButton, skin.PauseButton} {
		if view.Missing(name) {
			t.Errorf("%s unexpectedly marked missing", name)
			continue
		}
		def, _ := skin.Definition(name)
		if got, want := view.Sprite(name).Bounds().Size(), def.Src.Size(); got != want {
			t.Errorf("%s sprite size = %v, want %v", name, got, want)
		}
	}

	first := view.Sprite(skin.PlayButton)
	second := view.Sprite(skin.PlayButton)
	if first != second {
		t.Fatal("Sprite did not return the cached SubImage")
	}

	if _, ok := view.SheetSize("main.bmp"); !ok {
		t.Fatal("SheetSize(main.bmp) failed")
	}
	if got, ok := view.SheetSize("nope.bmp"); ok {
		t.Fatalf("SheetSize(nope.bmp) = %v, true; want false", got)
	}
}

func TestViewUsesSizedPlaceholderForMissingSprite(t *testing.T) {
	dir := t.TempDir()
	mainData, err := os.ReadFile(filepath.Join("..", "..", "..", "assets", "dev-skin", "main.bmp"))
	if err != nil {
		t.Fatalf("read main.bmp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MAIN.BMP"), mainData, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := skin.Open(dir)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	view := New(s)

	if !view.Missing(skin.PlayButton) {
		t.Fatal("play button should be missing without cbuttons.bmp")
	}
	def, _ := skin.Definition(skin.PlayButton)
	if got, want := view.Sprite(skin.PlayButton).Bounds().Size(), def.Src.Size(); got != want {
		t.Fatalf("placeholder size = %v, want %v", got, want)
	}
	if got, want := view.Sprite("UNKNOWN").Bounds().Size(), image.Pt(16, 16); got != want {
		t.Fatalf("unknown placeholder size = %v, want %v", got, want)
	}
	if !view.Missing("UNKNOWN") {
		t.Fatal("unknown sprite should be marked missing")
	}
}

func TestViewTreatsTruncatedSheetAsMissing(t *testing.T) {
	dir := t.TempDir()
	mainData, err := os.ReadFile(filepath.Join("..", "..", "..", "assets", "dev-skin", "main.bmp"))
	if err != nil {
		t.Fatalf("read main.bmp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MAIN.BMP"), mainData, 0o600); err != nil {
		t.Fatal(err)
	}
	// A cbuttons sheet that exists but is far too small for its sprites.
	tiny := image.NewRGBA(image.Rect(0, 0, 10, 10))
	f, err := os.Create(filepath.Join(dir, "CBUTTONS.BMP"))
	if err != nil {
		t.Fatal(err)
	}
	if err := bmp.Encode(f, tiny); err != nil {
		f.Close()
		t.Fatalf("write tiny cbuttons: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := skin.Open(dir)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	view := New(s)

	if !view.Missing(skin.PlayButton) {
		t.Fatal("truncated cbuttons sheet should mark the play button missing")
	}
	def, _ := skin.Definition(skin.PlayButton)
	if got, want := view.Sprite(skin.PlayButton).Bounds().Size(), def.Src.Size(); got != want {
		t.Fatalf("placeholder size = %v, want %v", got, want)
	}
}

func TestViewCachesSliderFramesGlyphsAndColors(t *testing.T) {
	s, err := skin.Open(filepath.Join("..", "..", "..", "assets", "dev-skin"))
	if err != nil {
		t.Fatalf("Open(dev skin) = %v", err)
	}
	view := New(s)

	if vf := view.VolumeFrame(0.5); vf.Bounds().Size() != image.Pt(68, 13) {
		t.Fatalf("VolumeFrame size = %v, want 68x13", vf.Bounds().Size())
	}
	if bf := view.BalanceFrame(-0.5); bf.Bounds().Size() != image.Pt(38, 13) {
		t.Fatalf("BalanceFrame size = %v, want 38x13", bf.Bounds().Size())
	}
	if ef := view.EQFrame(50); ef.Bounds().Size() != image.Pt(14, 63) {
		t.Fatalf("EQFrame size = %v, want 14x63", ef.Bounds().Size())
	}
	if g, ok := view.Glyph('A'); !ok || g.Bounds().Size() != image.Pt(5, 6) {
		t.Fatalf("Glyph('A') = (%v, %v), want 5x6", g, ok)
	}
	if g, ok := view.Glyph('🚀'); !ok || g.Bounds().Size() != image.Pt(5, 6) {
		t.Fatalf("Glyph fallback for unmapped rune = (%v, %v), want '?' 5x6", g, ok)
	}
	if c := view.EQGraphColor(0); c.A != 0xff {
		t.Fatalf("EQGraphColor(0) alpha = %d, want 255", c.A)
	}
	if ps := view.PlaylistStyle(); ps.Normal.A != 0xff {
		t.Fatalf("PlaylistStyle = %+v", ps)
	}
}

func TestViewDeallocateClearsCachesAndIsIdempotent(t *testing.T) {
	s, err := skin.Open(filepath.Join("..", "..", "..", "assets", "dev-skin"))
	if err != nil {
		t.Fatalf("Open(dev skin) = %v", err)
	}
	view := New(s)
	_ = view.Sprite("UNKNOWN_TEST_SPRITE")
	view.Deallocate()
	view.Deallocate()

	if _, ok := view.SheetSize("main.bmp"); ok {
		t.Fatal("SheetSize(main.bmp) should be false after Deallocate")
	}
	if view.HasSprite(skin.MainBackground) {
		t.Fatal("HasSprite should be false after Deallocate")
	}

	var nilView *View
	nilView.Deallocate()
}
