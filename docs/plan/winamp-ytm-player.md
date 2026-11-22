# Plan: from the PoC to a full YouTube Music player with classic `.wsz` skins

Status: active execution plan. Phases 0, 1, 2, 3A, 3.5 cleanup, 3B, 4
(complete classic skin system), 5 (full player UX and persistence), 6A (EQ DSP),
6B (spectrum/oscilloscope visualizer), 6C partial (shade mode + double-size `D`
button), and 6E partial (Linux MPRIS2 D-Bus media controls) are complete;
remaining optional Phase 6 chunks (`region.txt` masks, 6D, gapless in 6E) and
Phase 7 (hardening and v0.1 release) remain. This document is the single source
of truth for architecture, status, and upcoming milestones.

Last updated: 2026-11-21.

## 1. Where the project is today

Phases 1–6C (partial) turned the PoC into a multi-track, catalog-backed, fully
skinned desktop player with clear architectural seams, pure-Go YouTube Music
InnerTube (`WEB_REMIX`) and `yt-dlp` catalog backends, classic main, playlist,
and equalizer chrome (including WindowShade mode), versioned `config.json`
persistence, live 10-band biquad EQ DSP, and a `viscolor.txt`-styled
spectrum/oscilloscope visualizer.

Implemented and covered by tests:

- `cmd/ytm-wmp` is wiring only: `-url`, `-skin`, `-catalog`, `-query`,
  `-playlist`, `-autoplay`, `-debug`, dependency checks (`deps.CheckPath`),
  `config.json` cold-start restore, signal handling, skin loading,
  catalog/artwork wiring, and `ebiten.RunGame`.
- `cmd/ytmctl` provides a debug CLI for `search`, `track`, `playlist`, `radio`,
  and `artwork` with `-catalog ytmusic|ytdlp` (resolving cold-cache artwork via
  `catalog.Catalog` or `model.DefaultThumbnailURL`).
- Ebitengine v2.10.5 opens the docked/stacked Winamp rack (`Main 275x116` or
  `14px` shade, toggleable `EQ 275x116` or `14px` shade, toggleable
  `Playlist/Browser 275x232` or `14px` shade) scaled in `0.10x` increments from
  `1.0x` to `2.0x` (plus `D` / `Ctrl+D` double-size toggle).
- `internal/model` owns shared domain types (`Track`, `StreamInfo`) and pure
  YouTube ID helpers (`VideoID`, `IsVideoID`, `NormalizeTrackID`,
  `DefaultThumbnailURL`).
- `internal/deps` performs `yt-dlp` / `ffmpeg` discovery and version checks,
  supporting both `PATH` lookup and custom user-configured binary paths
  (`CheckPath`).
- `internal/catalog` defines the `Catalog` interface, `RadioProvider`,
  `TabCatalog` (`SONGS`/`PLAYLST`/`ALBUMS`/`ARTISTS` + collection detail
  browsing), `IsPlaylistTarget` (playlist URLs/IDs plus YTM `/browse/...`,
  `VL...`, `MPREb_...`, and `RDCLAK5uy_...` targets), and a debounced,
  generation-tagged async `Searcher` (including direct single-track resolution
  via `Catalog.Track` for pasted YouTube watch URLs and `yt:` / `ytdlp:` prefix
  routing to the regular YouTube `yt-dlp` fallback catalog);
  `internal/catalog/ytmusic` implements pure-Go YouTube Music InnerTube
  `WEB_REMIX` (`search`, `next`, `browse`, tab search, collection/artist detail,
  plus unauthenticated `RDAMVM...` Up Next radio via `catalog.RadioProvider`)
  with response byte caps and search/track/radio TTL caches;
  `internal/catalog/ytdlp` implements `yt-dlp -J` search, track metadata, and
  playlist extraction with stdout caps and search/track caches.
- `internal/artwork` fetches, decodes (JPEG/PNG/WebP), resizes (64x64 list and
  300x300 now-playing), and caches thumbnails on disk and in an in-memory LRU,
  with disk cache `Stats`, `PruneDisk`, and `ClearDisk` management.
- `internal/store` implements versioned (`CurrentVersion = 1`) atomic
  `config.json` persistence (`Migrate`, `Sanitize`, `SanitizeScale`),
  `TrackRef` queue serialization (stripping signed URLs), `WindowState`
  visibility/shade state, `DebouncedSaver` (`500 ms` + flush on close), and
  `AtomicWriteFile`.
- `internal/skin` loads `.wsz`, `.zip`, `fs.FS`, or a directory of loose BMPs
  plus `pledit.txt` and `viscolor.txt`. It normalizes case, backslashes, and
  nested paths, rejects modern `skin.xml` skins, caps per-asset/total bytes,
  total pixels, and ZIP entries, handles 32-bit all-zero-alpha BMPs, provides
  the complete Webamp `skinSprites.ts` sprite table (including `eq_ex.bmp` and
  main/playlist/EQ shade sprites), slider-frame rects, and `text.bmp` glyph
  lookup, and is fuzzed via `FuzzSkinOpen`.
- `internal/ui/skinview` uploads each sheet once (and releases GPU textures via
  `View.Deallocate()` on skin reload/shutdown) and caches `SubImage` textures,
  28-frame volume/balance/EQ slider strips (tolerating partial slider sheets,
  missing `balance.bmp` via `volume.bmp` fallback, and optional slider thumbs),
  `EQ_GRAPH_LINE_COLORS`, `text.bmp` bitmap glyphs, `pledit.txt` styles, and
  `viscolor.txt` palettes. Missing or truncated sprites become labelled magenta
  placeholders.
- `internal/m3u` parses and formats `#EXTM3U` playlists of YouTube URLs/IDs.
- `internal/mpris` implements a pure-Go Linux MPRIS2 D-Bus server
  (`org.mpris.MediaPlayer2.ytm_wmp`, falling back to `.instance<pid>`) wired
  to `playback.Controller.Subscribe()`, exposing root/player methods
  (`Play`, `Pause`, `PlayPause`, `Stop`, `Next`, `Previous`, `Seek`,
  `SetPosition`, `OpenUri`, `Quit`), dynamic `Position` and metadata/artwork
  properties, writable `LoopStatus`/`Shuffle`/`Volume`, and `PropertiesChanged`
  / `Seeked` signals (`-mpris`, enabled by default, degrading gracefully when
  no session bus is available).
- `internal/ui` implements the docked/stacked Winamp rack (`Main 275x116`,
  toggleable `EQ 275x116`, toggleable `Playlist/Browser 275x232`) with discrete
  OS window resizes on `EQ`/`PL`/shade/scale changes, WindowShade `14px`
  collapsed bars for Main/EQ/Playlist, clutter-bar `D` double-size toggle
  (`1X` <-> `2X` / `Ctrl+D`), active/inactive titlebars, main window bitmap
  marquee/time digits/volume/balance/seek/shuffle/repeat/EQ/PL controls, a
  live `viscolor.txt` spectrum analyzer & oscilloscope (`vis.go`, click to cycle
  Spectrum -> Oscilloscope -> Off), a main-window skin picker button (`SK` to
  the right of `REP`) that opens the OS file dialog (`zenity`/`yad`/`qarma`/
  `kdialog`) and hot-reloads `.wsz`/`.zip` skins off the UI thread, skinned
  `pledit.bmp` playlist editor (virtualized rows, click/Ctrl-click/Shift-click
  selection, double-click play, scrollbar/wheel, `ADD`/`REM`/`SEL`/`MISC`/
  `LIST` popup menus, M3U import/export), skinned `eqmain.bmp` equalizer
  (preamp + 10 bands wired live to `audio.EQReader`, response graph, presets,
  atomic `eq.json` persistence), `browserPanel` catalog search/tabs
  (`SONGS`/`PLAYLST`/`ALBUMS`/`ARTISTS`/`QUEUE`), detail drill-down (`<BACK`),
  right-click/direct collection queueing, `ALL->Q` append, artwork preview,
  caret/IME `textField`, clipboard paste (`Ctrl+V` / `Super+V` / `Shift+Insert`
  / middle- or right-click), `Esc` to close catalog/modals/toasts, dismissible
  toasts with missing-dependency alerts, in-app Settings panel (`SET`/`F2`),
  and an `F1` debug overlay (FPS/TPS, playback state, queue/pos, volume,
  shuffle/repeat/autoplay, track ID, skin name, missing sprite count, cursor,
  and hit/sprite rects).
- `internal/playback` runs the Phase 2 actor (`Controller`), state machine,
  queue (history, shuffle bag, repeat off/one/all, `RemoveIndices`, `MoveTrack`,
  `SortQueue`), request-generation tracking, seek-by-reopen
  (`baseOffset + sink.Position()`), zero-copy read-only `Snapshot.Queue`, URL
  prefetching, optional `RadioProvider` queue-end autoplay (with pre-EOF radio +
  stream URL prefetching), and error taxonomy (`ErrToolMissing`,
  `ErrStaleStream`, `ErrFFmpeg`, `ErrNetwork`, `ErrUnavailable`, `ErrCanceled`).
- `internal/audio` runs ffmpeg with `f32le`, stereo, 48 kHz PCM (returning
  clean `io.EOF` on pipe teardown), logs `first_pcm_ms`, pipes through
  `EQReader` (10-band peaking biquad EQ + preamp) and `VisTapReader` (`VisRing`
  + 1024-pt Hann FFT `ComputeSpectrum`), and exposes `SetVolume`/`Volume`/
  `Position`/`SetBufferSize`/`SetEQ`/`ReadVisSamples` on `EbitenSink`.
- `internal/ytdlp` resolves stream URLs (`yt-dlp -g`) with bounded stdout/stderr
  and caches them for five hours keyed by video ID; a zero-frame stream is
  treated as stale and retried once with a fresh URL.
- Shutdown cancels the playback actor, waits for all worker goroutines and
  ffmpeg processes, flushes `config.json` / `eq.json`, deallocates cached GPU
  textures, and closes the audio sink. Natural EOF waits for a normal ffmpeg exit
  instead of killing in the EOF window.
