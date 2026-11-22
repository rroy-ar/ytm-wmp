package mpris

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/rroy-ar/ytm-wmp/internal/model"
	"github.com/rroy-ar/ytm-wmp/internal/playback"
)

type fakeController struct {
	mu   sync.Mutex
	snap playback.Snapshot
	cmds []playback.Command
	subs map[int]chan struct{}
	next int
}

func newFakeController(snap playback.Snapshot) *fakeController {
	return &fakeController{
		snap: snap,
		subs: make(map[int]chan struct{}),
	}
}

func (f *fakeController) Send(cmd playback.Command) bool {
	f.mu.Lock()
	f.cmds = append(f.cmds, cmd)
	f.mu.Unlock()
	return true
}

func (f *fakeController) Snapshot() playback.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap
}

func (f *fakeController) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	f.mu.Lock()
	f.next++
	id := f.next
	f.subs[id] = ch
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		delete(f.subs, id)
		f.mu.Unlock()
	}
}

func (f *fakeController) setSnapshot(snap playback.Snapshot) {
	f.mu.Lock()
	f.snap = snap
	for _, ch := range f.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	f.mu.Unlock()
}

func (f *fakeController) commands() []playback.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]playback.Command, len(f.cmds))
	copy(out, f.cmds)
	return out
}

func (f *fakeController) clearCommands() {
	f.mu.Lock()
	f.cmds = nil
	f.mu.Unlock()
}

func TestTrackObjectPathEscaping(t *testing.T) {
	if got := trackObjectPath(""); got != noTrackPath {
		t.Fatalf("trackObjectPath(\"\") = %q, want %q", got, noTrackPath)
	}
	p := trackObjectPath("aB-1_2")
	if !p.IsValid() {
		t.Fatalf("trackObjectPath returned invalid dbus.ObjectPath: %q", p)
	}
	if !strings.HasPrefix(string(p), trackPathPrefix) {
		t.Fatalf("trackObjectPath = %q, want prefix %q", p, trackPathPrefix)
	}
	if trackObjectPath("a-b") == trackObjectPath("a_b") {
		t.Fatal("trackObjectPath collided for distinct IDs")
	}
}

func TestTrackMetadata(t *testing.T) {
	empty := trackMetadata(playback.Snapshot{})
	if len(empty) != 0 {
		t.Fatalf("empty snapshot metadata = %v, want empty", empty)
	}

	snap := playback.Snapshot{
		Track: model.Track{
			ID:       "0wRYvsfhsR8",
			Title:    "Around the World",
			Artist:   "Daft Punk",
			Album:    "Homework",
			Duration: 3*time.Minute + 20*time.Second,
		},
		Duration: 3*time.Minute + 20*time.Second,
		Queue:    []model.Track{{ID: "0wRYvsfhsR8"}},
		Index:    0,
	}
	meta := trackMetadata(snap)

	if got := meta["mpris:trackid"].Value().(dbus.ObjectPath); got != trackObjectPath("0wRYvsfhsR8") {
		t.Fatalf("mpris:trackid = %q, want %q", got, trackObjectPath("0wRYvsfhsR8"))
	}
	if got := meta["mpris:length"].Value().(int64); got != snap.Duration.Microseconds() {
		t.Fatalf("mpris:length = %d, want %d", got, snap.Duration.Microseconds())
	}
	if got := meta["mpris:artUrl"].Value().(string); got != model.DefaultThumbnailURL("0wRYvsfhsR8") {
		t.Fatalf("mpris:artUrl = %q, want default thumbnail", got)
	}
	if got := meta["xesam:title"].Value().(string); got != "Around the World" {
		t.Fatalf("xesam:title = %q", got)
	}
	artists := meta["xesam:artist"].Value().([]string)
	if len(artists) != 1 || artists[0] != "Daft Punk" {
		t.Fatalf("xesam:artist = %v", artists)
	}
	if got := meta["xesam:album"].Value().(string); got != "Homework" {
		t.Fatalf("xesam:album = %q", got)
	}
	if got := meta["xesam:url"].Value().(string); got != "https://music.youtube.com/watch?v=0wRYvsfhsR8" {
		t.Fatalf("xesam:url = %q", got)
	}
}

