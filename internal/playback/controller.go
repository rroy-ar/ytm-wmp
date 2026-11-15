package playback

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"ytm-wmp/internal/audio"
	"ytm-wmp/internal/model"
)

const (
	defaultPrefetchWindow = 30 * time.Second
	defaultTickInterval   = 500 * time.Millisecond
)

// Resolver resolves a track target (video ID, URL, or query) to a stream URL.
type Resolver interface {
	Resolve(ctx context.Context, target string) (audio.StreamInfo, error)
	Forget(target string)
}

// Opener starts an audio source for a resolved stream URL at an offset.
type Opener interface {
	Open(ctx context.Context, streamURL string, offset time.Duration) (audio.Source, error)
}

// Sink consumes an audio source and exposes pause/resume/volume/position.
type Sink interface {
	Play(ctx context.Context, src audio.Source) error
	Begin()
	Pause()
	Resume()
	SetVolume(volume float64)
	Position() time.Duration
}

type snapshotState struct {
	snap       Snapshot
	baseOffset time.Duration
}

// Option configures a Controller.
type Option func(*Controller)

// WithIntN overrides the random source used by the shuffle bag (for tests).
func WithIntN(fn func(int) int) Option {
	return func(c *Controller) {
		if fn != nil {
			c.intn = fn
		}
	}
}

// WithPrefetchWindow sets how close to the end of a track prefetching starts.
func WithPrefetchWindow(d time.Duration) Option {
	return func(c *Controller) {
		if d > 0 {
			c.prefetchWindow = d
		}
	}
}

// WithTickInterval sets the actor's periodic check interval while playing.
func WithTickInterval(d time.Duration) Option {
	return func(c *Controller) {
		if d > 0 {
			c.tickInterval = d
		}
	}
}

// Controller owns the playback actor goroutine, queue, and atomic Snapshot.
type Controller struct {
	resolver Resolver
	opener   Opener
	sink     Sink

	intn           func(int) int
	prefetchWindow time.Duration
	tickInterval   time.Duration

	commands chan Command
	events   chan Event
	workerCh chan workerMsg
	done     chan struct{}

	snap atomic.Pointer[snapshotState]
}

// NewController creates a playback controller wired to resolver, opener, and sink.
func NewController(resolver Resolver, opener Opener, sink Sink, opts ...Option) *Controller {
	c := &Controller{
		resolver:       resolver,
		opener:         opener,
		sink:           sink,
		prefetchWindow: defaultPrefetchWindow,
		tickInterval:   defaultTickInterval,
		commands:       make(chan Command, 32),
		events:         make(chan Event, 64),
		workerCh:       make(chan workerMsg, 8),
		done:           make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}
	c.snap.Store(&snapshotState{
		snap: Snapshot{
			State:  StateIdle,
			Index:  -1,
			Volume: 1,
		},
	})
	return c
}

// Commands returns the channel for sending playback commands to the actor.
func (c *Controller) Commands() chan<- Command {
	return c.commands
}

// Send submits cmd without blocking after the actor has stopped. It returns
// false if the actor is already done.
func (c *Controller) Send(cmd Command) bool {
	select {
	case <-c.done:
		return false
	case c.commands <- cmd:
		return true
	}
}

// Events returns the stream of playback events emitted by the actor.
func (c *Controller) Events() <-chan Event {
	return c.events
}

// Snapshot returns a race-free copy of the current playback state.
func (c *Controller) Snapshot() Snapshot {
	cur := c.snap.Load()
	if cur == nil {
		return Snapshot{State: StateIdle, Index: -1, Volume: 1}
	}
	out := cur.snap
	if len(cur.snap.Queue) > 0 {
		out.Queue = append([]model.Track(nil), cur.snap.Queue...)
	}
	switch out.State {
	case StatePlaying, StatePaused:
		pos := cur.baseOffset + c.sink.Position()
		if out.Duration > 0 && pos > out.Duration {
			pos = out.Duration
		}
		out.Position = pos
	case StateSeeking:
		out.Position = cur.baseOffset
	}
	return out
}

// Done is closed when Run has stopped all worker goroutines and exited.
func (c *Controller) Done() <-chan struct{} {
	return c.done
}

