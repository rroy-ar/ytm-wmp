package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"ytm-wmp/internal/playback"
	"ytm-wmp/internal/skin"
	"ytm-wmp/internal/ui/skinview"
)

const (
	seekStep   = 5 * time.Second
	volumeStep = 0.1
)

// PlaybackController drives playback via commands, events, and atomic snapshots.
type PlaybackController interface {
	Run(ctx context.Context)
	Send(cmd playback.Command) bool
	Events() <-chan playback.Event
	Snapshot() playback.Snapshot
}

// AudioLifecycle initializes and closes the audio device on UI lifecycle
// boundaries; playback control goes through PlaybackController.
type AudioLifecycle interface {
	Init() error
	Close()
}

// Game is the Ebitengine application: a fixed 275x116 logical canvas, a skinned
// main window, and transport controls wired to the playback actor.
type Game struct {
	ctx    context.Context
	cancel context.CancelFunc

	skin *skin.Skin
	ctrl PlaybackController
	sink AudioLifecycle

	view      *skinview.View
	pointer   Pointer
	debug     bool
	audioInit bool

	errText string

	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewGame wires the UI to a loaded skin, playback controller, and audio sink.
// It starts the playback actor under a derived context canceled by Close.
func NewGame(parent context.Context, s *skin.Skin, ctrl PlaybackController, sink AudioLifecycle) *Game {
	ctx, cancel := context.WithCancel(parent)
	g := &Game{
		ctx:    ctx,
		cancel: cancel,
		skin:   s,
		ctrl:   ctrl,
		sink:   sink,
	}
	if ctrl != nil {
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			ctrl.Run(ctx)
		}()
	}
	return g
}

// Update advances input, drains playback events, and enforces the fixed
// timestep. It returns ebiten.Termination when the parent context is canceled.
func (g *Game) Update() error {
	if g.ctx.Err() != nil {
		return ebiten.Termination
	}

	// The audio context and GPU textures must be created after Ebitengine has
	// started, so both happen on the first tick rather than in main.
	if !g.audioInit {
		g.audioInit = true
		if g.sink != nil {
			if err := g.sink.Init(); err != nil {
				g.setError(fmt.Sprintf("opening audio device: %v", err))
			}
		}
	}
	if g.view == nil && g.skin != nil {
		g.view = skinview.New(g.skin)
	}

	g.drainEvents()

	cursorX, cursorY := ebiten.CursorPosition()
	clicked := g.pointer.Update(cursorX, cursorY, ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft), mainWidgets)
	if clicked != "" {
		g.onClick(clicked)
	}
	g.handleKeys()

	if inpututil.IsKeyJustPressed(ebiten.KeyF1) {
		g.toggleDebug()
	}
	return nil
}

// Draw renders the main window. It never does I/O, resolves URLs, or allocates
// textures; those happen in Update or inside skinview.New.
func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{R: 0x0c, G: 0x0f, B: 0x14, A: 0xff})
	if g.view == nil {
		ebitenutil.DebugPrintAt(screen, "skin view not ready", 8, 8)
		return
	}

	for _, p := range mainPlacements {
		name := g.spriteName(p.Sprite)
		g.drawSprite(screen, name, p.At)
	}

	if g.errText != "" {
		g.drawError(screen)
	}
	if g.debug {
		g.drawDebug(screen)
	}
}

// Layout keeps a fixed logical canvas. Ebitengine scales the window outside
// this function, so every skin coordinate and hit area stays in 1x space.
func (g *Game) Layout(_, _ int) (int, int) {
	return WindowWidth, WindowHeight
}

// Close cancels the playback actor, waits for it to stop all workers and
// ffmpeg processes, then releases the audio device.
func (g *Game) Close() {
	g.closeOnce.Do(func() {
		if g.cancel != nil {
			g.cancel()
		}
		g.wg.Wait()
		if g.sink != nil {
			g.sink.Close()
		}
	})
}

