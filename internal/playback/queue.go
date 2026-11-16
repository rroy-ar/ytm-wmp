package playback

import (
	"math/rand/v2"
	"sort"
	"strings"

	"ytm-wmp/internal/model"
)

const maxHistory = 256

// queue owns the track list, current index, play history, and shuffle bag.
// It is used exclusively from the actor goroutine.
type queue struct {
	tracks  []model.Track
	index   int
	history []int
	shuffle bool
	repeat  RepeatMode
	bag     []int
	intn    func(int) int
}

func newQueue(intn func(int) int) *queue {
	if intn == nil {
		intn = rand.IntN
	}
	return &queue{
		index: -1,
		intn:  intn,
	}
}

func (q *queue) Set(tracks []model.Track, index int) {
	q.tracks = append([]model.Track(nil), tracks...)
	q.history = nil
	q.bag = nil

	if len(q.tracks) == 0 {
		q.index = -1
		return
	}
	if index < 0 || index >= len(q.tracks) {
		index = 0
	}
	q.index = index
	if q.shuffle {
		q.rebuildBag(q.index)
	}
}

func (q *queue) Enqueue(tracks []model.Track) {
	if len(tracks) == 0 {
		return
	}
	oldLen := len(q.tracks)
	q.tracks = append(q.tracks, tracks...)

	if q.index < 0 {
		q.index = 0
		if q.shuffle {
			q.rebuildBag(q.index)
		}
		return
	}

	if q.shuffle {
		for idx := oldLen; idx < len(q.tracks); idx++ {
			pos := q.intn(len(q.bag) + 1)
			q.bag = append(q.bag, 0)
			copy(q.bag[pos+1:], q.bag[pos:])
			q.bag[pos] = idx
		}
	}
}

func (q *queue) SetShuffle(enabled bool) {
	if q.shuffle == enabled {
		return
	}
	q.shuffle = enabled
	if !enabled {
		q.bag = nil
		return
	}
	q.rebuildBag(q.index)
}

func (q *queue) SetRepeat(mode RepeatMode) {
	q.repeat = mode
}

func (q *queue) Current() (model.Track, bool) {
	if q.index < 0 || q.index >= len(q.tracks) {
		return model.Track{}, false
	}
	return q.tracks[q.index], true
}

func (q *queue) Select(index int) bool {
	if index < 0 || index >= len(q.tracks) {
		return false
	}
	if q.index >= 0 && q.index != index {
		q.pushHistory(q.index)
	}
	q.index = index
	if q.shuffle {
		q.removeFromBag(index)
	}
	return true
}

// Next advances to the next track for a manual Next command.
func (q *queue) Next() bool {
	if len(q.tracks) == 0 {
		return false
	}

	if q.shuffle {
		if len(q.bag) == 0 {
			if q.repeat != RepeatAll {
				return false
			}
			q.refillBagAvoiding(q.index)
		}
		if len(q.bag) == 0 {
			return false
		}
		nextIdx := q.bag[0]
		q.bag = q.bag[1:]
		if q.index >= 0 {
			q.pushHistory(q.index)
		}
		q.index = nextIdx
		return true
	}

	if q.index+1 < len(q.tracks) {
		if q.index >= 0 {
			q.pushHistory(q.index)
		}
		q.index++
		return true
	}
	if q.repeat == RepeatAll {
		if q.index >= 0 {
			q.pushHistory(q.index)
		}
		q.index = 0
		return true
	}
	return false
}

// AutoAdvance advances after a track reaches natural EOF.
func (q *queue) AutoAdvance() bool {
	if len(q.tracks) == 0 {
		return false
	}
	if q.repeat == RepeatOne {
		return q.index >= 0 && q.index < len(q.tracks)
	}
	return q.Next()
}

// PeekNext returns the track that AutoAdvance would play next without advancing
// the queue cursor.
func (q *queue) PeekNext() (model.Track, bool) {
	if len(q.tracks) == 0 || q.repeat == RepeatOne {
		return model.Track{}, false
	}
	if q.shuffle {
		if len(q.bag) == 0 {
			if q.repeat != RepeatAll {
				return model.Track{}, false
			}
			q.refillBagAvoiding(q.index)
		}
		if len(q.bag) == 0 {
			return model.Track{}, false
		}
		return q.tracks[q.bag[0]], true
	}
	if q.index+1 < len(q.tracks) {
		return q.tracks[q.index+1], true
	}
	if q.repeat == RepeatAll {
		return q.tracks[0], true
	}
	return model.Track{}, false
}

