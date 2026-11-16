// Command ytmctl is a debug CLI for exercising catalog search, track metadata,
// playlist extraction, and artwork caching without launching the GUI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"ytm-wmp/internal/artwork"
	"ytm-wmp/internal/catalog"
	catalogytdlp "ytm-wmp/internal/catalog/ytdlp"
	catalogytmusic "ytm-wmp/internal/catalog/ytmusic"
	"ytm-wmp/internal/deps"
	"ytm-wmp/internal/model"
)

type catalogFactory func(ctx context.Context, backend string) (catalog.Catalog, error)

type catalogSelection struct {
	defaultBackend string
	explicit       bool
	injected       catalog.Catalog
	build          catalogFactory
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := execute(ctx, os.Args[1:], os.Stdout, os.Stderr, nil, nil); err != nil {
		fmt.Fprintf(os.Stderr, "ytmctl: %v\n", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer, cat catalog.Catalog, art *artwork.Cache) error {
	return executeWithFactory(ctx, args, stdout, stderr, cat, art, defaultCatalogFactory)
}

func executeWithFactory(ctx context.Context, args []string, stdout, stderr io.Writer, cat catalog.Catalog, art *artwork.Cache, factory catalogFactory) error {
	if factory == nil {
		factory = defaultCatalogFactory
	}

	globalFS := flag.NewFlagSet("ytmctl", flag.ContinueOnError)
	globalFS.SetOutput(stderr)
	globalCat := globalFS.String("catalog", "ytmusic", "catalog backend: ytmusic or ytdlp")
	if err := globalFS.Parse(args); err != nil {
		return err
	}
	globalExplicit := flagSetExplicitly(globalFS, "catalog")
	backend, err := normalizeCatalogBackend(*globalCat)
	if err != nil {
		return err
	}

	rest := globalFS.Args()
	if len(rest) == 0 {
		printUsage(stderr)
		return errors.New("missing subcommand")
	}

	sel := catalogSelection{
		defaultBackend: backend,
		explicit:       globalExplicit,
		injected:       cat,
		build:          factory,
	}

	subcmd := rest[0]
	subArgs := rest[1:]

	switch subcmd {
	case "search":
		return runSearch(ctx, subArgs, stdout, stderr, sel)
	case "track":
		return runTrack(ctx, subArgs, stdout, stderr, sel)
	case "playlist":
		return runPlaylist(ctx, subArgs, stdout, stderr, sel)
	case "artwork":
		return runArtwork(ctx, subArgs, stdout, stderr, sel, art)
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown subcommand %q", subcmd)
	}
}

func defaultCatalogFactory(ctx context.Context, backend string) (catalog.Catalog, error) {
	name, err := normalizeCatalogBackend(backend)
	if err != nil {
		return nil, err
	}
	switch name {
	case "ytmusic":
		return catalogytmusic.New(), nil
	case "ytdlp":
		tool, err := deps.Check(ctx, "yt-dlp")
		if err != nil {
			return nil, err
		}
		return catalogytdlp.New(catalogytdlp.WithBinary(tool.Path)), nil
	default:
		return nil, fmt.Errorf("unknown catalog backend %q (want ytmusic or ytdlp)", backend)
	}
}

func normalizeCatalogBackend(backend string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(backend))
	switch name {
	case "", "ytmusic":
		return "ytmusic", nil
	case "ytdlp":
		return "ytdlp", nil
	default:
		return "", fmt.Errorf("unknown catalog backend %q (want ytmusic or ytdlp)", backend)
	}
}

func flagSetExplicitly(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func (s catalogSelection) resolve(ctx context.Context, fs *flag.FlagSet, subFlag string) (catalog.Catalog, error) {
	backend, err := normalizeCatalogBackend(subFlag)
	if err != nil {
		return nil, err
	}
	subExplicit := flagSetExplicitly(fs, "catalog")
	if s.injected != nil && !s.explicit && !subExplicit {
		return s.injected, nil
	}
	cat, err := s.build(ctx, backend)
	if err != nil {
		return nil, err
	}
	if cat == nil {
		return nil, errors.New("catalog unavailable")
	}
	return cat, nil
}

func runSearch(ctx context.Context, args []string, stdout, stderr io.Writer, sel catalogSelection) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	limit := fs.Int("limit", catalog.DefaultSearchLimit, "maximum search results")
	asJSON := fs.Bool("json", false, "output JSON")
	catFlag := fs.String("catalog", sel.defaultBackend, "catalog backend: ytmusic or ytdlp")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return errors.New("usage: ytmctl search [-catalog ytmusic|ytdlp] [-limit 10] [-json] <query...>")
	}
	cat, err := sel.resolve(ctx, fs, *catFlag)
	if err != nil {
		return err
	}

	tracks, err := cat.Search(ctx, query, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, tracks)
	}
	return writeTracks(stdout, tracks)
}

