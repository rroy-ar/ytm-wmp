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

func TestViewSliderFallbacksAndPartialSheets(t *testing.T) {
	writeBMP := func(t *testing.T, dir, name string, w, h int) {
		t.Helper()
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := bmp.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("narrow 27-frame volume and balance without thumbs, 248px posbar", func(t *testing.T) {
		dir := t.TempDir()
		writeBMP(t, dir, "main.bmp", 275, 116)
		writeBMP(t, dir, "volume.bmp", 66, 405)
		writeBMP(t, dir, "balance.bmp", 47, 405)
		writeBMP(t, dir, "posbar.bmp", 248, 10)

		s, err := skin.Open(dir)
		if err != nil {
			t.Fatalf("Open = %v", err)
		}
		view := New(s)

		if vf := view.VolumeFrame(1.0); vf.Bounds().Size() != image.Pt(66, 13) {
			t.Fatalf("VolumeFrame(1.0) size = %v, want 66x13", vf.Bounds().Size())
		}
		if view.Missing("VOLUME_FRAME") {
			t.Fatal("VolumeFrame should not fall back to magenta placeholder")
		}
		if bf := view.BalanceFrame(1.0); bf.Bounds().Size() != image.Pt(38, 13) {
			t.Fatalf("BalanceFrame(1.0) size = %v, want 38x13", bf.Bounds().Size())
		}
		if view.Missing("BALANCE_FRAME") {
			t.Fatal("BalanceFrame should not fall back to magenta placeholder")
		}
		if view.HasSprite(skin.MainVolumeThumb) || view.HasSprite(skin.MainBalanceThumb) {
			t.Fatal("thumbs should be absent when sheets omit the thumb row")
		}
		if !view.HasSprite(skin.MainPositionBackground) || view.HasSprite(skin.MainPositionThumb) {
			t.Fatal("248px posbar should have background but no thumb")
		}
	})

	t.Run("68x419 volume without thumb row", func(t *testing.T) {
		dir := t.TempDir()
		writeBMP(t, dir, "main.bmp", 275, 116)
		writeBMP(t, dir, "volume.bmp", 68, 419)

		s, err := skin.Open(dir)
		if err != nil {
			t.Fatalf("Open = %v", err)
		}
		view := New(s)

		if vf := view.VolumeFrame(1.0); vf.Bounds().Size() != image.Pt(68, 13) {
			t.Fatalf("VolumeFrame(1.0) size = %v, want 68x13", vf.Bounds().Size())
		}
		if view.Missing("VOLUME_FRAME") {
			t.Fatal("VolumeFrame should not fall back to magenta placeholder")
		}
		if view.HasSprite(skin.MainVolumeThumb) {
			t.Fatal("MainVolumeThumb should be absent for 68x419 volume.bmp")
		}
	})

	t.Run("missing balance.bmp falls back to volume.bmp", func(t *testing.T) {
		dir := t.TempDir()
		writeBMP(t, dir, "main.bmp", 275, 116)
		writeBMP(t, dir, "volume.bmp", 68, 433)

		s, err := skin.Open(dir)
		if err != nil {
			t.Fatalf("Open = %v", err)
		}
		view := New(s)

		if bf := view.BalanceFrame(-0.5); bf.Bounds().Size() != image.Pt(38, 13) {
			t.Fatalf("BalanceFrame(-0.5) size = %v, want 38x13", bf.Bounds().Size())
		}
		if view.Missing("BALANCE_FRAME") {
			t.Fatal("BalanceFrame should fall back to volume.bmp when balance.bmp is missing")
		}
		if !view.HasSprite(skin.MainBalanceThumb) || !view.HasSprite(skin.MainBalanceThumbActive) {
			t.Fatal("balance thumbs should fall back to volume.bmp when balance.bmp is missing")
		}
	})

	t.Run("275x294 eqmain omits graph sprites without falling back to window well", func(t *testing.T) {
		dir := t.TempDir()
		writeBMP(t, dir, "main.bmp", 275, 116)
		writeBMP(t, dir, "eqmain.bmp", 275, 294)

		s, err := skin.Open(dir)
		if err != nil {
			t.Fatalf("Open = %v", err)
		}
		view := New(s)

		if !view.HasSprite(skin.EQWindowBackground) {
			t.Fatal("EQWindowBackground should load on 275x294 eqmain.bmp")
		}
		if got, want := view.EQFrame(50).Bounds().Size(), image.Pt(14, 63); got != want {
			t.Fatalf("EQFrame(50) size = %v, want %v", got, want)
		}
		if view.HasSprite(skin.EQGraphBackground) {
			t.Fatal("EQGraphBackground should be absent for 275x294 eqmain.bmp")
		}
		if view.HasSprite(skin.EQGraphLineColors) {
			t.Fatal("EQGraphLineColors should be absent for 275x294 eqmain.bmp")
		}
		if view.HasSprite(skin.EQPreampLine) {
			t.Fatal("EQPreampLine should be absent for 275x294 eqmain.bmp")
		}
		if c := view.EQGraphColor(0); c.A != 0xff {
			t.Fatalf("EQGraphColor(0) alpha = %d, want 255", c.A)
		}
	})
}
