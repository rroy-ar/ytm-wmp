package playback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"ytm-wmp/internal/audio"
	"ytm-wmp/internal/deps"
	"ytm-wmp/internal/model"
	"ytm-wmp/internal/ytdlp"
)

func TestControllerTenTrackQueueAutoAdvancesAndNavigates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	ctrl := NewController(resolver, opener, sink)
	go ctrl.Run(ctx)

	tracks := makeTracks(10)
	ctrl.Send(SetQueue{Tracks: tracks, Index: 0, AutoPlay: true})

	for i := 0; i < 10; i++ {
		waitTrackPlaying(t, ctrl, tracks[i].ID)
		snap := ctrl.Snapshot()
		if snap.Index != i || snap.Track.ID != tracks[i].ID {
			t.Fatalf("track %d: got index=%d id=%q", i, snap.Index, snap.Track.ID)
		}
		sink.finishCurrent(nil)
	}

	waitState(t, ctrl, StateEnded)

	// Prev from end steps back to track 8.
	ctrl.Send(Prev{})
	waitTrackPlaying(t, ctrl, "track-08")
	if snap := ctrl.Snapshot(); snap.Index != 8 {
		t.Fatalf("after Prev from track 9: index = %d, want 8", snap.Index)
	}
	ctrl.Send(Next{})
	waitTrackPlaying(t, ctrl, "track-09")
	if snap := ctrl.Snapshot(); snap.Index != 9 {
		t.Fatalf("after Next: index = %d, want 9", snap.Index)
	}

	cancel()
	waitDone(t, ctrl)
}

func TestControllerPrevAtStartRestartsCurrentTrack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	ctrl := NewController(resolver, opener, sink)
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{Tracks: makeTracks(1), Index: 0, AutoPlay: true})
	waitState(t, ctrl, StatePlaying)

	sink.setPosition(20 * time.Second)
	ctrl.Send(Prev{})
	waitCondition(t, func() bool {
		return opener.callCount() == 2 && ctrl.Snapshot().State == StatePlaying
	})
	if got := ctrl.Snapshot().Position; got != 0 {
		t.Fatalf("Position after Prev restart = %v, want 0", got)
	}

	cancel()
	waitDone(t, ctrl)
}

func TestControllerNextPrevWithActivelyReadingSink(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	opener.queueSources(
		newBlockingSource(),
		newBlockingSource(),
		newBlockingSource(),
		newBlockingSource(),
	)
	sink := &pcmReadingSink{gotPCM: make(chan struct{})}
	ctrl := NewController(resolver, opener, sink)
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{Tracks: makeTracks(3), Index: 0, AutoPlay: true})
	waitTrackPlaying(t, ctrl, "track-00")

	ctrl.Send(Next{})
	waitTrackPlaying(t, ctrl, "track-01")

	ctrl.Send(Next{})
	waitTrackPlaying(t, ctrl, "track-02")

	ctrl.Send(Prev{})
	waitTrackPlaying(t, ctrl, "track-01")

	cancel()
	waitDone(t, ctrl)
}

func TestControllerPauseResumeAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	ctrl := NewController(resolver, opener, sink)
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{Tracks: makeTracks(2), Index: 0, AutoPlay: true})
	waitState(t, ctrl, StatePlaying)

	sink.setPosition(15 * time.Second)
	ctrl.Send(Pause{})
	waitState(t, ctrl, StatePaused)
	if snap := ctrl.Snapshot(); snap.Position != 15*time.Second {
		t.Fatalf("paused Position = %v, want 15s", snap.Position)
	}
	if sink.pauseCount() != 1 {
		t.Fatalf("sink.Pause calls = %d, want 1", sink.pauseCount())
	}

	ctrl.Send(Resume{})
	waitState(t, ctrl, StatePlaying)
	if sink.resumeCount() != 1 {
		t.Fatalf("sink.Resume calls = %d, want 1", sink.resumeCount())
	}

	ctrl.Send(Stop{})
	waitState(t, ctrl, StateIdle)
	if snap := ctrl.Snapshot(); snap.Position != 0 || snap.Track.ID != "track-00" {
		t.Fatalf("after Stop: pos=%v track=%q, want 0, track-00", snap.Position, snap.Track.ID)
	}

	cancel()
	waitDone(t, ctrl)
}