// Run executes the playback actor loop until ctx is canceled.
func (c *Controller) Run(ctx context.Context) {
	defer close(c.done)

	a := &actor{
		ctrl:   c,
		ctx:    ctx,
		q:      newQueue(c.intn),
		state:  StateIdle,
		volume: 1,
	}
	a.publish()

	ticker := time.NewTicker(c.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.shutdown()
			return
		case cmd := <-c.commands:
			a.handleCommand(cmd)
		case msg := <-c.workerCh:
			a.handleWorker(msg)
		case <-ticker.C:
			a.maybePrefetch()
		}
	}
}

type workerMsg interface {
	isWorkerMsg()
}

type workerStarted struct {
	gen       uint64
	startedAt time.Time
}

type workerEnded struct {
	gen uint64
	eof bool
	err error
}

func (workerStarted) isWorkerMsg() {}
func (workerEnded) isWorkerMsg()   {}

type actor struct {
	ctrl *Controller
	ctx  context.Context
	q    *queue

	state          State
	baseOffset     time.Duration
	volume         float64
	lastError      string
	pauseRequested bool

	gen          uint64
	activeCancel context.CancelFunc
	activeDone   chan struct{}

	prefetched     bool
	prefetchCancel context.CancelFunc
	prefetchDone   chan struct{}
}

func (a *actor) handleCommand(cmd Command) {
	switch c := cmd.(type) {
	case Play:
		a.cmdPlay()
	case PlayIndex:
		a.cmdPlayIndex(c.Index)
	case Pause:
		a.cmdPause()
	case Resume:
		a.cmdResume()
	case Stop:
		a.cmdStop()
	case Next:
		a.cmdNext()
	case Prev:
		a.cmdPrev()
	case Seek:
		a.cmdSeek(c.Position)
	case SetVolume:
		a.cmdSetVolume(c.Volume)
	case SetQueue:
		a.cmdSetQueue(c)
	case Enqueue:
		a.cmdEnqueue(c.Tracks)
	case SetShuffle:
		a.q.SetShuffle(c.Enabled)
		a.publish()
	case SetRepeat:
		a.q.SetRepeat(c.Mode)
		a.publish()
	}
}

func (a *actor) cmdPlay() {
	if a.state == StatePaused {
		a.cmdResume()
		return
	}
	if a.state == StateResolving || a.state == StateSeeking {
		if a.pauseRequested {
			a.pauseRequested = false
			a.ctrl.sink.Resume()
		}
		return
	}
	if a.state == StatePlaying {
		return
	}
	track, ok := a.q.Current()
	if !ok {
		return
	}
	a.startTrack(track, 0, StateResolving)
}

func (a *actor) cmdPlayIndex(index int) {
	if !a.q.Select(index) {
		return
	}
	track, _ := a.q.Current()
	a.emit(TrackChanged{Index: a.q.Index(), Track: track})
	a.startTrack(track, 0, StateResolving)
}

func (a *actor) cmdPause() {
	switch a.state {
	case StatePlaying:
		a.ctrl.sink.Pause()
		a.transition(StatePaused)
		a.publish()
	case StateResolving, StateSeeking:
		a.pauseRequested = true
		a.ctrl.sink.Pause()
	}
}

func (a *actor) cmdResume() {
	switch a.state {
	case StatePaused:
		a.ctrl.sink.Resume()
		a.transition(StatePlaying)
		a.publish()
	case StateResolving, StateSeeking:
		if a.pauseRequested {
			a.pauseRequested = false
			a.ctrl.sink.Resume()
		}
	}
}

func (a *actor) cmdStop() {
	a.gen++
	a.pauseRequested = false
	a.baseOffset = 0
	a.lastError = ""
	a.transition(StateIdle)
	a.publish()
	a.stopActive()
}

func (a *actor) cmdNext() {
	if !a.q.Next() {
		slog.Debug("next ignored", "index", a.q.Index(), "len", a.q.Len(), "repeat", a.q.Repeat().String(), "shuffle", a.q.Shuffle())
		return
	}
	track, _ := a.q.Current()
	slog.Debug("next track", "index", a.q.Index(), "len", a.q.Len(), "track_id", track.ID)
	a.emit(TrackChanged{Index: a.q.Index(), Track: track})
	a.startTrack(track, 0, StateResolving)
}

