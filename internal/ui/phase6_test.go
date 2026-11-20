package ui

import (
	"context"
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/rroy-ar/ytm-wmp/internal/audio"
	"github.com/rroy-ar/ytm-wmp/internal/model"
	"github.com/rroy-ar/ytm-wmp/internal/playback"
	"github.com/rroy-ar/ytm-wmp/internal/skin"
	"github.com/rroy-ar/ytm-wmp/internal/store"
)

type fakeEQVisSink struct {
	fakeAudioLifecycle
	eqCalls []audio.EQSettings
	samples []float32
}

func (s *fakeEQVisSink) SetEQ(settings audio.EQSettings) {
	s.eqCalls = append(s.eqCalls, settings)
}

func (s *fakeEQVisSink) ReadVisSamples(dst []float32) int {
	n := len(s.samples)
	if n > len(dst) {
		n = len(dst)
	}
	copy(dst, s.samples[:n])
	return n
}

func TestPhase6AEQLiveWiring(t *testing.T) {
	sink := &fakeEQVisSink{}
	eqPath := filepath.Join(t.TempDir(), "eq.json")
	g := NewGame(context.Background(), nil, newFakeController(), sink, WithEQPath(eqPath))
	defer g.Close()

	if len(sink.eqCalls) != 1 {
		t.Fatalf("initial SetEQ calls = %d, want 1", len(sink.eqCalls))
	}
	if sink.eqCalls[0].Enabled {
		t.Fatalf("expected initial EQ disabled")
	}

	g.onClick(WidgetEQOn)
	if len(sink.eqCalls) != 2 || !sink.eqCalls[1].Enabled {
		t.Fatalf("expected SetEQ(Enabled=true) after WidgetEQOn, got %+v", sink.eqCalls)
	}

	g.onClick(WidgetEQPlus12)
	last := sink.eqCalls[len(sink.eqCalls)-1]
	for i, db := range last.BandsDB {
		if math.Abs(db-12.0) > 1e-6 {
			t.Fatalf("band %d after +12 = %v, want 12", i, db)
		}
	}

	g.onClick(WidgetEQMinus12)
	last = sink.eqCalls[len(sink.eqCalls)-1]
	for i, db := range last.BandsDB {
		if math.Abs(db+12.0) > 1e-6 {
			t.Fatalf("band %d after -12 = %v, want -12", i, db)
		}
	}

	g.onClick(WidgetEQPresets)
	last = sink.eqCalls[len(sink.eqCalls)-1]
	if last.BandsDB[0] <= 0 {
		t.Fatalf("expected ROCK preset (index 1) to boost band 0, got %v", last.BandsDB[0])
	}
}

func TestPhase6BVisualizerModesAndUpdate(t *testing.T) {
	samples := make([]float32, audio.VisFFTSize)
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * 1000.0 * float64(i) / float64(audio.SampleRate)))
	}
	sink := &fakeEQVisSink{samples: samples}
	g := NewGame(context.Background(), nil, newFakeController(), sink)
	defer g.Close()

	if g.visMode != visModeSpectrum {
		t.Fatalf("initial visMode = %v, want visModeSpectrum", g.visMode)
	}

	g.updateVisualizer(true)
	maxBar := 0.0
	for _, b := range g.visBars {
		if b > maxBar {
			maxBar = b
		}
	}
	if maxBar < 0.5 {
		t.Fatalf("expected spectrum bar peak > 0.5 for 1kHz sine, got %v", maxBar)
	}

	// Pausing decays bars.
	prevMax := maxBar
	g.updateVisualizer(false)
	newMax := 0.0
	for _, b := range g.visBars {
		if b > newMax {
			newMax = b
		}
	}
	if newMax >= prevMax {
		t.Fatalf("expected bars to decay when not playing: prev=%v new=%v", prevMax, newMax)
	}

	// Cycle Spectrum -> Oscilloscope -> Off -> Spectrum.
	g.onClick(WidgetMainVis)
	if g.visMode != visModeOscilloscope {
		t.Fatalf("visMode after 1st click = %v, want visModeOscilloscope", g.visMode)
	}
	g.updateVisualizer(true)
	nonZeroScope := false
	for _, v := range g.visScope {
		if math.Abs(v) > 0.05 {
			nonZeroScope = true
			break
		}
	}
	if !nonZeroScope {
		t.Fatalf("expected non-zero oscilloscope samples when playing")
	}

	g.onClick(WidgetMainVis)
	if g.visMode != visModeOff {
		t.Fatalf("visMode after 2nd click = %v, want visModeOff", g.visMode)
	}

	g.onClick(WidgetMainVis)
	if g.visMode != visModeSpectrum {
		t.Fatalf("visMode after 3rd click = %v, want visModeSpectrum", g.visMode)
	}
}

