package ui

import (
	"reflect"
	"testing"
)

func TestClipboardCandidates(t *testing.T) {
	tests := []struct {
		name           string
		goos           string
		waylandDisplay string
		want           [][]string
	}{
		{
			name: "darwin",
			goos: "darwin",
			want: [][]string{{"pbpaste"}},
		},
		{
			name: "windows",
			goos: "windows",
			want: [][]string{
				{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard", "-Raw"},
				{"powershell", "-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard", "-Raw"},
			},
		},
		{
			name:           "linux x11",
			goos:           "linux",
			waylandDisplay: "",
			want: [][]string{
				{"xclip", "-selection", "clipboard", "-o"},
				{"xclip", "-selection", "primary", "-o"},
				{"xsel", "--clipboard", "--output"},
				{"xsel", "--primary", "--output"},
				{"wl-paste", "--no-newline"},
			},
		},
		{
			name:           "linux wayland",
			goos:           "linux",
			waylandDisplay: "wayland-0",
			want: [][]string{
				{"wl-paste", "--no-newline"},
				{"wl-paste", "--no-newline", "--primary"},
				{"xclip", "-selection", "clipboard", "-o"},
				{"xclip", "-selection", "primary", "-o"},
				{"xsel", "--clipboard", "--output"},
				{"xsel", "--primary", "--output"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clipboardCandidates(tt.goos, tt.waylandDisplay)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("clipboardCandidates(%q, %q) = %#v, want %#v", tt.goos, tt.waylandDisplay, got, tt.want)
			}
		})
	}
}

func TestSanitizePasteText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty",
			raw:  "",
			want: "",
		},
		{
			name: "whitespace only",
			raw:  " \t\r\n ",
			want: "",
		},
		{
			name: "url with trailing newline",
			raw:  "https://www.youtube.com/watch?v=oz3LxqoyQFQ&list=RDoz3LxqoyQFQ&start_radio=1\r\n",
			want: "https://www.youtube.com/watch?v=oz3LxqoyQFQ&list=RDoz3LxqoyQFQ&start_radio=1",
		},
		{
			name: "tabs newlines and control chars",
			raw:  "\x00  daft\tpunk\x1b\naround\rthe world\x7f  ",
			want: "daft punk around the world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizePasteText(tt.raw); got != tt.want {
				t.Fatalf("sanitizePasteText(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
