# Plan: from the PoC to a full YouTube Music player with classic `.wsz` skins

Status: active execution plan. Phases 0, 1, 2, 3A, and the Phase 3.5 cleanup
are complete; Phase 3B (`internal/catalog/ytmusic` InnerTube `WEB_REMIX` backend)
is next ahead of Phase 4 (complete classic skin system). This document is the
single source of truth for architecture, status, and upcoming milestones.

Last updated: 2026-11-15.

## 1. Where the project is today

Phases 1–3A and the Phase 3.5 hardening pass turned the PoC into a multi-track,
catalog-backed desktop player with clear architectural seams.

Implemented and covered by tests:

- `cmd/ytm-wmp` is wiring only: `-url`, `-skin`, `-query`, `-playlist`, `-debug`,
  dependency checks, signal handling, skin loading, catalog/artwork wiring, and
  `ebiten.RunGame`.
- `cmd/ytmctl` provides a debug CLI for `search`, `track`, `playlist`, and
  `artwork` (resolving cold-cache artwork via `catalog.Catalog` or
  `model.DefaultThumbnailURL`).
- Ebitengine v2.10.5 opens a 275x348 logical canvas (275x116 skinned main window
  plus 275x232 catalog browser panel).
- `internal/model` owns shared domain types (`Track`, `StreamInfo`) and pure
  YouTube ID helpers (`VideoID`, `IsVideoID`, `NormalizeTrackID`,
  `DefaultThumbnailURL`).
- `internal/catalog` defines the `Catalog` interface, `IsPlaylistTarget`, and a
  debounced, generation-tagged async `Searcher`; `internal/catalog/ytdlp`
  implements `yt-dlp -J` search, track metadata, and playlist extraction with
  stdout caps and search/track caches.
- `internal/artwork` fetches, decodes (JPEG/PNG/WebP), resizes (64x64 list and
  300x300 now-playing), and caches thumbnails on disk and in an in-memory LRU.
- `internal/skin` loads `.wsz`, `.zip`, or a directory of loose BMPs. It
  normalizes case, backslashes, and nested paths, rejects modern `skin.xml`
  skins, caps per-asset/total bytes, total pixels, and ZIP entries, and handles
  32-bit all-zero-alpha BMPs. Phase 1 covers the main/titlebar/cbuttons sprite
  subset; a broken optional sheet becomes a warning.
- `internal/ui/skinview` uploads each sheet once and caches `SubImage`
  textures. Missing or truncated sprites become labelled magenta placeholders.
- `internal/ui` has hot/active press/release input, all five main-window
  transport controls (previous, play, pause, stop, next) plus Eject/search
  focus, keyboard shortcuts, named layout bounds (`layout.go`), `browserPanel`,
  bounded background enrichment/artwork workers, lazy `300x300` texture uploads
  with deterministic `Deallocate()`, and an F1 debug overlay showing state,
  queue index, position, volume, shuffle/repeat, and hit areas.
- `internal/playback` runs the Phase 2 actor (`Controller`), state machine,
  queue (history, shuffle bag, repeat off/one/all), request-generation tracking,
  seek-by-reopen (`baseOffset + sink.Position()`), zero-copy read-only
  `Snapshot.Queue`, URL prefetching, and error taxonomy (`ErrToolMissing`,
  `ErrStaleStream`, `ErrFFmpeg`, `ErrNetwork`, `ErrUnavailable`, `ErrCanceled`).
- `internal/audio` runs ffmpeg with `f32le`, stereo, 48 kHz PCM, logs
  `first_pcm_ms`, and exposes `SetVolume`/`Volume`/`Position` on `EbitenSink`.
- `internal/ytdlp` resolves stream URLs (`yt-dlp -g`) with bounded stdout/stderr
  and caches them for five hours keyed by video ID; a zero-frame stream is
  treated as stale and retried once with a fresh URL.
- Shutdown cancels the playback actor, waits for all worker goroutines and
  ffmpeg processes, deallocates cached GPU textures, and closes the audio sink.
  Natural EOF waits for a normal ffmpeg exit instead of killing in the EOF
  window.
- `assets/dev-skin/` contains original BMPs at classic dimensions. No external
  skin art is committed.

What is still missing:

- Phase 3B: pure-Go YouTube Music InnerTube `WEB_REMIX` catalog backend in
  `internal/catalog/ytmusic` and `-catalog ytmusic|ytdlp` selection.
- Persistence (config, queue, layout) and M3U import/export.
- Skinned playlist/EQ windows, bitmap text, volume/balance/seek bars, shuffle
  and repeat buttons, and the complete Webamp sprite table.
- Skin fuzzing, a skin corpus, and cross-platform CI.