func TestPhase6CShadeModeRackLayoutAndDrawing(t *testing.T) {
	s, err := skin.Open(filepath.Join("..", "..", "assets", "dev-skin"))
	if err != nil {
		t.Fatalf("Open(dev skin) = %v", err)
	}
	ctrl := newFakeController()
	ctrl.setSnapshot(playback.Snapshot{
		State:    playback.StatePlaying,
		Track:    model.Track{ID: "0wRYvsfhsR8", Title: "Something About Us", Artist: "Daft Punk", Duration: 232 * time.Second},
		Position: 65 * time.Second,
		Duration: 232 * time.Second,
		Volume:   0.75,
		Queue:    []model.Track{{ID: "0wRYvsfhsR8", Title: "Something About Us", Artist: "Daft Punk", Duration: 232 * time.Second}},
		Index:    0,
	})

	var resizes []image.Point
	g := NewGame(
		context.Background(),
		s,
		ctrl,
		&fakeAudioLifecycle{},
		WithWindowResizer(func(w, h int) {
			resizes = append(resizes, image.Pt(w, h))
		}),
	)
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatalf("Update = %v", err)
	}

	// Open EQ -> 275x464.
	g.onClick(WidgetMainEQ)
	if _, h := g.Layout(0, 0); h != MainWindowHeight+EQWindowHeight+PlaylistHeight {
		t.Fatalf("with EQ height = %d, want %d", h, MainWindowHeight+EQWindowHeight+PlaylistHeight)
	}

	// Shade Main -> 14 + 116 + 232 = 362.
	g.onClick(WidgetMainShade)
	if !g.shadeMain {
		t.Fatal("expected shadeMain=true")
	}
	if _, h := g.Layout(0, 0); h != ShadeWindowHeight+EQWindowHeight+PlaylistHeight {
		t.Fatalf("Main shaded height = %d, want %d", h, ShadeWindowHeight+EQWindowHeight+PlaylistHeight)
	}
	if g.eqTop() != ShadeWindowHeight {
		t.Fatalf("eqTop = %d, want %d", g.eqTop(), ShadeWindowHeight)
	}

	// Shade EQ -> 14 + 14 + 232 = 260.
	g.onClick(WidgetEQShade)
	if !g.shadeEQ {
		t.Fatal("expected shadeEQ=true")
	}
	if _, h := g.Layout(0, 0); h != 2*ShadeWindowHeight+PlaylistHeight {
		t.Fatalf("Main+EQ shaded height = %d, want %d", h, 2*ShadeWindowHeight+PlaylistHeight)
	}
	if g.plTop() != 2*ShadeWindowHeight {
		t.Fatalf("plTop = %d, want %d", g.plTop(), 2*ShadeWindowHeight)
	}

	// Shade Playlist -> 14 + 14 + 14 = 42.
	g.onClick(WidgetPlaylistShade)
	if !g.shadePL {
		t.Fatal("expected shadePL=true")
	}
	if _, h := g.Layout(0, 0); h != 3*ShadeWindowHeight {
		t.Fatalf("all shaded height = %d, want %d", h, 3*ShadeWindowHeight)
	}

	// Focus tracking respects shaded heights.
	g.updatePanelFocus(image.Pt(10, 5))
	if g.focusedPanel != panelMain {
		t.Fatalf("focusedPanel at y=5 = %v, want panelMain", g.focusedPanel)
	}
	g.updatePanelFocus(image.Pt(10, 20))
	if g.focusedPanel != panelEQ {
		t.Fatalf("focusedPanel at y=20 = %v, want panelEQ", g.focusedPanel)
	}
	g.updatePanelFocus(image.Pt(10, 35))
	if g.focusedPanel != panelPlaylist {
		t.Fatalf("focusedPanel at y=35 = %v, want panelPlaylist", g.focusedPanel)
	}

	// Draw smoke with all panels shaded, plus debug overlay.
	screen := ebiten.NewImage(WindowWidth, 3*ShadeWindowHeight)
	g.Draw(screen)
	g.toggleDebug()
	g.Draw(screen)
	g.toggleDebug()

	// Eject / focusSearch unshades PL so search is usable.
	g.onClick(WidgetMainShadeEject)
	if g.shadePL || !g.browserMode {
		t.Fatalf("expected WidgetMainShadeEject to unshade PL and enter browserMode (shadePL=%v browserMode=%v)", g.shadePL, g.browserMode)
	}
	if _, h := g.Layout(0, 0); h != 2*ShadeWindowHeight+BrowserHeight {
		t.Fatalf("after unshade PL height = %d, want %d", h, 2*ShadeWindowHeight+BrowserHeight)
	}
	if len(resizes) != 5 {
		t.Fatalf("resize count = %d (%v), want 5", len(resizes), resizes)
	}
}

