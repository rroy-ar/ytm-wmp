// Package playback runs the headless playback actor, queue, state machine,
// and error classification. The UI interacts exclusively via Commands, Events,
// and atomic Snapshots.
package playback

import (
	"fmt"
	"time"

	"ytm-wmp/internal/model"
)

// State represents the playback state machine.
type State int

const (
	StateIdle State = iota
	StateResolving
	StatePlaying
	StatePaused
	StateSeeking
	StateEnded
	StateError
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateResolving:
		return "resolving"
	case StatePlaying:
		return "playing"
	case StatePaused:
		return "paused"
	case StateSeeking:
		return "seeking"
	case StateEnded:
		return "ended"
	case StateError:
		return "error"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}

// RepeatMode controls end-of-track and end-of-queue wrapping.
type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatAll
	RepeatOne
)

func (r RepeatMode) String() string {
	switch r {
	case RepeatOff:
		return "off"
	case RepeatAll:
		return "all"
	case RepeatOne:
		return "one"
	default:
		return fmt.Sprintf("RepeatMode(%d)", int(r))
	}
}

// transitionTable encodes every legal state transition in the playback actor.
// Self-transitions (from == to) are valid when restarting playback (e.g.
// Resolving -> Resolving on Next, or Seeking -> Seeking on a superseding Seek).
var transitionTable = map[State]map[State]bool{
	StateIdle: {
		StateIdle:      true,
		StateResolving: true,
	},
	StateResolving: {
		StateResolving: true,
		StatePlaying:   true,
		StatePaused:    true,
		StateIdle:      true,
		StateError:     true,
	},
	StatePlaying: {
		StatePlaying:   true,
		StatePaused:    true,
		StateSeeking:   true,
		StateResolving: true,
		StateIdle:      true,
		StateEnded:     true,
		StateError:     true,
	},
	StatePaused: {
		StatePaused:    true,
		StatePlaying:   true,
		StateSeeking:   true,
		StateResolving: true,
		StateIdle:      true,
		StateEnded:     true,
		StateError:     true,
	},
	StateSeeking: {
		StateSeeking:   true,
		StatePlaying:   true,
		StatePaused:    true,
		StateResolving: true,
		StateIdle:      true,
		StateError:     true,
	},
	StateEnded: {
		StateEnded:     true,
		StateResolving: true,
		StateIdle:      true,
	},
	StateError: {
		StateError:     true,
		StateResolving: true,
		StateIdle:      true,
	},
}

func canTransition(from, to State) bool {
	return transitionTable[from][to]
}

// Snapshot is an immutable point-in-time view of playback state for the UI.
// Queue is shared across snapshots until the queue changes; callers must treat
// Queue as read-only and never mutate its elements.
type Snapshot struct {
	State     State
	Track     model.Track
	Position  time.Duration
	Duration  time.Duration
	Volume    float64
	Queue     []model.Track
	Index     int
	Shuffle   bool
	Repeat    RepeatMode
	LastError string
}

// Command is a sealed input message handled by the playback actor.
type Command interface {
	isCommand()
}

type Play struct{}
type PlayIndex struct{ Index int }
type Pause struct{}
type Resume struct{}
type Stop struct{}
type Next struct{}
type Prev struct{}
type Seek struct{ Position time.Duration }
type SetVolume struct{ Volume float64 }
type SetQueue struct {
	Tracks   []model.Track
	Index    int
	AutoPlay bool
}
type Enqueue struct{ Tracks []model.Track }
type UpdateTrack struct{ Track model.Track }
type SetShuffle struct{ Enabled bool }
type SetRepeat struct{ Mode RepeatMode }

func (Play) isCommand()        {}
func (PlayIndex) isCommand()   {}
func (Pause) isCommand()       {}
func (Resume) isCommand()      {}
func (Stop) isCommand()        {}
func (Next) isCommand()        {}
func (Prev) isCommand()        {}
func (Seek) isCommand()        {}
func (SetVolume) isCommand()   {}
func (SetQueue) isCommand()    {}
func (Enqueue) isCommand()     {}
func (UpdateTrack) isCommand() {}
func (SetShuffle) isCommand()  {}
func (SetRepeat) isCommand()   {}

// Event is a sealed output notification emitted by the playback actor.
type Event interface {
	isEvent()
}

type StateChanged struct {
	From State
	To   State
}

type TrackChanged struct {
	Index int
	Track model.Track
}

type QueueChanged struct {
	Len   int
	Index int
}

type VolumeChanged struct {
	Volume float64
}

type PlaybackError struct {
	Err     error
	Message string
	Track   model.Track
}

func (StateChanged) isEvent()  {}
func (TrackChanged) isEvent()  {}
func (QueueChanged) isEvent()  {}
func (VolumeChanged) isEvent() {}
func (PlaybackError) isEvent() {}