Keep the good parts: process cancellation, the stale-URL retry, the PCM pipe,
and the short time-to-first-audio. Do not rewrite them into a framework.

## 2. Definition of "full player" (v1)

This is already a large v1. Do not add to it without cutting something else.

- [x] Search YouTube Music (`yt-dlp` in 3A; InnerTube `WEB_REMIX` in 3B) or paste
      a watch/playlist URL (M3U import arrives in Phase 4C/5).
- [x] Queue with play, pause, stop, next, previous, seek, volume, shuffle, repeat.
- [x] Metadata, duration, and artwork for the current track.
- [ ] Classic `.wsz` main window, playlist, and equalizer chrome.
- [ ] Working skinned controls and playlist interaction.
- [ ] Persist skin, volume, queue, playlists, and window positions.
- [x] Recover from expired URLs, network failures, and missing tools with
      actionable errors.
- [x] Run on Linux first; keep Windows/macOS buildable.

Explicitly out of scope for v1:

- Modern `.wal` skins (XML + Maki). Detect and reject with a clear message.
- AVS/MilkDrop, video, minibrowser, CD ripping, device sync.
- True multiple OS windows. Ebitengine owns one window; use internal windows.
- Account login/library sync. Public catalog first.
- EQ DSP, spectrum analyzer, shade/double-size/region masks. These are
  independently shippable Phase 6 chunks, not v1 blockers.

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

Phases 1–3A implemented D1, D2 (fixed main+browser rack), D3 (`yt-dlp`
catalog first), D4, D5, D6 (recorded in ADR 0003), D8, and D10. D2 is refined in
ADR 0002 to use a docked/stacked rack in Phase 4B. D7 and D9 arrive in Phases
4–5.

### Deliberately not doing

- A generic UI framework or plugin system.
- A custom decoder for every BMP bit depth before the corpus proves it is needed.
- InnerTube authentication/cookies until the public player is solid.
- SQLite, an ORM, a dependency-injection container, or a message bus.
- Gapless playback (deep rabbit hole), EQ DSP, or visualizers in v1.
- Native file dialogs or clipboard support unless the Ebitengine limitation
  becomes a real usability blocker.

## 4. Target architecture

```text
cmd/ytm-wmp/main.go          flags, dependency checks, skin load, wiring, RunGame
cmd/ytmctl/main.go           Phase 3 debug CLI (search, track, playlist, artwork)
internal/ui/                 Ebitengine Game, input, layout, browserPanel, debug overlay
internal/ui/skinview/        one-time sheet upload + SubImage cache
internal/ui/screens/         planned: main, playlist, equalizer, browser, settings
internal/skin/               pure .wsz/.zip loader (Phase 1 subset)
internal/playback/           actor Controller, queue, state machine, error taxonomy
internal/audio/              ffmpeg f32le Source + Ebitengine sink
internal/ytdlp/              yt-dlp -g stream resolver, bounded ExecRunner, 5h URL cache
internal/model/              Track, StreamInfo, VideoID, IsVideoID, NormalizeTrackID, DefaultThumbnailURL
internal/deps/               yt-dlp/ffmpeg discovery and version checks
internal/catalog/            Catalog interface, IsPlaylistTarget, debounced Searcher
internal/catalog/ytdlp/      Phase 3A yt-dlp -J catalog implementation
internal/catalog/ytmusic/    Phase 3B pure-Go InnerTube WEB_REMIX catalog implementation (next)
internal/artwork/            thumbnail fetch/decode/resize/disk+LRU cache/placeholder
internal/store/              planned config, queue, M3U, atomic writes
internal/testutil/           planned fake runner/source/skin helpers
assets/dev-skin/             original loose BMPs; no copyrighted skins
docs/adr/                    short decision records (0001, 0002, 0003)
```

`internal/app` was planned for the game loop, but Phase 1 kept `Game` in
`internal/ui` because there is only one screen and no routing layer yet. Split it
out when the playlist/EQ/browser window manager arrives in Phase 4/5, not before.

Dependency direction is one-way:

