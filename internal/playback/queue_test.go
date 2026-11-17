package playback

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/rroy-ar/ytm-wmp/internal/model"
)

func makeTracks(n int) []model.Track {
	out := make([]model.Track, n)
	for i := range out {
		out[i] = model.Track{
			ID:    fmt.Sprintf("track-%02d", i),
			Title: fmt.Sprintf("Track %d", i),
		}
	}
	return out
}

func TestQueueLinearNextPrevAndRepeat(t *testing.T) {
	q := newQueue(nil)
	q.Set(makeTracks(10), 0)

	for i := 0; i < 9; i++ {
		if cur, ok := q.Current(); !ok || cur.ID != fmt.Sprintf("track-%02d", i) {
			t.Fatalf("step %d: Current = %+v, %v", i, cur, ok)
		}
		if !q.Next() {
			t.Fatalf("step %d: Next() = false, want true", i)
		}
	}
	if cur, _ := q.Current(); cur.ID != "track-09" {
		t.Fatalf("at end: Current = %q, want track-09", cur.ID)
	}
	if q.Next() {
		t.Fatal("Next() past end with RepeatOff should return false")
	}

	// Step backwards through all 10 tracks.
	for i := 8; i >= 0; i-- {
		if !q.Prev() {
			t.Fatalf("Prev() at %d = false, want true", i+1)
		}
		if cur, _ := q.Current(); cur.ID != fmt.Sprintf("track-%02d", i) {
			t.Fatalf("Prev() landed on %q, want track-%02d", cur.ID, i)
		}
	}
	if q.Prev() {
		t.Fatal("Prev() before 0 with RepeatOff should return false")
	}

	// RepeatAll wraps both forward and backward.
	q.SetRepeat(RepeatAll)
	if !q.Prev() {
		t.Fatal("Prev() at 0 with RepeatAll should wrap")
	}
	if q.Index() != 9 {
		t.Fatalf("Index after wrapped Prev = %d, want 9", q.Index())
	}
	if !q.Next() {
		t.Fatal("Next() at 9 with RepeatAll should wrap")
	}
	if q.Index() != 0 {
		t.Fatalf("Index after wrapped Next = %d, want 0", q.Index())
	}
}

func TestQueueRepeatOneAutoAdvanceVsManualNext(t *testing.T) {
	q := newQueue(nil)
	q.Set(makeTracks(3), 1)
	q.SetRepeat(RepeatOne)

	if peek, ok := q.PeekNext(); ok {
		t.Fatalf("PeekNext with RepeatOne = %+v, true; want false (already resolved)", peek)
	}
	if !q.AutoAdvance() {
		t.Fatal("AutoAdvance with RepeatOne should return true")
	}
	if q.Index() != 1 {
		t.Fatalf("Index after AutoAdvance(RepeatOne) = %d, want 1", q.Index())
	}

	if !q.Next() {
		t.Fatal("manual Next with RepeatOne should still advance")
	}
	if q.Index() != 2 {
		t.Fatalf("Index after manual Next = %d, want 2", q.Index())
	}
}

func TestQueueShuffleBagExhaustsBeforeRepeating(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 99))
	q := newQueue(rng.IntN)
	q.Set(makeTracks(10), 0)
	q.SetShuffle(true)

	seen := map[int]int{q.Index(): 1}
	for i := 0; i < 9; i++ {
		peek, ok := q.PeekNext()
		if !ok {
			t.Fatalf("PeekNext at step %d = false", i)
		}
		if !q.Next() {
			t.Fatalf("Next at step %d = false", i)
		}
		if cur, _ := q.Current(); cur.ID != peek.ID {
			t.Fatalf("PeekNext predicted %q, Next chose %q", peek.ID, cur.ID)
		}
		seen[q.Index()]++
	}

	if len(seen) != 10 {
		t.Fatalf("visited %d distinct tracks, want all 10: %v", len(seen), seen)
	}
	for idx, count := range seen {
		if count != 1 {
			t.Fatalf("track %d visited %d times in one cycle", idx, count)
		}
	}

	if q.Next() {
		t.Fatal("Next after bag exhausted with RepeatOff should return false")
	}

	// RepeatAll refills without repeating the last-played track immediately.
	lastIdx := q.Index()
	q.SetRepeat(RepeatAll)
	peek, ok := q.PeekNext()
	if !ok {
		t.Fatal("PeekNext with RepeatAll after bag empty should refill and succeed")
	}
	if !q.Next() {
		t.Fatal("Next with RepeatAll should refill")
	}
	if cur, _ := q.Current(); cur.ID != peek.ID {
		t.Fatalf("refill PeekNext=%q != Next=%q", peek.ID, cur.ID)
	}
	if q.Index() == lastIdx {
		t.Fatalf("refill immediately repeated track %d", lastIdx)
	}

	// Prev follows shuffle history back to lastIdx.
	if !q.Prev() || q.Index() != lastIdx {
		t.Fatalf("Prev after refill landed on %d, want %d", q.Index(), lastIdx)
	}
}

