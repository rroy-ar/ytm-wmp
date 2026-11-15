# ytm-wmp

A lightweight YouTube Music player with classic `.wsz` skin support, written in
Go.

Phase 3 (3A) adds catalog search, playlist import, track metadata enrichment,
artwork caching, the `cmd/ytmctl` debug CLI, and an integrated browser panel
below the skinned 275x116 main window (total logical canvas 275x348). Persistence
and the full classic playlist/EQ skin system come in Phases 4-5.

## Project Status

### What has been done

- **Phase 0 — Baseline & Seams**: isolated `internal/model`, `internal/deps`
  startup checks, `internal/ytdlp` stream resolution with a 5-hour video-ID URL
  cache, `internal/audio` `ffmpeg` `f32le` 48 kHz stereo PCM source with bounded
  stderr ring buffer, and offline unit/fake-process tests.
- **Phase 1 — Walking Skeleton**: Ebitengine v2.10.5 UI loop, pure-Go
  `.wsz`/`.zip`/directory skin loader (`internal/skin`) with resource caps and
  modern `skin.xml` rejection, GPU sheet upload and `SubImage` cache
  (`internal/ui/skinview`), immediate-mode hit testing, `ebiten/v2/audio` sink
  with clean EOF/teardown handling, original `assets/dev-skin/`, and `F1` debug
  overlay.
- **Phase 2 — Playback Core**: headless actor `playback.Controller`, state
  transition table, queue with history, non-repeating shuffle bag, repeat modes
  (`off`/`all`/`one`), generation-tagged workers, seek-by-reopen (`-ss`),
  next-track URL prefetching, stale-URL retry, error taxonomy, and full
  transport button/keyboard wiring.
- **Phase 3A — Catalog, Metadata & Artwork**: `catalog.Catalog` interface,
  debounced generation-tagged `Searcher`, `internal/catalog/ytdlp` (`yt-dlp -J`
  search, track metadata, playlist extraction, stdout caps, in-memory caches),
  `internal/artwork` (JPEG/PNG/WebP decode, `64x64` and `300x300` resize,
  atomic disk cache, in-memory LRU, placeholder), `cmd/ytmctl` debug CLI,
  `playback.UpdateTrack` background enrichment, and the integrated browser
  panel with `-query` / `-playlist` flags.

### What's next

- **Phase 3B (optional)**: YouTube Music InnerTube `WEB_REMIX` catalog backend
  in `internal/catalog/ytmusic` (keeping `yt-dlp` for stream resolution).
- **Phase 4 — Classic Skin System**: complete Webamp sprite table, bitmap
  `text.bmp`/`numbers.bmp` rendering, position/volume/balance sliders,
  shuffle/repeat/EQ/PL toggles, `pledit.txt`/`viscolor.txt`, internal draggable
  window manager, skinned playlist (`pledit.bmp`) and equalizer (`eqmain.bmp`)
  windows, and skin loader fuzzing.
- **Phase 5 — Full Player UX & Persistence**: versioned atomic JSON persistence
  for skin, volume, queue, and window positions; M3U import/export; error
  toasts; and settings UI.
- **Phase 6 (optional) & Phase 7**: EQ DSP, spectrum/oscilloscope visualizer,
  shade/2x/`region.txt` modes, performance/allocation profiling, packaging, and
  `v0.1` release hardening.

See [`docs/plan/winamp-ytm-player.md`](docs/plan/winamp-ytm-player.md) for the
detailed plan and [`AGENTS.md`](AGENTS.md) for repo conventions.

## Requirements

