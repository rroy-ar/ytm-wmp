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
