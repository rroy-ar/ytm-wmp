package model

import "testing"

func TestVideoID(t *testing.T) {
	tests := []struct {
		target string
		want   string
	}{
		{"dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"  dQw4w9WgXcQ  ", "dQw4w9WgXcQ"},
		{"https://youtu.be/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://youtu.be/dQw4w9WgXcQ?si=tracking", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42", "dQw4w9WgXcQ"},
		{"https://m.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://music.youtube.com/watch?v=dQw4w9WgXcQ&list=RDAMVM", "dQw4w9WgXcQ"},
		{"https://youtube.com/shorts/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/live/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"daft punk one more time", ""},
		{"https://example.com/watch?v=dQw4w9WgXcQ", ""},
		{"https://youtu.be/too-short", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := VideoID(tt.target); got != tt.want {
				t.Fatalf("VideoID(%q) = %q, want %q", tt.target, got, tt.want)
			}
		})
	}
}

func TestIsVideoID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"dQw4w9WgXcQ", true},
		{"0wRYvsfhsR8", true},
		{"aB_-0123459", true},
		{"too-short", false},
		{"waytoolongid", false},
		{"invalid!chr", false},
		{" dQw4w9WgXcQ", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := IsVideoID(tt.in); got != tt.want {
				t.Fatalf("IsVideoID(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeTrackID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"0wRYvsfhsR8", "0wRYvsfhsR8"},
		{" https://www.youtube.com/watch?v=0wRYvsfhsR8&list=PL123 ", "0wRYvsfhsR8"},
		{"https://music.youtube.com/watch?v=FGBhQbmPwH8", "FGBhQbmPwH8"},
		{"  daft punk one more time  ", "daft punk one more time"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := NormalizeTrackID(tt.in); got != tt.want {
				t.Fatalf("NormalizeTrackID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDefaultThumbnailURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"0wRYvsfhsR8", "https://i.ytimg.com/vi/0wRYvsfhsR8/hqdefault.jpg"},
		{"https://www.youtube.com/watch?v=0wRYvsfhsR8", "https://i.ytimg.com/vi/0wRYvsfhsR8/hqdefault.jpg"},
		{"daft punk", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := DefaultThumbnailURL(tt.in); got != tt.want {
				t.Fatalf("DefaultThumbnailURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