func TestPlayerMethods(t *testing.T) {
	tr := model.Track{ID: "0wRYvsfhsR8", Title: "Song", Duration: 2 * time.Minute}
	fc := newFakeController(playback.Snapshot{
		State:    playback.StatePlaying,
		Track:    tr,
		Position: 30 * time.Second,
		Duration: 2 * time.Minute,
		Queue:    []model.Track{tr},
		Index:    0,
	})
	s := &Server{ctrl: fc}
	p := &playerObject{server: s}

	// PlayPause while playing -> Pause
	if err := p.PlayPause(); err != nil {
		t.Fatalf("PlayPause err = %v", err)
	}
	// PlayPause while paused -> Resume
	fc.setSnapshot(playback.Snapshot{State: playback.StatePaused, Track: tr, Queue: []model.Track{tr}})
	_ = p.PlayPause()
	// PlayPause while idle -> Play
	fc.setSnapshot(playback.Snapshot{State: playback.StateIdle, Track: tr, Queue: []model.Track{tr}})
	_ = p.PlayPause()

	_ = p.Play()
	_ = p.Pause()
	_ = p.Stop()
	_ = p.Next()
	_ = p.Previous()

	cmds := fc.commands()
	if len(cmds) != 8 {
		t.Fatalf("got %d commands, want 8: %#v", len(cmds), cmds)
	}
	if _, ok := cmds[0].(playback.Pause); !ok {
		t.Fatalf("cmds[0] = %T, want Pause", cmds[0])
	}
	if _, ok := cmds[1].(playback.Resume); !ok {
		t.Fatalf("cmds[1] = %T, want Resume", cmds[1])
	}
	if _, ok := cmds[2].(playback.Play); !ok {
		t.Fatalf("cmds[2] = %T, want Play", cmds[2])
	}
	if _, ok := cmds[3].(playback.Play); !ok {
		t.Fatalf("cmds[3] = %T, want Play", cmds[3])
	}
	if _, ok := cmds[4].(playback.Pause); !ok {
		t.Fatalf("cmds[4] = %T, want Pause", cmds[4])
	}
	if _, ok := cmds[5].(playback.Stop); !ok {
		t.Fatalf("cmds[5] = %T, want Stop", cmds[5])
	}
	if _, ok := cmds[6].(playback.Next); !ok {
		t.Fatalf("cmds[6] = %T, want Next", cmds[6])
	}
	if _, ok := cmds[7].(playback.Prev); !ok {
		t.Fatalf("cmds[7] = %T, want Prev", cmds[7])
	}
}

