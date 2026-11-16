// Default palette and playlist style tables are derived from Webamp
// (captbaritone/webamp, MIT License; see THIRD_PARTY_NOTICES.md).
package skin

import (
	"image/color"
	"strconv"
	"strings"
)

// PlaylistStyle holds colors and font metadata parsed from pledit.txt.
type PlaylistStyle struct {
	Normal     color.RGBA
	Current    color.RGBA
	NormalBG   color.RGBA
	SelectedBG color.RGBA
	Font       string
}

// DefaultPlaylistStyle returns Webamp's classic green-on-black defaults.
func DefaultPlaylistStyle() PlaylistStyle {
	return PlaylistStyle{
		Normal:     color.RGBA{R: 0x00, G: 0xff, B: 0x00, A: 0xff},
		Current:    color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
		NormalBG:   color.RGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xff},
		SelectedBG: color.RGBA{R: 0x00, G: 0x00, B: 0xff, A: 0xff},
		Font:       "Arial",
	}
}

// DefaultVisColors returns Webamp's 24-entry default visualizer palette.
func DefaultVisColors() [24]color.RGBA {
	return [24]color.RGBA{
		{R: 0, G: 0, B: 0, A: 0xff},
		{R: 24, G: 33, B: 41, A: 0xff},
		{R: 239, G: 49, B: 16, A: 0xff},
		{R: 206, G: 41, B: 16, A: 0xff},
		{R: 214, G: 90, B: 0, A: 0xff},
		{R: 214, G: 102, B: 0, A: 0xff},
		{R: 214, G: 115, B: 0, A: 0xff},
		{R: 198, G: 123, B: 8, A: 0xff},
		{R: 222, G: 165, B: 24, A: 0xff},
		{R: 214, G: 181, B: 33, A: 0xff},
		{R: 189, G: 222, B: 41, A: 0xff},
		{R: 148, G: 222, B: 33, A: 0xff},
		{R: 41, G: 206, B: 16, A: 0xff},
		{R: 50, G: 190, B: 16, A: 0xff},
		{R: 57, G: 181, B: 16, A: 0xff},
		{R: 49, G: 156, B: 8, A: 0xff},
		{R: 41, G: 148, B: 0, A: 0xff},
		{R: 24, G: 132, B: 8, A: 0xff},
		{R: 255, G: 255, B: 255, A: 0xff},
		{R: 214, G: 214, B: 222, A: 0xff},
		{R: 181, G: 189, B: 189, A: 0xff},
		{R: 160, G: 170, B: 175, A: 0xff},
		{R: 148, G: 156, B: 165, A: 0xff},
		{R: 150, G: 150, B: 150, A: 0xff},
	}
}

func parsePleditTxt(data []byte) PlaylistStyle {
	style := DefaultPlaylistStyle()
	if len(data) == 0 {
		return style
	}

	section := ""
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		if section != "text" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := strings.TrimSpace(line[eq+1:])
		if extraEq := strings.IndexByte(val, '='); extraEq >= 0 {
			val = strings.TrimSpace(val[:extraEq])
		}
		val = strings.Trim(val, `"'`)

		switch key {
		case "normal":
			if c, ok := parseHexColor(val); ok {
				style.Normal = c
			}
		case "current":
			if c, ok := parseHexColor(val); ok {
				style.Current = c
			}
		case "normalbg":
			if c, ok := parseHexColor(val); ok {
				style.NormalBG = c
			}
		case "selectedbg":
			if c, ok := parseHexColor(val); ok {
				style.SelectedBG = c
			}
		case "font":
			if val != "" {
				style.Font = val
			}
		}
	}
	return style
}

func parseHexColor(s string) (color.RGBA, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	if len(s) < 6 {
		return color.RGBA{}, false
	}
	// Classic skins occasionally include extra trailing digits or comments.
	s = s[:6]
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{
		R: uint8((v >> 16) & 0xff),
		G: uint8((v >> 8) & 0xff),
		B: uint8(v & 0xff),
		A: 0xff,
	}, true
}

func parseViscolorTxt(data []byte) [24]color.RGBA {
	colors := DefaultVisColors()
	if len(data) == 0 {
		return colors
	}

	idx := 0
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for _, raw := range lines {
		if idx >= len(colors) {
			break
		}
		if c, ok := parseViscolorLine(raw); ok {
			colors[idx] = c
			idx++
		}
	}
	return colors
}

func parseViscolorLine(line string) (color.RGBA, bool) {
	if slash := strings.Index(line, "//"); slash >= 0 {
		line = line[:slash]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return color.RGBA{}, false
	}

	fields := strings.FieldsFunc(line, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(fields) < 3 {
		return color.RGBA{}, false
	}
	r, errR := strconv.Atoi(fields[0])
	g, errG := strconv.Atoi(fields[1])
	b, errB := strconv.Atoi(fields[2])
	if errR != nil || errG != nil || errB != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{
		R: clampByte(r),
		G: clampByte(g),
		B: clampByte(b),
		A: 0xff,
	}, true
}

func clampByte(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
