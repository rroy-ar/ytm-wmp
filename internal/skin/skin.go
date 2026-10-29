// Package skin loads classic Winamp skin assets.
//
// A classic skin is a ZIP archive (usually .wsz or .zip) or a directory of
// loose files. The parser is deliberately pure: it returns image.Image values
// and sprite rectangles, never Ebitengine types. That keeps it unit-testable
// and lets the UI decide when (and how) to upload textures to the GPU.
package skin

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/image/bmp"
)

const (
	// maxAssetBytes caps a single decoded sheet. Classic skins are tiny;
	// anything larger is either corrupt or a decompression bomb.
	maxAssetBytes = 8 << 20
	// maxAssets caps the number of files inspected in one skin.
	maxAssets = 256
	// maxImageSide rejects absurd dimensions before the image is allocated.
	maxImageSide = 4096
)

// ErrModernSkin reports a .wal-style modern skin. They use XML + Maki and are
// a completely different format; pretending to load one would produce a
// confusing half-rendered window.
var ErrModernSkin = errors.New("modern Winamp skins are not supported")

// Skin is the decoded contents of one classic skin.
//
// Sheets are keyed by lowercased basename, so "Main.BMP" from any nested
// directory becomes "main.bmp". The zero value is not usable.
type Skin struct {
	sheets map[string]image.Image
}

// Open loads a skin from a directory, .wsz, or .zip. The extension is not
// trusted: files are opened as ZIP archives regardless of their name.
func Open(name string) (*Skin, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("open skin %q: %w", name, err)
	}

	var assets map[string][]byte
	if info.IsDir() {
		assets, err = readDir(name)
	} else {
		assets, err = readZip(name)
	}
	if err != nil {
		return nil, fmt.Errorf("open skin %q: %w", name, err)
	}

	return decode(assets)
}

// SheetNames returns the decoded sheet basenames in sorted order.
func (s *Skin) SheetNames() []string {
	names := make([]string, 0, len(s.sheets))
	for name := range s.sheets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Sheet returns a decoded sheet by lowercased basename.
func (s *Skin) Sheet(name string) (image.Image, bool) {
	img, ok := s.sheets[strings.ToLower(name)]
	return img, ok
}

// Lookup resolves a named sprite to its sheet and source rectangle.
//
// It returns false when the sprite is unknown or the loaded sheet is too small
// to contain it. Callers should draw a visible placeholder in that case rather
// than silently skipping the sprite.
func (s *Skin) Lookup(name string) (image.Image, image.Rectangle, bool) {
	def, ok := Definition(name)
	if !ok {
		return nil, image.Rectangle{}, false
	}
	img, ok := s.sheets[def.Sheet]
	if !ok {
		return nil, image.Rectangle{}, false
	}
	if !def.Src.In(img.Bounds()) {
		return nil, image.Rectangle{}, false
	}
	return img, def.Src, true
}

func readDir(root string) (map[string][]byte, error) {
	buf := newAssetBuffer()
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if file != root && strings.EqualFold(entry.Name(), "__MACOSX") {
				return fs.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		name, skip, err := normalizeName(rel)
		if err != nil || skip {
			return err
		}
		if name == "skin.xml" {
			return fmt.Errorf("%w: found skin.xml (a modern .wal skin renamed?)", ErrModernSkin)
		}
		if !strings.HasSuffix(name, ".bmp") {
			return nil
		}

		f, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("read asset %q: %w", name, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(f, maxAssetBytes+1))
		closeErr := f.Close()
		if readErr != nil {
			return fmt.Errorf("read asset %q: %w", name, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("read asset %q: %w", name, closeErr)
		}
		if len(data) > maxAssetBytes {
			return fmt.Errorf("asset %q exceeds the %d-byte limit", name, maxAssetBytes)
		}
		return buf.add(name, data)
	})
	if err != nil {
		return nil, err
	}
	return buf.assets, nil
}

func readZip(name string) (map[string][]byte, error) {
	zr, err := zip.OpenReader(name)
	if err != nil {
		return nil, fmt.Errorf("not a readable ZIP archive: %w", err)
	}
	defer zr.Close()

	buf := newAssetBuffer()
	for _, file := range zr.File {
		if file.FileInfo().IsDir() {
			continue
		}

		assetName, skip, err := normalizeName(file.Name)
		if err != nil || skip {
			return nil, err
		}
		if assetName == "skin.xml" {
			return nil, fmt.Errorf("%w: found skin.xml (a modern .wal skin renamed?)", ErrModernSkin)
		}
		if !strings.HasSuffix(assetName, ".bmp") {
			continue
		}
		if file.UncompressedSize64 > maxAssetBytes {
			return nil, fmt.Errorf("asset %q is too large (%d bytes)", assetName, file.UncompressedSize64)
		}

		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("read asset %q: %w", assetName, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, maxAssetBytes+1))
		closeErr := rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read asset %q: %w", assetName, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("read asset %q: %w", assetName, closeErr)
		}
		if len(data) > maxAssetBytes {
			return nil, fmt.Errorf("asset %q exceeds the %d-byte limit", assetName, maxAssetBytes)
		}
		if err := buf.add(assetName, data); err != nil {
			return nil, err
		}
	}
	return buf.assets, nil
}

// normalizeName turns a ZIP or filesystem path into a lowercased basename.
// Nested directories, Windows separators, and macOS metadata are handled here
// so the decoder only sees flat, predictable names.
func normalizeName(name string) (normalized string, skip bool, err error) {
	name = strings.ReplaceAll(name, `\`, "/")
	clean := path.Clean(name)
	if clean == "." || clean == "/" || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", false, fmt.Errorf("invalid skin asset path %q", name)
	}

	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if strings.EqualFold(part, "__MACOSX") {
			return "", true, nil
		}
	}

	base := path.Base(clean)
	switch strings.ToLower(base) {
	case ".ds_store", "thumbs.db":
		return "", true, nil
	}
	return strings.ToLower(base), false, nil
}

type assetBuffer struct {
	assets map[string][]byte
}

func newAssetBuffer() *assetBuffer {
	return &assetBuffer{assets: make(map[string][]byte)}
}

func (b *assetBuffer) add(name string, data []byte) error {
	if len(b.assets) >= maxAssets {
		return fmt.Errorf("skin contains more than %d BMP assets", maxAssets)
	}
	if _, exists := b.assets[name]; exists {
		return fmt.Errorf("duplicate skin asset %q", name)
	}
	b.assets[name] = data
	return nil
}

func decode(assets map[string][]byte) (*Skin, error) {
	if _, ok := assets["main.bmp"]; !ok {
		return nil, errors.New("skin is missing main.bmp")
	}

	names := make([]string, 0, len(assets))
	for name := range assets {
		names = append(names, name)
	}
	sort.Strings(names)

	s := &Skin{sheets: make(map[string]image.Image, len(assets))}
	for _, name := range names {
		data := assets[name]

		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode skin asset %q: %w", name, err)
		}
		if cfg.Width > maxImageSide || cfg.Height > maxImageSide {
			return nil, fmt.Errorf("decode skin asset %q: image is %dx%d, limit is %dx%d",
				name, cfg.Width, cfg.Height, maxImageSide, maxImageSide)
		}

		img, err := bmp.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode skin asset %q: %w", name, err)
		}
		s.sheets[name] = ensureOpaque(img)
	}
	return s, nil
}
