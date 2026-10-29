// Command ytm-wmp plays one YouTube target through yt-dlp and ffmpeg.
//
// Phase 0 keeps the proof-of-concept's behavior while moving process control,
// caching, and playback orchestration out of main. This file is only flags,
// dependency checks, wiring, and signal handling.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"ytm-wmp/internal/audio"
	"ytm-wmp/internal/deps"
	"ytm-wmp/internal/playback"
	"ytm-wmp/internal/ytdlp"
)

// defaultTarget keeps the PoC's one hardcoded track so a bare `go run` still
// demonstrates playback. Phase 3 replaces this with search and a queue.
const defaultTarget = "https://www.youtube.com/watch?v=0wRYvsfhsR8"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ytm-wmp: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	target := flag.String("url", defaultTarget, "YouTube video ID, watch URL, or yt-dlp search query")
	flag.Parse()

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

	resolver := ytdlp.New(ytdlp.WithBinary(ytdlpTool.Path))
	opener := audio.Opener{FFmpeg: ffmpegTool.Path}
	player := playback.New(resolver, opener, audio.NewBeepPlayer())

	err = player.Play(ctx, *target)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
