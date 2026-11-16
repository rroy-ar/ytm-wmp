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
	"sort"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"

	"ytm-wmp/internal/skin"
)

// View is a GPU-backed view of one skin.
type View struct {
	sheets        map[string]*ebiten.Image
	sprites       map[string]*ebiten.Image
	placeholders  map[string]*ebiten.Image
	missing       map[string]bool
	volFrames     [28]*ebiten.Image
	balFrames     [28]*ebiten.Image
	eqFrames      [28]*ebiten.Image
	eqGraphColors [19]color.RGBA
	hasNumbersEx  bool
	playlistStyle skin.PlaylistStyle
	visColors     [24]color.RGBA
}

// New uploads all decoded sheets and caches all known sprite rectangles.
// It must be called after Ebitengine has started (for example, in the first
// Update tick), never from main before RunGame.
func New(s *skin.Skin) *View {
	v := &View{
		sheets:        make(map[string]*ebiten.Image),
		sprites:       make(map[string]*ebiten.Image),
		placeholders:  make(map[string]*ebiten.Image),
		missing:       make(map[string]bool),
		playlistStyle: s.PlaylistStyle(),
		visColors:     s.VisColors(),
	}

	for _, name := range s.SheetNames() {
		img, ok := s.Sheet(name)
		if !ok {
			continue
		}
		v.sheets[name] = ebiten.NewImageFromImage(img)
	}

	for name, def := range skin.Definitions() {
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

	v.hasNumbersEx = true
	for d := 0; d <= 9; d++ {
		if !v.HasSprite(skin.NumberSpriteName(d, true)) {
			v.hasNumbersEx = false
			break
		}
	}

	v.cacheSliderFrames()
	v.cacheEQGraphColors(s)
	return v
}

func (v *View) cacheSliderFrames() {
	if sheet, ok := v.sheets["volume.bmp"]; ok {
		for i := range v.volFrames {
			rect := skin.VolumeFrameRect(float64(i) / 27.0)
			if rect.In(sheet.Bounds()) {
				if sub, ok := sheet.SubImage(rect).(*ebiten.Image); ok {
					v.volFrames[i] = sub
				}
			}
		}
	}
	if sheet, ok := v.sheets["balance.bmp"]; ok {
		for i := range v.balFrames {
			rect := skin.BalanceFrameRect(float64(i) / 27.0)
			if rect.In(sheet.Bounds()) {
				if sub, ok := sheet.SubImage(rect).(*ebiten.Image); ok {
					v.balFrames[i] = sub
				}
			}
		}
	}
	if sheet, ok := v.sheets["eqmain.bmp"]; ok {
		for i := range v.eqFrames {
			rect := skin.EQSliderFrameRect(float64(i) * 100.0 / 27.0)
			if rect.In(sheet.Bounds()) {
				if sub, ok := sheet.SubImage(rect).(*ebiten.Image); ok {
					v.eqFrames[i] = sub
				}
			}
		}
	}
}

func (v *View) cacheEQGraphColors(s *skin.Skin) {
	def, ok := skin.Definition(skin.EQGraphLineColors)
	sheet, hasSheet := s.Sheet(def.Sheet)
	if ok && hasSheet && def.Src.In(sheet.Bounds()) {
		for row := 0; row < 19; row++ {
			c := color.RGBAModel.Convert(sheet.At(def.Src.Min.X, def.Src.Min.Y+row)).(color.RGBA)
			c.A = 0xff
			v.eqGraphColors[row] = c
		}
		return
	}
	// Fallback gradient from red (+12dB) through yellow to green (-12dB).
	for row := 0; row < 19; row++ {
		t := float64(row) / 18.0
		var r, g uint8
		if t < 0.5 {
			r = 0xff
			g = uint8(t * 2 * 0xff)
		} else {
			r = uint8((1 - (t-0.5)*2) * 0xff)
			g = 0xff
		}
		v.eqGraphColors[row] = color.RGBA{R: r, G: g, B: 0x20, A: 0xff}
	}
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

// HasSprite reports whether name resolved cleanly from a loaded skin sheet.
func (v *View) HasSprite(name string) bool {
	_, ok := v.sprites[name]
	return ok
}

// Glyph returns the cached 5x6 bitmap glyph for r from text.bmp, falling back
// to '?' if r is unmapped. Returns false when text.bmp itself is missing.
func (v *View) Glyph(r rune) (*ebiten.Image, bool) {
	if r >= 'A' && r <= 'Z' {
		r = unicode.ToLower(r)
	}
	if img, ok := v.sprites[skin.CharacterSpriteName(r)]; ok {
		return img, true
	}
	if img, ok := v.sprites[skin.CharacterSpriteName('?')]; ok {
		return img, true
	}
	return nil, false
}

// HasNumbersEx reports whether nums_ex.bmp digits are available.
func (v *View) HasNumbersEx() bool { return v.hasNumbersEx }

// VolumeFrame returns the 68x13 background frame for volume in [0, 1].
func (v *View) VolumeFrame(volume float64) *ebiten.Image {
	rect := skin.VolumeFrameRect(volume)
	idx := rect.Min.Y / 15
	if idx >= 0 && idx < len(v.volFrames) && v.volFrames[idx] != nil {
		return v.volFrames[idx]
	}
	return v.sizedPlaceholder("VOLUME_FRAME", 68, 13)
}

// BalanceFrame returns the 38x13 background frame for balance in [-1, 1].
func (v *View) BalanceFrame(balance float64) *ebiten.Image {
	rect := skin.BalanceFrameRect(balance)
	idx := rect.Min.Y / 15
	if idx >= 0 && idx < len(v.balFrames) && v.balFrames[idx] != nil {
		return v.balFrames[idx]
	}
	return v.sizedPlaceholder("BALANCE_FRAME", 38, 13)
}

// EQFrame returns the 14x63 background frame for an EQ slider in [0, 100].
func (v *View) EQFrame(value float64) *ebiten.Image {
	rect := skin.EQSliderFrameRect(value)
	col := (rect.Min.X - 13) / 15
	row := (rect.Min.Y - 164) / 65
	idx := row*14 + col
	if idx >= 0 && idx < len(v.eqFrames) && v.eqFrames[idx] != nil {
		return v.eqFrames[idx]
	}
	return v.sizedPlaceholder("EQ_SLIDER_FRAME", 14, 63)
}

// EQGraphColor returns the graph spline color for row 0..18.
func (v *View) EQGraphColor(row int) color.RGBA {
	if row < 0 {
		row = 0
	} else if row >= len(v.eqGraphColors) {
		row = len(v.eqGraphColors) - 1
	}
	return v.eqGraphColors[row]
}

// PlaylistStyle returns the parsed pledit.txt colors.
func (v *View) PlaylistStyle() skin.PlaylistStyle { return v.playlistStyle }

// VisColors returns the parsed 24-entry viscolor.txt palette.
func (v *View) VisColors() [24]color.RGBA { return v.visColors }

// MissingNames returns the sorted list of missing sprite names (excluding individual
// font glyph entries when text.bmp is absent, summarizing text.bmp once).
func (v *View) MissingNames() []string {
	var out []string
	for name := range v.missing {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (v *View) markMissing(name string) *ebiten.Image {
	width, height := placeholderSize(name)
	return v.sizedPlaceholder(name, width, height)
}

func (v *View) sizedPlaceholder(name string, width, height int) *ebiten.Image {
	if img, ok := v.placeholders[name]; ok {
		return img
	}
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
