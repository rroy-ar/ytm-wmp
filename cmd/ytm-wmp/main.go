// Command ytm-wmp is a skinned YouTube Music player.
//
// Phase 1 is the walking skeleton: one hardcoded target, a real classic skin,
// and play/pause through Ebitengine's audio context. This file is only flags,
// dependency checks, wiring, and signal handling.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/hajimehoshi/ebiten/v2"

	"ytm-wmp/internal/audio"
	"ytm-wmp/internal/deps"
	"ytm-wmp/internal/playback"
	"ytm-wmp/internal/skin"
	"ytm-wmp/internal/ui"
	"ytm-wmp/internal/ytdlp"
)

const (
	// defaultTarget keeps the PoC's one hardcoded track so a bare `go run`
	// already demonstrates the full vertical slice. Phase 3 replaces it with
	// search and a queue.
	defaultTarget = "https://www.youtube.com/watch?v=0wRYvsfhsR8"
	devSkinPath   = "assets/dev-skin"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ytm-wmp: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	target := flag.String("url", defaultTarget, "YouTube video ID, watch URL, or yt-dlp search query")
	skinFlag := flag.String("skin", "", "skin directory, .wsz, or .zip (default: bundled dev skin)")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	configureLogging(*debug)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ytdlpTool, err := deps.Check(ctx, "yt-dlp")
	if err != nil {
		return err
	}
	ffmpegTool, err := deps.Check(ctx, "ffmpeg")
	if err != nil {
		return err
	}
	slog.Info("dependencies ready",
		"yt-dlp", ytdlpTool.Version,
		"ffmpeg", ffmpegTool.Version,
	)

	skinPath := chooseSkin(*skinFlag)
	s, err := skin.Open(skinPath)
	if err != nil {
		return err
	}
	slog.Info("skin loaded", "path", skinPath, "sheets", s.SheetNames())
	if warnings := s.Warnings(); len(warnings) > 0 {
		slog.Warn("skin loaded with warnings", "warnings", warnings)
	}

	resolver := ytdlp.New(ytdlp.WithBinary(ytdlpTool.Path))
	sink := audio.NewEbitenSink()
	player := playback.New(resolver, audio.Opener{FFmpeg: ffmpegTool.Path}, sink)
	game := ui.NewGame(ctx, *target, s, player, sink)
	defer game.Close()

	ebiten.SetWindowTitle("ytm-wmp")
	ebiten.SetWindowSize(ui.WindowWidth, ui.WindowHeight)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeDisabled)
	if err := ebiten.RunGame(game); err != nil {
		return fmt.Errorf("run game: %w", err)
	}
	return nil
}

func configureLogging(debug bool) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

// chooseSkin prefers the explicit flag. Otherwise it looks for the dev skin in
// the working directory first (the `go run` case), then next to the executable
// (the installed-binary case).
func chooseSkin(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if _, err := os.Stat(devSkinPath); err == nil {
		return devSkinPath
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), devSkinPath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return devSkinPath
}