func TestPhase6CMainAndEQShadeControls(t *testing.T) {
	ctrl := newFakeController()
	ctrl.setSnapshot(playback.Snapshot{
		State:    playback.StatePlaying,
		Duration: 100 * time.Second,
		Volume:   0.5,
	})

	devDir := filepath.Join("..", "..", "assets", "dev-skin")
	devSkin, err := skin.Open(devDir)
	if err != nil {
		t.Fatalf("Open(dev skin) = %v", err)
	}

	g := NewGame(context.Background(), devSkin, ctrl, &fakeAudioLifecycle{})
	defer g.Close()
	if err := g.Update(); err != nil {
		t.Fatalf("Update = %v", err)
	}
	g.onClick(WidgetMainEQ)
	g.onClick(WidgetMainShade)
	g.onClick(WidgetEQShade)

	// Mini transport controls in Main shade bar.
	g.onClick(WidgetMainShadePrev)
	g.onClick(WidgetMainShadePlay)
	g.onClick(WidgetMainShadePause)
	g.onClick(WidgetMainShadeStop)
	g.onClick(WidgetMainShadeNext)
	cmds := ctrl.sentCommands()
	if len(cmds) < 5 {
		t.Fatalf("expected 5 mini transport commands, got %d", len(cmds))
	}

	// Main shade position seek drag.
	g.dragSlider = WidgetMainShadePos
	g.updateMainSliders(234, true)
	if g.seekPreviewFrac <= 0 || g.seekPreviewFrac >= 1 {
		t.Fatalf("seekPreviewFrac = %v, expected middle range", g.seekPreviewFrac)
	}
	g.updateMainSliders(234, false)
	var sawSeek bool
	for _, cmd := range ctrl.sentCommands() {
		if _, ok := cmd.(playback.Seek); ok {
			sawSeek = true
		}
	}
	if !sawSeek {
		t.Fatal("expected Seek command on MainShadePos release")
	}

	// EQ shade volume and balance drags.
	g.eqShadeDrag = WidgetEQShadeVol
	g.updateEQShadeDrag(156, true)
	var sawVol bool
	for _, cmd := range ctrl.sentCommands() {
		if v, ok := cmd.(playback.SetVolume); ok && v.Volume > 0.9 {
			sawVol = true
		}
	}
	if !sawVol {
		t.Fatal("expected SetVolume command near 1.0 from EQShadeVol drag")
	}
	g.eqShadeDrag = WidgetEQShadeBal
	g.updateEQShadeDrag(185, true)
	if g.balance != 0 {
		t.Fatalf("expected center snap balance=0, got %v", g.balance)
	}
	g.updateEQShadeDrag(204, true)
	if g.balance <= 0.8 {
		t.Fatalf("expected right balance > 0.8, got %v", g.balance)
	}
	g.updateEQShadeDrag(204, false)

	// Titlebar double-click unshades Main and EQ.
	t0 := time.Now()
	g.handleTitlebarDoubleClick(image.Pt(40, 5), t0)
	g.handleTitlebarDoubleClick(image.Pt(40, 5), t0.Add(100*time.Millisecond))
	if g.shadeMain {
		t.Fatal("expected double-click on Main titlebar to unshade")
	}
	eqBarY := g.eqTop() + 5
	g.handleTitlebarDoubleClick(image.Pt(20, eqBarY), t0.Add(300*time.Millisecond))
	g.handleTitlebarDoubleClick(image.Pt(20, eqBarY), t0.Add(400*time.Millisecond))
	if g.shadeEQ {
		t.Fatal("expected double-click on EQ titlebar to unshade")
	}

	// Verify drawEQShade fallback does not panic even if eq_ex.bmp is absent.
	g.view.Deallocate()
	noEQExFiles := make(map[string][]byte)
	for _, name := range []string{"main.bmp", "cbuttons.bmp", "titlebar.bmp", "eqmain.bmp"} {
		data, readErr := os.ReadFile(filepath.Join(devDir, name))
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		noEQExFiles[name] = data
	}
	zipPath := filepath.Join(t.TempDir(), "no-eqex.wsz")
	writeTestZip(t, zipPath, noEQExFiles)
	noEQExSkin, err := skin.Open(zipPath)
	if err != nil {
		t.Fatalf("Open(no-eqex) = %v", err)
	}
	g.skin = noEQExSkin
	g.view = nil
	if err := g.Update(); err != nil {
		t.Fatalf("Update(no-eqex) = %v", err)
	}
	g.shadeEQ = true
	g.Draw(ebiten.NewImage(WindowWidth, g.canvasHeight()))
}

