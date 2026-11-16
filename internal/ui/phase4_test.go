package ui

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"ytm-wmp/internal/m3u"
	"ytm-wmp/internal/model"
	"ytm-wmp/internal/playback"
	"ytm-wmp/internal/skin"
)

func TestMainControlsShuffleRepeatTimeAndClose(t *testing.T) {
	ctrl := newFakeController()
	ctrl.setSnapshot(playback.Snapshot{
		State:    playback.StatePlaying,
		Position: 30 * time.Second,
		Duration: 120 * time.Second,
		Volume:   0.8,
		Repeat:   playback.RepeatOff,
	})
	g := NewGame(context.Background(), nil, ctrl, &fakeAudioLifecycle{})
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatalf("Update = %v", err)
	}

	g.onClick(WidgetMainShuffle)
	g.onClick(WidgetMainRepeat)
	g.onClick(WidgetMainTime)
	if !g.timeRemaining {
		t.Fatal("WidgetMainTime did not toggle remaining time mode")
	}

	cmds := ctrl.sentCommands()
	if len(cmds) != 2 {
		t.Fatalf("cmds = %#v, want 2", cmds)
	}
	if sh, ok := cmds[0].(playback.SetShuffle); !ok || !sh.Enabled {
		t.Fatalf("cmd[0] = %#v, want SetShuffle{true}", cmds[0])
	}
	if rp, ok := cmds[1].(playback.SetRepeat); !ok || rp.Mode != playback.RepeatAll {
		t.Fatalf("cmd[1] = %#v, want SetRepeat{RepeatAll}", cmds[1])
	}

	g.onClick(WidgetMainClose)
	if err := g.Update(); err != ebiten.Termination {
		t.Fatalf("Update after close = %v, want ebiten.Termination", err)
	}
}

func TestMainSlidersVolumeBalanceAndSeek(t *testing.T) {
	ctrl := newFakeController()
	ctrl.setSnapshot(playback.Snapshot{
		State:    playback.StatePlaying,
		Position: 10 * time.Second,
		Duration: 100 * time.Second,
		Volume:   0.5,
	})
	g := NewGame(context.Background(), nil, ctrl, &fakeAudioLifecycle{})
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatalf("Update = %v", err)
	}

	// Simulate volume slider drag to right edge.
	g.dragSlider = WidgetMainVolume
	g.updateMainSliders(MainVolumeBounds.Max.X, true)
	g.updateMainSliders(MainVolumeBounds.Max.X, false)

	// Simulate balance slider drag to center (should snap to 0).
	g.dragSlider = WidgetMainBalance
	g.updateMainSliders(MainBalanceBounds.Min.X+MainBalanceBounds.Dx()/2, true)
	if g.balance != 0 {
		t.Fatalf("balance near center = %v, want 0", g.balance)
	}
	g.updateMainSliders(MainBalanceBounds.Min.X+MainBalanceBounds.Dx()/2, false)

	// Simulate position slider drag to midpoint and release -> sends Seek.
	g.dragSlider = WidgetMainPosition
	midX := MainPositionBounds.Min.X + MainPositionBounds.Dx()/2
	g.updateMainSliders(midX, true)
	if g.seekPreviewFrac <= 0.4 || g.seekPreviewFrac >= 0.6 {
		t.Fatalf("seekPreviewFrac = %v, want ~0.5", g.seekPreviewFrac)
	}
	g.updateMainSliders(midX, false)

	var sawVol, sawSeek bool
	for _, cmd := range ctrl.sentCommands() {
		switch c := cmd.(type) {
		case playback.SetVolume:
			if c.Volume > 0.9 {
				sawVol = true
			}
		case playback.Seek:
			if c.Position > 40*time.Second && c.Position < 60*time.Second {
				sawSeek = true
			}
		}
	}
	if !sawVol || !sawSeek {
		t.Fatalf("missing expected slider commands: vol=%v seek=%v (%#v)", sawVol, sawSeek, ctrl.sentCommands())
	}
}