- `assets/dev-skin/` contains original BMPs at classic dimensions (including
  Phase 4 sheets and Phase 6C `eq_ex.bmp` / shade / clutter-bar sprites). No
  external skin art is committed.

What is still missing:

- Remaining optional Phase 6 fidelity chunks: `region.txt` polygon masks (rest
  of 6C), skin browser/cursors (6D), and gapless playback (rest of 6E).
- Phase 7 hardening: real `.wsz` skin corpus smoke verification, performance
  profiling, packaging, Linux CI, and `v0.1` release.

Keep the good parts: process cancellation, the stale-URL retry, the PCM pipe,
and the short time-to-first-audio. Do not rewrite them into a framework.

## 2. Definition of "full player" (v1)

This is already a large v1. Do not add to it without cutting something else.

- [x] Search YouTube Music (`yt-dlp` in 3A; InnerTube `WEB_REMIX` in 3B) or paste
      a watch/playlist URL (M3U import/export landed in Phase 4).
- [x] Queue with play, pause, stop, next, previous, seek, volume, shuffle, repeat.
- [x] Metadata, duration, and artwork for the current track.
- [x] Classic `.wsz` main window, playlist, and equalizer chrome.
- [x] Working skinned controls and playlist interaction.
- [x] Persist skin, volume, queue, playlists, and window positions.
- [x] Recover from expired URLs, network failures, and missing tools with
      actionable errors.
- [x] Run on Linux.

Explicitly out of scope for v1:

- Windows and macOS compilation/support (Linux only).

- Modern `.wal` skins (XML + Maki). Detect and reject with a clear message.
- AVS/MilkDrop, video, minibrowser, CD ripping, device sync.
- True multiple OS windows. Ebitengine owns one window; use internal windows.
- Account login/library sync. Public catalog first.
- EQ DSP, spectrum analyzer, shade/double-size/region masks. These are
  independently shippable Phase 6 chunks, not v1 blockers (6A EQ DSP, 6B
  spectrum/oscilloscope, and 6C shade/double-size have now shipped; `region.txt`
  masks remain optional).

## 3. Decisions and trade-offs

| ID | Decision | Recommendation | Trade-off / why |
|----|----------|----------------|-----------------|
| D1 | Audio backend | `ebiten/v2/audio` with `f32le` from ffmpeg; context on the first `Update` tick; `Pause` for pause, `PauseAndStopReading` for teardown | One audio device/event loop. The Phase 1 spike passed; `beep`/`oto` direct use is gone. Risk now is teardown ordering, handled in section 7.3. |
| D2 | Window model | One OS window containing draggable internal windows | Ebitengine creates one window per process. True Winamp multi-window behavior needs a second UI stack; not worth it for v1. |
| D3 | Catalog source | `yt-dlp` first; optional InnerTube `WEB_REMIX` behind the same interface | yt-dlp owns signature/format churn. InnerTube gives true YTM results but is fragile and adds auth/API work. |
| D4 | Skin parser | Pure Go in `internal/skin`, no Ebitengine imports | Testable/fuzzable with `image.Image`, rects, and strings. UI uploads decoded sheets to the GPU once. |
| D5 | UI model | Immediate-mode hit testing over a small window list | Winamp widgets are fixed sprites with a few states. A retained UI framework is over-engineering. |
| D6 | Concurrency | Playback actor + commands/events + atomic snapshot | One goroutine owns playback state. UI never touches the player or the queue. |
| D7 | Persistence | Versioned JSON, atomic temp+rename | Config/queue are small. SQLite is overkill. Write on debounce and on exit. |
| D8 | Tool distribution | Require system `yt-dlp` and `ffmpeg`; check at startup | Bundling/auto-updating external tools adds size and supply-chain work. Show actionable install/update messages. |
| D9 | Text | Bitmap `text.bmp` for chrome; embedded TTF fallback for non-ASCII | Classic skins have no Unicode. Search/playlist titles do. Use bitmap for classic look, TTF where needed. |
| D10 | Artwork cache | Files on disk by video ID; in-memory LRU of decoded images | Simple, inspectable, no database. Fetch/decode off the UI goroutine. |

Phases 1–5 implemented all of D1–D10: D1 (`ebiten/v2/audio`), D2 (refined in
ADR 0002 to the docked/stacked Winamp rack in Phase 4B), D3 (both `yt-dlp` and
pure-Go InnerTube `WEB_REMIX` behind `catalog.Catalog`, selectable via
`-catalog`), D4, D5, D6 (recorded in ADR 0003), D7 (`internal/store` atomic
JSON persistence), D8, D9 (`text.bmp` + TTF fallback), and D10.

### Deliberately not doing

- A generic UI framework or plugin system.
- A custom decoder for every BMP bit depth before the corpus proves it is needed.
- InnerTube authentication/cookies until the public player is solid.
- SQLite, an ORM, a dependency-injection container, or a message bus.
- Gapless playback in v1 (deferred to optional Phase 6E; EQ DSP and
  spectrum/oscilloscope visualizers were subsequently shipped in Phase 6A–6B).
- Heavy Go GUI/toolkit dependencies for clipboard or file dialogs (clipboard
  paste and the skin picker shell out to native OS utilities in
  `internal/ui/clipboard.go` and `internal/ui/skinpicker.go`).

## 4. Target architecture

```text
cmd/ytm-wmp/main.go          flags, dependency checks, config restore, skin load, wiring, RunGame
cmd/ytmctl/main.go           debug CLI (search, track, playlist, radio, artwork)
internal/ui/                 Ebitengine Game, input, layout, main/EQ/vis/playlist/browser/settings/toasts
internal/ui/skinview/        one-time sheet upload + SubImage/slider/glyph cache + Deallocate
internal/skin/               pure .wsz/.zip/fs.FS/dir loader, sprite table, pledit/viscolor, fuzzing
internal/playback/           actor Controller, queue, state machine, radio autoplay, error taxonomy
internal/audio/              ffmpeg f32le Source, 10-band EQReader, VisRing/VisTapReader/FFT, EbitenSink
internal/ytdlp/              yt-dlp -g stream resolver, bounded ExecRunner, 5h URL cache
internal/model/              Track, StreamInfo, VideoID, IsVideoID, NormalizeTrackID, DefaultThumbnailURL
internal/deps/               yt-dlp/ffmpeg discovery and version checks (PATH + custom paths)
internal/catalog/            Catalog, RadioProvider, TabCatalog interfaces, IsPlaylistTarget, Searcher
internal/catalog/ytdlp/      yt-dlp -J catalog implementation and caches
internal/catalog/ytmusic/    pure-Go InnerTube WEB_REMIX catalog, radio, and tab/detail implementation
internal/artwork/            thumbnail fetch/decode/resize/disk+LRU cache/prune+stats/placeholder
internal/store/              versioned atomic config.json, TrackRef queue, debounced saver
internal/m3u/                #EXTM3U parser and formatter for YouTube URLs/IDs
internal/mpris/              pure-Go Linux MPRIS2 D-Bus server (org.mpris.MediaPlayer2)
assets/dev-skin/             original loose BMPs; no copyrighted skins
docs/adr/                    short decision records (0001, 0002, 0003)
```

`internal/app` and `internal/ui/screens` were sketched early on, but keeping
`Game` and panel files (`playlist.go`, `eq.go`, `vis.go`, `browser.go`,
`settings.go`, `toast.go`) directly in `internal/ui` kept the docked rack and
shared widget state simple without an extra routing layer.

Dependency direction is one-way:

```text
main -> ui, playback, catalog, artwork, ytdlp, deps, skin, store, m3u
ui -> skinview -> skin + ebiten
ui -> playback, catalog, artwork, model, store, m3u (never ytdlp)
playback -> audio, model (never ui, catalog, or ytdlp)
catalog -> model (never ebiten, ui, playback, or ytdlp)
artwork -> stdlib + x/image only (never ebiten, ui, or playback)
store -> model (never ebiten, ui, playback, or ytdlp)
m3u -> model (never ebiten, ui, playback, or ytdlp)
ytdlp -> model (never audio)
skin must not import ebiten
```

The `StreamInfo` seam and ID-parsing seam are resolved in `internal/model`:
`StreamInfo` (`audio.StreamInfo` alias) and `VideoID` / `IsVideoID` /
`NormalizeTrackID` / `DefaultThumbnailURL` live in `internal/model`, so
`internal/ytdlp`, `internal/catalog`, `internal/playback`, and `internal/ui`
never cross-import one another.

`skin` works with `image.Image`, `image.Rectangle`, strings, and structs. That is
what makes it unit-testable and fuzzable without a graphics context.

### Runtime flow

```text
Ebitengine Update (60 TPS)
  ├─ first tick: audio.Init + skinview.New + UI TTF face init
  ├─ drain events/tasks          playback events, async skin loads, settings results, toast expiry
  ├─ refresh Game.snap           single Controller.Snapshot() read per tick
  ├─ updateBrowser               search results, enrichments, queue enrich, lazy modal art, artwork drain
  ├─ pointer.Update              transport, shade bars, sliders, EQ, playlist, browser/settings, toasts
  ├─ textinput / keys / vis      IME search input, shortcuts, post-EQ visualizer FFT/scope update
  └─ debounced persistence       maybeSaveEQ (eq.json) + maybeSaveConfig (config.json)

Ebitengine Draw
  └─ main/EQ/playlist or shade bars + visualizer + browser/settings + toasts + F1 debug overlay
     (pure; no I/O or texture creation)
```

Rules:

- Never call `yt-dlp`, `ffmpeg`, HTTP, disk I/O, or `ebiten.NewImageFromImage`
  inside `Draw()`. Playback, catalog search, metadata enrichment, and artwork
  fetch/decode run on background goroutines; GPU texture uploads and audio
  context initialization happen inside `Update()`.
- Only the `playback.Controller` actor goroutine mutates playback and queue
  state.
- Tag every async operation with a generation/request ID; drop stale results
  (implemented in both `playback.Controller` and `catalog.Searcher`).