```text
main -> ui, playback, catalog, artwork, ytdlp, deps, skin
ui -> skinview -> skin + ebiten
ui -> playback, catalog, artwork, model (never ytdlp)
playback -> audio, model (never ui, catalog, or ytdlp)
catalog -> model (never ebiten, ui, playback, or ytdlp)
artwork -> stdlib + x/image only (never ebiten, ui, or playback)
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
  ├─ first tick: audio.Init + skinview.New (textures uploaded once)
  ├─ drain playback events       errors, state changes, TrackChanged, QueueChanged
  ├─ refresh Game.snap           single Controller.Snapshot() read per tick
  ├─ updateBrowser               search results, enrichments, queue enrich, lazy modal art, artwork drain
  ├─ pointer.Update              transport + browser widget clicks
  └─ handleKeys / F1 debug       commands sent via Controller.Send(...)

Ebitengine Draw
  └─ cached sprites + browser panel + placeholders + optional error/debug overlay (pure; no I/O or texture creation)
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
func Placeholder(size int) image.Image

// internal/skin (implemented Phase 1 subset)
type Sprite struct {
    Sheet string          // "main.bmp", "cbuttons.bmp", ...
    Src   image.Rectangle // source rect in the sheet
}

type Skin struct { /* sheets + warnings; accessors below */ }

func Open(path string) (*Skin, error)
func (s *Skin) Lookup(name string) (image.Image, image.Rectangle, bool)
func (s *Skin) Sheet(name string) (image.Image, bool)
func (s *Skin) SheetNames() []string
func (s *Skin) Warnings() []string

// Planned: OpenFS/OpenBytes for fuzzing; Font and Colors in Phase 4.
```

Commands/events are sealed types (`Play`, `PlayIndex`, `Pause`, `Resume`,
`Stop`, `Next`, `Prev`, `Seek`, `SetVolume`, `SetQueue`, `Enqueue`,
`UpdateTrack`, `SetShuffle`, `SetRepeat`; `StateChanged`, `TrackChanged`,
`QueueChanged`, `VolumeChanged`, `PlaybackError`).

## 6. `.wsz` parser design

A `.wsz` is a ZIP with classic Winamp assets. The practical spec is "what
Winamp 2.x and Webamp accept", so build against a corpus and Webamp's sprite
table, not blog posts.

### 6.1 Loading pipeline

Implemented in Phase 1:

1. Accept `.wsz`, `.zip`, or a directory of loose files. The file extension is
   not trusted; files are opened as ZIP.
2. Read BMP entries through hard limits: 8 MiB per asset, 32 MiB total BMP
   bytes, 16M total decoded pixels, 256 BMP assets, and 1024 ZIP entries.
   Never extract to disk; loose files are read through `io.LimitReader`.
