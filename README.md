# ytm-wmp

A lightweight YouTube Music player with classic `.wsz` skin support, written in
Go.

Phase 5 delivers full player UX and versioned atomic JSON persistence
(`config.json`): cold-start restore of skin, volume, balance, queue, window
layout, scale (`1X`/`2X`), audio buffer, artwork cache limits, tool paths, and
last search; browser tabs (`SONGS`/`PLAYLST`/`ALBUMS`/`ARTISTS`/`QUEUE`) with
artist/album/playlist detail views; caret/IME search input; dismissible
error/status toasts with missing-dependency alerts; and an in-app Settings panel.

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
- **Phase 3.5 — Hardening & Cleanup**: centralized YouTube ID helpers
  (`VideoID`, `IsVideoID`, `NormalizeTrackID`, `DefaultThumbnailURL`) in
  `internal/model` (decoupling `playback`, `ui`, and `catalog` from `ytdlp`),
  fixed cold-cache `ytmctl artwork`, bounded stdout/stderr in
  `internal/ytdlp.ExecRunner`, eliminated per-frame `Snapshot.Queue` cloning,
  prevented playlist searches from clobbering active playback, added lazy
  `300x300` GPU texture uploads with `Deallocate()`, bounded background
  enrichment/artwork workers, and extracted `browserPanel` + layout bounds.
- **Phase 3B — InnerTube `WEB_REMIX` Catalog**: pure-Go YouTube Music InnerTube
  backend in `internal/catalog/ytmusic` (`search`, `next`, `browse`) with
  response byte caps, search/track TTL caches, browse/playlist URL target
  detection, offline `WEB_REMIX` JSON fixture tests plus `YTM_LIVE=1` live
  tests, and `-catalog ytmusic|ytdlp` selection (default `ytmusic`) in
  `cmd/ytm-wmp` and `cmd/ytmctl` while keeping `yt-dlp` for stream resolution.

- **Phase 4 — Classic Skin System**: ported Webamp's complete `skinSprites.ts`
  sprite table and `FONT_LOOKUP`, added `pledit.txt`/`viscolor.txt` parsing,
  `OpenBytes`/`OpenFS`, and `FuzzSkinOpen`, extended `assets/dev-skin/` with
  full Phase 4 assets, added `internal/m3u` import/export and queue
  removal/sort commands, built the docked/stacked Winamp rack (`Main 275x116`,
  toggleable `EQ 275x116`, toggleable `Playlist/Browser 275x232`), main
  window bitmap marquee/time digits/volume/balance/seek/shuffle/repeat/EQ/PL
  controls, skinned `pledit.bmp` playlist editor with popup menus, and skinned
  `eqmain.bmp` equalizer with atomic `eq.json` persistence.
- **Phase 5 — Full Player UX & Persistence**: added `internal/store` for
  versioned, debounced, atomic `config.json` persistence (`TrackRef` queue,
  skin, volume, balance, scale, audio buffer, artwork cache dir/cap, window
  layout, last search, tool paths) and cold-start restore; added `TabCatalog`
  (`SONGS`/`PLAYLST`/`ALBUMS`/`ARTISTS`) and collection detail views with back
  stack; added an in-browser `QUEUE` tab (`MoveTrack` reorder, remove, clear);
  added `textField` caret navigation and Ebitengine `exp/textinput.Composer`
  IME support; added dismissible error/status toasts and actionable missing
  dependency alerts (`deps.CheckPath`); and built the in-app Settings panel
  (`SET` / `F2`) for skin, `1X`/`2X` scale, audio buffer, artwork cache
  stats/prune/clear, and `yt-dlp`/`ffmpeg` path configuration.

### What's next

- **Phase 6 (optional) & Phase 7**: EQ DSP, spectrum/oscilloscope visualizer,
  shade/`region.txt` modes, performance/allocation profiling, multi-skin corpus
  verification, packaging, and `v0.1` release hardening.

See [`docs/plan/winamp-ytm-player.md`](docs/plan/winamp-ytm-player.md) for the
detailed plan and [`AGENTS.md`](AGENTS.md) for repo conventions.

## Requirements

