# ytm-wmp

A lightweight YouTube Music player with classic `.wsz` skin support, written in
Go.

Phase 1 is the walking skeleton: Ebitengine opens a 275x116 skinned main window,
the play and pause buttons drive a hardcoded track, and audio flows from a live
`yt-dlp`/`ffmpeg` pipe through Ebitengine's audio context. Search, queueing, and
the full skin system come in later phases.

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
go run ./cmd/ytm-wmp <track1> <track2>
go run ./cmd/ytm-wmp -skin /path/to/skin.wsz
go run ./cmd/ytm-wmp -skin ./some/loose-bmp-directory
go run ./cmd/ytm-wmp -debug
```

`-url` (repeatable) and positional arguments accept YouTube video IDs, watch/short
URLs, or yt-dlp search queries, forming the initial queue (default: the single
proof-of-concept track). `-skin` accepts a `.wsz`, `.zip`, or a directory of
loose BMPs. A file is opened as a ZIP regardless of its extension; modern `.wal`
skins (`skin.xml`) are rejected with a clear message.

Controls:

- Mouse: Previous, Play, Pause, Stop, Next transport buttons.
- Keyboard: `Space` play/pause, `S` stop, `Z`/`B` previous/next, `Left`/`Right`
  seek ±5s, `Up`/`Down` volume ±10%, `R` cycle repeat (`off`/`all`/`one`), `H`
  toggle shuffle, `F1` debug overlay.

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
| `internal/model` | shared domain types |
| `internal/ytdlp` | yt-dlp invocation and stream URL cache |
| `internal/audio` | ffmpeg PCM source and Ebitengine audio sink |
| `internal/playback` | resolve → open → play orchestration |
| `internal/deps` | external tool discovery and versions |
| `internal/skin` | pure `.wsz`/`.zip`/directory loader and sprite table |
| `internal/ui` | game loop, immediate-mode input, layout |
| `internal/ui/skinview` | one-time GPU upload and sprite cache |
| `assets/dev-skin` | original loose-BMP development skin |
| `docs/plan` | delivery plan |
| `docs/adr` | architecture decisions |

## House rules

- No binaries, signed stream URLs, cookies, or copyrighted skins in git.
- Keep skins for testing in a local, gitignored `skins/` directory.