// Prev steps back through history, or to the previous index when history is empty.
func (q *queue) Prev() bool {
	if len(q.tracks) == 0 {
		return false
	}
	for len(q.history) > 0 {
		prev := q.history[len(q.history)-1]
		q.history = q.history[:len(q.history)-1]
		if prev >= 0 && prev < len(q.tracks) && prev != q.index {
			q.index = prev
			return true
		}
	}
	if q.shuffle {
		return false
	}
	if q.index > 0 {
		q.index--
		return true
	}
	if q.repeat == RepeatAll {
		q.index = len(q.tracks) - 1
		return true
	}
	return false
}

func (q *queue) TracksCopy() []model.Track {
	if len(q.tracks) == 0 {
		return nil
	}
	return append([]model.Track(nil), q.tracks...)
}

func (q *queue) Index() int    { return q.index }
func (q *queue) Len() int      { return len(q.tracks) }
func (q *queue) Shuffle() bool { return q.shuffle }
func (q *queue) Repeat() RepeatMode {
	return q.repeat
}

// Remove deletes tracks at indices, remapping the current cursor, history, and
// shuffle bag. It reports whether anything changed and whether the active track
// at q.index was removed.
func (q *queue) Remove(indices []int) (changed, currentRemoved bool) {
	if len(indices) == 0 || len(q.tracks) == 0 {
		return false, false
	}
	removeSet := make(map[int]bool, len(indices))
	for _, idx := range indices {
		if idx >= 0 && idx < len(q.tracks) {
			removeSet[idx] = true
		}
	}
	if len(removeSet) == 0 {
		return false, false
	}

	oldLen := len(q.tracks)
	newIdx := make([]int, oldLen)
	kept := make([]model.Track, 0, oldLen-len(removeSet))
	for i, tr := range q.tracks {
		if removeSet[i] {
			newIdx[i] = -1
			continue
		}
		newIdx[i] = len(kept)
		kept = append(kept, tr)
	}
	q.tracks = kept

	if len(q.tracks) == 0 {
		currentRemoved = q.index >= 0
		q.index = -1
		q.history = nil
		q.bag = nil
		return true, currentRemoved
	}

	if q.index >= 0 && q.index < oldLen {
		if removeSet[q.index] {
			currentRemoved = true
			// Pick the next surviving track after the removed position, or the last.
			next := -1
			for i := q.index + 1; i < oldLen; i++ {
				if newIdx[i] >= 0 {
					next = newIdx[i]
					break
				}
			}
			if next < 0 {
				next = len(q.tracks) - 1
			}
			q.index = next
		} else {
			q.index = newIdx[q.index]
		}
	}

	var nextHist []int
	for _, h := range q.history {
		if h >= 0 && h < oldLen && newIdx[h] >= 0 {
			nextHist = append(nextHist, newIdx[h])
		}
	}
	q.history = nextHist

	if q.shuffle {
		var nextBag []int
		for _, b := range q.bag {
			if b >= 0 && b < oldLen && newIdx[b] >= 0 && newIdx[b] != q.index {
				nextBag = append(nextBag, newIdx[b])
			}
		}
		q.bag = nextBag
	}
	return true, currentRemoved
}

// Move shifts the track at from to index to while preserving the active track
// selection.
func (q *queue) Move(from, to int) bool {
	if from == to || from < 0 || from >= len(q.tracks) || to < 0 || to >= len(q.tracks) {
		return false
	}
	moved := q.tracks[from]
	if from < to {
		copy(q.tracks[from:to], q.tracks[from+1:to+1])
		q.tracks[to] = moved
		if q.index == from {
			q.index = to
		} else if q.index > from && q.index <= to {
			q.index--
		}
	} else {
		copy(q.tracks[to+1:from+1], q.tracks[to:from])
		q.tracks[to] = moved
		if q.index == from {
			q.index = to
		} else if q.index >= to && q.index < from {
			q.index++
		}
	}
	q.history = nil
	if q.shuffle {
		q.rebuildBag(q.index)
	}
	return true
}