func TestDockedRackTogglesResizeOnlyOnDiscreteChangesAndDrawsAllModes(t *testing.T) {
	s, err := skin.Open(filepath.Join("..", "..", "assets", "dev-skin"))
	if err != nil {
		t.Fatalf("Open(dev skin) = %v", err)
	}
	var resizes []image.Point
	g := NewGame(
		context.Background(),
		s,
		newFakeController(),
		&fakeAudioLifecycle{},
		WithSkinName("dev-skin"),
		WithWindowResizer(func(w, h int) {
			resizes = append(resizes, image.Pt(w, h))
		}),
	)
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatalf("Update = %v", err)
	}

	if w, h := g.Layout(0, 0); w != 275 || h != 348 {
		t.Fatalf("initial Layout = %dx%d, want 275x348", w, h)
	}
	if len(resizes) != 0 {
		t.Fatalf("unexpected initial resizes: %v", resizes)
	}

	// Open EQ -> 275x464.
	g.onClick(WidgetMainEQ)
	if w, h := g.Layout(0, 0); w != 275 || h != 464 {
		t.Fatalf("Layout with EQ+PL = %dx%d, want 275x464", w, h)
	}
	// Draw playlist + EQ + debug overlay.
	screen := ebiten.NewImage(275, 464)
	g.Draw(screen)
	g.toggleDebug()
	g.Draw(screen)

	// Switch to Browser mode while EQ is open and draw.
	g.onClick(WidgetPlaylistModeSearch)
	if !g.browserMode {
		t.Fatal("WidgetPlaylistModeSearch should switch to browserMode")
	}
	g.Draw(screen)

	// Hide PL -> 275x232.
	g.onClick(WidgetMainPL)
	if w, h := g.Layout(0, 0); w != 275 || h != 232 {
		t.Fatalf("Layout with EQ only = %dx%d, want 275x232", w, h)
	}
	g.Draw(ebiten.NewImage(275, 232))

	// Close EQ -> 275x116.
	g.onClick(WidgetEQClose)
	if w, h := g.Layout(0, 0); w != 275 || h != 116 {
		t.Fatalf("Layout main only = %dx%d, want 275x116", w, h)
	}
	g.Draw(ebiten.NewImage(275, 116))

	wantResizes := []image.Point{{275, 464}, {275, 232}, {275, 116}}
	if len(resizes) != len(wantResizes) {
		t.Fatalf("resizes = %v, want %v", resizes, wantResizes)
	}
	for i, want := range wantResizes {
		if resizes[i] != want {
			t.Fatalf("resize[%d] = %v, want %v", i, resizes[i], want)
		}
	}
}