func TestPlayerSeekSetPositionAndOpenUri(t *testing.T) {
	tr := model.Track{ID: "0wRYvsfhsR8", Title: "Song", Duration: 60 * time.Second}
	fc := newFakeController(playback.Snapshot{
		State:    playback.StatePlaying,
		Track:    tr,
		Position: 20 * time.Second,
		Duration: 60 * time.Second,
		Queue:    []model.Track{tr},
		Index:    0,
	})
	p := &playerObject{server: &Server{ctrl: fc}}

	// Seek +10s -> 30s
	_ = p.SeekOffset((10 * time.Second).Microseconds())
	// Seek -50s -> clamped to 0
	_ = p.SeekOffset((-50 * time.Second).Microseconds())
	// Seek past duration -> Next
	_ = p.SeekOffset((50 * time.Second).Microseconds())

	// SetPosition with wrong track ID -> ignored
	_ = p.SetPosition(trackObjectPath("otherTrack1"), (15 * time.Second).Microseconds())
	// SetPosition with negative or > duration -> ignored
	_ = p.SetPosition(trackObjectPath(tr.ID), -1)
	_ = p.SetPosition(trackObjectPath(tr.ID), (61 * time.Second).Microseconds())
	// SetPosition valid -> Seek(15s)
	_ = p.SetPosition(trackObjectPath(tr.ID), (15 * time.Second).Microseconds())

	// OpenUri invalid -> error
	if err := p.OpenUri("https://example.com/not-youtube"); err == nil {
		t.Fatal("OpenUri with non-YouTube URL should fail")
	}
	// OpenUri valid -> SetQueue
	if err := p.OpenUri("https://music.youtube.com/watch?v=dQw4w9WgXcQ"); err != nil {
		t.Fatalf("OpenUri valid failed: %v", err)
	}

	cmds := fc.commands()
	if len(cmds) != 5 {
		t.Fatalf("got %d commands, want 5: %#v", len(cmds), cmds)
	}
	if sk, ok := cmds[0].(playback.Seek); !ok || sk.Position != 30*time.Second {
		t.Fatalf("cmds[0] = %#v, want Seek(30s)", cmds[0])
	}
	if sk, ok := cmds[1].(playback.Seek); !ok || sk.Position != 0 {
		t.Fatalf("cmds[1] = %#v, want Seek(0)", cmds[1])
	}
	if _, ok := cmds[2].(playback.Next); !ok {
		t.Fatalf("cmds[2] = %#v, want Next", cmds[2])
	}
	if sk, ok := cmds[3].(playback.Seek); !ok || sk.Position != 15*time.Second {
		t.Fatalf("cmds[3] = %#v, want Seek(15s)", cmds[3])
	}
	if sq, ok := cmds[4].(playback.SetQueue); !ok || len(sq.Tracks) != 1 || sq.Tracks[0].ID != "dQw4w9WgXcQ" || !sq.AutoPlay {
		t.Fatalf("cmds[4] = %#v, want SetQueue(dQw4w9WgXcQ, AutoPlay=true)", cmds[4])
	}
}

