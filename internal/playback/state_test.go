package playback

import "testing"

func TestStateTransitionTable(t *testing.T) {
	allStates := []State{
		StateIdle,
		StateResolving,
		StatePlaying,
		StatePaused,
		StateSeeking,
		StateEnded,
		StateError,
	}

	legal := map[[2]State]bool{
		{StateIdle, StateIdle}:      true,
		{StateIdle, StateResolving}: true,

		{StateResolving, StateResolving}: true,
		{StateResolving, StatePlaying}:   true,
		{StateResolving, StatePaused}:    true,
		{StateResolving, StateIdle}:      true,
		{StateResolving, StateError}:     true,

		{StatePlaying, StatePlaying}:   true,
		{StatePlaying, StatePaused}:    true,
		{StatePlaying, StateSeeking}:   true,
		{StatePlaying, StateResolving}: true,
		{StatePlaying, StateIdle}:      true,
		{StatePlaying, StateEnded}:     true,
		{StatePlaying, StateError}:     true,

		{StatePaused, StatePaused}:    true,
		{StatePaused, StatePlaying}:   true,
		{StatePaused, StateSeeking}:   true,
		{StatePaused, StateResolving}: true,
		{StatePaused, StateIdle}:      true,
		{StatePaused, StateEnded}:     true,
		{StatePaused, StateError}:     true,

		{StateSeeking, StateSeeking}:   true,
		{StateSeeking, StatePlaying}:   true,
		{StateSeeking, StatePaused}:    true,
		{StateSeeking, StateResolving}: true,
		{StateSeeking, StateIdle}:      true,
		{StateSeeking, StateError}:     true,

		{StateEnded, StateEnded}:     true,
		{StateEnded, StateResolving}: true,
		{StateEnded, StateIdle}:      true,

		{StateError, StateError}:     true,
		{StateError, StateResolving}: true,
		{StateError, StateIdle}:      true,
	}

	for _, from := range allStates {
		for _, to := range allStates {
			got := canTransition(from, to)
			want := legal[[2]State{from, to}]
			if got != want {
				t.Errorf("canTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestStateAndRepeatStrings(t *testing.T) {
	if got := StatePlaying.String(); got != "playing" {
		t.Fatalf("StatePlaying.String() = %q, want %q", got, "playing")
	}
	if got := State(99).String(); got != "State(99)" {
		t.Fatalf("unknown State.String() = %q", got)
	}
	if got := RepeatAll.String(); got != "all" {
		t.Fatalf("RepeatAll.String() = %q, want %q", got, "all")
	}
	if got := RepeatMode(99).String(); got != "RepeatMode(99)" {
		t.Fatalf("unknown RepeatMode.String() = %q", got)
	}
}
