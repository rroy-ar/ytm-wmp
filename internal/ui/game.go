package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"ytm-wmp/internal/skin"
	"ytm-wmp/internal/ui/skinview"
)

// Runner plays one hardcoded target. Phase 2 replaces this with the playback
// actor, but the UI only needs the narrow "start and report completion" view.
type Runner interface {
	Play(ctx context.Context, target string) error
}

// AudioControl is the consumer-owned view of the audio sink. The UI needs to
// initialize the device, pause/resume, and release it on shutdown; decoding and
// ffmpeg ownership stay inside internal/audio.
type AudioControl interface {
	Init() error
	Pause()
	Resume()
	Close()
}

type playbackResult struct {
	err error
}

// Game is the Ebitengine application: a fixed 275x116 logical canvas, a skinned
// main window, and play/pause controls.
type Game struct {
	ctx    context.Context
	cancel context.CancelFunc

	skin   *skin.Skin
	runner Runner
	sink   AudioControl
	target string

	view      *skinview.View
	pointer   Pointer
	debug     bool
	audioInit bool

	started bool
	paused  bool
	errText string
	events  chan playbackResult

	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewGame wires the UI to a loaded skin, playback runner, and audio sink.
// The parent context is canceled by Close, which stops in-flight playback.
func NewGame(parent context.Context, target string, s *skin.Skin, runner Runner, sink AudioControl) *Game {
	ctx, cancel := context.WithCancel(parent)
	return &Game{
		ctx:    ctx,
		cancel: cancel,
		skin:   s,
		runner: runner,
		sink:   sink,
		target: target,
		events: make(chan playbackResult, 1),
	}
}

// Update advances input, drains playback results, and enforces the fixed
// timestep. It returns ebiten.Termination when the parent context is canceled.
func (g *Game) Update() error {
	if g.ctx.Err() != nil {
		return ebiten.Termination
	}

	// The audio context and GPU textures must be created after Ebitengine has
	// started, so both happen on the first tick rather than in main.
	if !g.audioInit {
		g.audioInit = true
		if err := g.sink.Init(); err != nil {
			g.setError(fmt.Errorf("opening audio device: %w", err))
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

// Close cancels playback, waits for the playback goroutine, then releases the
// audio device. The wait is important: it ensures no goroutine can start a new
// ffmpeg process after the sink has been closed.
func (g *Game) Close() {
	g.closeOnce.Do(func() {
		g.cancel()
		g.wg.Wait()
		g.sink.Close()
	})
}

func (g *Game) onClick(id string) {
	switch id {
	case WidgetPlay:
		g.errText = ""
		if g.started {
			if g.paused {
				g.sink.Resume()
				g.paused = false
			}
			return
		}
		g.start()
	case WidgetPause:
		if g.started && !g.paused {
			g.sink.Pause()
			g.paused = true
		}
	}
}

func (g *Game) start() {
	g.started = true
	g.paused = false
	g.errText = ""

	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		err := g.runner.Play(g.ctx, g.target)
		if errors.Is(err, context.Canceled) {
			return
		}
		select {
		case g.events <- playbackResult{err: err}:
		default:
			// The window is closing; nobody will read this result.
		}
	}()
}

func (g *Game) drainEvents() {
	for {
		select {
		case result := <-g.events:
			g.started = false
			g.paused = false
			if result.err != nil {
				g.setError(result.err)
			} else {
				g.errText = ""
			}
		default:
			return
		}
	}
}

func (g *Game) setError(err error) {
	g.errText = err.Error()
	slog.Error("playback failed", "err", err)
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
	case skin.PlayButton:
		if g.pointer.Active() == WidgetPlay && g.pointer.Pressed() {
			return skin.PlayButtonActive
		}
	case skin.PauseButton:
		if g.pointer.Active() == WidgetPause && g.pointer.Pressed() {
			return skin.PauseButtonActive
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

	var report strings.Builder
	fmt.Fprintf(&report, "FPS %.1f TPS %.1f\n", ebiten.ActualFPS(), ebiten.ActualTPS())
	fmt.Fprintf(&report, "state playing=%v paused=%v hot=%q active=%q\n", g.started, g.paused, g.pointer.Hot(), g.pointer.Active())
	fmt.Fprintf(&report, "cursor=%v\n", g.pointer.Position())

	for _, p := range mainPlacements {
		name := g.spriteName(p.Sprite)
		def, ok := skin.Definition(name)
		if !ok {
			continue
		}
		width, height := def.Src.Dx(), def.Src.Dy()
		vector.StrokeRect(screen, float32(p.At.X)+0.5, float32(p.At.Y)+0.5, float32(width), float32(height), 1, cyan, false)
		fmt.Fprintf(&report, "%s src=%v dst=%v\n", name, def.Src, p.At)
	}
	for _, widget := range mainWidgets {
		b := widget.Bounds
		vector.StrokeRect(screen, float32(b.Min.X)+0.5, float32(b.Min.Y)+0.5, float32(b.Dx()), float32(b.Dy()), 1, magenta, false)
		fmt.Fprintf(&report, "hit %s %v\n", widget.ID, b)
	}

	// A dark panel keeps the text readable over bright skins.
	vector.DrawFilledRect(screen, 3, 15, 269, 70, color.RGBA{A: 0xd0}, false)
	ebitenutil.DebugPrintAt(screen, report.String(), 5, 15)
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
