package skin

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/bmp"
)

func FuzzSkinOpen(f *testing.F) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.SetRGBA(0, 0, color.RGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff})
	var bmpBuf bytes.Buffer
	if err := bmp.Encode(&bmpBuf, img); err == nil {
		var zipBuf bytes.Buffer
		zw := zip.NewWriter(&zipBuf)
		if w, err := zw.Create("main.bmp"); err == nil {
			_, _ = w.Write(bmpBuf.Bytes())
		}
		if w, err := zw.Create("pledit.txt"); err == nil {
			_, _ = w.Write([]byte("[Text]\nNormal=#00FF00\n"))
		}
		_ = zw.Close()
		f.Add(zipBuf.Bytes())
	}
	f.Add([]byte("not a zip"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		s, err := OpenBytes(data)
		if err != nil {
			return
		}
		_ = s.SheetNames()
		_ = s.Warnings()
		_ = s.PlaylistStyle()
		_ = s.VisColors()
		_, _, _ = s.Lookup(MainBackground)
	})
}