- The UI reads an immutable `playback.Snapshot` (`Queue` shared read-only) and
  never calls audio playback methods directly (only `AudioLifecycle.Init`/
  `Close` on lifecycle boundaries).

### Playback state machine

Implemented in `internal/playback` (`state.go` `transitionTable` and
`controller.go`):

```text
Idle -> Resolving -> Playing <-> Paused
          |             |
          |             +-> Seeking -> Playing
          +-> Error     |
                        +-> Ended -> (next track) Resolving
```

- `Stop` kills the source and resets position to zero, keeping the selected
  track. `Pause` keeps the source and buffered audio for instant resume.
- A seek is a new source and a new `audio.Player`, never `SetPosition` on a
  non-seeker pipe.
- Auto-advance happens when the source reports EOF and the player exits
  cleanly.

## 5. Key interfaces

Implemented signatures:

```go
// internal/model (implemented)
type Track struct {
    ID           string // Normalized YouTube video ID when extractable
    Title        string
    Artist       string
    Album        string
    Duration     time.Duration
    ThumbnailURL string
}

type StreamInfo struct {
    URL       string
    ExpiresAt time.Time
}

func VideoID(target string) string
func IsVideoID(s string) bool
func NormalizeTrackID(target string) string
func DefaultThumbnailURL(idOrURL string) string

// internal/audio (implemented)
type Source interface {
    io.ReadCloser
    Err() error
    Frames() int64
    Done() <-chan struct{} // EOF, read error, or close
}

type Opener interface {
    Open(ctx context.Context, streamURL string, offset time.Duration) (Source, error)
}

// internal/playback (implemented)
type Snapshot struct {
    State     State
    Track     model.Track
    Position  time.Duration
    Duration  time.Duration
    Volume    float64
    Queue     []model.Track // Read-only shared slice; never mutated by callers
    Index     int
    Shuffle   bool
    Repeat    RepeatMode
    Autoplay  bool
    LastError string
}

type Controller struct { /* actor goroutine, queue, atomic.Pointer[snapshotState] */ }

func NewController(resolver Resolver, opener Opener, sink Sink, opts ...Option) *Controller
func (c *Controller) Commands() chan<- Command
func (c *Controller) Send(cmd Command) bool
func (c *Controller) Events() <-chan Event
func (c *Controller) Snapshot() Snapshot // lock-free atomic read
func (c *Controller) Done() <-chan struct{}
func (c *Controller) Run(ctx context.Context)

// internal/catalog (implemented)
type Catalog interface {
    Search(ctx context.Context, query string, limit int) ([]model.Track, error)
    Playlist(ctx context.Context, idOrURL string) ([]model.Track, error)
    Track(ctx context.Context, id string) (model.Track, error)
}

type RadioProvider interface {
    Related(ctx context.Context, trackID string) ([]model.Track, error)
}

type TabCatalog interface {
    SupportedTabs() []SearchTab
    SearchCollections(ctx context.Context, tab SearchTab, query string, limit int) ([]Collection, error)
    BrowseCollection(ctx context.Context, col Collection) (CollectionDetail, error)
}

type Searcher struct { /* debounced, generation-tagged async search */ }

func IsPlaylistTarget(input string) bool
func NewSearcher(cat Catalog, opts ...SearcherOption) *Searcher
func (s *Searcher) SetQuery(query string) uint64
func (s *Searcher) Submit(query string) uint64
func (s *Searcher) Results() <-chan SearchResult
func (s *Searcher) Close()

// internal/artwork (implemented)
type Bundle struct {
    VideoID    string
    List       image.Image // 64x64
    NowPlaying image.Image // 300x300
}

type Cache struct { /* disk cache + in-memory LRU + single-flight dedup */ }

func NewCache(opts ...Option) *Cache
func (c *Cache) LookupMemory(videoID string) (Bundle, bool)
func (c *Cache) Get(ctx context.Context, videoID, thumbnailURL string) (Bundle, error)
func (c *Cache) Stats() (DiskStats, error)
func (c *Cache) PruneDisk(maxBytes int64) error
func (c *Cache) ClearDisk() error
func Placeholder(size int) image.Image

// internal/skin (implemented)
type Sprite struct {
    Sheet string          // "main.bmp", "cbuttons.bmp", ...
    Src   image.Rectangle // source rect in the sheet
}

type Skin struct { /* sheets + playlistStyle + visColors + warnings */ }

func Open(path string) (*Skin, error)
func OpenBytes(data []byte) (*Skin, error)
func OpenFS(fsys fs.FS) (*Skin, error)
func (s *Skin) Lookup(name string) (image.Image, image.Rectangle, bool)
func (s *Skin) Sheet(name string) (image.Image, bool)
func (s *Skin) SheetNames() []string
func (s *Skin) PlaylistStyle() PlaylistStyle
func (s *Skin) VisColors() [24]color.RGBA
func (s *Skin) Warnings() []string
```

Commands/events are sealed types (`Play`, `PlayIndex`, `Pause`, `Resume`,
`Stop`, `Next`, `Prev`, `Seek`, `SetVolume`, `SetQueue`, `Enqueue`,
`RemoveIndices`, `MoveTrack`, `SortQueue`, `UpdateTrack`, `SetShuffle`,
`SetRepeat`, `SetAutoplay`; `StateChanged`, `TrackChanged`, `QueueChanged`,
`VolumeChanged`, `PlaybackError`).

## 6. `.wsz` parser design

A `.wsz` is a ZIP with classic Winamp assets. The practical spec is "what
Winamp 2.x and Webamp accept", so build against a corpus and Webamp's sprite
table, not blog posts.

### 6.1 Loading pipeline

Implemented in Phases 1 and 4 (plus `eq_ex.bmp` in 6C):

1. Accept `.wsz`, `.zip`, `fs.FS` (`OpenFS`), raw bytes (`OpenBytes`), or a
   directory of loose files. The file extension is not trusted; files are
   opened as ZIP.
2. Read assets through hard limits: 8 MiB per asset, 32 MiB total bytes,
   16M total decoded pixels, 256 assets, 1024 ZIP entries, and 4096px max image
   side. Never extract to disk; loose files are read through `io.LimitReader`.
3. Build an index of lowercase basenames. Normalize `\` to `/`, ignore
   `__MACOSX/`, `.DS_Store`, and `Thumbs.db`. Duplicate basenames return a clear
   error.
4. Detect `skin.xml` and return `ErrModernSkin` with a clear message.
5. Decode BMPs eagerly and deterministically. `main.bmp` is required. Optional
   sheets (`cbuttons.bmp`, `titlebar.bmp`, `shufrep.bmp`, `posbar.bmp`,
   `volume.bmp`, `balance.bmp`, `playpaus.bmp`, `monoster.bmp`, `numbers.bmp`,
   `nums_ex.bmp`, `text.bmp`, `pledit.bmp`, `eqmain.bmp`, `eq_ex.bmp`) that are
   missing or fail to decode become warnings and placeholders (with `balance.bmp`
   falling back to `volume.bmp` when omitted, and partial slider strips or
   missing thumbs tolerated gracefully).
6. Parse `pledit.txt` (INI-like colors/font) and `viscolor.txt` (24 RGB lines)
   with classic Winamp defaults when absent or malformed.
7. Build sprites from the canonical Webamp sprite table. Validate every rect
   against its sheet; a missing or truncated sheet yields a placeholder, not a
   panic.
8. Return a `Skin` with decoded sheets, `PlaylistStyle`, `VisColors`, and
   warnings. (`region.txt` polygon masks remain optional Phase 6C work.)

### 6.2 BMP handling

- `golang.org/x/image/bmp` supports 8, 24, and 32 bpp directly.
- For 32-bit BMPs, if the entire alpha channel is zero, force alpha to opaque.
  Many classic skins store alpha as zero and expect opaque pixels. This is
  implemented for both RGBA and NRGBA decodes.
- If the corpus contains 1/4/16-bpp BMPs, add a small fallback decoder then.
  Do not write it speculatively. A broken optional sheet already falls back to a
  placeholder; a broken `main.bmp` fails with the asset name.
- `image.DecodeConfig` runs first and enforces per-image and total pixel budgets
  before allocation.
- Errors wrap the asset name; `ErrModernSkin` is a named sentinel while broken
  optional BMPs emit warnings and fall back to placeholders.

### 6.3 Sprite table and text

- Phase 4 ported Webamp's `skinSprites.ts` sprite table (MIT attribution) and
  `FONT_LOOKUP` into `internal/skin/sprites.go`, with tests keeping every rect
  inside its sheet and validating `assets/dev-skin/`. Phase 6C added `eq_ex.bmp`
  and main/playlist/EQ shade + clutter-bar sprites.
- `text.bmp` is a 5x6 glyph grid (31 columns x 3 rows) covering lowercase,
  digits, punctuation, and `Å Ö Ä ? *`; non-ASCII/unsupported runes fall back
  to an embedded TTF in the UI.
- `numbers.bmp`/`nums_ex.bmp` digits are 9x13 (`nums_ex.bmp` preferred when
  present, including minus sign support for remaining-time display).
- Volume, balance, and EQ backgrounds are 28-frame multi-frame strips
  (`VolumeFrameRect`, `BalanceFrameRect`, `EQSliderFrameRect`), pre-sliced in
  `internal/ui/skinview` with bounds intersection for partial slider sheets.
- Playlist colors and current-track highlight come from `pledit.txt` with
  readable classic defaults.

### 6.4 Security and robustness

- Implemented: per-asset and total byte caps, total pixel cap, asset/ZIP-entry
  caps, image dimension caps, symlink-root resolution, and duplicate-name
  rejection. ZIP traversal paths are rejected. Nothing is extracted to disk.
- Never trust names for filesystem access; read only through the archive or
  directory walk.
- Per-asset validation: a missing/broken optional sheet must not block the main
  window. Implemented via warnings and placeholders.
- Implemented: `FuzzSkinOpen` (`internal/skin/fuzz_test.go`) fuzzes `OpenBytes`
  with arbitrary ZIP/BMP input so malformed skins never panic or allocate
  unbounded memory.
- Original dev skin lives in `assets/dev-skin/` for first run. Do not commit
  copyrighted Winamp skins; keep local test skins in a gitignored `skins/`
  directory.

## 7. Audio and playback design

### 7.1 Ebitengine audio constraints

Verified against Ebitengine v2.10.5:

- `audio.NewContext(sampleRate)` creates the single process-wide context. A
  second call panics. Phase 1 creates it on the first `Update` tick, which the
  spike verified; calling it before `RunGame` is also valid but not what this
  codebase does. If a context already exists at 48 kHz, reuse it; a different
  rate is a startup error.
- One context has one sample rate. Use 48 kHz and let ffmpeg resample.
- `ctx.NewPlayerF32(src)` expects 32-bit float little-endian, stereo, 48 kHz
  PCM. Prefer it over `NewPlayer` (16-bit).
- The source must not be shared between players. A player does not close its
  source; closing remains our responsibility.
- `Player.Pause()` is the pause primitive: it stops the clock but keeps the
  source and buffer alive. It does not stop the underlying reader from reading.
- `Player.PauseAndStopReading()` (v2.10+) stops reads and blocks until an
  in-flight read finishes. It is the stop/teardown primitive, not the normal
  pause control.
- `Player.Close()` is deprecated as of v2.10; do not use it.
- A player is seekable only if its source is `io.Seeker`. A pipe is not.
  `SetPosition` on a non-seeker panics. Never seek this source; recreate the
  ffmpeg pipeline.
- `Player.Position()` is the audio clock. Phase 2 uses it for the UI; do not
  trust wall-clock time alone.
- Natural EOF and cancellation are different: after EOF, wait for ffmpeg to
  exit normally; only cancellation/stop kills it.

### 7.2 ffmpeg pipeline

```text
yt-dlp --get-url -f bestaudio/best --no-playlist <videoID>
  -> URL cache (key: videoID, TTL 5h)
  -> ffmpeg -nostdin -loglevel error -fflags +nobuffer
            -reconnect 1 -reconnect_streamed 1 -reconnect_delay_max 5
            [-ss <offset>] -i <url>
            -vn -f f32le -ac 2 -ar 48000 -
  -> Source (reader + process handle + bounded stderr ring)
  -> ctx.NewPlayerF32(source)
