package skin

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/bmp"
)

func TestOpenDirectoryNormalizesNamesAndLoadsSprites(t *testing.T) {
	dir := t.TempDir()
	writeSheet(t, dir, "Main.BMP", 275, 116)
	writeSheet(t, dir, "nested/CBUTTONS.BMP", 136, 36)
	writeSheet(t, dir, "TITLEBAR.bmp", 312, 87)
	writeText(t, dir, "readme.txt", "not a sheet")

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}

	want := []string{"cbuttons.bmp", "main.bmp", "titlebar.bmp"}
	if got := s.SheetNames(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("SheetNames = %v, want %v", got, want)
	}

	img, rect, ok := s.Lookup(PlayButton)
	if !ok {
		t.Fatal("Lookup(PlayButton) failed")
	}
	if got, want := img.Bounds().Size(), image.Pt(136, 36); got != want {
		t.Fatalf("play sheet size = %v, want %v", got, want)
	}
	if got, want := rect, image.Rect(23, 0, 46, 18); got != want {
		t.Fatalf("play sprite = %v, want %v", got, want)
	}
}

func TestOpenZipHandlesNestedDirsAndBackslashes(t *testing.T) {
	for _, extension := range []string{".wsz", ".zip"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "skin"+extension)
			writeZip(t, path, map[string][]byte{
				`MySkin\MAIN.BMP`:            bmpBytes(t, 275, 116),
				"nested/CBUTTONS.BMP":        bmpBytes(t, 136, 36),
				"nested/deeper/TITLEBAR.BMP": bmpBytes(t, 312, 87),
			})

			s, err := Open(path)
			if err != nil {
				t.Fatalf("Open = %v", err)
			}
			if _, _, ok := s.Lookup(TitlebarActive); !ok {
				t.Fatal("Lookup(TitlebarActive) failed")
			}
		})
	}
}

func TestOpenRejectsDuplicateBasenames(t *testing.T) {
	dir := t.TempDir()
	writeSheet(t, dir, "one/main.bmp", 275, 116)
	writeSheet(t, dir, "two/MAIN.BMP", 275, 116)

	_, err := Open(dir)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Open = %v, want duplicate asset error", err)
	}
}

func TestOpenRejectsModernSkins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.wsz")
	writeZip(t, path, map[string][]byte{
		"main.bmp": bmpBytes(t, 275, 116),
		"skin.xml": []byte("<WinampXML/>"),
	})

	_, err := Open(path)
	if !errors.Is(err, ErrModernSkin) {
		t.Fatalf("Open = %v, want ErrModernSkin", err)
	}
	if !strings.Contains(err.Error(), "skin.xml") {
		t.Fatalf("error %q should name skin.xml", err)
	}
}

func TestOpenRequiresMainBMP(t *testing.T) {
	dir := t.TempDir()
	writeSheet(t, dir, "cbuttons.bmp", 136, 36)

	_, err := Open(dir)
	if err == nil || !strings.Contains(err.Error(), "main.bmp") {
		t.Fatalf("Open = %v, want missing main.bmp error", err)
	}
}

func TestOpenRejectsBrokenAssetsWithoutPanicking(t *testing.T) {
	t.Run("broken bmp", func(t *testing.T) {
		dir := t.TempDir()
		writeText(t, dir, "main.bmp", "this is not a bitmap")

		_, err := Open(dir)
		if err == nil || !strings.Contains(err.Error(), "main.bmp") {
			t.Fatalf("Open = %v, want an error naming main.bmp", err)
		}
	})

	t.Run("not a zip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "broken.wsz")
		writeText(t, filepath.Dir(path), filepath.Base(path), "not a zip")

		_, err := Open(path)
		if err == nil || !strings.Contains(err.Error(), "ZIP") {
			t.Fatalf("Open = %v, want ZIP error", err)
		}
	})
}

func TestOpenRejectsOversizedDimensionsBeforeDecoding(t *testing.T) {
	dir := t.TempDir()
	writeTextBytes(t, dir, "main.bmp", bmpHeader(5000, 1))

	_, err := Open(dir)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("Open = %v, want image dimension limit error", err)
	}
}