func TestControllerPauseDuringResolveStartsPaused(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	resolver.gate = make(chan struct{})
	opener := newFakeOpener()
	sink := newFakeSink()
	ctrl := NewController(resolver, opener, sink)
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{Tracks: makeTracks(1), Index: 0, AutoPlay: true})
	waitState(t, ctrl, StateResolving)

	ctrl.Send(Pause{})
	close(resolver.gate)

	waitState(t, ctrl, StatePaused)
	if sink.pauseCount() == 0 {
		t.Fatal("expected sink.Pause to be called for deferred pause")
	}

	cancel()
	waitDone(t, ctrl)
}

func TestControllerSeekRecreatesPipelineAtOffset(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	ctrl := NewController(resolver, opener, sink)
	go ctrl.Run(ctx)

	tracks := []model.Track{{ID: "vid-1", Title: "Song", Duration: 2 * time.Minute}}
	ctrl.Send(SetQueue{Tracks: tracks, Index: 0, AutoPlay: true})
	waitState(t, ctrl, StatePlaying)

	sink.setPosition(10 * time.Second)
	ctrl.Send(Seek{Position: 60 * time.Second})
	waitCondition(t, func() bool {
		return opener.callCount() == 2 && ctrl.Snapshot().State == StatePlaying
	})

	offsets := opener.offsets()
	if len(offsets) != 2 || offsets[1] != 60*time.Second {
		t.Fatalf("opener offsets = %v, want [0s 60s]", offsets)
	}

	sink.setPosition(5 * time.Second)
	if snap := ctrl.Snapshot(); snap.Position != 65*time.Second {
		t.Fatalf("Position after seek + 5s = %v, want 65s", snap.Position)
	}

	// Seeking past duration clamps to duration.
	ctrl.Send(Seek{Position: 5 * time.Minute})
	waitCondition(t, func() bool {
		return opener.callCount() == 3 && ctrl.Snapshot().State == StatePlaying
	})
	offsets = opener.offsets()
	if offsets[2] != 2*time.Minute {
		t.Fatalf("clamped seek offset = %v, want 2m", offsets[2])
	}

	cancel()
	waitDone(t, ctrl)
}

func TestControllerStaleURLRetryAndExhaustion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("recovers on second attempt", func(t *testing.T) {
		resolver := newFakeResolver()
		opener := newFakeOpener()
		opener.queueSources(&fakeSource{frames: 0}, &fakeSource{frames: 100})
		sink := newFakeSink()
		sink.autoFinish = true

		ctrl := NewController(resolver, opener, sink)
		go ctrl.Run(ctx)

		ctrl.Send(SetQueue{Tracks: makeTracks(1), Index: 0, AutoPlay: true})
		waitState(t, ctrl, StateEnded)

		if resolver.resolveCount() != 2 {
			t.Fatalf("resolves = %d, want 2", resolver.resolveCount())
		}
		if resolver.forgetCount() != 1 {
			t.Fatalf("forgets = %d, want 1", resolver.forgetCount())
		}
	})

	t.Run("surfaces ErrStaleStream after two zero-frame attempts", func(t *testing.T) {
		resolver := newFakeResolver()
		opener := newFakeOpener()
		opener.queueSources(&fakeSource{frames: 0}, &fakeSource{frames: 0})
		sink := newFakeSink()
		sink.autoFinish = true

		ctrl := NewController(resolver, opener, sink)
		go ctrl.Run(ctx)

		ctrl.Send(SetQueue{Tracks: makeTracks(1), Index: 0, AutoPlay: true})
		waitState(t, ctrl, StateError)

		snap := ctrl.Snapshot()
		if snap.LastError == "" {
			t.Fatal("LastError is empty, want stale stream message")
		}
		ev := waitEvent[PlaybackError](t, ctrl.Events())
		if !errors.Is(ev.Err, ErrStaleStream) {
			t.Fatalf("PlaybackError.Err = %v, want ErrStaleStream", ev.Err)
		}

		// Retry from StateError works via Play{}.
		opener.queueSources(&fakeSource{frames: 50})
		ctrl.Send(Play{})
		waitState(t, ctrl, StateEnded)
	})

	t.Run("does not retry read or process errors", func(t *testing.T) {
		readFailure := errors.New("connection reset by peer")
		resolver := newFakeResolver()
		opener := newFakeOpener()
		opener.queueSources(&fakeSource{readErr: readFailure})
		sink := newFakeSink()
		sink.autoFinish = true

		ctrl := NewController(resolver, opener, sink)
		go ctrl.Run(ctx)

		ctrl.Send(SetQueue{Tracks: makeTracks(1), Index: 0, AutoPlay: true})
		waitState(t, ctrl, StateError)

		if resolver.resolveCount() != 1 {
			t.Fatalf("resolves = %d, want 1 (no retry on read error)", resolver.resolveCount())
		}
		ev := waitEvent[PlaybackError](t, ctrl.Events())
		if !errors.Is(ev.Err, ErrNetwork) {
			t.Fatalf("PlaybackError.Err = %v, want ErrNetwork", ev.Err)
		}
	})
}

func TestControllerDropsStaleGenerationResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	resolver.onResolve = func(rCtx context.Context, target string) (audio.StreamInfo, error) {
		if target == "track-00" {
			close(slowStarted)
			<-releaseSlow
			return audio.StreamInfo{}, errors.New("stale failure")
		}
		return audio.StreamInfo{URL: "https://stream.example/" + target}, nil
	}

	opener := newFakeOpener()
	sink := newFakeSink()
	ctrl := NewController(resolver, opener, sink)
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{Tracks: makeTracks(2), Index: 0, AutoPlay: true})
	<-slowStarted

	// Immediately skip to track-01 while track-00's resolve ignores ctx.
	nextDone := make(chan struct{})
	go func() {
		ctrl.Send(Next{})
		close(nextDone)
	}()
	// Release the slow resolver; its error must be ignored because gen advanced.
	close(releaseSlow)
	<-nextDone

	waitState(t, ctrl, StatePlaying)
	snap := ctrl.Snapshot()
	if snap.Track.ID != "track-01" || snap.State != StatePlaying || snap.LastError != "" {
		t.Fatalf("unexpected state after stale gen: %+v", snap)
	}

	cancel()
	waitDone(t, ctrl)
}

func TestControllerPrefetchesNextURLNearEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	ctrl := NewController(
		resolver, opener, sink,
		WithPrefetchWindow(30*time.Second),
		WithTickInterval(10*time.Millisecond),
	)
	go ctrl.Run(ctx)

	tracks := []model.Track{
		{ID: "first", Duration: 100 * time.Second},
		{ID: "second", Duration: 100 * time.Second},
	}
	ctrl.Send(SetQueue{Tracks: tracks, Index: 0, AutoPlay: true})
	waitState(t, ctrl, StatePlaying)

	sink.setPosition(75 * time.Second)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if resolver.resolvedTarget("second") > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if resolver.resolvedTarget("second") == 0 {
		t.Fatal("expected next track URL to be prefetched near end of current track")
	}
	// Prefetch must NOT open ffmpeg audio ahead of time.
	if opener.callCount() != 1 {
		t.Fatalf("opener calls = %d, want 1 (URL prefetch only)", opener.callCount())
	}

	cancel()
	waitDone(t, ctrl)
}

func TestControllerVolumeAndNoGoroutineLeakAfterTwentyTrackChanges(t *testing.T) {
	runtime.GC()
	baseline := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	ctrl := NewController(resolver, opener, sink)
	go ctrl.Run(ctx)

	ctrl.Send(SetVolume{Volume: 0.35})
	ctrl.Send(SetQueue{Tracks: makeTracks(25), Index: 0, AutoPlay: true})
	waitState(t, ctrl, StatePlaying)

	if got := ctrl.Snapshot().Volume; got != 0.35 {
		t.Fatalf("Snapshot().Volume = %v, want 0.35", got)
	}

	for i := 0; i < 20; i++ {
		ctrl.Send(Next{})
		waitIndex(t, ctrl, i+1)
	}
	waitState(t, ctrl, StatePlaying)

	cancel()
	waitDone(t, ctrl)

	// Allow runtime scheduler a brief moment to park/reap exited goroutines.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= baseline+1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines after shutdown = %d, baseline = %d", runtime.NumGoroutine(), baseline)
}