```

- `-ss` before `-i` for fast seek. If accuracy is poor, move it after `-i` and
  accept slower seeks.
- Keep `-reconnect*` before `-i`; they are HTTP protocol options.
- Never pass a shell string. Queries and titles contain spaces, quotes, and `&`.
- Bound ffmpeg stderr; log it only when the exit is unexpected.
- `cmd.Wait` must run after the read loop has drained stdout. Use a read-loop
  done channel; otherwise `Wait` can close the pipe under the reader.

### 7.3 Teardown order (the critical part)

Implemented in Phase 1:

1. On cancel or stop: close the source (close stdout, kill ffmpeg if EOF has not
   been seen), then `PauseAndStopReading`, then wait for the read loop and
   process. Closing first is what unblocks a read stalled on the network.
2. On natural EOF: do not kill. Wait up to two seconds for a normal exit, reap
   the process, and treat a kill after that timeout as teardown rather than a
   playback failure. A non-zero exit code is still reported.
3. `Game.Close` cancels the context, waits for the playback goroutine, flushes
   debounced `config.json`/`eq.json` savers, deallocates GPU textures, and
   closes the sink. No new ffmpeg process can start after the sink is closed.
4. A seek discards the player and creates a new pipeline (implemented in Phase 2).
5. On pipe teardown, `ffmpeg.go` returns `io.EOF` rather than `os.ErrClosed`.

For pause, the source, process, and connection are kept alive via `Player.Pause()`
for instant resume, while `PauseAndStopReading` is reserved for stop/teardown so
it cannot block the UI thread. Phase 6A–6B insert `audio.EQReader` and
`audio.VisTapReader` in the reader chain inside `EbitenSink.Play` between
`audio.Source` and `ctx.NewPlayerF32`.

### 7.4 Seek, EOF, and recovery

All implemented in Phase 2 (`internal/playback`):

- EOF: implemented. The source records `io.EOF` and closes `Done()`; the actor
  stops reading, tears down cleanly, and runs `AutoAdvance()`.
- Stale URL: implemented. If the source produced zero frames (`ErrNoAudio`),
  invalidate the cache entry via `Resolver.Forget` and retry once with a fresh
  URL.
- Seek: implemented (`cmdSeek`). Cancels the active pipeline, creates a new
  source at `offset`, sets `baseOffset = offset`, and transitions through
  `StateSeeking` -> `StatePlaying`.
- Position shown to the user = `baseOffset + sink.Position()` (clamped to
  `Duration`).
- Prefetch: implemented (`maybePrefetch` ticks within `30s` of end of track
  and resolves the next track's URL without opening audio).
- Error taxonomy: implemented (`ErrToolMissing`, `ErrStaleStream`, `ErrFFmpeg`,
  `ErrNetwork`, `ErrUnavailable`, and `ErrCanceled`, each mapped to a
  user-facing message via `ClassifyError`).

## 8. UI design

### 8.1 SkinView

Implemented in Phase 1 and extended in Phases 4–6:

- Upload each decoded sheet to `*ebiten.Image` once on the first `Update` tick
  (or after an async skin-picker reload), after the graphics context exists.
  `NewImageFromImage` never runs per frame. Release prior GPU textures via
  `View.Deallocate()` on reload and shutdown.
- Cache `sheet.SubImage(rect)` results per sprite name, precompute all known
  sprites, 28-frame slider strips (`volFrames`, `balFrames`, `eqFrames`),
  `EQ_GRAPH_LINE_COLORS`, `text.bmp` glyphs, `pledit.txt` styles, and
  `viscolor.txt` palettes when the view is built.
- Check every source rect against the uploaded sheet before `SubImage` (with
  `balance.bmp` falling back to `volume.bmp`, partial slider backgrounds
  intersected to sheet bounds, and optional slider thumbs skipped if absent).
  Otherwise missing/invalid rects become labelled magenta placeholders created
  once in `skinview.New`, not during `Draw`.
- Draw at integer coordinates with `FilterNearest`.

### 8.2 Immediate-mode widgets

All classic and browser widget primitives are implemented in `internal/ui`:

- **Buttons & toggles:** normal/pressed sprites and 2-/4-state toggles
  (`SHUF`, `REP`, `EQ`, `PL`, `ON`, `AUTO`, `D` double-size, shade buttons).
  Clicks fire on release inside the widget; presses capture the widget;
  dragging out cancels. Hot/active IDs are tracked; a press first observed
  outside a widget does not click it.
- **Sliders & scrollbars:** horizontal seek/volume/balance/mini-shade sliders
  and vertical EQ band/preamp sliders + playlist scrollbar with drag capture and
  mouse wheel support.
- **Lists, menus, toasts & text fields:** virtualized playlist/browser rows
  (click, `Ctrl+Click`, `Shift+Click`, double-click, right-click), `pledit`
  popup menus (`ADD`/`REM`/`SEL`/`MISC`/`LIST`), dismissible toasts, and a
  caret/IME-aware `textField` (`exp/textinput.Composer`).

Use stable IDs (e.g. `"main.volume"`, `"eq.band.0"`, `"playlist.row.0"`),
hot/active state, and press/release semantics without a retained widget tree.

### 8.3 Windows

- **Implemented:** docked/stacked Winamp rack (`Main 275x116`, toggleable `EQ
  275x116`, toggleable `Playlist/Browser 275x232`) inside one OS window,
  resizing the OS window only on discrete `EQ` / `PL` / shade / scale changes
  rather than continuously during mouse drags (see ADR 0002).
- **Implemented (Phase 6C partial):** WindowShade mode (`14px` collapsed bars
  via the shade button or titlebar double-click) for Main (mini transport,
  clock, mini seek), EQ (`eq_ex.bmp` + fallback volume/balance sliders), and
  Playlist (`pledit.bmp` shade bar + track/duration), persisted in `config.json`,
  plus the clutter-bar `D` double-size toggle (`1X` <-> `2X`, also `Ctrl+D`).
  Arbitrary floating drags and `region.txt` polygon masks remain deferred/optional.
- **Playlist (`internal/ui/playlist.go`):** uses `pledit.bmp` frames for
  top/bottom/scrollbar and `pledit.txt` colors; virtualizes rows;
  click/shift-click/ctrl-click; double-click to play; wheel and scrollbar; popup
  menus + M3U import/export.
- **EQ (`internal/ui/eq.go`):** renders `eqmain.bmp` chrome, 10 bands + preamp,
  and response graph; wired live to `audio.EQReader` (Phase 6A) and persisted
  atomically to `eq.json`.
- **Visualizer (`internal/ui/vis.go`):** renders `viscolor.txt`-styled 19-bar
  spectrum analyzer (with falling peaks) and oscilloscope inside
  `MainVisBounds` (`24, 43, 100, 59`); click to cycle Spectrum -> Oscilloscope
  -> Off (Phase 6B).
- **Browser/Settings (`internal/ui/browser.go`, `settings.go`):** `browserPanel`
  with `SONGS`/`PLAYLST`/`ALBUMS`/`ARTISTS`/`QUEUE` tabs, collection detail
  drill-down (`<BACK`), caret/IME search input, artwork preview modal, and
  in-app Settings panel (`SET` / `F2`).
- **Implemented:** `F1` debug overlay with FPS/TPS, playback state, `eq`/`pl`
  visibility, queue index/len, position/duration, volume, shuffle/repeat/autoplay,
  track ID, skin name, missing-sprite count (`miss=%d`), cursor/hot/active IDs,
  sprite destination rects (cyan), and widget hit areas (magenta). Missing
  sprites are labelled on the canvas.

### 8.4 Text and scale

- **Implemented:** bitmap `text.bmp` font for main-window marquee/time/shade
  chrome and ASCII playlist/title rendering, with embedded TTF fallback
  (`initUITextFace`) for non-ASCII/browser/settings text.
- **Implemented:** marquee scrolling clips to `MainMarqueeBounds` and scrolls
  only when text exceeds the clip width, without per-frame offscreen image
  allocations.
- **Implemented:** logical coordinates stay fixed at 1x (`275` px wide); display
  scale is configurable in `0.10x` increments from `1.0x` to `2.0x` in Settings
  (plus `1X` <-> `2X` toggle via main clutter-bar `D` / `Ctrl+D`) and applied at
  the window/draw boundary (`ScaledWindowSize`).

## 9. Catalog, search, and artwork

Implemented in Phase 3A behind `catalog.Catalog`, `catalog.Searcher`, and
`artwork.Cache`; Phase 3B adds a pure-Go InnerTube `WEB_REMIX` implementation.

### 9.1 yt-dlp catalog (3A — done)

- Search: `yt-dlp -J --flat-playlist --playlist-end N "ytsearchN:<query>"`.
- Track metadata: `yt-dlp -J --no-playlist <videoID>`.
- Playlist: `yt-dlp -J --flat-playlist --playlist-end 100 <url>`.
- Parses permissively into `model.Track` without leaking yt-dlp JSON outside
  `internal/catalog/ytdlp`.
- Keyed and cached by normalized video ID (`model.VideoID`).
- Bounded stdout (`DefaultMaxStdoutBytes = 16 MiB`) and stderr (`64 KiB`).
- Debounced, generation-tagged `catalog.Searcher` (`DefaultDebounce = 275ms`);
  `Enter` submits immediately and cancels prior requests.
- In-memory TTL caches for search (`10m`) and track metadata (`1h`).

### 9.2 InnerTube `WEB_REMIX` catalog (3B — done, extended in 4–5)

- Package `internal/catalog/ytmusic` implements `catalog.Catalog` (`Search`,
  `Track`, `Playlist`), `catalog.RadioProvider` (`Related` via unauthenticated
  `RDAMVM...` Up Next seeds), and `catalog.TabCatalog` (`SONGS`, `PLAYLST`,
  `ALBUMS`, `ARTISTS`, plus playlist/album/artist detail browsing) via pure-Go
  HTTP POST requests to YouTube Music InnerTube endpoints
  (`https://music.youtube.com/youtubei/v1/search`, `next`, `browse`) using the
  `WEB_REMIX` client context, `DefaultMaxResponseBytes` (`16 MiB`), `10m`
  search TTL cache, and `1h` track metadata cache.
