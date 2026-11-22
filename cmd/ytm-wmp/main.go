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
	"sync"
	"syscall"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/rroy-ar/ytm-wmp/internal/artwork"
	"github.com/rroy-ar/ytm-wmp/internal/audio"
	"github.com/rroy-ar/ytm-wmp/internal/catalog"
	catalogytdlp "github.com/rroy-ar/ytm-wmp/internal/catalog/ytdlp"
	catalogytmusic "github.com/rroy-ar/ytm-wmp/internal/catalog/ytmusic"
	"github.com/rroy-ar/ytm-wmp/internal/deps"
	"github.com/rroy-ar/ytm-wmp/internal/m3u"
	"github.com/rroy-ar/ytm-wmp/internal/model"
	"github.com/rroy-ar/ytm-wmp/internal/mpris"
	"github.com/rroy-ar/ytm-wmp/internal/playback"
	"github.com/rroy-ar/ytm-wmp/internal/skin"
	"github.com/rroy-ar/ytm-wmp/internal/store"
	"github.com/rroy-ar/ytm-wmp/internal/ui"
	"github.com/rroy-ar/ytm-wmp/internal/ytdlp"
)

const (
	// defaultTarget keeps the PoC's one track when launched with no flags so a
	// bare `go run ./cmd/ytm-wmp` still demonstrates playback immediately.
	defaultTarget = "0wRYvsfhsR8"
	devSkinPath   = "assets/dev-skin"
)

type dynamicOpener struct {
	mu     sync.RWMutex
	ffmpeg string
}

func (o *dynamicOpener) SetFFmpeg(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "ffmpeg"
	}
	o.mu.Lock()
	o.ffmpeg = path
	o.mu.Unlock()
}