func TestUpdateTrackEnrichesActiveAndQueuedTrackWithoutInterruptingPlayback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	ctrl := NewController(resolver, opener, sink)
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{
		Tracks: []model.Track{
			{ID: "https://www.youtube.com/watch?v=0wRYvsfhsR8"},
			{ID: "FGBhQbmPwH8"},
		},
		Index:    0,
		AutoPlay: true,
	})
	waitState(t, ctrl, StatePlaying)

	// Drain initial events before UpdateTrack.
	ev := waitEvent[TrackChanged](t, ctrl.Events())
	if ev.Index != 0 {
		t.Fatalf("initial TrackChanged index = %d, want 0", ev.Index)
	}

	sink.setPosition(12 * time.Second)
	ctrl.Send(UpdateTrack{Track: model.Track{
		ID:           "0wRYvsfhsR8",
		Title:        "Something About Us",
		Artist:       "Daft Punk",
		Album:        "Discovery",
		Duration:     232 * time.Second,
		ThumbnailURL: "https://i.ytimg.com/vi/0wRYvsfhsR8/mqdefault.jpg",
	}})

	updated := waitEvent[TrackChanged](t, ctrl.Events())
	if updated.Track.Title != "Something About Us" || updated.Track.Artist != "Daft Punk" {
		t.Fatalf("updated TrackChanged = %+v", updated.Track)
	}

	snap := ctrl.Snapshot()
	if snap.State != StatePlaying {
		t.Fatalf("state after UpdateTrack = %s, want playing (must not restart)", snap.State)
	}
	if opener.callCount() != 1 {
		t.Fatalf("opener calls = %d, want 1", opener.callCount())
	}
	if snap.Position != 12*time.Second || snap.Duration != 232*time.Second {
		t.Fatalf("pos=%v dur=%v, want 12s / 232s", snap.Position, snap.Duration)
	}

	// Update queued (non-active) track too; should update Queue without emitting TrackChanged.
	ctrl.Send(UpdateTrack{Track: model.Track{
		ID:     "FGBhQbmPwH8",
		Title:  "One More Time",
		Artist: "Daft Punk",
	}})
	waitCondition(t, func() bool {
		q := ctrl.Snapshot().Queue
		return len(q) == 2 && q[1].Title == "One More Time"
	})

	cancel()
	waitDone(t, ctrl)
}

func TestControllerSnapshotSharesReadOnlyQueueSlice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctrl := NewController(newFakeResolver(), newFakeOpener(), newFakeSink())
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{Tracks: makeTracks(3), Index: 0, AutoPlay: true})
	waitState(t, ctrl, StatePlaying)

	s1 := ctrl.Snapshot()
	s2 := ctrl.Snapshot()
	if len(s1.Queue) != 3 || unsafe.SliceData(s1.Queue) != unsafe.SliceData(s2.Queue) {
		t.Fatalf("expected Snapshot() to reuse immutable Queue backing array without per-call cloning")
	}

	cancel()
	waitDone(t, ctrl)
}

