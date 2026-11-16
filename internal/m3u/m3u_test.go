package m3u

import (
	"strings"
	"testing"
	"time"

	"ytm-wmp/internal/model"
)

func TestRoundTripFormatAndParse(t *testing.T) {
	in := []model.Track{
		{
			ID:       "0wRYvsfhsR8",
			Title:    "Something About Us",
			Artist:   "Daft Punk",
			Duration: 232 * time.Second,
		},
		{
			ID:       "https://music.youtube.com/watch?v=oz3LxqoyQFQ&list=RDoz3LxqoyQFQ",
			Title:    "No Artist Track",
			Duration: 0,
		},
	}

	raw := Format(in)
	if !strings.HasPrefix(raw, "#EXTM3U\n") {
		t.Fatalf("Format missing #EXTM3U header:\n%s", raw)
	}

	got, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (%#v)", len(got), got)
	}

	if got[0].ID != "0wRYvsfhsR8" || got[0].Artist != "Daft Punk" || got[0].Title != "Something About Us" || got[0].Duration != 232*time.Second {
		t.Fatalf("got[0] = %#v", got[0])
	}
	if got[1].ID != "oz3LxqoyQFQ" || got[1].Title != "No Artist Track" || got[1].Duration != 0 {
		t.Fatalf("got[1] = %#v", got[1])
	}
}

func TestParseHandlesBareIDsShortURLsAndIgnoresUnknownLines(t *testing.T) {
	input := "\ufeff#EXTM3U\r\n" +
		"# comment\r\n" +
		"0wRYvsfhsR8\r\n" +
		"/local/music/song.mp3\r\n" +
		"#EXTINF:120 tvg-id=\"x\",Artist Name - Song Name\r\n" +
		"https://youtu.be/dQw4w9WgXcQ?t=10\r\n"

	got, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (%#v)", len(got), got)
	}
	if got[0].ID != "0wRYvsfhsR8" {
		t.Fatalf("got[0].ID = %q", got[0].ID)
	}
	if got[1].ID != "dQw4w9WgXcQ" || got[1].Artist != "Artist Name" || got[1].Title != "Song Name" || got[1].Duration != 120*time.Second {
		t.Fatalf("got[1] = %#v", got[1])
	}
}