- Go 1.27+ (matching `go.mod`)
- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp#installation) on `PATH`
- [`ffmpeg`](https://ffmpeg.org/download.html) on `PATH`
- A desktop with X11 and OpenGL for Ebitengine (Linux first)
- On Debian/Ubuntu, the X11 development headers are needed to compile:

  ```sh
  sudo apt install libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev \
    libxi-dev libxrender-dev libgl1-mesa-dev
  ```

Both tools are checked at startup and their versions are logged. A missing tool
fails before the window opens with an install hint.

## Run

```sh
go run ./cmd/ytm-wmp
```

This opens the player with the original development skin in `assets/dev-skin`
and the proof-of-concept track. Use the mouse to click play, pause, and resume.
Close the window (or press Ctrl+C in the terminal) to stop; shutdown kills
ffmpeg and releases the audio device.

Flags and arguments:

```sh
go run ./cmd/ytm-wmp -url 'https://www.youtube.com/watch?v=...' -url 'https://www.youtube.com/watch?v=...'
go run ./cmd/ytm-wmp -query 'daft punk'
go run ./cmd/ytm-wmp -playlist 'https://www.youtube.com/playlist?list=...'
go run ./cmd/ytm-wmp <track1> <track2>
go run ./cmd/ytm-wmp -skin /path/to/skin.wsz
go run ./cmd/ytm-wmp -skin ./some/loose-bmp-directory
go run ./cmd/ytm-wmp -debug
```

`-url` (repeatable) and positional arguments accept YouTube video IDs, watch/short
URLs, or yt-dlp search queries, forming the initial queue (default: the single
proof-of-concept track unless `-query` or `-playlist` is set). `-query` runs an
initial catalog search on startup, and `-playlist` loads a playlist URL or ID.
`-skin` accepts a `.wsz`, `.zip`, or a directory of loose BMPs. A file is opened
as a ZIP regardless of its extension; modern `.wal` skins (`skin.xml`) are
rejected with a clear message.

Debug CLI (`cmd/ytmctl`):

```sh
go run ./cmd/ytmctl search -limit 10 "daft punk"
go run ./cmd/ytmctl track 0wRYvsfhsR8
go run ./cmd/ytmctl playlist -limit 20 "PL..."
go run ./cmd/ytmctl artwork 0wRYvsfhsR8
```

Controls:

- Mouse: Previous, Play, Pause, Stop, Next, Eject (focuses search) transport
  buttons; browser search bar (`GO`, `CLR`), result rows (click to select,
  click selected row to play), `PLAY`/`QUEUE`/`ALL->Q` actions, mouse wheel
  scroll, and now-playing artwork preview toggle.
- Keyboard: `/` focus search, `Enter` submit search, `Esc` unfocus search/modal,
  `Space` play/pause, `S` stop, `Z`/`B` previous/next, `Left`/`Right` seek ±5s,
  `Up`/`Down` volume ±10% (or move selection while searching), `R` cycle repeat
  (`off`/`all`/`one`), `H` toggle shuffle, `F1` debug overlay.

Press `F1` for the debug overlay: FPS/TPS, playback state, queue index,
position, volume, shuffle/repeat, cursor, sprite destinations (cyan), and widget
hit areas (magenta). Missing sprites render as magenta boxes labelled with
their names.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
go build ./...
```

Ebitengine uses Cgo on Linux, so the X11/GL development packages must be
available to build and test the UI packages.

Layout:

| Path | Responsibility |
|------|----------------|
| `cmd/ytm-wmp` | flags, dependency checks, wiring, signals |
| `cmd/ytmctl` | catalog search, track, playlist, and artwork debug CLI |
| `internal/model` | shared domain types (`Track`, `StreamInfo`) |
| `internal/ytdlp` | yt-dlp stream resolver, video ID normalization, URL cache |
| `internal/catalog` | `Catalog` interface, target classification, debounced `Searcher` |
| `internal/catalog/ytdlp` | `yt-dlp -J` catalog implementation and in-memory caches |
| `internal/artwork` | thumbnail fetch, JPEG/PNG/WebP decode, resize, disk + LRU cache |
| `internal/audio` | ffmpeg PCM source and Ebitengine audio sink |
| `internal/playback` | actor controller, queue, state machine, error taxonomy |
| `internal/deps` | external tool discovery and versions |
| `internal/skin` | pure `.wsz`/`.zip`/directory loader and sprite table |
| `internal/ui` | game loop, immediate-mode input, layout, catalog browser |
| `internal/ui/skinview` | one-time GPU upload and sprite cache |
| `assets/dev-skin` | original loose-BMP development skin |
| `docs/plan` | delivery plan |
| `docs/adr` | architecture decisions |
| `AGENTS.md` | repository guide and workflow rules for agents |

## House rules

- No binaries, signed stream URLs, cookies, or copyrighted skins in git.
- Keep skins for testing in a local, gitignored `skins/` directory.