3. Build an index of lowercase basenames. Normalize `\` to `/`, ignore
   `__MACOSX/`, `.DS_Store`, and `Thumbs.db`. Duplicate basenames currently
   return a clear error; Phase 4 can choose "shortest path, then lexical" if a
   real corpus needs it.
4. Detect `skin.xml` and return `ErrModernSkin` with a clear message.
5. Decode BMPs eagerly and deterministically. `main.bmp` is required. Optional
   sheets that fail to decode become warnings and placeholders rather than
   rejecting a usable main window. Phase 1 needs `main.bmp`, `cbuttons.bmp`, and
   `titlebar.bmp`; the rest of the sheet list below is Phase 4.
6. Build sprites from the canonical table. Validate every rect against its
   sheet; a missing or truncated sheet yields a placeholder, not a panic.
7. Return a `Skin` with decoded sheets and warnings. Bitmap font and colors are
   Phase 4.

Planned for Phase 4:

8. Full required sheet set: `shufrep.bmp`, `posbar.bmp`, `volume.bmp`,
   `balance.bmp`, `playpaus.bmp`, `monoster.bmp`, `numbers.bmp` (or
   `nums_ex.bmp`), and `text.bmp`. Optional: `nums_ex.bmp`, `pledit.bmp`,
   `eqmain.bmp`, `eq_ex.bmp`, `gen.bmp`, `genex.bmp`, `viscolor.txt`,
   `pledit.txt`, `region.txt`.
9. Decode lazily/targeted if corpus memory measurements justify it; eager
   decoding is simpler and currently bounded.
10. Parse `pledit.txt` (INI-like colors) and `viscolor.txt` (24 RGB lines) with
    sensible defaults when absent or malformed.

### 6.2 BMP handling

- `golang.org/x/image/bmp` supports 8, 24, and 32 bpp. Phase 1 uses it directly.
- For 32-bit BMPs, if the entire alpha channel is zero, force alpha to opaque.
  Many classic skins store alpha as zero and expect opaque pixels. This is
  implemented for both RGBA and NRGBA decodes.
- If the corpus contains 1/4/16-bpp BMPs, add a small fallback decoder then.
  Do not write it speculatively. A broken optional sheet already falls back to a
  placeholder; a broken `main.bmp` fails with the asset name.
- `image.DecodeConfig` runs first and enforces per-image and total pixel budgets
  before allocation.
- Current errors wrap the asset name; `ErrModernSkin` is a named sentinel. A
  fuller `ErrUnsupportedBMP`/`ErrBadImage` taxonomy is still planned.

### 6.3 Sprite table and text

- Phase 1 implemented the subset used by the walking skeleton: main background,
  active/inactive title bar, and six transport buttons (normal/pressed). The
  table lives in `internal/skin/sprites.go` with tests that keep every rect
  inside its sheet.
- Port the rest of Webamp's `skinSprites.ts` (MIT; keep attribution) in Phase 4.
  It is the canonical source for coordinates, including the awkward
  `titlebar.bmp` clutter bar and the playlist frames.
- Do not hand-transcribe coordinates from tutorials. A wrong rect is hard to
  debug; the table is data, not logic.
- `text.bmp` is a 5x6 glyph grid (31 columns x 3 rows). Port Webamp's
  `FONT_LOOKUP` (lowercase, digits, punctuation, `Å Ö Ä ? *`). Unsupported
  runes fall back to a TTF or `?`.
- `numbers.bmp`/`nums_ex.bmp` digits are 9x13. Use `nums_ex.bmp` when present.
  Match Webamp's time-display layout for the colon/minus.
- Volume and balance backgrounds are multi-frame strips (e.g. 68x420 and
  38x420), not a single static image. Implement a slider-frame helper and
  verify frame height/offset against Webamp's slider implementation.
- Playlist colors and current-track highlight come from `pledit.txt`; fall back
  to readable defaults.

### 6.4 Security and robustness

- Implemented: per-asset and total byte caps, total pixel cap, asset/ZIP-entry
  caps, image dimension caps, symlink-root resolution, and duplicate-name
  rejection. ZIP traversal paths are rejected. Nothing is extracted to disk.
- Never trust names for filesystem access; read only through the archive or
  directory walk.
- Per-asset validation: a missing/broken optional sheet must not block the main
  window. Implemented via warnings and placeholders.
- Planned: fuzz `OpenFS`/`OpenBytes` with arbitrary ZIP/BMP input. A malformed
  skin must never panic or allocate unbounded memory.
- Ship an original dev skin in `assets/dev-skin/` for first run. Done. Do not
  commit copyrighted Winamp skins; keep local test skins in a gitignored
  `skins/`.

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
3. `Game.Close` cancels the context, waits for the playback goroutine, then
   closes the sink. No new ffmpeg process can start after the sink is closed.
4. A seek will discard the player and create a new pipeline; that is Phase 2.

For a long pause, Phase 1 keeps the source, process, and connection alive for
instant resume. `PauseAndStopReading` is deferred to stop/teardown so it cannot
block the UI thread. Phase 2 can decide whether a long pause should stop and
reopen instead.

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

Implemented in Phase 1:

- Upload each decoded sheet to `*ebiten.Image` once on the first `Update` tick,
  after the graphics context exists. `NewImageFromImage` never runs per frame.
- Cache `sheet.SubImage(rect)` results per sprite name and precompute all known
  sprites when the view is built.
- Check every source rect against the uploaded sheet before `SubImage`. A rect
  that does not fit is treated as missing; Ebitengine would otherwise clip it to
  an empty image and hide the bug.
- Draw at integer coordinates with `FilterNearest`.
- Missing sprite: draw a magenta box with the sprite name. Known missing
  placeholders are created once in `skinview.New`, not during `Draw`.

### 8.2 Immediate-mode widgets

Implement only what the skin needs. Phase 1 has the first two pieces:

- **Implemented:** button (play/pause) with normal/pressed sprites; clicks fire
  on release inside the widget, presses capture the widget, and dragging out
  cancels. Hot/active IDs are tracked; a press first observed outside a widget
  does not click it.
- **Implemented:** fixed logical layout and logical cursor coordinates.
- **Pending:** toggle (4 sprites), horizontal/vertical slider, scrollbar, list
  row, text field.

Use stable IDs (hash of `"window.widget"`), hot/active state, and press/release
semantics. Do not build a full retained widget tree.

### 8.3 Windows

- **Implemented:** fixed 275x348 logical canvas stacking the 275x116 main
  window (active/inactive title bar, transport buttons + Eject) above the
  275x232 catalog browser panel (`browserPanel`).
- **Pending (Phase 4B):** skinned playlist (`275x232`) and equalizer (`275x116`)
  windows in a **docked/stacked Winamp rack** (`Main 275x116`, toggleable `EQ
  275x116`, toggleable `Playlist/Browser 275x232`), resizing the OS window only
  on discrete `EQ` / `PL` show/hide toggles rather than dynamically morphing the
  OS window bounding box during mouse drags (see ADR 0002).
- Shade mode, arbitrary floating drags, and double-size are Phase 6. Keep all
  coordinates in 1x logical space and scale only at the draw boundary.
- Playlist: use `pledit.bmp` frames for top/bottom/scrollbar; virtualize rows;
  click/shift-click/ctrl-click; double-click to play; wheel and scrollbar.
- EQ: render `eqmain.bmp` chrome and sliders. Visual-only in Phases 4-5; DSP is
  optional Phase 6A.
- Browser/search: implemented as `browserPanel` in Phase 3A; Phase 4/5 can style
  it from `pledit` colors (or `gen.bmp` if present), with a TTF search
  field/results and a bitmap-font header.
- **Implemented:** F1 debug overlay with FPS/TPS, playback state, queue index,
  position/duration, volume, shuffle/repeat, track ID, cursor/hot/active IDs,
  sprite destination rects (cyan), and widget hit areas (magenta). Missing
  sprites are labelled on the canvas.
- **Pending in debug:** skin name, missing-asset list.

### 8.4 Text and scale

Not implemented in Phase 1. The window is fixed at 1x; Ebitengine reports
cursor positions in logical pixels, which the input code relies on.

- Bitmap font for main-window chrome and ASCII playlist text; TTF fallback for
  non-ASCII. Tinting via `ColorScale` works when glyphs are white.
- Marquee: clip with a subimage of the destination; scroll only when the text
  exceeds the clip width. Do not allocate a new offscreen image every frame.
- For 2x mode, keep logical coordinates at 1x and set the window size to 2x.
  Verify mouse coordinates are logical; if not, divide by the scale.

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

### 9.2 InnerTube `WEB_REMIX` catalog (3B — next)

- Package `internal/catalog/ytmusic` implementing `catalog.Catalog` (`Search`,
  `Track`, `Playlist`) via pure-Go HTTP POST requests to YouTube Music
  InnerTube endpoints (`https://music.youtube.com/youtubei/v1/search`, `next`,
  `browse`) using the `WEB_REMIX` client context.
