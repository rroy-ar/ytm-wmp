// Package skinview turns decoded skin sheets into Ebitengine textures.
//
// This is the only layer that knows both skin and Ebitengine types. It uploads
// every sheet once and caches SubImage results, so Draw never allocates images
// or decodes BMPs. Missing sprites become magenta placeholders instead of
// silently disappearing.
package skinview

import (
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"

	"ytm-wmp/internal/skin"
)

// View is a GPU-backed view of one skin.
type View struct {
	sheets       map[string]*ebiten.Image
	sprites      map[string]*ebiten.Image
	placeholders map[string]*ebiten.Image
	missing      map[string]bool
}

// New uploads all decoded sheets and caches all known sprite rectangles.
// It must be called after Ebitengine has started (for example, in the first
// Update tick), never from main before RunGame.
func New(s *skin.Skin) *View {
	v := &View{
		sheets:       make(map[string]*ebiten.Image),
		sprites:      make(map[string]*ebiten.Image),
		placeholders: make(map[string]*ebiten.Image),
		missing:      make(map[string]bool),
	}

	for _, name := range s.SheetNames() {
		img, ok := s.Sheet(name)
		if !ok {
			continue
		}
		v.sheets[name] = ebiten.NewImageFromImage(img)
	}

	for name := range skin.Definitions() {
		def, _ := skin.Definition(name)
		sheet, ok := v.sheets[def.Sheet]
		if !ok || !def.Src.In(sheet.Bounds()) {
			v.markMissing(name)
			continue
		}
		sub, ok := sheet.SubImage(def.Src).(*ebiten.Image)
		if !ok {
			v.markMissing(name)
			continue
		}
		v.sprites[name] = sub
	}
	return v
}

// Sprite returns the cached texture for name. Unknown or missing sprites get a
// magenta placeholder of the expected size.
func (v *View) Sprite(name string) *ebiten.Image {
	if img, ok := v.sprites[name]; ok {
		return img
	}
	if img, ok := v.placeholders[name]; ok {
		return img
	}

	return v.markMissing(name)
}

func (v *View) markMissing(name string) *ebiten.Image {
	if img, ok := v.placeholders[name]; ok {
		return img
	}
	width, height := placeholderSize(name)
	img := ebiten.NewImage(width, height)
	img.Fill(color.RGBA{R: 0xff, G: 0x00, B: 0xff, A: 0xff})
	v.placeholders[name] = img
	v.missing[name] = true
	return img
}

// Missing reports whether name had no usable sheet rectangle.
func (v *View) Missing(name string) bool { return v.missing[name] }

func placeholderSize(name string) (width, height int) {
	if def, ok := skin.Definition(name); ok {
		return def.Src.Dx(), def.Src.Dy()
	}
	return 16, 16
}

// SheetSize exposes the uploaded sheet dimensions for debug overlays.
func (v *View) SheetSize(name string) (image.Point, bool) {
	img, ok := v.sheets[name]
	if !ok {
		return image.Point{}, false
	}
	return img.Bounds().Size(), true
}