- Handles `VL...` / `PL...` / `OLAK5uy_...` / `MPREb_...` / `RDCLAK5uy_...`
  playlists and albums (inheriting album header metadata/artwork when track rows
  omit them and falling back to `/next` when `/browse` yields no items).
- Keeps `internal/ytdlp` (`yt-dlp -g`) as the stream URL resolver so signature and
  cipher churn remain isolated in `yt-dlp`, with `yt:` / `ytdlp:` prefix
  routing to `catalog/ytdlp` for videos outside YouTube Music.
- Added `-catalog ytmusic|ytdlp` (default `ytmusic`) in both `cmd/ytm-wmp` and
  `cmd/ytmctl`.
- No account/auth/cookies in v1.
- Tested offline against recorded `WEB_REMIX` JSON response fixtures in
  `internal/catalog/ytmusic/testdata/`, with live InnerTube tests gated behind
  `YTM_LIVE=1` / `YTM_WMP_LIVE_TEST=1`.

### 9.3 Artwork (3A — done, cache management added in 5)

- Picks thumbnail closest to `360px` (preferring non-WebP on ties), falling back
  to `model.DefaultThumbnailURL`; fetches with a `10s` timeout and
  `github.com/rroy-ar/ytm-wmp/0.1` User-Agent.
- Decodes JPEG, PNG, and WebP (`golang.org/x/image/webp`).
- Resizes to list (`64x64`) and now-playing (`300x300`) sizes with
  `golang.org/x/image/draw` (`ApproxBiLinear`), plus `artwork.Placeholder`.
- Caches original bytes atomically on disk by video ID (`os.UserCacheDir()/ytm-wmp/artwork`,
  configurable via `CacheDir`) and decoded `Bundle`s in a 64-entry in-memory
  LRU with single-flight dedup. Supports `Stats`, `PruneDisk(maxBytes)` (run
  at startup when `CacheMaxMB > 0` and from Settings), and `ClearDisk`.
- Never fetches, decodes, or allocates GPU textures inside `Draw()`; `64x64`
  textures upload inside `Update()`, `300x300` textures upload lazily when the
  modal opens, and evicted/closed textures call `Deallocate()`.

## 10. Persistence

Implemented in Phase 5 (`internal/store`) and extended with `Shaded` in Phase 6C:

```go
type WindowState struct {
    Visible bool
    Shaded  bool
    X       int
    Y       int
}

type Config struct {
    Version       int
    Skin          string
    Volume        float64
    Balance       float64
    Scale         float64 // [1.0, 2.0] rounded to 0.10 increments
    BufferMs      int
    CacheDir      string
    CacheMaxMB    int
    Windows       map[string]WindowState
    BrowserMode   bool
    TimeRemaining bool
    Queue         []TrackRef
    QueueIndex    int
    Shuffle       bool
    Repeat        string
    Autoplay      bool
    LastSearch    string
    ToolPaths     ToolPaths
}
```

- Location: `os.UserConfigDir()/ytm-wmp/config.json` (plus `eq.json` and
  `playlist.m3u` in the same directory); cache/artwork under
  `os.UserCacheDir()/ytm-wmp/`.
- Atomic write: `store.AtomicWriteFile` writes to a temp file in the same
  directory, `Sync`, `Close`, and `Rename` (with Windows replace fallback).
  Reused by `config.json`, `eq.json`, and `playlist.m3u` saves.
- Debounced writes (`500 ms` via `store.DebouncedSaver`) and immediate flush on
  `Game.Close()`. Never writes per frame.
- `TrackRef` stores normalized video IDs and display metadata, stripping signed
  stream URLs (`googlevideo.com`) and never storing cookies.
- Schema versioned (`CurrentVersion = 1`) with `Migrate` + `Sanitize` /
  `SanitizeScale` (clamping volume/balance/scale/buffer/queue index and
  rejecting future versions).

## 11. Phases

Sizes are relative: S = a few sessions, M = about a week of evenings, L =
multiple weeks. The critical path is 0 -> 1 -> 2 -> 3A -> 3B -> 4 -> 5 -> 7.
Phases 0, 1, 2, 3A, 3.5 cleanup, 3B, 4, 5, 6A, 6B, and 6C (partial) are
complete; remaining optional Phase 6 chunks (`region.txt`, 6D–6E) and Phase 7
are next.

| Phase | Outcome | Size | Status |
|-------|---------|------|--------|
| 0 | Clean seams, tests, CLI still works | S | done |
| 1 | Ebitengine window with a skinned main window and audio | M | done |
| 2 | Playback core: queue, seek, recovery | L | done |
| 3A | `yt-dlp` catalog, artwork cache, browser panel | M | done |
| 3B | Pure-Go YTM InnerTube `WEB_REMIX` catalog backend | S-M | done |
| 4 | Complete classic skin system | L | done |
| 5 | Full player UX and persistence | L | done |
| 6 | Fidelity: EQ DSP, visualizer, shade/region (optional) | L | in progress (6A, 6B, 6C partial done; 6C `region.txt`, 6D–6E optional) |
| 7 | Hardening and v0.1 release | M | planned |

### Phase 0 — Baseline and seams (done)

**Goal:** keep the CLI behavior, make the code testable, prepare for Ebitengine.
No new features.

**Outcome:** completed in commit `1858514`. The CLI and one-track playback
survived the move; resolver/cache, ffmpeg source, orchestration, dependency
checks, and domain types are isolated and tested. The work list below is
historical; all items are complete.

**Work**

- Remove the built `main`/`winamp-ytm` binaries from the worktree; ignore both
  in `.gitignore`. Keep no binaries in git.
- Move the command to `cmd/ytm-wmp`; `main` becomes flags, dependency checks,
  wiring, and signals only.
- Extract `internal/ytdlp`:
  - `Resolve(ctx, target) (StreamInfo, error)` (target may be a video ID, URL,
    or search query)
  - URL cache keyed by video ID, injectable clock
  - `Runner` interface so tests never invoke real yt-dlp
- Extract `internal/audio`:
  - `Open(ctx, streamURL, offset) (Source, error)`
  - pure `ffmpegArgs(url, offset) []string`
  - `Source` with `Read`, `Err`, `Frames`, `Done`, `Close`
  - bounded stderr ring buffer
- Add `internal/model.Track`.
- Add `internal/deps` for `yt-dlp`/`ffmpeg` discovery and version strings.
- Write ADRs: audio backend, single-window model.
- Tests: cache expiry/stale retry, ffmpeg args, zero-frame classification.

**Files:** `cmd/ytm-wmp/main.go`, `internal/ytdlp/*`, `internal/audio/*`,
`internal/model/track.go`, `internal/deps/*`, `.gitignore`, `docs/adr/*`.

**Verify (at Phase 0):** `go test ./...`; `go run ./cmd/ytm-wmp -url <url>`
played; no binary was tracked; `main` had no business logic.

**Learn:** interfaces owned by the consumer, `exec.CommandContext`, context
cancellation, `io.Reader` contracts, TTL caches, table-driven tests.

### Phase 1 — Walking skeleton: Ebitengine + first skin + audio (done)

**Goal:** open a 275x116 window, draw `main.bmp`, `titlebar.bmp`, and a play
button, and play/pause a track through `ebiten/audio`. This de-risks the project.

**Outcome:** completed. Live smoke runs delivered ~250k PCM frames; pause kept
ffmpeg alive for resume; window close and SIGTERM left no ffmpeg process.

**Work as implemented**

- Added Ebitengine v2.10.5 and `golang.org/x/image`; removed direct
  `beep`/`oto` use.
- Created the single audio context on the first `Update` tick rather than in
  `main`. This works and keeps device setup out of startup.
- Implemented `ui.Game` (`Update`, `Draw`, `Layout`) instead of an `app.Game`;
  there is no separate `internal/app` yet.