// TestLivePlaybackIntegration exercises real yt-dlp + ffmpeg when explicitly
// enabled via YTM_WMP_LIVE_TEST=1.
func TestLivePlaybackIntegration(t *testing.T) {
	if os.Getenv("YTM_WMP_LIVE_TEST") != "1" {
		t.Skip("set YTM_WMP_LIVE_TEST=1 to run live yt-dlp/ffmpeg integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ytdlpTool, err := deps.Check(ctx, "yt-dlp")
	if err != nil {
		t.Skipf("yt-dlp unavailable: %v", err)
	}
	ffmpegTool, err := deps.Check(ctx, "ffmpeg")
	if err != nil {
		t.Skipf("ffmpeg unavailable: %v", err)
	}

	resolver := ytdlp.New(ytdlp.WithBinary(ytdlpTool.Path))
	opener := audio.Opener{FFmpeg: ffmpegTool.Path}
	sink := &pcmReadingSink{gotPCM: make(chan struct{})}
	ctrl := NewController(resolver, opener, sink)
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{
		Tracks:   []model.Track{{ID: "0wRYvsfhsR8"}},
		Index:    0,
		AutoPlay: true,
	})

	waitLivePlaying(t, ctrl, 25*time.Second)
	select {
	case <-sink.gotPCM:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for first PCM frame from ffmpeg")
	}

	ctrl.Send(Stop{})
	waitState(t, ctrl, StateIdle)

	cancel()
	waitDone(t, ctrl)
}

func waitLivePlaying(t *testing.T, ctrl *Controller, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snap := ctrl.Snapshot()
		if snap.State == StatePlaying {
			return
		}
		if snap.State == StateError {
			t.Fatalf("playback entered error state: %s", snap.LastError)
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap := ctrl.Snapshot()
	t.Fatalf("timed out after %s waiting for StatePlaying (got %s, err=%q)", timeout, snap.State, snap.LastError)
}

type pcmReadingSink struct {
	gotPCM chan struct{}
	once   sync.Once
}

func (s *pcmReadingSink) Begin()                  {}
func (s *pcmReadingSink) Pause()                  {}
func (s *pcmReadingSink) Resume()                 {}
func (s *pcmReadingSink) SetVolume(float64)       {}
func (s *pcmReadingSink) Position() time.Duration { return 0 }

func (s *pcmReadingSink) Play(ctx context.Context, src audio.Source) error {
	playDone := make(chan struct{})
	defer close(playDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = src.Close()
		case <-playDone:
		}
	}()
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 && src.Frames() > 0 {
			s.once.Do(func() { close(s.gotPCM) })
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func waitState(t *testing.T, ctrl *Controller, want State) {
	t.Helper()
	waitCondition(t, func() bool {
		return ctrl.Snapshot().State == want
	}, func() string {
		snap := ctrl.Snapshot()
		return fmt.Sprintf("state=%s want=%s err=%q", snap.State, want, snap.LastError)
	})
}

func waitTrackPlaying(t *testing.T, ctrl *Controller, trackID string) {
	t.Helper()
	waitCondition(t, func() bool {
		snap := ctrl.Snapshot()
		return snap.State == StatePlaying && snap.Track.ID == trackID
	}, func() string {
		snap := ctrl.Snapshot()
		return fmt.Sprintf("state=%s track=%q want=%q err=%q", snap.State, snap.Track.ID, trackID, snap.LastError)
	})
}

func waitCondition(t *testing.T, cond func() bool, describe ...func() string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if len(describe) > 0 && describe[0] != nil {
		t.Fatalf("timed out waiting for condition (%s)", describe[0]())
	}
	t.Fatal("timed out waiting for condition")
}

func waitIndex(t *testing.T, ctrl *Controller, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ctrl.Snapshot().Index == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for index %d (got %d)", want, ctrl.Snapshot().Index)
}

func TestControllerRemoveIndicesAndSortQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	ctrl := NewController(resolver, opener, sink)
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{
		Tracks: []model.Track{
			{ID: "t1", Artist: "Zed", Title: "Z"},
			{ID: "t2", Artist: "Alpha", Title: "A"},
			{ID: "t3", Artist: "Mid", Title: "M"},
		},
		Index:    0,
		AutoPlay: true,
	})
	waitState(t, ctrl, StatePlaying)

	// Sorting moves "t1" (Zed - Z) to index 2 without interrupting playback.
	ctrl.Send(SortQueue{})
	waitIndex(t, ctrl, 2)
	if snap := ctrl.Snapshot(); snap.Track.ID != "t1" {
		t.Fatalf("Track after sort = %q, want t1", snap.Track.ID)
	}

	// Removing the currently playing track advances to surviving track and starts it.
	ctrl.Send(RemoveIndices{Indices: []int{2}})
	waitCondition(t, func() bool {
		snap := ctrl.Snapshot()
		return len(snap.Queue) == 2 && snap.State == StatePlaying && snap.Track.ID != "t1"
	})

	// Removing all remaining tracks stops playback.
	ctrl.Send(RemoveIndices{Indices: []int{0, 1}})
	waitState(t, ctrl, StateIdle)
	if snap := ctrl.Snapshot(); len(snap.Queue) != 0 || snap.Index != -1 {
		t.Fatalf("after removing all: len=%d index=%d", len(snap.Queue), snap.Index)
	}

	cancel()
	waitDone(t, ctrl)
}

func waitDone(t *testing.T, ctrl *Controller) {
	t.Helper()
	select {
	case <-ctrl.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("controller did not exit")
	}
}

func waitEvent[T Event](t *testing.T, ch <-chan Event) T {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if matched, ok := ev.(T); ok {
				return matched
			}
		case <-deadline:
			var zero T
			t.Fatalf("timed out waiting for event %T", zero)
			return zero
		}
	}
}