func TestOpenRejectsTotalPixelBudget(t *testing.T) {
	dir := t.TempDir()
	writeSheet(t, dir, "main.bmp", 275, 116)
	writeTextBytes(t, dir, "huge.bmp", bmpHeader(4096, 4096))

	_, err := Open(dir)
	if err == nil || !strings.Contains(err.Error(), "pixel budget") {
		t.Fatalf("Open = %v, want total pixel budget error", err)
	}
}

func TestAssetBufferRejectsTotalBytes(t *testing.T) {
	buf := newAssetBuffer()
	if err := buf.add("first.bmp", make([]byte, maxSkinBytes)); err != nil {
		t.Fatalf("add first: %v", err)
	}
	if err := buf.add("second.bmp", []byte{1}); err == nil {
		t.Fatal("add beyond the total byte limit succeeded")
	}
}

func TestOpenFallsBackFromBrokenOptionalSheet(t *testing.T) {
	dir := t.TempDir()
	writeSheet(t, dir, "main.bmp", 275, 116)
	writeText(t, dir, "cbuttons.bmp", "not a bitmap")

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open = %v, want a usable main window", err)
	}
	if _, _, ok := s.Lookup(PlayButton); ok {
		t.Fatal("broken optional sheet produced a play sprite")
	}
	if got := strings.Join(s.Warnings(), "|"); !strings.Contains(got, "cbuttons.bmp") {
		t.Fatalf("Warnings = %q, want a cbuttons.bmp warning", got)
	}
}

func TestOpenFollowsSymlinkedDirectory(t *testing.T) {
	realDir := filepath.Join(t.TempDir(), "real-skin")
	writeSheet(t, realDir, "main.bmp", 275, 116)
	link := filepath.Join(t.TempDir(), "linked-skin")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	s, err := Open(link)
	if err != nil {
		t.Fatalf("Open(symlink) = %v", err)
	}
	if _, ok := s.Sheet("main.bmp"); !ok {
		t.Fatal("symlinked skin did not load main.bmp")
	}
}

func TestOpenRejectsZipTraversal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evil.wsz")
	writeZip(t, path, map[string][]byte{
		"../main.bmp": bmpBytes(t, 275, 116),
	})

	_, err := Open(path)
	if err == nil || !strings.Contains(err.Error(), "invalid skin asset path") {
		t.Fatalf("Open = %v, want path traversal rejection", err)
	}
}

func TestLookupMissingAndUnknownSprites(t *testing.T) {
	dir := t.TempDir()
	writeSheet(t, dir, "main.bmp", 100, 100) // too small for the full background

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	if _, _, ok := s.Lookup(MainBackground); ok {
		t.Fatal("Lookup(MainBackground) succeeded with a 100x100 sheet")
	}
	if _, _, ok := s.Lookup("NOT_A_SPRITE"); ok {
		t.Fatal("Lookup(unknown) succeeded")
	}
	if _, ok := s.Sheet("MAIN.BMP"); !ok {
		t.Fatal("Sheet lookup should be case-insensitive")
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		in   string
		want string
		skip bool
	}{
		{"main.bmp", "main.bmp", false},
		{"Main.BMP", "main.bmp", false},
		{`folder\sub\CButtons.BMP`, "cbuttons.bmp", false},
		{"folder/sub/titlebar.bmp", "titlebar.bmp", false},
		{"__MACOSX/skin/._main.bmp", "", true},
		{".DS_Store", "", true},
		{"folder/Thumbs.db", "", true},
	}
	for _, tt := range tests {
		got, skip, err := normalizeName(tt.in)
		if err != nil {
			t.Fatalf("normalizeName(%q) error = %v", tt.in, err)
		}
		if got != tt.want || skip != tt.skip {
			t.Fatalf("normalizeName(%q) = %q, %v; want %q, %v", tt.in, got, skip, tt.want, tt.skip)
		}
	}
}

