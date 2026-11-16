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
	"ytm-wmp/internal/deps"
	"ytm-wmp/internal/model"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	args := os.Args[1:]
	var (
		cat catalog.Catalog
		art *artwork.Cache
	)
	if len(args) > 0 {
		switch args[0] {
		case "search", "track", "playlist":
			tool, err := deps.Check(ctx, "yt-dlp")
			if err != nil {
				fmt.Fprintf(os.Stderr, "ytmctl: %v\n", err)
				os.Exit(1)
			}
			cat = catalogytdlp.New(catalogytdlp.WithBinary(tool.Path))
		case "artwork":
			art = artwork.NewCache(artwork.WithCacheDir(defaultArtworkDir()))
			if tool, err := deps.Check(ctx, "yt-dlp"); err == nil {
				cat = catalogytdlp.New(catalogytdlp.WithBinary(tool.Path))
			}
		}
	}

	if err := execute(ctx, args, os.Stdout, os.Stderr, cat, art); err != nil {
		fmt.Fprintf(os.Stderr, "ytmctl: %v\n", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer, cat catalog.Catalog, art *artwork.Cache) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("missing subcommand")
	}

	subcmd := args[0]
	rest := args[1:]

	switch subcmd {
	case "search":
		return runSearch(ctx, rest, stdout, stderr, cat)
	case "track":
		return runTrack(ctx, rest, stdout, stderr, cat)
	case "playlist":
		return runPlaylist(ctx, rest, stdout, stderr, cat)
	case "artwork":
		return runArtwork(ctx, rest, stdout, stderr, cat, art)
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown subcommand %q", subcmd)
	}
}

func runSearch(ctx context.Context, args []string, stdout, stderr io.Writer, cat catalog.Catalog) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	limit := fs.Int("limit", catalog.DefaultSearchLimit, "maximum search results")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return errors.New("usage: ytmctl search [-limit 10] [-json] <query...>")
	}
	if cat == nil {
		return errors.New("catalog unavailable")
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

func runTrack(ctx context.Context, args []string, stdout, stderr io.Writer, cat catalog.Catalog) error {
	fs := flag.NewFlagSet("track", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: ytmctl track [-json] <id-or-url>")
	}
	if cat == nil {
		return errors.New("catalog unavailable")
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

func runPlaylist(ctx context.Context, args []string, stdout, stderr io.Writer, cat catalog.Catalog) error {
	fs := flag.NewFlagSet("playlist", flag.ContinueOnError)
	fs.SetOutput(stderr)
	limit := fs.Int("limit", catalogytdlp.DefaultPlaylistLimit, "maximum playlist items to display")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: ytmctl playlist [-limit 100] [-json] <id-or-url>")
	}
	if cat == nil {
		return errors.New("catalog unavailable")
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

func runArtwork(ctx context.Context, args []string, stdout, stderr io.Writer, cat catalog.Catalog, art *artwork.Cache) error {
	fs := flag.NewFlagSet("artwork", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cacheDir := fs.String("cache-dir", "", "artwork disk cache directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return errors.New("usage: ytmctl artwork [-cache-dir dir] <video-id> [thumbnail-url]")
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

	if thumbURL == "" && cat != nil {
		if tr, err := cat.Track(ctx, videoID); err == nil {
			thumbURL = tr.ThumbnailURL
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
	fmt.Fprintln(w, "usage: ytmctl <search|track|playlist|artwork> [flags] <args>")
}
