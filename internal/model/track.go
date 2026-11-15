// Package model contains the shared domain types used by the player.
package model

import "time"

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
