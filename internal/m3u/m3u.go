// Package m3u parses and formats M3U playlists of YouTube URLs and video IDs.
package m3u

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"ytm-wmp/internal/model"
)

// Parse reads an M3U/M3U8 stream and extracts YouTube track entries.
// Non-YouTube entries and empty/comment lines are ignored.
func Parse(r io.Reader) ([]model.Track, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)

	var (
		tracks      []model.Track
		hasExtInf   bool
		extDuration time.Duration
		extArtist   string
		extTitle    string
	)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if strings.HasPrefix(strings.ToUpper(line), "#EXTINF:") {
				extDuration, extArtist, extTitle = parseExtInf(line[len("#EXTINF:"):])
				hasExtInf = true
			}
			continue
		}

		id := model.NormalizeTrackID(line)
		if !model.IsVideoID(id) {
			hasExtInf = false
			continue
		}
		tr := model.Track{
			ID:           id,
			ThumbnailURL: model.DefaultThumbnailURL(id),
		}
		if hasExtInf {
			tr.Duration = extDuration
			tr.Artist = extArtist
			tr.Title = extTitle
			hasExtInf = false
		}
		tracks = append(tracks, tr)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return tracks, nil
}

func parseExtInf(rest string) (time.Duration, string, string) {
	comma := strings.IndexByte(rest, ',')
	var durPart, labelPart string
	if comma >= 0 {
		durPart = strings.TrimSpace(rest[:comma])
		labelPart = strings.TrimSpace(rest[comma+1:])
	} else {
		durPart = strings.TrimSpace(rest)
	}
	// Some M3U writers attach key=value attributes after the duration before the comma.
	if fields := strings.Fields(durPart); len(fields) > 0 {
		durPart = fields[0]
	}

	var dur time.Duration
	if secs, err := strconv.Atoi(durPart); err == nil && secs > 0 {
		dur = time.Duration(secs) * time.Second
	}

	artist, title := "", labelPart
	if sep := strings.Index(labelPart, " - "); sep > 0 {
		artist = strings.TrimSpace(labelPart[:sep])
		title = strings.TrimSpace(labelPart[sep+3:])
	}
	return dur, artist, title
}

// Format serializes tracks as an #EXTM3U playlist using canonical YouTube watch URLs.
func Format(tracks []model.Track) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, tr := range tracks {
		id := model.NormalizeTrackID(tr.ID)
		if id == "" {
			continue
		}
		secs := -1
		if tr.Duration > 0 {
			secs = int(math.Round(tr.Duration.Seconds()))
		}
		label := tr.Title
		if tr.Artist != "" && tr.Title != "" {
			label = tr.Artist + " - " + tr.Title
		} else if label == "" {
			label = id
		}
		// Strip newlines from #EXTINF labels so output remains valid line-oriented M3U.
		label = strings.NewReplacer("\r", " ", "\n", " ").Replace(label)
		fmt.Fprintf(&b, "#EXTINF:%d,%s\n", secs, label)
		if model.IsVideoID(id) {
			fmt.Fprintf(&b, "https://www.youtube.com/watch?v=%s\n", id)
		} else {
			fmt.Fprintf(&b, "%s\n", id)
		}
	}
	return b.String()
}