- Implemented the initial `internal/skin` loader: `.wsz`/`.zip`/directory,
  lowercase basenames, backslashes/nested paths, size/pixel/entry caps, modern
  skin rejection, alpha handling, and `main.bmp`/`cbuttons.bmp`/`titlebar.bmp`
  decoding. Optional broken sheets warn instead of failing.
- Implemented `ui/skinview` to upload sheets and cache subimages.
- Drew the main background, active/inactive title bar, and play/pause buttons.
  Stop is deferred to Phase 2.
- Added immediate-mode press/release input for play/pause.
- Played a `-url`/hardcoded target: resolve, ffmpeg `f32le`, `NewPlayerF32`,
  play/pause/resume, clean shutdown, natural-EOF handling.
- Added `-skin path` with `assets/dev-skin/` fallback (original BMPs).
- Added the F1 debug overlay for sprite rects and hit areas.

**Deviations from the original sketch**

- Game lives in `internal/ui`, not `internal/app`.
- Audio context is created on the first tick, not before `RunGame`.
- Natural EOF waits for ffmpeg to exit instead of killing it; killing in the EOF
  window produced false playback failures.

**Verify:** window shows the skin; play makes audio; pause stops it; closing
leaves no ffmpeg; broken skin returns a clear error; F1 works; `go test -race
./...` passes.

**Learn:** Ebitengine game loop, logical vs. physical pixels, `SubImage`,
input APIs, audio player lifecycle, PCM frame layout, process EOF vs. kill.

### Phase 2 — Playback core (done)

**Goal:** a headless engine good enough that the UI can be dumb. Replace the
current single-track `playback.Player` with the actor, queue, and snapshot.

**Outcome:** completed. Replaced the single-track `playback.Player` with
`playback.Controller`, explicit state transition table, queue with history,
shuffle bag, and repeat modes, generation-tagged resolve/open workers,
seek-by-reopen, URL prefetching, error taxonomy, and UI/CLI wiring for all five
main transport buttons and keyboard shortcuts.

**Work as implemented**

- Sealed `Command`/`Event` types, `Controller` actor goroutine, and atomic
  `Snapshot` (`baseOffset + sink.Position()`).
- Queue (`internal/playback/queue.go`): `[]model.Track`, index, bounded history,
  shuffle bag (non-repeating cycle with immediate-repeat avoidance on refill),
  and `RepeatOff`/`RepeatAll`/`RepeatOne`.
- Generation-tagged async resolve/open/play workers; seek restarts ffmpeg at
  the target offset via `-ss`.
- Natural-EOF auto-advance, next-track URL prefetching near end of track,
  stale-URL retry once on `ErrNoAudio`, and `EbitenSink` `SetVolume`/`Position`.
- Error taxonomy (`ErrToolMissing`, `ErrStaleStream`, `ErrFFmpeg`, `ErrNetwork`,
  `ErrUnavailable`, `ErrCanceled`) mapped to user-facing messages.
- Wired `ui.Game` and `cmd/ytm-wmp` to `playback.Controller` with Previous, Play,
  Pause, Stop, and Next buttons, keyboard controls, and updated F1 debug overlay.
- Unit tests with fakes (`go test -race ./...`), goroutine-leak verification
  across 20 track changes, and opt-in `YTM_WMP_LIVE_TEST=1` integration test.

### Phase 3 — Catalog: search, metadata, artwork (done — 3A & 3B)

**Goal:** stop hardcoding tracks.

**Outcome:** completed Phase 3A and Phase 3B. Phase 3A moved `StreamInfo` into
`internal/model`, added `internal/catalog` (`Catalog` interface,
`IsPlaylistTarget`, debounced/generation-tagged `Searcher`), implemented
`internal/catalog/ytdlp` (`yt-dlp -J` search, track metadata, and playlist
extraction with stdout cap, caches, and JSON fixtures), added `internal/artwork`
(HTTP fetch, JPEG/PNG/WebP decode, 64x64 & 300x300 resize, atomic disk cache,
in-memory LRU, placeholder), added `cmd/ytmctl`, added `playback.UpdateTrack`
for non-interrupting metadata enrichment, and integrated the catalog browser
panel, search box, artwork display, and `-query`/`-playlist` flags into
`internal/ui` and `cmd/ytm-wmp`. Phase 3B implemented `internal/catalog/ytmusic`
(pure-Go InnerTube `WEB_REMIX` `search`/`next`/`browse` with response caps,
search/track TTL caches, and recorded JSON fixtures), expanded
`catalog.IsPlaylistTarget` for `/browse/...`, `VL...`, `MPREb_...`, and
`RDCLAK5uy_...` targets, and wired `-catalog ytmusic|ytdlp` (default `ytmusic`)
into `cmd/ytm-wmp` and `cmd/ytmctl` while keeping `internal/ytdlp` (`yt-dlp -g`)
as the stream URL resolver.

**Work (3A, yt-dlp — done)**

- `Catalog` interface and yt-dlp implementation.
- Search, track metadata, playlist extraction; JSON fixtures.
- Search debounce/cancel/generation; result cache.
- Artwork fetch/decode/resize/disk cache/placeholder.
- Browser window: search field, results list, play/queue actions, metadata enrichment.
- `cmd/ytmctl` debug CLI (`search`, `track`, `playlist`, `artwork`).

**Work (3B, YouTube Music InnerTube — done)**

- Implemented `internal/catalog/ytmusic` behind `catalog.Catalog` (`Search`,
  `Track`, `Playlist`) using pure-Go HTTP POST calls to
  `https://music.youtube.com/youtubei/v1/search`, `next`, and `browse` with the
  `WEB_REMIX` client context.
- Kept `internal/ytdlp` (`yt-dlp -g`) as the stream URL resolver.
- Added `-catalog ytmusic|ytdlp` (default `ytmusic`) in `cmd/ytm-wmp` and
  `cmd/ytmctl`.
- Tested offline with recorded `WEB_REMIX` JSON response fixtures in
  `internal/catalog/ytmusic/testdata/` and live InnerTube requests gated behind
  `YTM_LIVE=1`.

**Verify:** 10 results with title/artist/duration/thumbnail; selecting plays;
playlist URL imports; repeat search is instant; UI stays responsive; fixtures
pass without network.

**Learn:** JSON decoding, subprocess output, HTTP timeouts, image decoding and
resizing, caching, debouncing.

### Phase 4 — Classic skin system (done)

**Goal:** the distinctive part: full main, playlist, and EQ chrome.

**Outcome:** completed Phases 4A–4D. Ported Webamp's `skinSprites.ts` sprite
table and `FONT_LOOKUP`, added `pledit.txt`/`viscolor.txt` parsing, `OpenBytes`/
`OpenFS`, and `FuzzSkinOpen`, extended `assets/dev-skin/` with all Phase 4
sheets, added `internal/m3u` and queue removal/sort commands in
`internal/playback`, pre-cached slider frames and bitmap glyphs in
`internal/ui/skinview`, and built the docked/stacked Winamp rack (`Main`,
toggleable `EQ`, toggleable `Playlist`/`Browser`) with main LCD marquee/digits/
sliders/toggles, skinned `pledit.bmp` playlist editor + popup menus + M3U
import/export, and skinned `eqmain.bmp` equalizer with atomic `eq.json`
persistence. Remaining from Phase 4 verify checklist: external multi-skin
corpus visual check (kept out of git).

**4A. Parser and main-window renderer**

- Loader hardening (ZIP/dir, names, modern rejection, caps, alpha, optional
  assets) already landed in Phase 1; `OpenBytes`/`OpenFS` and `FuzzSkinOpen`
  landed in Phase 4. Remaining: external multi-skin corpus check and any
  1/4/16-bpp fallback the corpus proves necessary.
- Port the full Webamp sprite table.
- Bitmap text, marquee, time digits, stereo indicator, position bar, volume,
  balance, shuffle/repeat, EQ/PL toggles.
- `pledit.txt`/`viscolor.txt`.
- Fuzz the loader; synthetic fixtures; local skin corpus.

**4B. Widgets and docked rack window manager**

- Button/toggle/slider/scrollbar/list/text-field widgets.
- **Docked/stacked Winamp rack**: `Main` (`275x116`), toggleable `EQ`
  (`275x116`), and toggleable `Playlist/Browser` (`275x232`) inside one OS
  window; resize the OS window only on discrete `EQ` / `PL` show/hide toggles
  rather than continuously during mouse drags (ADR 0002).
- Active/inactive title bars and panel focus.
- Arbitrary floating drags, shade mode, double-size, and region masks: Phase 6
  unless cheap.

**4C. Playlist**

- Virtualized list, selection, current highlight, double-click play, wheel and
  scrollbar, add/remove/clear, M3U import/export.
- Clip/ellipsize long titles; bitmap font for ASCII, TTF fallback.

**4D. EQ chrome**

- Render the EQ window and 10 bands + preamp; persist values. Visual only.

**Verify:** 10 diverse skins render; all main controls work; playlist
scrolls/selects/plays; EQ opens and persists; missing assets fall back; bad
skins never panic; debug overlay identifies sprites; fuzz passes.

**Learn:** ZIP/BMP, sprite atlases, immediate-mode UI, hit testing, clipping,
bitmap fonts, window management.

### Phase 5 — Full player UX and persistence (done)

**Goal:** a real application, not a skinned demo.

**Outcome:** completed. Added `internal/store` for versioned, debounced, atomic
`config.json` persistence and wired cold-start restore into `cmd/ytm-wmp` and
`internal/ui` (skin with dev-skin fallback warning, volume, balance, 1x/2x
display scale, audio buffer size, artwork cache dir/cap, window layout,
shuffle/repeat/autoplay, queue/index, last search, and tool paths); added
`catalog.TabCatalog` (`SONGS`/`PLAYLST`/`ALBUMS`/`ARTISTS`) with InnerTube and
yt-dlp playlist search + detail drill-down back stack (`<BACK`, artist songs/
albums toggle); added an in-browser `QUEUE` tab backed by `playback.MoveTrack`;
added `textField` caret navigation and Ebitengine `exp/textinput.Composer` IME
support (guarded behind `WithIME(true)` so headless tests remain safe); added
dismissible error/status toasts with actionable missing-dependency alerts
(`deps.CheckPath`, opening GUI gracefully instead of exiting on missing tools);
and built the Settings panel (`SET` / `F2`) for skin, scale, audio buffer,
artwork cache stats/prune/clear, and tool path overrides.