func (o *dynamicOpener) Open(ctx context.Context, streamURL string, offset time.Duration) (audio.Source, error) {
	o.mu.RLock()
	bin := o.ffmpeg
	o.mu.RUnlock()
	return (audio.Opener{FFmpeg: bin}).Open(ctx, streamURL, offset)
}

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
	mprisFlag := flag.Bool("mpris", true, "enable Linux MPRIS D-Bus media controls")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	explicitFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		explicitFlags[f.Name] = true
	})

	configureLogging(*debug)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	configPath := userConfigPath("config.json")
	cfg, configExisted, cfgErr := store.Load(configPath)
	var startupWarnings []string
	if cfgErr != nil {
		slog.Warn("could not load config; using defaults", "path", configPath, "err", cfgErr)
		startupWarnings = append(startupWarnings, "Config reset to defaults: "+cfgErr.Error())
		cfg = store.DefaultConfig()
		configExisted = false
	}

	ytdlpStatus := checkToolStatus(ctx, "yt-dlp", cfg.ToolPaths.YTDLP)
	ffmpegStatus := checkToolStatus(ctx, "ffmpeg", cfg.ToolPaths.FFmpeg)
	if !ytdlpStatus.OK {
		slog.Warn("dependency missing", "tool", "yt-dlp", "err", ytdlpStatus.Error)
	}
	if !ffmpegStatus.OK {
		slog.Warn("dependency missing", "tool", "ffmpeg", "err", ffmpegStatus.Error)
	}
	slog.Info("dependencies checked",
		"yt-dlp_ok", ytdlpStatus.OK, "yt-dlp_ver", ytdlpStatus.Version,
		"ffmpeg_ok", ffmpegStatus.OK, "ffmpeg_ver", ffmpegStatus.Version,
	)

	ytdlpBin := ytdlpStatus.ResolvedPath
	if ytdlpBin == "" {
		ytdlpBin = "yt-dlp"
	}
	ffmpegBin := ffmpegStatus.ResolvedPath
	if ffmpegBin == "" {
		ffmpegBin = "ffmpeg"
	}

	cat, catalogName, err := newCatalog(*catalogFlag, ytdlpBin)
	if err != nil {
		return err
	}
	slog.Info("catalog ready", "catalog", catalogName)

	s, skinPath, skinWarn, err := loadStartupSkin(*skinFlag, cfg.Skin)
	if err != nil {
		return err
	}
	if skinWarn != "" {
		startupWarnings = append(startupWarnings, skinWarn)
	}
	slog.Info("skin loaded", "path", skinPath, "sheets", s.SheetNames())
	if warnings := s.Warnings(); len(warnings) > 0 {
		slog.Warn("skin loaded with warnings", "warnings", warnings)
	}

	resolver := ytdlp.New(ytdlp.WithBinary(ytdlpBin))
	artDir := cfg.CacheDir
	if artDir == "" {
		artDir = artworkCacheDir()
	}
	artCache := artwork.NewCache(artwork.WithCacheDir(artDir))
	if cfg.CacheMaxMB > 0 {
		maxBytes := int64(cfg.CacheMaxMB) * 1024 * 1024
		go func() { _ = artCache.PruneDisk(maxBytes) }()
	}

	opener := &dynamicOpener{ffmpeg: ffmpegBin}
	sink := audio.NewEbitenSink()
	if cfg.BufferMs > 0 {
		sink.SetBufferSize(time.Duration(cfg.BufferMs) * time.Millisecond)
	}

	autoplay := cfg.Autoplay
	if explicitFlags["autoplay"] {
		autoplay = *autoplayFlag
	}
	ctrl := playback.NewController(resolver, opener, sink, controllerOptions(cat, autoplay)...)

	tracks, queueIdx, err := resolveStartupQueue(ctx, cat, urlFlags, flag.Args(), *playlistFlag, *queryFlag, cfg, configExisted)
	if err != nil {
		return err
	}

	var ytFallback *catalogytdlp.Client
	if catalogName != "ytdlp" {
		ytFallback = catalogytdlp.New(catalogytdlp.WithBinary(ytdlpBin))
	}
	var ytFallbackCat catalog.Catalog
	if ytFallback != nil {
		ytFallbackCat = ytFallback
	}
	ytPrimaryCat, _ := cat.(*catalogytdlp.Client)

	gameOpts := []ui.GameOption{
		ui.WithCatalog(cat),
		ui.WithYouTubeCatalog(ytFallbackCat),
		ui.WithArtwork(artCache),
		ui.WithInitialConfig(cfg),
		ui.WithConfigPath(configPath),
		ui.WithInitialQuery(*queryFlag),
		ui.WithSkinName(filepath.Base(skinPath)),
		ui.WithSkinPath(skinPath),
		ui.WithDefaultSkinPath(chooseSkin("")),
		ui.WithWindowResizer(ebiten.SetWindowSize),
		ui.WithEQPath(userConfigPath("eq.json")),
		ui.WithPlaylistPath(userConfigPath("playlist.m3u")),
		ui.WithIME(true),
		ui.WithToolStatus(ytdlpStatus, ffmpegStatus),
		ui.WithToolChecker(checkToolStatus),
		ui.WithToolApplier(func(ytPath, ffPath string) {
			resolver.SetBinary(ytPath)
			opener.SetFFmpeg(ffPath)
			if ytFallback != nil {
				ytFallback.SetBinary(ytPath)
			}
			if ytPrimaryCat != nil {
				ytPrimaryCat.SetBinary(ytPath)
			}
		}),
		ui.WithBufferApplier(sink.SetBufferSize),
	}
	if len(startupWarnings) > 0 {
		gameOpts = append(gameOpts, ui.WithStartupWarning(strings.Join(startupWarnings, "; ")))
	}

	appCtx, appCancel := context.WithCancel(ctx)
	defer appCancel()

	game := ui.NewGame(appCtx, s, ctrl, sink, gameOpts...)
	defer game.Close()

	if *mprisFlag {
		mprisSrv, mprisErr := mpris.New(ctrl, mpris.WithQuit(appCancel))
		if mprisErr != nil {
			slog.Warn("mpris unavailable; continuing without D-Bus media controls", "err", mprisErr)
		} else {
			defer func() {
				if err := mprisSrv.Close(); err != nil {
					slog.Debug("mpris close failed", "err", err)
				}
			}()
		}
	}

	applyInitialPlaybackState(ctrl, cfg, tracks, queueIdx)

	winW, winH := ui.ScaledWindowSize(ui.WindowWidth, initialCanvasHeight(cfg), cfg.Scale)
	ebiten.SetWindowTitle("ytm-wmp")
	ebiten.SetWindowSize(winW, winH)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeDisabled)
	if err := ebiten.RunGame(game); err != nil {
		return fmt.Errorf("run game: %w", err)
	}
	return nil
}