func TestPhase6CDoubleSizeButtonAndPersistence(t *testing.T) {
	var resizes []image.Point
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	g := NewGame(
		context.Background(),
		nil,
		newFakeController(),
		&fakeAudioLifecycle{},
		WithConfigPath(cfgPath),
		WithWindowResizer(func(w, h int) {
			resizes = append(resizes, image.Pt(w, h))
		}),
	)

	// Toggle double-size ON via clutter bar D button.
	g.onClick(WidgetMainDoubleSize)
	if g.scale != 2.0 {
		t.Fatalf("scale after D click = %v, want 2.0", g.scale)
	}
	if len(resizes) != 1 || resizes[0] != image.Pt(2*WindowWidth, 2*WindowHeight) {
		t.Fatalf("resizes after D ON = %v, want [%v]", resizes, image.Pt(2*WindowWidth, 2*WindowHeight))
	}

	// Toggle double-size OFF via clutter bar D button.
	g.onClick(WidgetMainDoubleSize)
	if g.scale != 1.0 {
		t.Fatalf("scale after 2nd D click = %v, want 1.0", g.scale)
	}
	if len(resizes) != 2 || resizes[1] != image.Pt(WindowWidth, WindowHeight) {
		t.Fatalf("resizes after D OFF = %v", resizes)
	}

	// Persist shaded states + 2x scale and restore on cold start.
	g.onClick(WidgetMainDoubleSize)
	g.onClick(WidgetMainEQ)
	g.onClick(WidgetMainShade)
	g.onClick(WidgetEQShade)
	g.onClick(WidgetPlaylistShade)
	g.Close()

	loaded, existed, err := store.Load(cfgPath)
	if err != nil || !existed {
		t.Fatalf("store.Load = existed:%v err:%v", existed, err)
	}
	if loaded.Scale != 2.0 {
		t.Fatalf("loaded.Scale = %v, want 2.0", loaded.Scale)
	}
	if !loaded.Windows[store.WindowMain].Shaded || !loaded.Windows[store.WindowEQ].Shaded || !loaded.Windows[store.WindowPlaylist].Shaded {
		t.Fatalf("loaded shaded states = %+v", loaded.Windows)
	}

	g2 := NewGame(context.Background(), nil, newFakeController(), &fakeAudioLifecycle{}, WithInitialConfig(loaded))
	defer g2.Close()
	if !g2.shadeMain || !g2.shadeEQ || !g2.shadePL || g2.scale != 2.0 {
		t.Fatalf("restored state mismatch: main=%v eq=%v pl=%v scale=%v", g2.shadeMain, g2.shadeEQ, g2.shadePL, g2.scale)
	}
	if _, h := g2.Layout(0, 0); h != 3*ShadeWindowHeight {
		t.Fatalf("restored shaded height = %d, want %d", h, 3*ShadeWindowHeight)
	}
}