// SortByTitle sorts queue tracks alphabetically by Artist then Title while
// keeping the current track selected at its new index.
func (q *queue) SortByTitle() bool {
	if len(q.tracks) < 2 {
		return false
	}
	type indexed struct {
		track model.Track
		old   int
	}
	items := make([]indexed, len(q.tracks))
	for i, tr := range q.tracks {
		items[i] = indexed{track: tr, old: i}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a := strings.ToLower(trackSortKey(items[i].track))
		b := strings.ToLower(trackSortKey(items[j].track))
		return a < b
	})

	newIdx := make([]int, len(items))
	for i, it := range items {
		q.tracks[i] = it.track
		newIdx[it.old] = i
	}
	if q.index >= 0 && q.index < len(newIdx) {
		q.index = newIdx[q.index]
	}
	q.history = nil
	if q.shuffle {
		q.rebuildBag(q.index)
	}
	return true
}

func trackSortKey(tr model.Track) string {
	title := tr.Title
	if title == "" {
		title = tr.ID
	}
	if tr.Artist == "" {
		return title
	}
	return tr.Artist + " - " + title
}

// UpdateTrack enriches matching tracks in the queue with non-empty metadata
// fields from meta and reports whether any track changed.
func (q *queue) UpdateTrack(meta model.Track) bool {
	normMeta := normalizedTrackID(meta.ID)
	if normMeta == "" {
		return false
	}
	changed := false
	for i := range q.tracks {
		if !tracksMatchID(q.tracks[i].ID, meta.ID, normMeta) {
			continue
		}
		if mergeTrackMetadata(&q.tracks[i], meta) {
			changed = true
		}
	}
	return changed
}

func normalizedTrackID(id string) string {
	return model.NormalizeTrackID(id)
}

func tracksMatchID(existingID, rawMetaID, normMetaID string) bool {
	if existingID == rawMetaID || strings.TrimSpace(existingID) == normMetaID {
		return true
	}
	vid := model.VideoID(existingID)
	return vid != "" && vid == normMetaID
}

func mergeTrackMetadata(dst *model.Track, src model.Track) bool {
	changed := false
	if src.Title != "" && dst.Title != src.Title {
		dst.Title = src.Title
		changed = true
	}
	if src.Artist != "" && dst.Artist != src.Artist {
		dst.Artist = src.Artist
		changed = true
	}
	if src.Album != "" && dst.Album != src.Album {
		dst.Album = src.Album
		changed = true
	}
	if src.Duration > 0 && dst.Duration != src.Duration {
		dst.Duration = src.Duration
		changed = true
	}
	if src.ThumbnailURL != "" && dst.ThumbnailURL != src.ThumbnailURL {
		dst.ThumbnailURL = src.ThumbnailURL
		changed = true
	}
	return changed
}

func (q *queue) pushHistory(idx int) {
	q.history = append(q.history, idx)
	if len(q.history) > maxHistory {
		copy(q.history, q.history[len(q.history)-maxHistory:])
		q.history = q.history[:maxHistory]
	}
}

func (q *queue) rebuildBag(exclude int) {
	q.bag = make([]int, 0, len(q.tracks))
	for i := range q.tracks {
		if i == exclude {
			continue
		}
		q.bag = append(q.bag, i)
	}
	q.shuffleSlice(q.bag)
}

// refillBagAvoiding builds a fresh full-cycle shuffle bag while ensuring the
// just-played track does not appear first when more than one track exists.
func (q *queue) refillBagAvoiding(last int) {
	q.rebuildBag(last)
	if last >= 0 && last < len(q.tracks) {
		if len(q.bag) == 0 {
			q.bag = append(q.bag, last)
			return
		}
		// Insert last at a non-zero index so it stays in this cycle without
		// playing twice in a row.
		pos := 1 + q.intn(len(q.bag))
		q.bag = append(q.bag, 0)
		copy(q.bag[pos+1:], q.bag[pos:])
		q.bag[pos] = last
	}
}

func (q *queue) removeFromBag(target int) {
	for i, idx := range q.bag {
		if idx == target {
			q.bag = append(q.bag[:i], q.bag[i+1:]...)
			return
		}
	}
}

func (q *queue) shuffleSlice(s []int) {
	for i := len(s) - 1; i > 0; i-- {
		j := q.intn(i + 1)
		s[i], s[j] = s[j], s[i]
	}
}
