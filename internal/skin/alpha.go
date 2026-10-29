package skin

import (
	"image"
	"image/draw"
)

// ensureOpaque works around a common classic-skin bug: some 32-bit BMPs store
// an all-zero alpha channel even though the author intended an opaque image.
// Translating that literally makes the whole window invisible.
//
// Only the pathological all-zero case is rewritten. Images with any real alpha
// information are left untouched for skins that intentionally use it.
func ensureOpaque(img image.Image) image.Image {
	switch m := img.(type) {
	case *image.RGBA:
		if hasAnyAlphaRGBA(m) {
			return m
		}
		out := image.NewRGBA(m.Bounds())
		draw.Draw(out, m.Bounds(), m, m.Bounds().Min, draw.Src)
		for i := 3; i < len(out.Pix); i += 4 {
			out.Pix[i] = 0xff
		}
		return out
	case *image.NRGBA:
		if hasAnyAlphaNRGBA(m) {
			return m
		}
		out := image.NewNRGBA(m.Bounds())
		draw.Draw(out, m.Bounds(), m, m.Bounds().Min, draw.Src)
		for i := 3; i < len(out.Pix); i += 4 {
			out.Pix[i] = 0xff
		}
		return out
	default:
		return img
	}
}

func hasAnyAlphaRGBA(img *image.RGBA) bool {
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if img.RGBAAt(x, y).A != 0 {
				return true
			}
		}
	}
	return false
}

func hasAnyAlphaNRGBA(img *image.NRGBA) bool {
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if img.NRGBAAt(x, y).A != 0 {
				return true
			}
		}
	}
	return false
}