func (a *actor) cmdPrev() {
	if a.q.Prev() {
		track, _ := a.q.Current()
		slog.Debug("prev track", "index", a.q.Index(), "len", a.q.Len(), "track_id", track.ID)
		a.emit(TrackChanged{Index: a.q.Index(), Track: track})
		a.startTrack(track, 0, StateResolving)
		return
	}
	if a.state == StatePlaying || a.state == StatePaused || a.state == StateSeeking {
		if track, ok := a.q.Current(); ok {
			slog.Debug("prev restarting current track", "index", a.q.Index(), "track_id", track.ID)
			a.startTrack(track, 0, StateResolving)
		}
	}
}

func (a *actor) cmdSeek(pos time.Duration) {
	if a.state != StatePlaying && a.state != StatePaused && a.state != StateSeeking {
		return
	}
	track, ok := a.q.Current()
	if !ok {
		return
	}
	if pos < 0 {
		pos = 0
	}
	if track.Duration > 0 && pos > track.Duration {
		pos = track.Duration
	}
	a.startTrack(track, pos, StateSeeking)
}

func (a *actor) cmdSetVolume(v float64) {
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	a.volume = v
	a.ctrl.sink.SetVolume(v)
	a.publish()
	a.emit(VolumeChanged{Volume: v})
}

func (a *actor) cmdSetQueue(c SetQueue) {
	a.q.Set(c.Tracks, c.Index)
	a.emit(QueueChanged{Len: a.q.Len(), Index: a.q.Index()})

	track, ok := a.q.Current()
	if !ok {
		a.cmdStop()
		return
	}
	a.emit(TrackChanged{Index: a.q.Index(), Track: track})
	if c.AutoPlay {
		a.startTrack(track, 0, StateResolving)
		return
	}
	a.gen++
	a.pauseRequested = false
	a.baseOffset = 0
	a.lastError = ""
	a.transition(StateIdle)
	a.publish()
	a.stopActive()
}

func (a *actor) cmdEnqueue(tracks []model.Track) {
	if len(tracks) == 0 {
		return
	}
	wasEmpty := a.q.Len() == 0
	a.q.Enqueue(tracks)
	a.emit(QueueChanged{Len: a.q.Len(), Index: a.q.Index()})
	if wasEmpty {
		if track, ok := a.q.Current(); ok {
			a.emit(TrackChanged{Index: a.q.Index(), Track: track})
		}
	}
	a.publish()
}

func (a *actor) startTrack(track model.Track, offset time.Duration, targetState State) {
	a.gen++
	gen := a.gen

	a.pauseRequested = false
	a.lastError = ""
	a.baseOffset = offset
	a.prefetched = false
	a.transition(targetState)
	a.publish()

	a.stopActive()
	a.stopPrefetch()

	a.ctrl.sink.Begin()
	a.ctrl.sink.SetVolume(a.volume)

	playCtx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})
	a.activeCancel = cancel
	a.activeDone = done

	go a.runWorker(playCtx, done, gen, track, offset)
}

func (a *actor) runWorker(ctx context.Context, done chan struct{}, gen uint64, track model.Track, offset time.Duration) {
	defer close(done)

	const maxAttempts = 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resolveStart := time.Now()
		info, err := a.ctrl.resolver.Resolve(ctx, track.ID)
		slog.Debug("resolve finished",
			"track_id", track.ID,
			"attempt", attempt,
			"resolve_ms", time.Since(resolveStart).Milliseconds(),
			"err", err,
		)
		if err != nil {
			a.sendWorkerMsg(ctx, workerEnded{gen: gen, err: err})
			return
		}

		openStart := time.Now()
		src, err := a.ctrl.opener.Open(ctx, info.URL, offset)
		if err != nil {
			a.sendWorkerMsg(ctx, workerEnded{gen: gen, err: err})
			return
		}

		if !a.sendWorkerMsg(ctx, workerStarted{gen: gen, startedAt: openStart}) {
			_ = src.Close()
			return
		}

		playErr := a.ctrl.sink.Play(ctx, src)
		closeErr := src.Close()

		if ctxErr := ctx.Err(); ctxErr != nil {
			a.sendWorkerMsg(ctx, workerEnded{gen: gen, err: ctxErr})
			return
		}
		if playErr != nil {
			a.sendWorkerMsg(ctx, workerEnded{gen: gen, err: playErr})
			return
		}

		endErr := audio.EndError(src, closeErr)
		if endErr == nil {
			a.sendWorkerMsg(ctx, workerEnded{gen: gen, eof: true})
			return
		}
		if attempt == maxAttempts || !errors.Is(endErr, audio.ErrNoAudio) {
			a.sendWorkerMsg(ctx, workerEnded{gen: gen, err: endErr})
			return
		}

		slog.Debug("stale stream detected; forgetting cached URL", "track_id", track.ID)
		a.ctrl.resolver.Forget(track.ID)
	}
}