**Work (all complete except optional Linux MPRIS, deferred to Phase 6E)**

- Browser tabs (`SONGS`/`PLAYLST`/`ALBUMS`/`ARTISTS`/`QUEUE`), detail views,
  and queue panel.
- Search field focus/caret/IME; keyboard shortcuts.
- Error/warn/info toasts and dependency status alerts.
- Settings: skin picker/reset, tool paths, cache location/size/prune/clear, 1x/2x
  scale, audio buffer.
- Persistence: `config.json` schema v1, migration, `TrackRef` queue, debounced
  writes, and flush on exit.
- M3U import/export of YouTube URLs/IDs (already landed in Phase 4).
- Optional Linux MPRIS/media keys remain in Phase 6E.

**Verify:** cold start restores last skin/volume/queue/layout; full mouse and
keyboard operation; missing dependency has an actionable message; a full evening
of use needs no terminal.

**Learn:** focus management, text input/IME, debounce, config dirs, atomic
writes, schema migration.

### Phase 6 — Fidelity chunks (optional, independently shippable)

- **6A EQ DSP (Complete):** 10-band peaking biquads (`60`–`16000` Hz) + preamp
  (`[-12, +12]` dB) as `audio.EQReader` between `ffmpeg` and Ebitengine's audio
  player, with float32 processing, bypass mode, hard clipping to `[-1, 1]`, and
  live UI wiring from `internal/ui/eq.go` via `EbitenSink.SetEQ`.
- **6B Spectrum/oscilloscope (Complete):** `audio.VisTapReader` tees post-EQ
  mono PCM into `audio.VisRing`, `audio.ComputeSpectrum` runs a Hann-windowed
  1024-point radix-2 FFT into 19 log-spaced bars, and `internal/ui/vis.go`
  renders spectrum bars (with falling peaks) and oscilloscope waveforms at
  `(24, 43, 100, 59)` using `viscolor.txt` (click to cycle Spectrum ->
  Oscilloscope -> Off).
- **6C Shade/double-size/region masks (Partial):** implemented WindowShade mode
  (`14px` collapsed bars via shade button or titlebar double-click) for Main
  (mini transport, clock, mini seek bar), EQ (`eq_ex.bmp` + fallback volume/balance
  sliders), and Playlist (`pledit.bmp` shade bar + track/duration), persisted in
  `config.json` (`WindowState.Shaded`), plus the main clutter-bar `D` double-size
  toggle (`1X` <-> `2X`, also `Ctrl+D`). Remaining in 6C: `region.txt` polygon
  masks and point-in-polygon hit testing.
- 6D Skin browser/cursors; skin folder scanning; `.cur` if cheap.
- **6E Gapless/MPRIS/media keys (Partial):** implemented Linux MPRIS2 D-Bus
  integration in `internal/mpris` (`org.mpris.MediaPlayer2.ytm_wmp` +
  `.instance<pid>` fallback) backed by `playback.Controller.Subscribe()` and
  wired into `cmd/ytm-wmp` behind `-mpris` (default `true`), supporting
  transport controls, `Seek`/`SetPosition`/`OpenUri`/`Quit`, track metadata &
  artwork, writable `LoopStatus`/`Shuffle`/`Volume`, and `PropertiesChanged`/
  `Seeked` signals. Remaining in 6E: gapless playback.

Each chunk must have a visible/audible demo and not regress v1.

### Phase 7 — Hardening and v0.1

**Work**

- Error paths/timeouts: missing/old yt-dlp, ffmpeg failure, offline, rate limit,
  age-restricted/members-only, expired URL.
- Performance: pprof CPU/heap, frame budget, allocation check in `Draw`,
  time-to-first-audio measurement.
- Tests: parser/queue/state unit tests, fake-process integration, corpus smoke,
  `-race`, fuzz skin/BMP.
- Packaging: Linux binary; Linux CI; desktop entry/icon.
- Docs: README install/troubleshooting, architecture/ADRs, legal note (no
  bundled skins/content; yt-dlp/YouTube ToS).
- Release: tag v0.1.0, smoke checklist, keep yt-dlp update instructions
  prominent.

**Verify:** v0.1 builds on Linux; a clean machine can install
deps, launch, load a skin, search, and play; 20-skin smoke test passes; known
limitations documented.

## 12. Risks and mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Ebitengine audio + live pipe teardown deadlocks/races | High | Phase 1 spike passed; close/kill the source before `PauseAndStopReading`; natural EOF waits for exit; fake-process + live shutdown tests; `-race` |
| `PauseAndStopReading` blocks on a stalled read | High | Close/kill ffmpeg first; close the pipe; two-second EOF reap timeout; still add a fake stalled-reader test |
| Paused session holds ffmpeg and the network connection | Medium | Deliberate for instant resume (kept in Phase 2+) |
| YouTube/yt-dlp changes break playback | High | Cache by video ID; one stale retry; version check; clear update message; keep catalog/resolver boundaries |
| InnerTube API fragility | High | Public yt-dlp path first; wrap third-party clients; yt-dlp fallback for streams |
| Skin incompatibility / BMP bit depths | Medium | Webamp sprite table; corpus-driven fallbacks; per-asset warnings/placeholders; fuzz loader |
| Alpha quirks in 32-bit BMPs | Medium | All-zero alpha -> opaque; test fixtures |
| Text/Unicode in bitmap skins | Medium | Bitmap chrome + TTF fallback; explicit `?` last resort |
| One-window limitation | Medium | Decide early; internal windows + snapping; document; transparent-canvas experiment only after v1 |
| Scope creep (EQ/visualizer/gapless/account) | High | v1 checklist; Phase 6 modular chunks (6A–6C partial shipped after v1 core); gapless/MPRIS/account still deferred |
| Cgo / X11 & GL build deps | Medium | Linux only (Windows/macOS out of scope); document X11/GL dev deps |
| Legal/ToS | Medium | No bundled skins/content; personal-use docs; don't log/redistribute streams |
| External tools in sandboxes | Medium | Startup checks; configurable paths; Flatpak notes |

## 13. Testing and instrumentation

### Current coverage (after Phase 6C partial)

- **Model, deps & yt-dlp:** `VideoID`/`IsVideoID`/`NormalizeTrackID`/`DefaultThumbnailURL`
  table-driven tests, 5h URL cache expiry, stale-URL `Forget`, bounded stdout/stderr
  subprocess caps (`ExecRunner`), `ffmpeg` argument construction, zero-frame
  classification, and `deps.Check`/`deps.CheckPath` discovery.
- **Catalog & artwork:** `catalog.IsPlaylistTarget` (playlist URLs/IDs, `/browse/...`,
  `VL...`, `MPREb_...`, `RDCLAK5uy_...`), debounced/generation-tagged
  `catalog.Searcher` cancellation, `internal/catalog/ytdlp` offline JSON fixture
  tests (`search`, `track`, `playlist`, stdout cap), `internal/catalog/ytmusic`
  offline `WEB_REMIX` JSON fixture tests (`search`, `next`, `browse`, radio,
  tab search, collection/artist detail, caching, max response bytes, empty/not-found
  errors), `internal/artwork` JPEG/PNG/WebP decode/resize/disk+LRU cache/prune/stats/
  single-flight tests, and `cmd/ytmctl` / `cmd/ytm-wmp` `-catalog` / startup queue tests.
- **Persistence & M3U:** `internal/m3u` `#EXTM3U` parse/format tests and
  `internal/store` `config.json` v1 `Load`/`Migrate`/`Sanitize`/`SanitizeScale`,
  `WindowState.Shaded`, `TrackRef`, `DebouncedSaver`, and `AtomicWriteFile` tests.
- **Playback:** queue navigation/history/non-repeating shuffle bag/repeat modes,
  `RemoveIndices`, `MoveTrack`, `SortQueue`, `UpdateTrack` enrichment, zero-copy
  read-only `Snapshot.Queue`, seek-by-reopen, URL prefetching, `RadioProvider`
  queue-end autoplay, stale-URL retry, error taxonomy, and 20-track-change
  goroutine leak check.
- **Skin loader & skinview:** directory and `.wsz`/`.zip`/`OpenBytes`/`OpenFS`
  loading, backslashes/nested paths, duplicate rejection, modern-skin rejection,
  broken/oversized BMPs, total byte/pixel budgets, symlinked directories, ZIP
  traversal, RGBA/NRGBA alpha, `pledit.txt`/`viscolor.txt`, sprite definitions,
  `assets/dev-skin/` dimensions (including Phase 4 + Phase 6C `eq_ex.bmp`/shade),
  `FuzzSkinOpen` fuzz target, plus `skinview` partial slider sheet / missing
  `balance.bmp` / optional thumb / `Deallocate()` tests.
- **UI:** press/release/cancel/drag behavior, topmost hit testing, named layout
  bounds, docked Winamp rack (`Main`/`EQ`/`Playlist`/`Browser`) + shade/double-size
  (`D` / `Ctrl+D`) geometry and persistence, skinned playlist rows/selection/menus/M3U,
  EQ window + presets + live `SetEQ` DSP wiring, visualizer spectrum/oscilloscope
  modes and decay, `browserPanel` tabs (`SONGS`/`PLAYLST`/`ALBUMS`/`ARTISTS`/`QUEUE`),
  detail drill-down, right-click/direct collection queueing, `ALL->Q` append,
  retryable/queue-aware enrichment, bounded 4-worker concurrency, lazy `300x300`
  texture upload + `Deallocate()`, caret/IME `textField`, clipboard/skinpicker
  selection, toasts, Settings panel (`0.10x` scale, buffer, cache prune/clear,
  tool paths), debug wrapping, and draw smoke tests.
