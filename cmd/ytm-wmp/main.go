// Command ytm-wmp is a skinned YouTube Music player.
//
// This file is only flags, dependency checks, wiring, and signal handling.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hajimehoshi/ebiten/v2"

	"ytm-wmp/internal/artwork"
	"ytm-wmp/internal/audio"
	"ytm-wmp/internal/catalog"
	catalogytdlp "ytm-wmp/internal/catalog/ytdlp"
	catalogytmusic "ytm-wmp/internal/catalog/ytmusic"
	"ytm-wmp/internal/deps"
	"ytm-wmp/internal/m3u"
	"ytm-wmp/internal/model"
	"ytm-wmp/internal/playback"
	"ytm-wmp/internal/skin"
	"ytm-wmp/internal/ui"
	"ytm-wmp/internal/ytdlp"
)

const (
	// defaultTarget keeps the PoC's one track when launched with no flags so a
	// bare `go run ./cmd/ytm-wmp` still demonstrates playback immediately.
	defaultTarget = "0wRYvsfhsR8"
	devSkinPath   = "assets/dev-skin"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ytm-wmp: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var urlFlags stringListFlag
	flag.Var(&urlFlags, "url", "YouTube video ID, watch URL, or yt-dlp search query (repeatable)")
	skinFlag := flag.String("skin", "", "skin directory, .wsz, or .zip (default: bundled dev skin)")
	catalogFlag := flag.String("catalog", "ytmusic", "catalog backend: ytmusic or ytdlp")
	queryFlag := flag.String("query", "", "initial catalog search query")
	playlistFlag := flag.String("playlist", "", "YouTube/YTM playlist URL or ID to load at startup")
	autoplayFlag := flag.Bool("autoplay", true, "queue YouTube Music radio suggestions when the queue ends")
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

	cat, catalogName, err := newCatalog(*catalogFlag, ytdlpTool.Path)
	if err != nil {
		return err
	}
	slog.Info("dependencies ready",
		"yt-dlp", ytdlpTool.Version,
		"ffmpeg", ffmpegTool.Version,
		"catalog", catalogName,
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
	artCache := artwork.NewCache(artwork.WithCacheDir(artworkCacheDir()))
	sink := audio.NewEbitenSink()
	ctrl := playback.NewController(resolver, audio.Opener{FFmpeg: ffmpegTool.Path}, sink, controllerOptions(cat, *autoplayFlag)...)

	tracks, err := startupTracks(ctx, cat, urlFlags, flag.Args(), *playlistFlag, *queryFlag)
	if err != nil {
		return err
	}

	var ytFallback catalog.Catalog
	if catalogName != "ytdlp" {
		ytFallback = catalogytdlp.New(catalogytdlp.WithBinary(ytdlpTool.Path))
	}

	game := ui.NewGame(
		ctx,
		s,
		ctrl,
		sink,
		ui.WithCatalog(cat),
		ui.WithYouTubeCatalog(ytFallback),
		ui.WithArtwork(artCache),
		ui.WithInitialQuery(*queryFlag),
		ui.WithSkinName(filepath.Base(skinPath)),
		ui.WithSkinPath(skinPath),
		ui.WithWindowResizer(ebiten.SetWindowSize),
		ui.WithEQPath(userConfigPath("eq.json")),
		ui.WithPlaylistPath(userConfigPath("playlist.m3u")),
	)
	defer game.Close()

	trackIDs := make([]string, len(tracks))
	for i, t := range tracks {
		trackIDs[i] = t.ID
	}
	slog.Debug("initial queue", "count", len(tracks), "tracks", trackIDs)
	if len(tracks) > 0 {
		ctrl.Send(playback.SetQueue{Tracks: tracks, Index: 0})
	}

	ebiten.SetWindowTitle("ytm-wmp")
	ebiten.SetWindowSize(ui.WindowWidth, ui.WindowHeight)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeDisabled)
	if err := ebiten.RunGame(game); err != nil {
		return fmt.Errorf("run game: %w", err)
	}
	return nil
}

func controllerOptions(cat catalog.Catalog, autoplay bool) []playback.Option {
	radio, ok := cat.(playback.RadioProvider)
	if !ok || radio == nil {
		return nil
	}
	return []playback.Option{
		playback.WithRadio(radio),
		playback.WithAutoplay(autoplay),
	}
}

func newCatalog(backend, ytdlpPath string) (catalog.Catalog, string, error) {
	name := strings.ToLower(strings.TrimSpace(backend))
	switch name {
	case "", "ytmusic":
		return catalogytmusic.New(), "ytmusic", nil
	case "ytdlp":
		return catalogytdlp.New(catalogytdlp.WithBinary(ytdlpPath)), "ytdlp", nil
	default:
		return nil, "", fmt.Errorf("unknown catalog backend %q (want ytmusic or ytdlp)", backend)
	}
}

func startupTracks(ctx context.Context, cat catalog.Catalog, flagTargets, extra []string, playlistTarget, initialQuery string) ([]model.Track, error) {
	explicit := explicitTracks(flagTargets, extra)
	if playlistTarget != "" {
		lower := strings.ToLower(playlistTarget)
		if strings.HasSuffix(lower, ".m3u") || strings.HasSuffix(lower, ".m3u8") {
			f, err := os.Open(playlistTarget)
			if err != nil {
				return nil, fmt.Errorf("opening M3U playlist %q: %w", playlistTarget, err)
			}
			plTracks, parseErr := m3u.Parse(f)
			_ = f.Close()
			if parseErr != nil {
				return nil, fmt.Errorf("parsing M3U playlist %q: %w", playlistTarget, parseErr)
			}
			explicit = append(explicit, plTracks...)
		} else if cat != nil {
			plTracks, err := cat.Playlist(ctx, playlistTarget)
			if err != nil {
				return nil, fmt.Errorf("loading playlist %q: %w", playlistTarget, err)
			}
			explicit = append(explicit, plTracks...)
		}
	}
	if len(explicit) > 0 {
		return explicit, nil
	}
	if initialQuery != "" {
		return nil, nil
	}
	return []model.Track{{ID: defaultTarget}}, nil
}

func userConfigPath(name string) string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "ytm-wmp", name)
}

func artworkCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "ytm-wmp", "artwork")
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

type stringListFlag []string

func (f *stringListFlag) String() string {
	return fmt.Sprintf("%v", []string(*f))
}

func (f *stringListFlag) Set(value string) error {
	if value != "" {
		*f = append(*f, value)
	}
	return nil
}

func explicitTracks(flagTargets, extra []string) []model.Track {
	var tracks []model.Track
	for _, target := range flagTargets {
		if id := model.NormalizeTrackID(target); id != "" {
			tracks = append(tracks, model.Track{ID: id})
		}
	}
	for _, arg := range extra {
		if id := model.NormalizeTrackID(arg); id != "" {
			tracks = append(tracks, model.Track{ID: id})
		}
	}
	return tracks
}

func initialTracks(flagTargets, extra []string) []model.Track {
	tracks := explicitTracks(flagTargets, extra)
	if len(tracks) == 0 {
		tracks = append(tracks, model.Track{ID: defaultTarget})
	}
	return tracks
}