func (a *actor) sendWorkerMsg(ctx context.Context, msg workerMsg) bool {
	select {
	case <-ctx.Done():
		return false
	case a.ctrl.workerCh <- msg:
		return true
	}
}

func (a *actor) handleWorker(msg workerMsg) {
	switch m := msg.(type) {
	case workerStarted:
		if m.gen != a.gen {
			return
		}
		slog.Debug("playback pipeline started", "play_ms", time.Since(m.startedAt).Milliseconds(), "paused", a.pauseRequested)
		nextState := StatePlaying
		if a.pauseRequested {
			nextState = StatePaused
		}
		a.pauseRequested = false
		a.lastError = ""
		a.transition(nextState)
		a.publish()

	case workerEnded:
		if m.gen != a.gen {
			return
		}
		a.waitActiveDone()

		if m.eof {
			a.baseOffset = 0
			a.transition(StateEnded)
			a.publish()

			prevIdx := a.q.Index()
			if a.q.AutoAdvance() {
				track, _ := a.q.Current()
				if a.q.Index() != prevIdx {
					a.emit(TrackChanged{Index: a.q.Index(), Track: track})
				}
				a.startTrack(track, 0, StateResolving)
			}
			return
		}

		pe := ClassifyError(m.err)
		if errors.Is(pe, ErrCanceled) {
			a.baseOffset = 0
			a.transition(StateIdle)
			a.publish()
			return
		}

		a.lastError = pe.UserMessage
		a.transition(StateError)
		a.publish()
		track, _ := a.q.Current()
		a.emit(PlaybackError{Err: pe, Message: pe.UserMessage, Track: track})
	}
}

func (a *actor) maybePrefetch() {
	if a.state != StatePlaying || a.prefetched {
		return
	}
	cur, ok := a.q.Current()
	if !ok || cur.Duration <= 0 {
		return
	}
	pos := a.baseOffset + a.ctrl.sink.Position()
	if cur.Duration-pos > a.ctrl.prefetchWindow {
		return
	}

	nextTrack, ok := a.q.PeekNext()
	if !ok || nextTrack.ID == "" {
		return
	}

	a.prefetched = true
	a.stopPrefetch()

	pCtx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})
	a.prefetchCancel = cancel
	a.prefetchDone = done

	go func() {
		defer close(done)
		_, err := a.ctrl.resolver.Resolve(pCtx, nextTrack.ID)
		slog.Debug("prefetched next track URL", "track_id", nextTrack.ID, "err", err)
	}()
}

func (a *actor) stopActive() {
	if a.activeCancel != nil {
		a.activeCancel()
	}
	a.waitActiveDone()
}

func (a *actor) waitActiveDone() {
	if a.activeDone != nil {
		<-a.activeDone
		a.activeDone = nil
	}
	a.activeCancel = nil
}

func (a *actor) stopPrefetch() {
	if a.prefetchCancel != nil {
		a.prefetchCancel()
	}
	if a.prefetchDone != nil {
		<-a.prefetchDone
		a.prefetchDone = nil
	}
	a.prefetchCancel = nil
}

func (a *actor) shutdown() {
	a.gen++
	a.stopActive()
	a.stopPrefetch()
}

func (a *actor) transition(to State) {
	from := a.state
	if !canTransition(from, to) {
		slog.Warn("unexpected state transition", "from", from, "to", to)
	}
	a.state = to
	if from != to {
		a.emit(StateChanged{From: from, To: to})
	}
}

func (a *actor) publish() {
	track, _ := a.q.Current()
	snap := Snapshot{
		State:     a.state,
		Track:     track,
		Duration:  track.Duration,
		Volume:    a.volume,
		Queue:     a.q.TracksCopy(),
		Index:     a.q.Index(),
		Shuffle:   a.q.Shuffle(),
		Repeat:    a.q.Repeat(),
		LastError: a.lastError,
	}
	a.ctrl.snap.Store(&snapshotState{
		snap:       snap,
		baseOffset: a.baseOffset,
	})
}

func (a *actor) emit(ev Event) {
	select {
	case a.ctrl.events <- ev:
	default:
	}
}