- Keep `internal/ytdlp` (`yt-dlp -g`) as the stream URL resolver so signature and
  cipher churn remain isolated in `yt-dlp`.
- Add a `-catalog ytmusic|ytdlp` flag in both `cmd/ytm-wmp` and `cmd/ytmctl`.
- Do not add account/auth/cookies in v1. If needed later, isolate auth in a
  separate component and never log secrets.
- Test offline against recorded `WEB_REMIX` JSON response fixtures in
  `internal/catalog/ytmusic/testdata/`; gate live InnerTube tests behind
  `YTM_LIVE=1`.

### 9.3 Artwork (3A — done)

- Picks thumbnail closest to `360px` (preferring non-WebP on ties), falling back
  to `model.DefaultThumbnailURL`; fetches with a `10s` timeout and `ytm-wmp/0.1`
  User-Agent.
- Decodes JPEG, PNG, and WebP (`golang.org/x/image/webp`).
- Resizes to list (`64x64`) and now-playing (`300x300`) sizes with
  `golang.org/x/image/draw` (`ApproxBiLinear`), plus `artwork.Placeholder`.
- Caches original bytes atomically on disk by video ID (`os.UserCacheDir()/ytm-wmp/artwork`)
  and decoded `Bundle`s in a 64-entry in-memory LRU with single-flight dedup.
- Never fetches, decodes, or allocates GPU textures inside `Draw()`; `64x64`
  textures upload inside `Update()`, `300x300` textures upload lazily when the
  modal opens, and evicted/closed textures call `Deallocate()`.

## 10. Persistence

Not implemented yet; Phase 5.

```go
type Config struct {
    Version    int
    Skin       string
    Volume     float64
    Scale      int
    Windows    map[string]WindowState
    Queue      []TrackRef
    QueueIndex int
    Shuffle    bool
    Repeat     string
    LastSearch string
    ToolPaths  struct{ YTDLP, FFmpeg string }
}
```

- Location: `os.UserConfigDir()/ytm-wmp/config.json`; cache/artwork under
  `os.UserCacheDir()/ytm-wmp/`.
- Atomic write: temp file in the same directory, `Sync`, `Rename`. Handle
  Windows rename-over-existing explicitly.
- Debounce writes (e.g. 500 ms) and flush on exit/SIGTERM. Never write per frame.
- Store video IDs and metadata, never signed stream URLs or cookies.
- Version the schema and write a migration function even if v1 has only one
  version; it is cheap now.

## 11. Phases

Sizes are relative: S = a few sessions, M = about a week of evenings, L =
multiple weeks. The critical path is 0 -> 1 -> 2 -> 3A -> 3B -> 4 -> 5 -> 7.
Phases 0, 1, 2, 3A, and 3.5 cleanup are complete; Phase 3B is next.