- Go 1.27+ (matching `go.mod`)
- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp#installation) on `PATH`
- [`ffmpeg`](https://ffmpeg.org/download.html) on `PATH`
- A Linux desktop with X11 and OpenGL for Ebitengine (Windows and macOS are out of scope)
- On Debian/Ubuntu, the X11 development headers are needed to compile:

  ```sh
  sudo apt install libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev \
    libxi-dev libxrender-dev libgl1-mesa-dev
  ```

Both tools are checked at startup (using configured Settings paths or `PATH`)
and their versions are logged. If a tool is missing, the player still opens and
surfaces an actionable dependency alert toast with a direct link to Settings so
custom binary paths can be configured without a terminal.

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
go run ./cmd/ytm-wmp -catalog ytdlp -query 'daft punk'
go run ./cmd/ytm-wmp -playlist 'https://music.youtube.com/playlist?list=...'
go run ./cmd/ytm-wmp -autoplay=false -url '0wRYvsfhsR8'
go run ./cmd/ytm-wmp <track1> <track2>
go run ./cmd/ytm-wmp -skin /path/to/skin.wsz
go run ./cmd/ytm-wmp -skin ./some/loose-bmp-directory
go run ./cmd/ytm-wmp -debug
```

`-url` (repeatable) and positional arguments accept YouTube video IDs, watch/short
URLs, or yt-dlp search queries, forming the initial queue (default: the single
proof-of-concept track unless `-query` or `-playlist` is set). `-catalog` chooses
the metadata/search backend (`ytmusic`, the pure-Go InnerTube `WEB_REMIX` default,
or `ytdlp`). Even with `-catalog ytmusic`, prefixing a search query with `yt:` or
`ytdlp:` searches regular YouTube via `yt-dlp` for videos not indexed on YouTube
Music. `-autoplay` (default `true` when using `ytmusic`) automatically queues
unauthenticated YouTube Music Up Next / radio suggestions (`RDAMVM...`) when the
queue ends with repeat off. `-query` runs an initial catalog search on startup, and `-playlist`
loads a playlist, album URL/ID, or local `.m3u`/`.m3u8` file. `-skin` accepts a `.wsz`, `.zip`, or a
directory of loose BMPs. A file is opened as a ZIP regardless of its extension;
modern `.wal` skins (`skin.xml`) are rejected with a clear message.

Debug CLI (`cmd/ytmctl`):

```sh
go run ./cmd/ytmctl search -limit 10 "daft punk"
go run ./cmd/ytmctl -catalog ytdlp search -limit 10 "daft punk"
go run ./cmd/ytmctl track 0wRYvsfhsR8
go run ./cmd/ytmctl playlist -limit 20 "PL..."
go run ./cmd/ytmctl radio -limit 20 0wRYvsfhsR8
go run ./cmd/ytmctl artwork 0wRYvsfhsR8
```

Controls:

- Mouse: Previous, Play, Pause, Stop, Next, Eject (opens/focuses catalog
  search) transport buttons; main window time display (click to toggle
  elapsed/remaining), volume/balance/seek bars, `SHUF`/`REP`/`EQ`/`PL` toggles,
  skin picker button (`SK` to the right of `REP`, opens the OS file explorer to
  hot-reload a `.wsz`/`.zip` skin), and close button; EQ window `ON`/`AUTO`/`PRESETS`/`+12dB`/`0dB`/`-12dB` and
  preamp/10-band sliders; skinned Playlist window (row
  click/`Ctrl+Click`/`Shift+Click`/double-click to play, scrollbar/wheel,
  `ADD`/`REM`/`SEL`/`MISC`/`LIST` popup menus including M3U save/load, mini
  transport controls); browser search bar (`PL` toggle, `GO`, `CLR`,
  middle/right-click search box to paste), result rows (double left-click
  to clear queue & play / open collection, right-click to add to queue),
  `PLAY`/`QUEUE`/`ALL->Q`/`AUTO` actions, browser tabs (`SONGS`/`PLAYLST`/`ALBUMS`/`ARTISTS`/`QUEUE`)
  with playlist/album/artist detail views (`<BACK`), Settings panel (`SET`),
  toast `[X]`/`SETTINGS` buttons, and now-playing artwork preview toggle.
- Keyboard: `/` focus search (`Left`/`Right`/`Home`/`End`/`Delete`/`Backspace`
  caret editing + IME), `E` toggle EQ window, `P` toggle Playlist/Browser
  panel, `F2` toggle Settings panel, `Delete` remove selected playlist rows,
  `Ctrl+A` select all playlist rows, `Ctrl+V` / `Super+V` / `Shift+Insert`
  paste into search (or import M3U/URLs when Playlist is focused), `Ctrl+U` /
  `Ctrl+Backspace` clear search while focused, `Enter` submit search, `Esc`
  unfocus search/modal/menu, dismiss active toast, or close catalog, `Space` play/pause, `S`
  stop, `Z`/`B` previous/next, `Left`/`Right` seek ±5s, `Up`/`Down` volume
  ±10% (or move selection while searching), `R` cycle repeat (`off`/`all`/
  `one`), `H` toggle shuffle, `A` toggle Up Next radio autoplay, `F1` debug
  overlay.

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
| `internal/model` | shared domain types (`Track`, `StreamInfo`) and YouTube ID helpers |
| `internal/ytdlp` | yt-dlp `-g` stream resolver with bounded output and 5-hour URL cache |
| `internal/catalog` | `Catalog` interface, target classification, debounced `Searcher` |
| `internal/catalog/ytdlp` | `yt-dlp -J` catalog implementation and in-memory caches |
| `internal/catalog/ytmusic` | Pure-Go YouTube Music InnerTube `WEB_REMIX` catalog backend |
| `internal/artwork` | thumbnail fetch, JPEG/PNG/WebP decode, resize, disk + LRU cache, disk stats/pruning |
| `internal/store` | versioned atomic `config.json` persistence, migration, and debounced saver |
| `internal/m3u` | `#EXTM3U` import/export of YouTube URLs and video IDs |
| `internal/audio` | ffmpeg PCM source and Ebitengine audio sink |
| `internal/playback` | actor controller, queue, state machine, error taxonomy |
| `internal/deps` | external tool discovery and versions |
| `internal/skin` | pure `.wsz`/`.zip`/directory loader, `pledit.txt`/`viscolor.txt`, sprite table |
| `internal/ui` | game loop, immediate-mode input, docked rack (Main/EQ/Playlist/Browser) |
| `internal/ui/skinview` | one-time GPU upload and sprite cache |
| `assets/dev-skin` | original loose-BMP development skin |
| `docs/plan` | delivery plan |
| `docs/adr` | architecture decisions (`0001` audio, `0002` window model, `0003` actor & catalog split) |
| `AGENTS.md` | repository guide and workflow rules for agents |

## House rules

- No binaries, signed stream URLs, cookies, or copyrighted skins in git.
- Keep skins for testing in a local, gitignored `skins/` directory.
