// Package catalog defines music catalog search, playlist import, and track
// metadata lookup abstractions decoupled from yt-dlp and UI packages.
package catalog

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/rroy-ar/ytm-wmp/internal/model"
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

// RadioProvider is an optional capability implemented by Catalog backends that
// can generate unauthenticated Up Next / radio track recommendations from a
// seed track ID.
type RadioProvider interface {
	Related(ctx context.Context, trackID string) ([]model.Track, error)
}

// SearchTab selects which catalog entity kind a browser search targets.
type SearchTab string

const (
	TabSongs     SearchTab = "songs"
	TabPlaylists SearchTab = "playlists"
	TabAlbums    SearchTab = "albums"
	TabArtists   SearchTab = "artists"
)

// CollectionKind identifies a browsable non-track catalog entity.
type CollectionKind string

const (
	CollectionPlaylist CollectionKind = "playlist"
	CollectionAlbum    CollectionKind = "album"
	CollectionArtist   CollectionKind = "artist"
)

// Collection represents a playlist, album, or artist entry returned by tab
// search or artist discography shelves.
type Collection struct {
	ID           string
	Kind         CollectionKind
	Title        string
	Subtitle     string
	ThumbnailURL string
}

// CollectionDetail contains the tracks (and optional albums for an artist)
// returned when opening a Collection.
type CollectionDetail struct {
	Collection Collection
	Tracks     []model.Track
	Albums     []Collection
}

// TabCatalog is an optional capability implemented by Catalog backends that
// support searching and browsing playlists, albums, and/or artists.
type TabCatalog interface {
	SupportedTabs() []SearchTab
	SearchCollections(ctx context.Context, tab SearchTab, query string, limit int) ([]Collection, error)
	BrowseCollection(ctx context.Context, col Collection) (CollectionDetail, error)
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