func TestQueueEnqueueWhileShuffled(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	q := newQueue(rng.IntN)
	q.Set(makeTracks(2), 0)
	q.SetShuffle(true)

	q.Enqueue([]model.Track{{ID: "added-a"}, {ID: "added-b"}})
	if q.Len() != 4 {
		t.Fatalf("Len = %d, want 4", q.Len())
	}

	visited := map[string]bool{q.tracks[q.Index()].ID: true}
	for q.Next() {
		cur, _ := q.Current()
		visited[cur.ID] = true
	}
	if len(visited) != 4 {
		t.Fatalf("visited %v, want all 4 tracks including enqueued ones", visited)
	}
}

func TestQueueRemoveAndSort(t *testing.T) {
	q := newQueue(nil)
	q.Set([]model.Track{
		{ID: "a", Artist: "Zed", Title: "Last"},
		{ID: "b", Artist: "Alpha", Title: "First"},
		{ID: "c", Artist: "Mid", Title: "Middle"},
		{ID: "d", Artist: "Beta", Title: "Second"},
	}, 2) // "c"

	if !q.SortByTitle() {
		t.Fatal("SortByTitle = false")
	}
	if cur, _ := q.Current(); cur.ID != "c" {
		t.Fatalf("current after sort = %q, want c", cur.ID)
	}
	// Sorted order: Alpha(b), Beta(d), Mid(c), Zed(a) -> index 2 is c.
	if q.Index() != 2 {
		t.Fatalf("index after sort = %d, want 2", q.Index())
	}

	// Remove non-active index 0 ("b") and active index 2 ("c").
	changed, curRemoved := q.Remove([]int{0, 2})
	if !changed || !curRemoved {
		t.Fatalf("Remove = (%v, %v), want (true, true)", changed, curRemoved)
	}
	if q.Len() != 2 {
		t.Fatalf("Len = %d, want 2", q.Len())
	}
	if cur, _ := q.Current(); cur.ID != "a" {
		t.Fatalf("current after removing active = %q, want a", cur.ID)
	}

	// Remove all remaining tracks.
	changed, curRemoved = q.Remove([]int{0, 1})
	if !changed || !curRemoved || q.Len() != 0 || q.Index() != -1 {
		t.Fatalf("Remove all = changed=%v curRemoved=%v len=%d idx=%d", changed, curRemoved, q.Len(), q.Index())
	}
}

func TestQueueMovePreservesCurrentIndex(t *testing.T) {
	q := newQueue(nil)
	q.Set([]model.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}}, 1) // current: b

	// Move c (2) up to 0 -> [c, a, b], current b moves to 2.
	if !q.Move(2, 0) {
		t.Fatal("Move(2, 0) = false")
	}
	if q.Index() != 2 || q.tracks[0].ID != "c" || q.tracks[2].ID != "b" {
		t.Fatalf("after Move(2,0): idx=%d tracks=%v", q.Index(), q.tracks)
	}

	// Move current b (2) up to 1 -> [c, b, a], current moves to 1.
	if !q.Move(2, 1) {
		t.Fatal("Move(2, 1) = false")
	}
	if q.Index() != 1 || q.tracks[1].ID != "b" {
		t.Fatalf("after Move(2,1): idx=%d tracks=%v", q.Index(), q.tracks)
	}

	// Move c (0) down to 2 -> [b, a, c], current moves to 0.
	if !q.Move(0, 2) {
		t.Fatal("Move(0, 2) = false")
	}
	if q.Index() != 0 || q.tracks[0].ID != "b" || q.tracks[2].ID != "c" {
		t.Fatalf("after Move(0,2): idx=%d tracks=%v", q.Index(), q.tracks)
	}
}