func runTrack(ctx context.Context, args []string, stdout, stderr io.Writer, sel catalogSelection) error {
	fs := flag.NewFlagSet("track", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output JSON")
	catFlag := fs.String("catalog", sel.defaultBackend, "catalog backend: ytmusic or ytdlp")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: ytmctl track [-catalog ytmusic|ytdlp] [-json] <id-or-url>")
	}
	cat, err := sel.resolve(ctx, fs, *catFlag)
	if err != nil {
		return err
	}

	tr, err := cat.Track(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, tr)
	}
	return writeTracks(stdout, []model.Track{tr})
}

func runPlaylist(ctx context.Context, args []string, stdout, stderr io.Writer, sel catalogSelection) error {
	fs := flag.NewFlagSet("playlist", flag.ContinueOnError)
	fs.SetOutput(stderr)
	limit := fs.Int("limit", catalogytdlp.DefaultPlaylistLimit, "maximum playlist items to display")
	asJSON := fs.Bool("json", false, "output JSON")
	catFlag := fs.String("catalog", sel.defaultBackend, "catalog backend: ytmusic or ytdlp")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: ytmctl playlist [-catalog ytmusic|ytdlp] [-limit 100] [-json] <id-or-url>")
	}
	cat, err := sel.resolve(ctx, fs, *catFlag)
	if err != nil {
		return err
	}

	tracks, err := cat.Playlist(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *limit > 0 && len(tracks) > *limit {
		tracks = tracks[:*limit]
	}
	if *asJSON {
		return writeJSON(stdout, tracks)
	}
	return writeTracks(stdout, tracks)
}

func runArtwork(ctx context.Context, args []string, stdout, stderr io.Writer, sel catalogSelection, art *artwork.Cache) error {
	fs := flag.NewFlagSet("artwork", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cacheDir := fs.String("cache-dir", "", "artwork disk cache directory")
	catFlag := fs.String("catalog", sel.defaultBackend, "catalog backend: ytmusic or ytdlp")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return errors.New("usage: ytmctl artwork [-catalog ytmusic|ytdlp] [-cache-dir dir] <video-id> [thumbnail-url]")
	}
	if _, err := normalizeCatalogBackend(*catFlag); err != nil {
		return err
	}

	videoID := strings.TrimSpace(fs.Arg(0))
	if vid := model.VideoID(videoID); vid != "" {
		videoID = vid
	}
	thumbURL := ""
	if fs.NArg() == 2 {
		thumbURL = strings.TrimSpace(fs.Arg(1))
	}

	if *cacheDir != "" {
		art = artwork.NewCache(artwork.WithCacheDir(*cacheDir))
	} else if art == nil {
		art = artwork.NewCache(artwork.WithCacheDir(defaultArtworkDir()))
	}

	if thumbURL == "" {
		if cat, err := sel.resolve(ctx, fs, *catFlag); err == nil && cat != nil {
			if tr, err := cat.Track(ctx, videoID); err == nil {
				thumbURL = tr.ThumbnailURL
			}
		} else if err != nil && (sel.explicit || flagSetExplicitly(fs, "catalog")) {
			return err
		}
	}
	if thumbURL == "" {
		thumbURL = model.DefaultThumbnailURL(videoID)
	}

	b, err := art.Get(ctx, videoID, thumbURL)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "video_id=%s list=%dx%d now_playing=%dx%d\n",
		b.VideoID,
		b.List.Bounds().Dx(), b.List.Bounds().Dy(),
		b.NowPlaying.Bounds().Dx(), b.NowPlaying.Bounds().Dy(),
	)
	return err
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeTracks(w io.Writer, tracks []model.Track) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tID\tDUR\tARTIST\tTITLE\tTHUMBNAIL")
	for i, tr := range tracks {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			i+1,
			tr.ID,
			formatDuration(tr.Duration),
			tr.Artist,
			tr.Title,
			tr.ThumbnailURL,
		)
	}
	return tw.Flush()
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "--:--"
	}
	total := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

func defaultArtworkDir() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "ytm-wmp", "artwork")
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: ytmctl [-catalog ytmusic|ytdlp] <search|track|playlist|artwork> [flags] <args>")
}