type fakeResolver struct {
	mu        sync.Mutex
	counts    map[string]int
	forgets   int
	gate      chan struct{}
	onResolve func(ctx context.Context, target string) (audio.StreamInfo, error)
}

func newFakeResolver() *fakeResolver {
	return &fakeResolver{counts: make(map[string]int)}
}

func (r *fakeResolver) Resolve(ctx context.Context, target string) (audio.StreamInfo, error) {
	if r.gate != nil {
		select {
		case <-r.gate:
		case <-ctx.Done():
			return audio.StreamInfo{}, ctx.Err()
		}
	}
	r.mu.Lock()
	r.counts[target]++
	fn := r.onResolve
	r.mu.Unlock()

	if fn != nil {
		return fn(ctx, target)
	}
	if err := ctx.Err(); err != nil {
		return audio.StreamInfo{}, err
	}
	return audio.StreamInfo{URL: fmt.Sprintf("https://stream.example/%s", target)}, nil
}

func (r *fakeResolver) Forget(string) {
	r.mu.Lock()
	r.forgets++
	r.mu.Unlock()
}

func (r *fakeResolver) resolveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, n := range r.counts {
		total += n
	}
	return total
}

func (r *fakeResolver) resolvedTarget(target string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[target]
}

func (r *fakeResolver) forgetCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.forgets
}

type fakeOpener struct {
	mu      sync.Mutex
	sources []audio.Source
	offs    []time.Duration
	calls   int
}

func newFakeOpener() *fakeOpener {
	return &fakeOpener{}
}

func (o *fakeOpener) queueSources(srcs ...audio.Source) {
	o.mu.Lock()
	o.sources = append(o.sources, srcs...)
	o.mu.Unlock()
}

func (o *fakeOpener) Open(_ context.Context, _ string, offset time.Duration) (audio.Source, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	o.offs = append(o.offs, offset)
	if len(o.sources) > 0 {
		src := o.sources[0]
		o.sources = o.sources[1:]
		return src, nil
	}
	return &fakeSource{frames: 100}, nil
}

func (o *fakeOpener) offsets() []time.Duration {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]time.Duration(nil), o.offs...)
}

func (o *fakeOpener) callCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls
}

type fakeSink struct {
	autoFinish bool

	mu       sync.Mutex
	pauses   int
	resumes  int
	volume   float64
	pos      time.Duration
	finishCh chan error
}

func newFakeSink() *fakeSink {
	return &fakeSink{volume: 1}
}

func (s *fakeSink) Begin() {
	s.mu.Lock()
	s.pos = 0
	s.mu.Unlock()
}

func (s *fakeSink) Play(ctx context.Context, _ audio.Source) error {
	if s.autoFinish {
		return nil
	}
	ch := make(chan error, 1)
	s.mu.Lock()
	s.finishCh = ch
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-ch:
		return err
	}
}

func (s *fakeSink) finishCurrent(err error) {
	s.mu.Lock()
	ch := s.finishCh
	s.mu.Unlock()
	if ch != nil {
		select {
		case ch <- err:
		default:
		}
	}
}

func (s *fakeSink) Pause() {
	s.mu.Lock()
	s.pauses++
	s.mu.Unlock()
}

func (s *fakeSink) Resume() {
	s.mu.Lock()
	s.resumes++
	s.mu.Unlock()
}

func (s *fakeSink) SetVolume(v float64) {
	s.mu.Lock()
	s.volume = v
	s.mu.Unlock()
}

func (s *fakeSink) Position() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pos
}

