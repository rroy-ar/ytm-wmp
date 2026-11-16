// Package model contains the shared domain types used by the player.
package model

import (
	"net/url"
	"strings"
	"time"
)

// Track is a playable YouTube track.
//
// ID is the YouTube video ID and the identity key: caches, artwork, and
// playlists are keyed by it, never by title or by a signed stream URL.
type Track struct {
	ID           string
	Title        string
	Artist       string
	Album        string
	Duration     time.Duration
	ThumbnailURL string
}

// StreamInfo is a resolved, directly playable stream URL.
//
// ExpiresAt comes from our TTL policy, not from YouTube's signature: the URL
// is reused until the cache entry is considered stale.
type StreamInfo struct {
	URL       string
	ExpiresAt time.Time
}

// VideoID extracts a YouTube video ID from a bare ID or common YouTube URL
// shapes. It returns "" for search queries and non-YouTube targets.
func VideoID(target string) string {
	target = strings.TrimSpace(target)
	if IsVideoID(target) {
		return target
	}

	u, err := url.Parse(target)
	if err != nil {
		return ""
	}

	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch host {
	case "youtu.be":
		id := strings.Trim(u.Path, "/")
		if IsVideoID(id) {
			return id
		}
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		if id := u.Query().Get("v"); IsVideoID(id) {
			return id
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 {
			switch parts[0] {
			case "shorts", "embed", "live":
				if IsVideoID(parts[1]) {
					return parts[1]
				}
			}
		}
	}
	return ""
}

// IsVideoID reports whether s matches the 11-character YouTube video ID shape.
func IsVideoID(s string) bool {
	if len(s) != 11 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// NormalizeTrackID extracts the canonical 11-character YouTube video ID from
// target when possible, otherwise returning trimmed target (or "" if blank).
func NormalizeTrackID(target string) string {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return ""
	}
	if vid := VideoID(trimmed); vid != "" {
		return vid
	}
	return trimmed
}

// DefaultThumbnailURL returns the standard YouTube hqdefault thumbnail URL for
// idOrURL when a video ID can be extracted, or "" otherwise.
func DefaultThumbnailURL(idOrURL string) string {
	if vid := VideoID(idOrURL); vid != "" {
		return "https://i.ytimg.com/vi/" + vid + "/hqdefault.jpg"
	}
	return ""
}