func TestPropertiesGetSetAndDiff(t *testing.T) {
	tr1 := model.Track{ID: "0wRYvsfhsR8", Title: "One", Artist: "Artist", Duration: 90 * time.Second}
	tr2 := model.Track{ID: "dQw4w9WgXcQ", Title: "Two", Artist: "Artist2", Duration: 120 * time.Second}
	fc := newFakeController(playback.Snapshot{
		State:    playback.StatePlaying,
		Track:    tr1,
		Position: 12 * time.Second,
		Duration: 90 * time.Second,
		Volume:   0.75,
		Queue:    []model.Track{tr1, tr2},
		Index:    0,
		Shuffle:  false,
		Repeat:   playback.RepeatOff,
	})
	quitCalled := false
	s := &Server{
		ctrl:         fc,
		identity:     "ytm-wmp",
		desktopEntry: "ytm-wmp",
		onQuit:       func() { quitCalled = true },
	}
	props := &propsObject{server: s}

	// Root Quit + CanQuit
	v, err := props.Get(rootInterface, "CanQuit")
	if err != nil || !v.Value().(bool) {
		t.Fatalf("CanQuit = (%v, %v), want true", v, err)
	}
	(&rootObject{server: s}).Quit()
	if !quitCalled {
		t.Fatal("Quit callback was not invoked")
	}

	// Dynamic Position
	posVar, err := props.Get(playerInterface, "Position")
	if err != nil || posVar.Value().(int64) != (12*time.Second).Microseconds() {
		t.Fatalf("Position = (%v, %v), want 12s", posVar, err)
	}

	// Writable properties
	if err := props.Set(playerInterface, "LoopStatus", dbus.MakeVariant("Playlist")); err != nil {
		t.Fatalf("Set LoopStatus err = %v", err)
	}
	if err := props.Set(playerInterface, "Shuffle", dbus.MakeVariant(true)); err != nil {
		t.Fatalf("Set Shuffle err = %v", err)
	}
	if err := props.Set(playerInterface, "Volume", dbus.MakeVariant(1.5)); err != nil {
		t.Fatalf("Set Volume err = %v", err)
	}
	if err := props.Set(playerInterface, "LoopStatus", dbus.MakeVariant("Invalid")); err == nil {
		t.Fatal("expected error for invalid LoopStatus")
	}
	if err := props.Set(playerInterface, "PlaybackStatus", dbus.MakeVariant("Paused")); err == nil {
		t.Fatal("expected read-only error for PlaybackStatus")
	}

	cmds := fc.commands()
	if len(cmds) != 3 {
		t.Fatalf("got %d commands, want 3: %#v", len(cmds), cmds)
	}
	if rep, ok := cmds[0].(playback.SetRepeat); !ok || rep.Mode != playback.RepeatAll {
		t.Fatalf("cmds[0] = %#v, want SetRepeat(RepeatAll)", cmds[0])
	}
	if shuf, ok := cmds[1].(playback.SetShuffle); !ok || !shuf.Enabled {
		t.Fatalf("cmds[1] = %#v, want SetShuffle(true)", cmds[1])
	}
	if vol, ok := cmds[2].(playback.SetVolume); !ok || vol.Volume != 1.0 {
		t.Fatalf("cmds[2] = %#v, want SetVolume(1.0)", cmds[2])
	}

	// Diff: Position change alone must not emit PropertiesChanged
	prev := fc.Snapshot()
	curPosOnly := prev
	curPosOnly.Position = 25 * time.Second
	changed, seeked := diffPlayerProperties(prev, curPosOnly)
	if len(changed) != 0 || seeked {
		t.Fatalf("position-only diff = (%v, %v), want empty/false", changed, seeked)
	}

	// Diff: Seeking emits Seeked
	curSeek := prev
	curSeek.State = playback.StateSeeking
	curSeek.Position = 45 * time.Second
	changed, seeked = diffPlayerProperties(prev, curSeek)
	if !seeked {
		t.Fatal("expected seeked=true when entering StateSeeking")
	}
	if _, hasPos := changed["Position"]; hasPos {
		t.Fatal("Position must never be included in PropertiesChanged")
	}

	// Diff: Track + Volume + Pause
	curTrack := prev
	curTrack.State = playback.StatePaused
	curTrack.Track = tr2
	curTrack.Duration = tr2.Duration
	curTrack.Index = 1
	curTrack.Volume = 0.5
	changed, seeked = diffPlayerProperties(prev, curTrack)
	if seeked {
		t.Fatal("unexpected seeked=true")
	}
	for _, key := range []string{"PlaybackStatus", "Metadata", "Volume", "CanGoNext"} {
		if _, ok := changed[key]; !ok {
			t.Fatalf("expected %q in changed properties: %v", key, changed)
		}
	}
}

func startPrivateDBus(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not found in PATH")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "--session", "--nofork", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("dbus-daemon stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start dbus-daemon: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatalf("dbus-daemon did not print address: %v", scanner.Err())
	}
	return strings.TrimSpace(scanner.Text())
}

func waitSignal(t *testing.T, ch <-chan *dbus.Signal, wantName string) *dbus.Signal {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case sig := <-ch:
			if sig.Name == wantName {
				return sig
			}
		case <-deadline:
			t.Fatalf("timed out waiting for signal %q", wantName)
		}
	}
}