func (s *fakeSink) setPosition(d time.Duration) {
	s.mu.Lock()
	s.pos = d
	s.mu.Unlock()
}

func (s *fakeSink) pauseCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pauses
}

func (s *fakeSink) resumeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resumes
}

type fakeSource struct {
	frames   int64
	readErr  error
	closeErr error
	closed   atomic.Bool
}

func (s *fakeSource) Read([]byte) (int, error) { return 0, io.EOF }
func (s *fakeSource) Close() error {
	s.closed.Store(true)
	return s.closeErr
}
func (s *fakeSource) Err() error            { return s.readErr }
func (s *fakeSource) Frames() int64         { return s.frames }
func (s *fakeSource) Done() <-chan struct{} { return nil }

type blockingSource struct {
	closeOnce sync.Once
	closed    chan struct{}
}

func newBlockingSource() *blockingSource {
	return &blockingSource{closed: make(chan struct{})}
}

func (s *blockingSource) Read(p []byte) (int, error) {
	select {
	case <-s.closed:
		return 0, io.EOF
	default:
	}
	if len(p) > 0 {
		time.Sleep(time.Millisecond)
		return len(p), nil
	}
	return 0, nil
}

func (s *blockingSource) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func (s *blockingSource) Err() error            { return nil }
func (s *blockingSource) Frames() int64         { return 10 }
func (s *blockingSource) Done() <-chan struct{} { return s.closed }

type fakeRadio struct {
	mu      sync.Mutex
	calls   []string
	gate    chan struct{}
	results map[string][]model.Track
	err     error
}

func newFakeRadio(results map[string][]model.Track) *fakeRadio {
	return &fakeRadio{results: results}
}

func (r *fakeRadio) Related(ctx context.Context, trackID string) ([]model.Track, error) {
	if r.gate != nil {
		select {
		case <-r.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	r.mu.Lock()
	r.calls = append(r.calls, trackID)
	err := r.err
	tracks := append([]model.Track(nil), r.results[trackID]...)
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return tracks, nil
}

func (r *fakeRadio) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func TestControllerAutoplayQueuesAndPlaysRadioOnQueueEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	radio := newFakeRadio(map[string][]model.Track{
		"seed-01": {
			{ID: "seed-01", Title: "Seed Dup"},
			{ID: "radio-01", Title: "Radio 1"},
			{ID: "radio-01", Title: "Radio 1 Dup"},
			{ID: "radio-02", Title: "Radio 2"},
		},
	})
	ctrl := NewController(resolver, opener, sink, WithRadio(radio), WithAutoplay(true))
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{
		Tracks:   []model.Track{{ID: "seed-01", Title: "Seed"}},
		Index:    0,
		AutoPlay: true,
	})
	waitTrackPlaying(t, ctrl, "seed-01")

	sink.finishCurrent(nil)
	waitTrackPlaying(t, ctrl, "radio-01")

	snap := ctrl.Snapshot()
	if !snap.Autoplay || snap.Index != 1 || len(snap.Queue) != 3 {
		t.Fatalf("snap = autoplay:%v index:%d len:%d, want true, 1, 3", snap.Autoplay, snap.Index, len(snap.Queue))
	}
	if snap.Queue[1].ID != "radio-01" || snap.Queue[2].ID != "radio-02" {
		t.Fatalf("unexpected queue after autoplay: %+v", snap.Queue)
	}

	// Disabling autoplay leaves queue at StateEnded when the last track finishes.
	ctrl.Send(SetAutoplay{Enabled: false})
	waitCondition(t, func() bool { return !ctrl.Snapshot().Autoplay })

	sink.finishCurrent(nil)
	waitTrackPlaying(t, ctrl, "radio-02")
	sink.finishCurrent(nil)
	waitState(t, ctrl, StateEnded)

	cancel()
	waitDone(t, ctrl)
}