func TestNormalizeNameRejectsTraversal(t *testing.T) {
	for _, name := range []string{"../main.bmp", "/etc/main.bmp", `..\main.bmp`} {
		if _, _, err := normalizeName(name); err == nil {
			t.Fatalf("normalizeName(%q) = nil error, want rejection", name)
		}
	}
}

func TestEnsureOpaqueOnlyRewritesAllZeroAlpha(t *testing.T) {
	transparent := image.NewRGBA(image.Rect(0, 0, 2, 2))
	transparent.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 0})
	transparent.SetRGBA(1, 0, color.RGBA{R: 40, G: 50, B: 60, A: 0})

	got := ensureOpaque(transparent)
	if got == transparent {
		t.Fatal("ensureOpaque kept the all-zero-alpha image")
	}
	if _, _, _, a := got.At(0, 0).RGBA(); a != 0xffff {
		t.Fatalf("alpha = %d, want opaque", a)
	}

	withAlpha := image.NewRGBA(image.Rect(0, 0, 1, 1))
	withAlpha.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 128})
	if got := ensureOpaque(withAlpha); got != withAlpha {
		t.Fatal("ensureOpaque rewrote an image with real alpha data")
	}

	nrgba := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	nrgba.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 0})
	got = ensureOpaque(nrgba)
	if got == nrgba {
		t.Fatal("ensureOpaque kept the all-zero-alpha NRGBA image")
	}
	if _, _, _, a := got.At(0, 0).RGBA(); a != 0xffff {
		t.Fatalf("NRGBA alpha = %d, want opaque", a)
	}
}

func TestDefinitionsAreInsideTheirSheets(t *testing.T) {
	sheetSizes := map[string]image.Point{
		"main.bmp":     image.Pt(275, 116),
		"cbuttons.bmp": image.Pt(136, 36),
		"titlebar.bmp": image.Pt(312, 87),
	}
	for name, def := range Definitions() {
		size, ok := sheetSizes[def.Sheet]
		if !ok {
			t.Fatalf("sprite %q references unknown sheet %q", name, def.Sheet)
		}
		if !def.Src.In(image.Rectangle{Max: size}) {
			t.Fatalf("sprite %q rect %v does not fit in %s %v", name, def.Src, def.Sheet, size)
		}
	}
}

func writeSheet(t *testing.T, root, rel string, width, height int) {
	t.Helper()
	writeTextBytes(t, root, rel, bmpBytes(t, width, height))
}

func bmpBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x40, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := bmp.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

func writeZip(t *testing.T, name string, files map[string][]byte) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for path, data := range files {
		w, err := zw.Create(path)
		if err != nil {
			t.Fatalf("zip create %q: %v", path, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("zip write %q: %v", path, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := os.WriteFile(name, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write zip: %v", err)
	}
}

func writeText(t *testing.T, root, rel, text string) {
	t.Helper()
	writeTextBytes(t, root, rel, []byte(text))
}

func writeTextBytes(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// bmpHeader builds a valid 24-bit BMP header with no pixel data. DecodeConfig
// only needs the header, which lets the test exercise the dimension guard
// without allocating a huge image.
func bmpHeader(width, height int) []byte {
	const headerSize = 54
	rowSize := (width*3 + 3) &^ 3
	fileSize := headerSize + rowSize*height

	buf := make([]byte, headerSize)
	copy(buf[0:], "BM")
	binary.LittleEndian.PutUint32(buf[2:], uint32(fileSize))
	binary.LittleEndian.PutUint32(buf[10:], headerSize)
	binary.LittleEndian.PutUint32(buf[14:], 40)
	binary.LittleEndian.PutUint32(buf[18:], uint32(width))
	binary.LittleEndian.PutUint32(buf[22:], uint32(height))
	binary.LittleEndian.PutUint16(buf[26:], 1)
	binary.LittleEndian.PutUint16(buf[28:], 24)
	binary.LittleEndian.PutUint32(buf[34:], uint32(rowSize*height))
	return buf
}
