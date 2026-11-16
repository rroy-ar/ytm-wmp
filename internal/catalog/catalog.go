// Package catalog defines music catalog search, playlist import, and track
// metadata lookup abstractions decoupled from yt-dlp and UI packages.
package catalog

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"ytm-wmp/internal/model"
)

var (
	// ErrEmptyQuery means a search, track, or playlist target was blank.
	ErrEmptyQuery = errors.New("empty search query")
	// ErrNotFound means the catalog returned no matching item or track ID.
	ErrNotFound = errors.New("catalog item not found")
	// ErrOutputTooLarge means the catalog subprocess exceeded its stdout cap.
	ErrOutputTooLarge = errors.New("catalog output exceeded size limit")
)

// Catalog provides search, playlist expansion, and track metadata lookups.
type Catalog interface {
	Search(ctx context.Context, query string, limit int) ([]model.Track, error)
	Playlist(ctx context.Context, idOrURL string) ([]model.Track, error)
	Track(ctx context.Context, id string) (model.Track, error)
}

// IsPlaylistTarget reports whether input looks like a YouTube/YouTube Music
// playlist URL or bare playlist ID rather than a single-track target or search
// query.
func IsPlaylistTarget(input string) bool {
	s := strings.TrimSpace(input)
	if s == "" {
		return false
	}

	if u, err := url.Parse(s); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		q := u.Query()
		path := strings.TrimRight(u.Path, "/")
		if q.Get("list") != "" {
			if path == "/playlist" {
				return true
			}
			return q.Get("v") == ""
		}
		if strings.HasPrefix(path, "/browse/") {
			browseID := strings.TrimPrefix(path, "/browse/")
			return isBarePlaylistID(browseID)
		}
		return false
	}

	return isBarePlaylistID(s)
}

func isBarePlaylistID(s string) bool {
	if len(s) < 13 {
		return false
	}
	hasPrefix := false
	for _, prefix := range []string{"OLAK5uy_", "VL", "MPREb_", "RDCLAK5uy_", "PL", "UU", "FL", "LL"} {
		if strings.HasPrefix(s, prefix) {
			hasPrefix = true
			break
		}
	}
	if !hasPrefix {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}