func (g *Game) onClick(id string) {
	if g.ctrl == nil {
		return
	}
	slog.Debug("ui button clicked", "widget", id)
	switch id {
	case WidgetPrevious:
		g.errText = ""
		g.ctrl.Send(playback.Prev{})
	case WidgetPlay:
		g.errText = ""
		g.ctrl.Send(playback.Play{})
	case WidgetPause:
		g.ctrl.Send(playback.Pause{})
	case WidgetStop:
		g.errText = ""
		g.ctrl.Send(playback.Stop{})
	case WidgetNext:
		g.errText = ""
		g.ctrl.Send(playback.Next{})
	}
}

func (g *Game) handleKeys() {
	if g.ctrl == nil {
		return
	}
	snap := g.ctrl.Snapshot()

	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		slog.Debug("ui key", "key", "space", "state", snap.State.String())
		switch snap.State {
		case playback.StatePlaying:
			g.ctrl.Send(playback.Pause{})
		case playback.StatePaused:
			g.ctrl.Send(playback.Resume{})
		default:
			g.errText = ""
			g.ctrl.Send(playback.Play{})
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyS) {
		slog.Debug("ui key", "key", "s")
		g.errText = ""
		g.ctrl.Send(playback.Stop{})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyZ) {
		slog.Debug("ui key", "key", "z")
		g.errText = ""
		g.ctrl.Send(playback.Prev{})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyB) {
		slog.Debug("ui key", "key", "b")
		g.errText = ""
		g.ctrl.Send(playback.Next{})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyLeft) {
		target := snap.Position - seekStep
		if target < 0 {
			target = 0
		}
		slog.Debug("ui key", "key", "left", "seek", target)
		g.ctrl.Send(playback.Seek{Position: target})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyRight) {
		target := snap.Position + seekStep
		slog.Debug("ui key", "key", "right", "seek", target)
		g.ctrl.Send(playback.Seek{Position: target})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
		g.ctrl.Send(playback.SetVolume{Volume: snap.Volume + volumeStep})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
		g.ctrl.Send(playback.SetVolume{Volume: snap.Volume - volumeStep})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyR) {
		next := (snap.Repeat + 1) % 3
		g.ctrl.Send(playback.SetRepeat{Mode: next})
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyH) {
		g.ctrl.Send(playback.SetShuffle{Enabled: !snap.Shuffle})
	}
}

func (g *Game) drainEvents() {
	if g.ctrl == nil {
		return
	}
	events := g.ctrl.Events()
	if events == nil {
		return
	}
	for {
		select {
		case ev := <-events:
			switch e := ev.(type) {
			case playback.PlaybackError:
				g.setError(e.Message)
			case playback.StateChanged:
				if e.To == playback.StateResolving || e.To == playback.StatePlaying || e.To == playback.StateIdle {
					g.errText = ""
				}
			}
		default:
			return
		}
	}
}

func (g *Game) setError(msg string) {
	g.errText = msg
	slog.Error("playback failed", "err", msg)
}

func (g *Game) toggleDebug() {
	g.debug = !g.debug
}

func (g *Game) spriteName(name string) string {
	switch name {
	case skin.TitlebarActive:
		if !ebiten.IsFocused() {
			return skin.TitlebarIdle
		}
	case skin.PreviousButton:
		if g.pointer.Active() == WidgetPrevious && g.pointer.Pressed() {
			return skin.PreviousButtonActive
		}
	case skin.PlayButton:
		if g.pointer.Active() == WidgetPlay && g.pointer.Pressed() {
			return skin.PlayButtonActive
		}
	case skin.PauseButton:
		if g.pointer.Active() == WidgetPause && g.pointer.Pressed() {
			return skin.PauseButtonActive
		}
	case skin.StopButton:
		if g.pointer.Active() == WidgetStop && g.pointer.Pressed() {
			return skin.StopButtonActive
		}
	case skin.NextButton:
		if g.pointer.Active() == WidgetNext && g.pointer.Pressed() {
			return skin.NextButtonActive
		}
	}
	return name
}