| Phase | Outcome | Size | Status |
|-------|---------|------|--------|
| 0 | Clean seams, tests, CLI still works | S | done |
| 1 | Ebitengine window with a skinned main window and audio | M | done |
| 2 | Playback core: queue, seek, recovery | L | done |
| 3A | `yt-dlp` catalog, artwork cache, browser panel | M | done |
| 3B | Pure-Go YTM InnerTube `WEB_REMIX` catalog backend | S-M | next |
| 4 | Complete classic skin system | L | planned |
| 5 | Full player UX and persistence | L | planned |
| 6 | Fidelity: EQ DSP, visualizer, shade/region (optional) | L | optional |
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

### Phase 3 — Catalog: search, metadata, artwork (done — 3A)

**Goal:** stop hardcoding tracks.

**Outcome:** completed Phase 3A. Moved `StreamInfo` into `internal/model` and
exported `ytdlp.VideoID`, added `internal/catalog` (`Catalog` interface,
`IsPlaylistTarget`, debounced/generation-tagged `Searcher`), implemented
`internal/catalog/ytdlp` (`yt-dlp -J` search, track metadata, and playlist
extraction with stdout cap, caches, and JSON fixtures), added `internal/artwork`
(HTTP fetch, JPEG/PNG/WebP decode, 64x64 & 300x300 resize, atomic disk cache,
in-memory LRU, placeholder), added `cmd/ytmctl`, added `playback.UpdateTrack`
for non-interrupting metadata enrichment, and integrated the catalog browser
panel, search box, artwork display, and `-query`/`-playlist` flags into
`internal/ui` and `cmd/ytm-wmp`.

**Work (3A, yt-dlp — done)**

- `Catalog` interface and yt-dlp implementation.
- Search, track metadata, playlist extraction; JSON fixtures.
- Search debounce/cancel/generation; result cache.
- Artwork fetch/decode/resize/disk cache/placeholder.
- Browser window: search field, results list, play/queue actions, metadata enrichment.
- `cmd/ytmctl` debug CLI (`search`, `track`, `playlist`, `artwork`).

**Work (3B, YouTube Music InnerTube — next)**

- Implement `internal/catalog/ytmusic` behind `catalog.Catalog` (`Search`,
  `Track`, `Playlist`) using pure-Go HTTP POST calls to
  `https://music.youtube.com/youtubei/v1/search`, `next`, and `browse` with the
  `WEB_REMIX` client context.
- Keep `internal/ytdlp` (`yt-dlp -g`) as the stream URL resolver.
- Add a `-catalog ytmusic|ytdlp` flag in `cmd/ytm-wmp` and `cmd/ytmctl`.
- Test offline with recorded `WEB_REMIX` JSON response fixtures in
  `internal/catalog/ytmusic/testdata/` and gate live requests behind `YTM_LIVE=1`.

**Verify:** 10 results with title/artist/duration/thumbnail; selecting plays;
playlist URL imports; repeat search is instant; UI stays responsive; fixtures
pass without network.

**Learn:** JSON decoding, subprocess output, HTTP timeouts, image decoding and
resizing, caching, debouncing.

### Phase 4 — Classic skin system

**Goal:** the distinctive part: full main, playlist, and EQ chrome.

**4A. Parser and main-window renderer**

- Loader hardening (ZIP/dir, names, modern rejection, caps, alpha, optional
  assets) already landed in Phase 1. Remaining: fuzzing, a real skin corpus, and
  any 1/4/16-bpp fallback the corpus proves necessary.
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

### Phase 5 — Full player UX and persistence

**Goal:** a real application, not a skinned demo.

**Work**

- Browser tabs (songs/playlists; albums/artists only with InnerTube), detail
  views, queue panel.
- Search field focus/caret/IME; keyboard shortcuts (space, arrows, `/`, Enter,
  Esc).
- Error toasts and a status area; dependency messages.
- Settings: skin path/picker, tool paths, cache location/size, scale, buffer.
- Persistence: config, queue, window layout, volume, skin; atomic/debounced
  writes; schema version.
- M3U import/export of YouTube URLs/IDs.
- Optional: MPRIS/media keys on Linux.

**Verify:** cold start restores last skin/volume/queue/layout; full mouse and
keyboard operation; missing dependency has an actionable message; a full evening
of use needs no terminal.

**Learn:** focus management, text input/IME, debounce, config dirs, atomic
writes, schema migration.

### Phase 6 — Fidelity chunks (optional, independently shippable)

- 6A EQ DSP: 10 biquads + preamp as an `io.Reader` between ffmpeg and the
  player; float32; bypass; clipping.
- 6B Spectrum/oscilloscope: tee PCM into a ring buffer, Hann window, FFT,
  `viscolor.txt`.