- **Audio:** f32le format contract, sink lifecycle, pause/resume, pause during
  resolve, teardown ordering, clean EOF (`io.EOF` on pipe teardown) vs. nonzero exit,
  explicit kill, 10-band biquad `EQReader` (peaking filters, preamp, bypass,
  clipping), `VisRing`/`VisTapReader`/`ComputeSpectrum` FFT analyzer, and
  `EbitenSink` EQ/vis wiring.
- **Opt-in live tests:** `YTM_WMP_LIVE_TEST=1` in `internal/playback` and
  `YTM_LIVE=1` in `internal/catalog/ytdlp` and `internal/catalog/ytmusic`.
- `go test -race ./...` and `go vet ./...` pass. Building Ebitengine tests
  requires X11/GL development headers (documented in the README).

### Testing pyramid

1. **Pure unit tests:** skin parser, sprite table, text lookup, queue logic,
   state transitions, config serialization, M3U, EQ biquads, FFT spectrum. Fast,
   run in CI.
2. **Fake-process integration:** replace yt-dlp/ffmpeg with Go fakes or tiny
   scripts emitting known JSON/PCM. Tests args, errors, and teardown without
   network.
3. **Opt-in live tests:** real yt-dlp + ffmpeg behind `YTM_LIVE=1` /
   `YTM_WMP_LIVE_TEST=1` (implemented; skipped in offline CI).
4. **Visual tests:** render a skin to a screenshot with Xvfb where possible;
   otherwise a manual corpus checklist. Not yet implemented.
5. **Audio tests:** fake source with silence/sine; assert frame counts, EQ gain
   response, FFT peaks, and state transitions. Current tests use fake sources
   and real short-lived fake processes.

### Commands

```sh
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
go build ./...

go test -fuzz=FuzzSkinOpen ./internal/skin -fuzztime=30s
YTM_LIVE=1 go test ./internal/catalog/ytdlp -run Live
YTM_LIVE=1 go test ./internal/catalog/ytmusic -run Live
```

### Instrumentation

- `log/slog` with `-debug`. Implemented.
- Current debug logs: playback start/finish, sink cancellation/source done with
  frame counts, skin warnings, dependency versions, skin sheets.
- Ring buffer of ffmpeg stderr per stream. Implemented.
- Debug overlay (`F1`): FPS/TPS, playback state, `eq`/`pl` toggles, queue index,
  position/duration, volume, shuffle/repeat/autoplay, track ID, skin name,
  missing-sprite count, cursor, sprite rects, and hit areas. Implemented.
- Log spans for resolve/open/first PCM/seek/teardown. Start/finish,
  `first_pcm_ms`, and teardown frame counts exist today.
- Never log signed URLs, cookies, or auth headers. Implemented for stream URLs
  in stderr and for playback targets.

### Performance budgets

Not yet measured; Phase 1 deliberately avoided per-frame texture creation and
only formats debug/error text when those overlays are visible.

- 60 FPS; `Draw` under ~5 ms on a normal machine.
- Input latency under one frame.
- Time-to-first-audio under ~2-3 s on a warm cache.
- No per-frame allocations of images or large slices.
- Memory under a few hundred MB in a normal session.

## 14. Commit trail and next work

Recent trail as of this update (newest first):

- `e7d1f9f` feat(cmd): wire MPRIS2 D-Bus server and -mpris flag into ytm-wmp
- `4d255a3` feat(mpris): add Linux MPRIS2 D-Bus server and tests
- `601250f` feat(playback): add snapshot Subscribe notifications to Controller
- `91d1825` docs: record Phase 6C shade mode and double-size D button
- `698e468` feat(ui): implement shade mode and clutter-bar double-size D button
- `3f206eb` feat(assets): add shade mode and clutter bar sprites to dev-skin
- `d5c79a8` feat(store): persist window shaded state in config.json
- `0fc37fa` fix(skinview,ui): handle partial slider sheets, missing balance.bmp, and optional thumbs
- `2560d9b` docs: mark Phase 6A EQ DSP and Phase 6B visualizer complete
- `5628388` feat(ui): wire live EQ DSP and add spectrum/oscilloscope visualizer
- `bfc0c39` feat(audio): wire EQ DSP and visualizer tap into EbitenSink
- `ba6808c` feat(audio): add PCM visualizer ring buffer tap and FFT analyzer
- `3427548` feat(audio): add 10-band biquad EQ DSP reader and tests
- `73de576` feat(ui): press Esc to close catalog browser
- `e1c1655` feat(ui): right-click catalog rows to queue and document controls
- `ae4939f` docs: mark Windows and macOS compilation out of scope (Linux only)
- `8faa3e8` chore: update module path to github.com/rroy-ar/ytm-wmp and remove open questions
- `d1f6b90` feat(ui): allow queueing albums and collections without opening detail view
- `f47d414` fix(ui): append tracks to queue on ALL->Q instead of replacing
- `054d913` fix(audio): return io.EOF instead of os.ErrClosed on pipe teardown
- `c005283` feat(ui): support 0.10x display scale increments from 1x to 2x
- `8cd48dc` fix(ui): remove SEARCH button from playlist titlebar
- `e6298f6` docs: record Phase 5 completion in AGENTS.md, README, and plan
- `48a4eeb` feat(ui,cmd): wire config.json persistence, cold-start restore, and graceful startup
- `a8ec50d` feat(ui): add error toasts, dependency alerts, and settings panel
- `11fb3de` feat(ui,playback): add IME text field, browser tabs, detail views, and queue tab
- `9b18db2` feat(catalog): add InnerTube tabs and collection detail browsing
- `9628365` feat(deps,ytdlp,audio,artwork): add tool overrides, buffer size, and cache pruning
- `7b7e253` feat(store): add versioned atomic config persistence and debounced saver
- `c5bd326` docs: document main skin picker button in README and plan
- `41b473d` feat(ui,cmd): add main skin button and async file picker skin reload
- `9974b50` feat(skinview): add View.Deallocate to release GPU textures on reload
- `f04e310` feat(assets): label skin button on dev-skin main.bmp
- `48e479a` docs(agents): emphasize commit early and often
- `fbfd889` docs: record radio autoplay and Phase 4 completion
- `727ae4a` feat(cmd): wire autoplay, yt fallback, rack persistence, and M3U startup
- `b96a4e0` feat(ui): implement docked Winamp rack, main controls, skinned playlist, and EQ
- `47721ce` feat(skinview): cache slider frames, bitmap glyphs, and skin color configs
- `d4ec6ff` feat(playback): add radio autoplay, RemoveIndices, and SortQueue
- `80f66d9` feat(m3u): add EXTM3U parser and formatter for YouTube URLs and IDs
- `e191bc5` feat(assets): extend dev-skin with Phase 4 classic sheets
- `decb02d` feat(skin): port Webamp sprite table, pledit/viscolor parsing, and fuzzing
- `66ed9c9` feat(catalog,ytmctl): add YTM radio provider and yt: fallback search
- `3b29368` docs: document browser paste shortcuts and watch-URL lookup
- `07de33b` feat(ui): support clipboard paste and line-clear in browser search
- `be03d09` feat(catalog): resolve single-track YouTube watch URLs in Searcher
- `b69554b` docs: record Phase 3B completion and document ytmusic catalog
- `2c127ac` feat(cmd): add -catalog ytmusic|ytdlp flag to ytm-wmp and ytmctl
- `fa6fd95` feat(catalog): implement ytmusic InnerTube WEB_REMIX catalog backend
- `a340fcd` refactor(ui): extract browserPanel state and replace magic rects with layout constants
- `b16110b` fix(ui): keep playlist search in browser results, bound workers, and manage GPU textures
- `be323ea` perf(playback,ui): avoid per-call queue slice allocation in Snapshot and read once per tick
- `f6c78d9` refactor(ytdlp): bound stdout and stderr in ExecRunner
- `ff657b2` fix(ytmctl): resolve thumbnail URL via catalog or default ytimg URL in artwork subcommand
- `8430014` refactor(model): centralize YouTube ID helpers and decouple playback/ui from ytdlp
- `9656710` chore: update .gitignore for current binaries/skins and remove stale root PLAN.md
- `f856d0e` docs: add AGENTS.md, project status roadmap in README, and Phase 3 trail
- `2e8140d` docs: record Phase 3A completion in plan and README
- `7afb246` feat(ui): add catalog browser, metadata enrichment, and artwork display
- `d430f21` feat(catalog): add catalog, ytdlp backend, artwork cache, and ytmctl
- `aaa192e` docs: record Phase 2 completion and multi-track controls
- `cb58bce` feat(playback): add actor controller, queue, seek, recovery, and UI wiring
- `e94e7dc` feat(audio): add volume, position, and first-pcm timing to audio sink
- `538516e` docs(plan): record phase 0-1 outcomes and current architecture
- `52c0c0a` docs(adr): record graceful EOF and deferred pause behavior
- `1cfc98b` fix(audio): clear pause intent at the start of each play attempt
- `7a733b1` fix(ui): treat truncated sprites as missing and warn on skin issues
- `ef1da1b` fix(skin): budget whole skins and tolerate broken optional sheets
- `d5a8470` fix(audio): finish tracks cleanly and honor pause during resolve
- `c722054` test(ui): cover texture cache, first tick, and draw path
- `781235b` fix: cap loose-skin reads and skip canceled playback
- `15b2e7d` feat(audio): play the f32le ffmpeg pipe through Ebitengine
- `a1db1a2` fix(assets): point the dev-skin play and next icons forward
- `8051004` feat(ui): add immediate-mode input, layout, and skinned game loop
- `d4d9042` feat(skin): load classic .wsz archives and loose BMP directories
- `1858514` refactor: land phase 0 architecture seams

Next up is Phase 7 (hardening, multi-skin corpus smoke testing, performance
profiling, and Linux CI for `v0.1`), plus remaining optional Phase 6 fidelity
chunks (`region.txt` polygon masks in 6C, 6D skin browser/cursors, and 6E
gapless/MPRIS/media keys).