func TestServerLivePrivateDBus(t *testing.T) {
	addr := startPrivateDBus(t)

	serverConn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatalf("connect server bus: %v", err)
	}
	defer serverConn.Close()

	tr := model.Track{ID: "0wRYvsfhsR8", Title: "Around the World", Artist: "Daft Punk", Duration: 100 * time.Second}
	fc := newFakeController(playback.Snapshot{
		State:    playback.StateIdle,
		Track:    tr,
		Duration: tr.Duration,
		Volume:   0.8,
		Queue:    []model.Track{tr},
		Index:    0,
	})

	srv, err := New(fc, WithConn(serverConn))
	if err != nil {
		t.Fatalf("New server: %v", err)
	}
	defer srv.Close()

	if srv.BusName() != DefaultBusName {
		t.Fatalf("BusName() = %q, want %q", srv.BusName(), DefaultBusName)
	}

	// Second instance on same bus falls back to .instance<pid>.
	serverConn2, err := dbus.Connect(addr)
	if err != nil {
		t.Fatalf("connect second bus: %v", err)
	}
	defer serverConn2.Close()
	srv2, err := New(fc, WithConn(serverConn2))
	if err != nil {
		t.Fatalf("New second server: %v", err)
	}
	defer srv2.Close()
	if !strings.HasPrefix(srv2.BusName(), DefaultBusName+".instance") {
		t.Fatalf("second BusName() = %q, want .instance prefix", srv2.BusName())
	}

	// Client connection verifies Properties.Get, Methods, and Signals.
	clientConn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatalf("connect client bus: %v", err)
	}
	defer clientConn.Close()

	sigCh := make(chan *dbus.Signal, 16)
	clientConn.Signal(sigCh)
	if err := clientConn.AddMatchSignal(
		dbus.WithMatchObjectPath(ObjectPath),
		dbus.WithMatchSender(DefaultBusName),
	); err != nil {
		t.Fatalf("AddMatchSignal: %v", err)
	}

	obj := clientConn.Object(DefaultBusName, ObjectPath)

	var ident string
	if err := obj.Call(propertiesInterface+".Get", 0, rootInterface, "Identity").Store(&ident); err != nil {
		t.Fatalf("Get Identity: %v", err)
	}
	if ident != "ytm-wmp" {
		t.Fatalf("Identity = %q, want ytm-wmp", ident)
	}

	if err := obj.Call(playerInterface+".PlayPause", 0).Err; err != nil {
		t.Fatalf("Call PlayPause: %v", err)
	}
	cmds := fc.commands()
	if len(cmds) != 1 {
		t.Fatalf("commands = %#v, want 1", cmds)
	}
	if _, ok := cmds[0].(playback.Play); !ok {
		t.Fatalf("cmds[0] = %T, want Play", cmds[0])
	}

	// Publish Playing -> expect PropertiesChanged.
	fc.setSnapshot(playback.Snapshot{
		State:    playback.StatePlaying,
		Track:    tr,
		Position: 10 * time.Second,
		Duration: tr.Duration,
		Volume:   0.8,
		Queue:    []model.Track{tr},
		Index:    0,
	})
	waitSignal(t, sigCh, propertiesInterface+".PropertiesChanged")

	// Verify mapped Seek method over D-Bus.
	fc.clearCommands()
	if err := obj.Call(playerInterface+".Seek", 0, (5 * time.Second).Microseconds()).Err; err != nil {
		t.Fatalf("Call Seek: %v", err)
	}
	cmds = fc.commands()
	if len(cmds) != 1 {
		t.Fatalf("seek commands = %#v, want 1", cmds)
	}
	if sk, ok := cmds[0].(playback.Seek); !ok || sk.Position != 15*time.Second {
		t.Fatalf("seek cmd = %#v, want Seek(15s)", cmds[0])
	}

	// Publish Seeking -> expect Seeked signal.
	fc.setSnapshot(playback.Snapshot{
		State:    playback.StateSeeking,
		Track:    tr,
		Position: 42 * time.Second,
		Duration: tr.Duration,
		Volume:   0.8,
		Queue:    []model.Track{tr},
		Index:    0,
	})
	sig := waitSignal(t, sigCh, playerInterface+".Seeked")
	if got := sig.Body[0].(int64); got != (42 * time.Second).Microseconds() {
		t.Fatalf("Seeked position = %d, want %d", got, (42 * time.Second).Microseconds())
	}
}