func TestPlaylistSelectionDoubleClickMenusAndM3U(t *testing.T) {
	dir := t.TempDir()
	m3uPath := filepath.Join(dir, "playlist.m3u")

	ctrl := newFakeController()
	ctrl.setSnapshot(playback.Snapshot{
		State: playback.StateIdle,
		Queue: []model.Track{
			{ID: "0wRYvsfhsR8", Artist: "Daft Punk", Title: "Something About Us", Duration: 232 * time.Second},
			{ID: "oz3LxqoyQFQ", Artist: "Artist B", Title: "Track B", Duration: 120 * time.Second},
			{ID: "dQw4w9WgXcQ", Artist: "Artist C", Title: "Track C", Duration: 180 * time.Second},
		},
		Index: 0,
	})

	clipData := "#EXTM3U\nhttps://youtu.be/abcDEF12345\n"
	g := NewGame(
		context.Background(),
		nil,
		ctrl,
		&fakeAudioLifecycle{},
		WithPlaylistPath(m3uPath),
		WithClipboard(func(context.Context) (string, error) { return clipData, nil }),
	)
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatalf("Update = %v", err)
	}

	now := time.Now()
	// Single click row 1 selects without playing.
	g.onPlaylistRowClickAt(1, now, false, false)
	if !g.plSelected[1] || len(g.plSelected) != 1 {
		t.Fatalf("plSelected = %v, want {1}", g.plSelected)
	}
	if len(ctrl.sentCommands()) != 0 {
		t.Fatalf("single click sent commands: %#v", ctrl.sentCommands())
	}

	// Double click row 1 plays it.
	g.onPlaylistRowClickAt(1, now.Add(100*time.Millisecond), false, false)
	cmds := ctrl.sentCommands()
	if len(cmds) != 1 {
		t.Fatalf("double click cmds = %#v, want 1", cmds)
	}
	if pi, ok := cmds[0].(playback.PlayIndex); !ok || pi.Index != 1 {
		t.Fatalf("cmd = %#v, want PlayIndex{1}", cmds[0])
	}

	// Shift+click row 2 selects range [1..2].
	g.onPlaylistRowClickAt(2, now.Add(time.Second), false, true)
	if !g.plSelected[1] || !g.plSelected[2] || g.plSelected[0] {
		t.Fatalf("shift selection = %v, want {1, 2}", g.plSelected)
	}

	// Ctrl+click row 0 toggles row 0 on.
	g.onPlaylistRowClickAt(0, now.Add(2*time.Second), true, false)
	if !g.plSelected[0] || !g.plSelected[1] || !g.plSelected[2] {
		t.Fatalf("ctrl selection = %v, want {0, 1, 2}", g.plSelected)
	}

	// Save M3U via LIST -> SAVE menu.
	g.onClick(WidgetPlaylistList)
	if g.plMenu != plMenuList {
		t.Fatalf("plMenu = %v, want plMenuList", g.plMenu)
	}
	g.onClick(WidgetPLMenuItem1) // list.save
	rawM3U, err := os.ReadFile(m3uPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) = %v", m3uPath, err)
	}
	if !strings.Contains(string(rawM3U), "0wRYvsfhsR8") {
		t.Fatalf("saved M3U missing track:\n%s", string(rawM3U))
	}

	// Load M3U via LIST -> LOAD menu.
	g.onClick(WidgetPlaylistList)
	g.onClick(WidgetPLMenuItem0) // list.load
	cmds = ctrl.sentCommands()
	lastSet, ok := cmds[len(cmds)-1].(playback.SetQueue)
	if !ok || len(lastSet.Tracks) != 3 {
		t.Fatalf("load M3U last cmd = %#v, want SetQueue(3 tracks)", cmds[len(cmds)-1])
	}

	// Import M3U from clipboard via ADD -> Paste M3U.
	g.onClick(WidgetPlaylistAdd)
	g.onClick(WidgetPLMenuItem0) // add.paste_m3u
	cmds = ctrl.sentCommands()
	lastEnq, ok := cmds[len(cmds)-1].(playback.Enqueue)
	if !ok || len(lastEnq.Tracks) != 1 || lastEnq.Tracks[0].ID != "abcDEF12345" {
		t.Fatalf("clipboard M3U enqueue cmd = %#v", cmds[len(cmds)-1])
	}
	_ = m3u.Format(nil)
}

func TestEQControlsAndPersistence(t *testing.T) {
	eqPath := filepath.Join(t.TempDir(), "eq.json")

	g1 := NewGame(context.Background(), nil, newFakeController(), &fakeAudioLifecycle{}, WithEQPath(eqPath))
	g1.onClick(WidgetEQOn)
	g1.onClick(WidgetEQAuto)
	g1.onClick(WidgetEQPresets)
	g1.onClick(WidgetEQPlus12)
	for i, val := range g1.eqBands {
		if val != 100 {
			t.Fatalf("band[%d] after +12dB = %v, want 100", i, val)
		}
	}
	g1.onClick(WidgetEQZero)
	g1.eqPreamp = 77
	g1.eqBands[3] = 88
	g1.markEQDirty()
	g1.Close()

	g2 := NewGame(context.Background(), nil, newFakeController(), &fakeAudioLifecycle{}, WithEQPath(eqPath))
	defer g2.Close()
	if !g2.eqOn || !g2.eqAuto || g2.eqPreamp != 77 || g2.eqBands[3] != 88 {
		t.Fatalf("reloaded EQ state = on=%v auto=%v preamp=%v band3=%v", g2.eqOn, g2.eqAuto, g2.eqPreamp, g2.eqBands[3])
	}
}