func TestControllerAutoplayPrefetchesRadioAndNextURLNearEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	radio := newFakeRadio(map[string][]model.Track{
		"seed-01": {
			{ID: "radio-01", Title: "Radio 1"},
			{ID: "radio-02", Title: "Radio 2"},
		},
	})
	ctrl := NewController(
		resolver,
		opener,
		sink,
		WithRadio(radio),
		WithAutoplay(true),
		WithPrefetchWindow(30*time.Second),
		WithTickInterval(5*time.Millisecond),
	)
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{
		Tracks:   []model.Track{{ID: "seed-01", Duration: 60 * time.Second}},
		Index:    0,
		AutoPlay: true,
	})
	waitTrackPlaying(t, ctrl, "seed-01")

	sink.setPosition(40 * time.Second)
	waitCondition(t, func() bool {
		return radio.callCount() == 1 && resolver.resolvedTarget("radio-01") == 1
	}, func() string {
		return fmt.Sprintf("radio=%d resolved=%d", radio.callCount(), resolver.resolvedTarget("radio-01"))
	})

	// Finishing the track should reuse the prefetched radio queue without a second Related call.
	sink.finishCurrent(nil)
	waitTrackPlaying(t, ctrl, "radio-01")
	if got := radio.callCount(); got != 1 {
		t.Fatalf("radio.callCount() = %d, want 1", got)
	}

	cancel()
	waitDone(t, ctrl)
}

func TestControllerNextAtQueueEndTriggersAutoplay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	radio := newFakeRadio(map[string][]model.Track{
		"seed-01": {{ID: "radio-01", Title: "Radio 1"}},
	})
	ctrl := NewController(resolver, opener, sink, WithRadio(radio), WithAutoplay(true))
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{
		Tracks:   []model.Track{{ID: "seed-01", Title: "Seed"}},
		Index:    0,
		AutoPlay: true,
	})
	waitTrackPlaying(t, ctrl, "seed-01")

	ctrl.Send(Next{})
	waitTrackPlaying(t, ctrl, "radio-01")

	cancel()
	waitDone(t, ctrl)
}

func TestControllerStopCancelsInFlightRadioFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	radio := newFakeRadio(map[string][]model.Track{
		"seed-01": {{ID: "radio-01", Title: "Radio 1"}},
	})
	radio.gate = make(chan struct{})
	ctrl := NewController(resolver, opener, sink, WithRadio(radio), WithAutoplay(true))
	go ctrl.Run(ctx)

	ctrl.Send(SetQueue{
		Tracks:   []model.Track{{ID: "seed-01", Title: "Seed"}},
		Index:    0,
		AutoPlay: true,
	})
	waitTrackPlaying(t, ctrl, "seed-01")

	sink.finishCurrent(nil)
	waitState(t, ctrl, StateEnded)

	// Stop before radio gate opens; releasing the gate must not enqueue or play radio-01.
	ctrl.Send(Stop{})
	waitState(t, ctrl, StateIdle)
	close(radio.gate)
	time.Sleep(25 * time.Millisecond)

	snap := ctrl.Snapshot()
	if snap.State != StateIdle || len(snap.Queue) != 1 {
		t.Fatalf("after Stop during radio fetch: state=%s len=%d, want idle, 1", snap.State, len(snap.Queue))
	}

	cancel()
	waitDone(t, ctrl)
}

func TestControllerRepeatTakesPrecedenceOverAutoplay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := newFakeResolver()
	opener := newFakeOpener()
	sink := newFakeSink()
	radio := newFakeRadio(map[string][]model.Track{
		"seed-01": {{ID: "radio-01", Title: "Radio 1"}},
	})
	ctrl := NewController(resolver, opener, sink, WithRadio(radio), WithAutoplay(true))
	go ctrl.Run(ctx)

	ctrl.Send(SetRepeat{Mode: RepeatOne})
	ctrl.Send(SetQueue{
		Tracks:   []model.Track{{ID: "seed-01", Title: "Seed"}},
		Index:    0,
		AutoPlay: true,
	})
	waitTrackPlaying(t, ctrl, "seed-01")

	sink.finishCurrent(nil)
	waitCondition(t, func() bool {
		return resolver.resolvedTarget("seed-01") == 2 && ctrl.Snapshot().State == StatePlaying
	})
	if got := radio.callCount(); got != 0 {
		t.Fatalf("radio.callCount() with RepeatOne = %d, want 0", got)
	}

	cancel()
	waitDone(t, ctrl)
}