- 6C Shade/double-size/region masks; point-in-polygon hit testing.
- 6D Skin browser/cursors; skin folder scanning; `.cur` if cheap.
- 6E Gapless/MPRIS/media keys; only after the rest is solid.

Each chunk must have a visible/audible demo and not regress v1.

### Phase 7 — Hardening and v0.1

**Work**

- Error paths/timeouts: missing/old yt-dlp, ffmpeg failure, offline, rate limit,
  age-restricted/members-only, expired URL.
- Performance: pprof CPU/heap, frame budget, allocation check in `Draw`,
  time-to-first-audio measurement.
- Tests: parser/queue/state unit tests, fake-process integration, corpus smoke,
  `-race`, fuzz skin/BMP.
- Packaging: Linux binary; Windows/macOS CI on native runners; Windows
  `-ldflags -H=windowsgui`; desktop entry/icon.
- Docs: README install/troubleshooting, architecture/ADRs, legal note (no
  bundled skins/content; yt-dlp/YouTube ToS).
- Release: tag v0.1.0, smoke checklist, keep yt-dlp update instructions
  prominent.

**Verify:** v0.1 builds on Linux and one other OS; a clean machine can install
deps, launch, load a skin, search, and play; 20-skin smoke test passes; known
limitations documented.

## 12. Risks and mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Ebitengine audio + live pipe teardown deadlocks/races | High | Phase 1 spike passed; close/kill the source before `PauseAndStopReading`; natural EOF waits for exit; fake-process + live shutdown tests; `-race` |
| `PauseAndStopReading` blocks on a stalled read | High | Close/kill ffmpeg first; close the pipe; two-second EOF reap timeout; still add a fake stalled-reader test |
| Paused session holds ffmpeg and the network connection | Medium | Deliberate for instant resume in Phase 1; Phase 2 decides whether long pauses stop and reopen |
| YouTube/yt-dlp changes break playback | High | Cache by video ID; one stale retry; version check; clear update message; keep catalog/resolver boundaries |
| InnerTube API fragility | High | Public yt-dlp path first; wrap third-party clients; yt-dlp fallback for streams |
| Skin incompatibility / BMP bit depths | Medium | Webamp sprite table; corpus-driven fallbacks; per-asset warnings/placeholders; fuzz loader |
| Alpha quirks in 32-bit BMPs | Medium | All-zero alpha -> opaque; test fixtures |
| Text/Unicode in bitmap skins | Medium | Bitmap chrome + TTF fallback; explicit `?` last resort |
| One-window limitation | Medium | Decide early; internal windows + snapping; document; transparent-canvas experiment only after v1 |
| Scope creep (EQ/visualizer/gapless/account) | High | v1 checklist; Phase 6 optional; ship Phase 4/5 first |
| Cross-compilation/Cgo | Medium | Linux first; native CI for Windows/macOS; document deps |
| Legal/ToS | Medium | No bundled skins/content; personal-use docs; don't log/redistribute streams |
| External tools in sandboxes | Medium | Startup checks; configurable paths; Flatpak notes |

## 13. Testing and instrumentation

### Current coverage (after Phase 3A & Phase 3.5 cleanup)

- **Model & yt-dlp:** `VideoID`/`IsVideoID`/`NormalizeTrackID`/`DefaultThumbnailURL`
  table-driven tests, 5h URL cache expiry, stale-URL `Forget`, bounded stdout/stderr
  subprocess caps (`ExecRunner`), `ffmpeg` argument construction, zero-frame
  classification, and dependency discovery.
- **Catalog & artwork:** `catalog.IsPlaylistTarget`, debounced/generation-tagged
  `catalog.Searcher` cancellation, `internal/catalog/ytdlp` offline JSON fixture
  tests (`search`, `track`, `playlist`, stdout cap), `internal/artwork`
  JPEG/PNG/WebP decode/resize/disk+LRU cache/single-flight tests, and
  `cmd/ytmctl` CLI tests.
- **Playback:** queue navigation/history/non-repeating shuffle bag/repeat modes,
  `UpdateTrack` enrichment, zero-copy read-only `Snapshot.Queue`, seek-by-reopen,
  URL prefetching, stale-URL retry, error taxonomy, and 20-track-change goroutine
  leak check.
- **Skin loader:** directory and `.wsz`/`.zip` loading, backslashes/nested
  paths, duplicate rejection, modern-skin rejection, broken/oversized BMPs,
  total byte/pixel budgets, symlinked directories, ZIP traversal, RGBA/NRGBA
  alpha, sprite definitions, and the dev-skin asset dimensions.