func checkToolStatus(ctx context.Context, name, overridePath string) ui.ToolStatus {
	tool, err := deps.CheckPath(ctx, name, overridePath)
	if err != nil {
		return ui.ToolStatus{
			Name:       name,
			Configured: overridePath,
			OK:         false,
			Error:      err.Error(),
			Hint:       deps.InstallHint(name),
		}
	}
	return ui.ToolStatus{
		Name:         name,
		Configured:   overridePath,
		ResolvedPath: tool.Path,
		Version:      tool.Version,
		OK:           true,
		Hint:         deps.InstallHint(name),
	}
}

func loadStartupSkin(explicitSkin, savedSkin string) (loaded *skin.Skin, usedPath, warning string, err error) {
	if explicitSkin != "" {
		s, err := skin.Open(explicitSkin)
		return s, explicitSkin, "", err
	}
	if savedSkin != "" {
		s, err := skin.Open(savedSkin)
		if err == nil {
			return s, savedSkin, "", nil
		}
		slog.Warn("saved skin failed to open; falling back to dev-skin", "skin", savedSkin, "err", err)
		warning = fmt.Sprintf("Saved skin %q unavailable (%v); using dev-skin", filepath.Base(savedSkin), err)
	}
	fallback := chooseSkin("")
	s, err := skin.Open(fallback)
	return s, fallback, warning, err
}

func resolveStartupQueue(ctx context.Context, cat catalog.Catalog, flagTargets, extra []string, playlistTarget, initialQuery string, cfg store.Config, configExisted bool) ([]model.Track, int, error) {
	explicit := explicitTracks(flagTargets, extra)
	if playlistTarget != "" {
		lower := strings.ToLower(playlistTarget)
		if strings.HasSuffix(lower, ".m3u") || strings.HasSuffix(lower, ".m3u8") {
			f, err := os.Open(playlistTarget)
			if err != nil {
				return nil, 0, fmt.Errorf("opening M3U playlist %q: %w", playlistTarget, err)
			}
			plTracks, parseErr := m3u.Parse(f)
			_ = f.Close()
			if parseErr != nil {
				return nil, 0, fmt.Errorf("parsing M3U playlist %q: %w", playlistTarget, parseErr)
			}
			explicit = append(explicit, plTracks...)
		} else if cat != nil {
			plTracks, err := cat.Playlist(ctx, playlistTarget)
			if err != nil {
				return nil, 0, fmt.Errorf("loading playlist %q: %w", playlistTarget, err)
			}
			explicit = append(explicit, plTracks...)
		}
	}
	if len(explicit) > 0 {
		return explicit, 0, nil
	}
	if initialQuery != "" {
		return nil, -1, nil
	}
	if configExisted {
		tracks := store.TracksToModel(cfg.Queue)
		idx := cfg.QueueIndex
		if len(tracks) == 0 {
			idx = -1
		}
		return tracks, idx, nil
	}
	return []model.Track{{ID: defaultTarget}}, 0, nil
}

func applyInitialPlaybackState(ctrl *playback.Controller, cfg store.Config, tracks []model.Track, index int) {
	if ctrl == nil {
		return
	}
	ctrl.Send(playback.SetVolume{Volume: cfg.Volume})
	ctrl.Send(playback.SetShuffle{Enabled: cfg.Shuffle})
	ctrl.Send(playback.SetRepeat{Mode: parseRepeatMode(cfg.Repeat)})
	if len(tracks) > 0 {
		ctrl.Send(playback.SetQueue{Tracks: tracks, Index: index})
	}
}

func parseRepeatMode(s string) playback.RepeatMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case store.RepeatAll:
		return playback.RepeatAll
	case store.RepeatOne:
		return playback.RepeatOne
	default:
		return playback.RepeatOff
	}
}

func initialCanvasHeight(cfg store.Config) int {
	h := ui.MainWindowHeight
	if cfg.Windows[store.WindowMain].Shaded {
		h = ui.ShadeWindowHeight
	}
	if cfg.Windows[store.WindowEQ].Visible {
		if cfg.Windows[store.WindowEQ].Shaded {
			h += ui.ShadeWindowHeight
		} else {
			h += ui.EQWindowHeight
		}
	}
	if cfg.Windows[store.WindowPlaylist].Visible {
		if cfg.Windows[store.WindowPlaylist].Shaded {
			h += ui.ShadeWindowHeight
		} else {
			h += ui.PlaylistHeight
		}
	}
	return h
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
	tracks, _, err := resolveStartupQueue(ctx, cat, flagTargets, extra, playlistTarget, initialQuery, store.DefaultConfig(), false)
	return tracks, err
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