func (g *Game) drawSprite(dst *ebiten.Image, name string, at image.Point) {
	img := g.view.Sprite(name)
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterNearest}
	op.GeoM.Translate(float64(at.X), float64(at.Y))
	dst.DrawImage(img, op)

	if g.view.Missing(name) {
		// A silent skip makes coordinate bugs nearly impossible to find.
		ebitenutil.DebugPrintAt(dst, name, at.X+1, at.Y+1)
	}
}

func (g *Game) drawError(screen *ebiten.Image) {
	const maxLines = 7
	lines := wrapText(g.errText, 42)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = strings.TrimSuffix(lines[maxLines-1], " ") + "..."
	}

	height := float32(10 + len(lines)*14)
	vector.DrawFilledRect(screen, 5, 17, 265, height, color.RGBA{R: 0x50, G: 0x00, B: 0x00, A: 0xe0}, false)
	vector.StrokeRect(screen, 5, 17, 265, height, 1, color.RGBA{R: 0xff, G: 0x90, B: 0x90, A: 0xff}, false)
	for i, line := range lines {
		ebitenutil.DebugPrintAt(screen, line, 10, 21+i*14)
	}
}

func (g *Game) drawDebug(screen *ebiten.Image) {
	cyan := color.RGBA{R: 0x00, G: 0xff, B: 0xff, A: 0xff}
	magenta := color.RGBA{R: 0xff, G: 0x00, B: 0xff, A: 0xff}

	for _, p := range mainPlacements {
		name := g.spriteName(p.Sprite)
		def, ok := skin.Definition(name)
		if !ok {
			continue
		}
		width, height := def.Src.Dx(), def.Src.Dy()
		vector.StrokeRect(screen, float32(p.At.X)+0.5, float32(p.At.Y)+0.5, float32(width), float32(height), 1, cyan, false)
	}
	for _, widget := range mainWidgets {
		b := widget.Bounds
		vector.StrokeRect(screen, float32(b.Min.X)+0.5, float32(b.Min.Y)+0.5, float32(b.Dx()), float32(b.Dy()), 1, magenta, false)
	}

	var snap playback.Snapshot
	if g.ctrl != nil {
		snap = g.ctrl.Snapshot()
	}

	// Keep the report to six short lines: the built-in debug font does not
	// wrap, and the window is only 275 logical pixels wide.
	var report strings.Builder
	fmt.Fprintf(&report, "FPS %.0f TPS %.0f state=%s\n", ebiten.ActualFPS(), ebiten.ActualTPS(), snap.State)
	fmt.Fprintf(&report, "q=%d/%d pos=%s/%s vol=%.0f%%\n", snap.Index+1, len(snap.Queue), snap.Position.Round(time.Second), snap.Duration.Round(time.Second), snap.Volume*100)
	fmt.Fprintf(&report, "shuf=%v rep=%s id=%s\n", snap.Shuffle, snap.Repeat, snap.Track.ID)
	fmt.Fprintf(&report, "hot=%q active=%q cur=%v\n", g.pointer.Hot(), g.pointer.Active(), g.pointer.Position())

	lines := wrapText(report.String(), 43)
	if len(lines) > 6 {
		lines = lines[:6]
	}
	height := float32(10 + len(lines)*14)
	vector.DrawFilledRect(screen, 3, 3, 269, height, color.RGBA{A: 0xe0}, false)
	for i, line := range lines {
		ebitenutil.DebugPrintAt(screen, line, 5, 5+i*14)
	}
}

func wrapText(text string, limit int) []string {
	if limit <= 0 {
		limit = 1
	}

	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		var line []rune
		for _, r := range paragraph {
			if len(line) == limit {
				lines = append(lines, string(line))
				line = line[:0]
			}
			line = append(line, r)
		}
		if len(line) > 0 {
			lines = append(lines, string(line))
		}
	}
	return lines
}