- **UI:** press/release/cancel/drag behavior, topmost hit testing, named layout
  bounds, `browserPanel` search/playlist/queue actions, retryable/queue-aware
  enrichment, bounded 4-worker concurrency, lazy `300x300` texture upload +
  `Deallocate()`, play/pause/resume/error/shutdown state, first-Update
  initialization, draw smoke test, debug wrapping, and texture cache/placeholders.
- **Audio:** f32le format contract, sink lifecycle, pause/resume, pause during
  resolve, teardown ordering, clean EOF vs. nonzero exit, and explicit kill.
- **Opt-in live tests:** `YTM_WMP_LIVE_TEST=1` in `internal/playback` and
  `YTM_LIVE=1` in `internal/catalog/ytdlp`.
- `go test -race ./...` and `go vet ./...` pass. Building Ebitengine tests
  requires X11/GL development headers (documented in the README).

### Testing pyramid

1. **Pure unit tests:** skin parser, sprite table, text lookup, queue logic,
   state transitions, config serialization. Fast, run in CI.
2. **Fake-process integration:** replace yt-dlp/ffmpeg with Go fakes or tiny
   scripts emitting known JSON/PCM. Tests args, errors, and teardown without
   network. Phase 0/1 already use this for playback and ffmpeg lifecycle.
3. **Opt-in live tests:** real yt-dlp + ffmpeg behind `YTM_LIVE=1`. CI never
   depends on YouTube. Not yet implemented.
4. **Visual tests:** render a skin to a screenshot with Xvfb where possible;
   otherwise a manual corpus checklist. Not yet implemented.
5. **Audio tests:** fake source with silence/sine; assert frame counts and state
   transitions, not sound quality. Current tests use fake sources and real
   short-lived fake processes.

### Commands

```sh
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
go build ./...

# Planned once the fuzz/live fixtures exist:
go test -fuzz=FuzzSkinOpen ./internal/skin -fuzztime=30s
YTM_LIVE=1 go test ./internal/catalog/ytdlp -run Live
```

### Instrumentation

- `log/slog` with `-debug`. Implemented.
- Current debug logs: playback start/finish, sink cancellation/source done with
  frame counts, skin warnings, dependency versions, skin sheets.
- Ring buffer of ffmpeg stderr per stream. Implemented.
- Debug overlay: FPS/TPS, play/pause state, cursor, sprite rects, and hit areas.
  Implemented; queue index/position/skin name/missing-asset list are pending.
- Log spans for resolve/open/first PCM/seek/teardown. Only start/finish and
  teardown frame counts exist today.
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

Next up is Phase 3B (`internal/catalog/ytmusic` pure-Go YouTube Music InnerTube
`WEB_REMIX` catalog backend + `-catalog ytmusic|ytdlp`), followed by Phase 4A/4B
(full Webamp sprite table, `pledit.txt`/`viscolor.txt`, bitmap text/digits,
sliders/toggles, and the docked/stacked Winamp rack window manager).

## 15. Open questions

Status is marked for each:

1. **Catalog:** Is `yt-dlp` search (YouTube results) acceptable for v1, or do you
   require true YouTube Music results via InnerTube? **Open.** Recommendation:
   ship yt-dlp search first behind `Catalog`; add InnerTube in 3B if needed.
2. **Account/library:** public catalog only, or cookies/OAuth for liked
   songs/private playlists? **Open.** Recommendation: public only; separate
   phase.
3. **Platforms:** Linux-only v1, or Windows/macOS CI from the start?
   **Decided for now: Linux first, keep the others buildable.** Native CI later.
4. **Window model:** one OS window with internal Winamp-style windows?
   **Decided: yes**, recorded in ADR 0002. Phase 1 uses one fixed main window.
5. **Skin fidelity:** main + playlist + EQ chrome enough for v1, with shade,
   double-size, and `region.txt` deferred to Phase 6? **Decided: yes.**
6. **Local files/M3U:** support local audio files, or only YouTube URLs/IDs?
   **Open.** Recommendation: URLs/IDs only in v1; local files are a separate
   source abstraction.
7. **Tool distribution:** require system `yt-dlp`/`ffmpeg`, or bundle/auto-
   download? **Decided: system tools with startup checks**, implemented in
   `internal/deps`. Bundling remains a release-time decision.
8. **Module path:** keep `module ytm-wmp`, or switch to a GitHub path before
   imports spread? **Open.** Recommendation: switch before the repo is public.
9. **Browser window styling:** classic-skin-colored panel or full `gen.bmp`
   frame? **Open.** Recommendation: simple panel first; `gen.bmp` later.
10. **Clipboard/file dialogs:** add a dependency or skip? **Decided for now:
    skip.** Use typed paths and a scanned skin folder.
