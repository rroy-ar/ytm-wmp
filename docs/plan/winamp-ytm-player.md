# Plan: from the PoC to a full YouTube Music player with classic `.wsz` skins

Status: proposal. This is the execution plan; the root `PLAN.md` is a broader
brainstorm and should be retired once this document is accepted so there is one
source of truth.

## 1. Where the project is today

The current PoC (`cmd/winamp-ytm/main.go`) proves the hard transport path:

- `yt-dlp -g` resolves a direct audio URL.
- `ffmpeg` turns that URL into a live `s16le`, 48 kHz, stereo PCM pipe.
- `beep`/`oto` plays the pipe with a ~100 ms buffer.
- Stream URLs are cached for five hours; a zero-frame stream is treated as a
  stale URL and retried once.
- Shutdown kills/closes the pipe before clearing the speaker, avoiding the
  classic blocked-pipe deadlock.

What is missing:

- No Ebitengine dependency and no real UI.
- No `.wsz` parser.
- One hardcoded track; no search, queue, metadata, artwork, or persistence.
- No seek, next/previous, volume, shuffle, or repeat.
- Audio is tied to `beep`; the UI and audio would use two different backends.
- Almost everything is in `package main`; there are no tests.
- Built binaries (`main`, `winamp-ytm`) are in the worktree; `.gitignore` only
  covers `/winamp-ytm`.

Keep the good parts: process cancellation, the stale-URL retry, the PCM pipe,
and the short time-to-first-audio. Do not rewrite them into a framework.

## 2. Definition of "full player" (v1)

This is already a large v1. Do not add to it without cutting something else.

- [ ] Search YouTube Music, paste a watch/playlist URL, or import an M3U list.
- [ ] Queue with play, pause, stop, next, previous, seek, volume, shuffle, repeat.
- [ ] Metadata, duration, and artwork for the current track.
- [ ] Classic `.wsz` main window, playlist, and equalizer chrome.
- [ ] Working skinned controls and playlist interaction.
- [ ] Persist skin, volume, queue, playlists, and window positions.
- [ ] Recover from expired URLs, network failures, and missing tools with
      actionable errors.
- [ ] Run on Linux first; keep Windows/macOS buildable.

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
| D1 | Audio backend | `ebiten/v2/audio` with `f32le` from ffmpeg | One audio device/event loop. `beep` + `ebiten/audio` would fight over oto. Risk: pipe teardown; spike it first. Fallback: keep `beep` and render with Ebitengine, but accept two backends. |
| D2 | Window model | One OS window containing draggable internal windows | Ebitengine creates one window per process. True Winamp multi-window behavior needs a second UI stack; not worth it for v1. |
| D3 | Catalog source | `yt-dlp` first; optional InnerTube `WEB_REMIX` behind the same interface | yt-dlp owns signature/format churn. InnerTube gives true YTM results but is fragile and adds auth/API work. |
| D4 | Skin parser | Pure Go in `internal/skin`, no Ebitengine imports | Testable/fuzzable with `image.Image`, rects, and strings. UI uploads decoded sheets to the GPU once. |
| D5 | UI model | Immediate-mode hit testing over a small window list | Winamp widgets are fixed sprites with a few states. A retained UI framework is over-engineering. |
| D6 | Concurrency | Playback actor + commands/events + atomic snapshot | One goroutine owns playback state. UI never touches the player or the queue. |
| D7 | Persistence | Versioned JSON, atomic temp+rename | Config/queue are small. SQLite is overkill. Write on debounce and on exit. |
| D8 | Tool distribution | Require system `yt-dlp` and `ffmpeg`; check at startup | Bundling/auto-updating external tools adds size and supply-chain work. Show actionable install/update messages. |
| D9 | Text | Bitmap `text.bmp` for chrome; embedded TTF fallback for non-ASCII | Classic skins have no Unicode. Search/playlist titles do. Use bitmap for classic look, TTF where needed. |
| D10 | Artwork cache | Files on disk by video ID; in-memory LRU of decoded images | Simple, inspectable, no database. Fetch/decode off the UI goroutine. |

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
cmd/ytm-wmp/main.go          flags, dependency checks, audio.NewContext, RunGame
cmd/ytmctl/main.go           optional debug CLI for catalog/resolver (Phase 3)
internal/app/                ebiten.Game, wiring, input routing, lifecycle
internal/ui/                 immediate-mode widgets, windows, SkinView, debug
internal/ui/screens/         main, playlist, equalizer, browser, settings
internal/skin/               pure .wsz/.zip parser: BMP, sprites, text, colors
internal/playback/           actor, queue, state machine, commands/events
internal/audio/              ffmpeg source, PCM reader, ebiten player wrapper
internal/catalog/            Catalog interface and domain-facing service
internal/catalog/ytdlp/      search, metadata, playlist extraction
internal/catalog/ytmusic/    optional InnerTube implementation (Phase 3B)
internal/artwork/            thumbnail fetch, decode, resize, disk cache
internal/model/              Track and other shared domain types
internal/store/              config, queue, M3U, atomic writes
internal/deps/               yt-dlp/ffmpeg discovery and version checks
internal/testutil/           fake runner, fake source, synthetic skins
assets/dev-skin/             original loose BMPs; no copyrighted skins
docs/adr/                    short decision records
```

Dependency direction is one-way:

```text
app -> ui -> skin
app -> playback -> audio
app -> playback -> catalog (via a narrow resolver interface)
app -> catalog -> model
ui -> model, playback snapshot/commands, store
skin must not import ebiten
catalog must not import ui or playback
playback must not import ui
```

`skin` works with `image.Image`, `image.Rectangle`, strings, and structs. That is
what makes it unit-testable and fuzzable without a graphics context.

### Runtime flow

```text
Ebitengine Update (60 TPS)
  ├─ ui.Update()                 mouse/keyboard -> widget events
  ├─ drain playback events       state changes, track changes, errors
  ├─ send playback commands      play/pause/seek/next/...
  └─ publish/read snapshot       atomic pointer; no blocking

Ebitengine Draw
  └─ ui.Draw(screen, snapshot)   cached sprites + bitmap text only
```

Rules:

- Never call `yt-dlp`, `ffmpeg`, HTTP, or disk I/O from `Update`/`Draw`.
- Only the playback actor mutates playback state.
- Tag every async operation with a generation/request ID; drop stale results.
- The UI reads an immutable `Snapshot`; it does not call audio methods.

### Playback state machine

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
- Auto-advance happens when the source reports EOF and the player stops, or when
  the known duration is reached. Do not rely on wall-clock time.

## 5. Key interfaces

These are signatures/pseudocode, not implementation.

```go
// internal/model
type Track struct {
    ID          string // YouTube video ID; the identity key
    Title       string
    Artist      string
    Album       string
    Duration    time.Duration
    ThumbnailURL string
}

// internal/catalog
type Catalog interface {
    Search(ctx context.Context, query string, limit int) ([]model.Track, error)
    Playlist(ctx context.Context, idOrURL string) ([]model.Track, error)
    Track(ctx context.Context, id string) (model.Track, error)
}

// internal/audio
type StreamInfo struct {
    URL       string
    ExpiresAt time.Time
}

type Resolver interface {
    Resolve(ctx context.Context, videoID string) (StreamInfo, error)
}

type Source interface {
    io.ReadCloser
    Err() error
    Frames() int64
    Done() <-chan struct{} // process/read loop ended
}

type Opener interface {
    Open(ctx context.Context, streamURL string, offset time.Duration) (Source, error)
}

// internal/playback
type Snapshot struct {
    State     State
    Track     model.Track
    Position  time.Duration
    Duration  time.Duration
    Volume    float64
    Queue     []model.Track
    Index     int
    Shuffle   bool
    Repeat    RepeatMode
    LastError string
}

type Controller struct { /* ... */ }

func (c *Controller) Commands() chan<- Command
func (c *Controller) Events() <-chan Event
func (c *Controller) Snapshot() Snapshot // atomic read for Draw
func (c *Controller) Run(ctx context.Context)

// internal/skin
type Sprite struct {
    Sheet string
    Rect  image.Rectangle
}

type Skin struct {
    Sheets  map[string]image.Image // "main", "cbuttons", ...
    Sprites map[string]Sprite      // canonical Webamp names
    Font    *BitmapFont
    Colors  Colors
}

func Open(path string) (*Skin, error)
func OpenFS(fsys fs.FS) (*Skin, error) // testable and fuzzable
```

Commands/events are sealed types (`Play`, `Pause`, `Resume`, `Stop`, `Next`,
`Prev`, `Seek`, `SetVolume`, `SetQueue`, `Enqueue`; `StateChanged`,
`TrackChanged`, `PlaybackError`). Keep them small and serializable if that helps
debugging.

## 6. `.wsz` parser design

A `.wsz` is a ZIP with classic Winamp assets. The practical spec is "what
Winamp 2.x and Webamp accept", so build against a corpus and Webamp's sprite
table, not blog posts.

### 6.1 Loading pipeline

1. Accept `.wsz`, `.zip`, or a directory of loose files.
2. Read the archive into memory with a hard cap (e.g. 64 MiB compressed). For a
   directory, walk with the same caps. Never extract to disk.
3. Build an index of lowercase basenames. Normalize `\` to `/`, ignore
   `__MACOSX/`, `.DS_Store`, and `Thumbs.db`. For duplicate basenames prefer the
   shortest path, then lexical order.
4. Detect `skin.xml` and return `ErrModernSkin` with a clear message.
5. Decode sheets lazily but deterministically. Required for the main window:
   `main.bmp`, `cbuttons.bmp`, `titlebar.bmp`, `shufrep.bmp`, `posbar.bmp`,
   `volume.bmp`, `balance.bmp`, `playpaus.bmp`, `monoster.bmp`, `numbers.bmp`
   (or `nums_ex.bmp`), `text.bmp`. Optional: `nums_ex.bmp`, `pledit.bmp`, `eqmain.bmp`,
   `eq_ex.bmp`, `gen.bmp`, `genex.bmp`, `viscolor.txt`, `pledit.txt`,
   `region.txt`.
6. Build sprites from the canonical table. Validate every rect against its
   sheet; a missing optional sheet yields a placeholder, not a panic.
7. Parse `pledit.txt` (INI-like colors) and `viscolor.txt` (24 RGB lines). Apply
   sensible defaults when absent or malformed.
8. Return a `Skin` with decoded sheets, sprites, bitmap font, and colors.

### 6.2 BMP handling

- Start with `golang.org/x/image/bmp`, which supports 8, 24, and 32 bpp.
- For 32-bit BMPs, if the entire alpha channel is zero, force alpha to opaque.
  Many classic skins store alpha as zero and expect opaque pixels.
- If the corpus contains 1/4/16-bpp BMPs, add a small fallback decoder then.
  Do not write it speculatively.
- Use `image.DecodeConfig` first and enforce max dimensions/pixels before
  allocating.
- Keep a named error (`ErrUnsupportedBMP`, `ErrBadImage`) with the asset name.

### 6.3 Sprite table and text

- Port the sprite table from Webamp's `skinSprites.ts` (MIT; keep attribution).
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

- Cap compressed size, total uncompressed size, file count, and image
  dimensions. Reject ZIP bombs before decompression.
- Never trust names for filesystem access; read only through the archive `fs.FS`.
- Per-asset validation: a missing `eq_ex.bmp` must not block the main window.
- Fuzz `OpenFS`/`OpenBytes` with arbitrary ZIP/BMP input. A malformed skin must
  never panic or allocate unbounded memory.
- Ship an original dev skin in `assets/dev-skin/` for first run. Do not commit
  copyrighted Winamp skins; keep local test skins in a gitignored `skins/`.

## 7. Audio and playback design

### 7.1 Ebitengine audio constraints

Verified against Ebitengine v2.10.5:

- `audio.NewContext(sampleRate)` creates the single process-wide context. Call
  it exactly once, before `RunGame` (the official WAV example does this). A
  second call panics.
- One context has one sample rate. Use 48 kHz and let ffmpeg resample.
- `ctx.NewPlayerF32(src)` expects 32-bit float little-endian, stereo, 48 kHz
  PCM. Prefer it over `NewPlayer` (16-bit).
- The source must not be shared between players. A player does not close its
  source; closing remains our responsibility.
- `Player.PauseAndStopReading()` (v2.10+) stops reads and blocks until an
  in-flight read finishes. It is the correct pause/teardown primitive.
- `Player.Close()` is deprecated as of v2.10; do not use it.
- A player is seekable only if its source is `io.Seeker`. A pipe is not.
  `SetPosition` on a non-seeker panics. Never seek this source; recreate the
  ffmpeg pipeline.
- `Player.Position()` is the audio clock. Use it for the UI; do not trust
  wall-clock time alone.

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

1. Cancel the stream context and kill ffmpeg so a blocked read returns.
2. `player.PauseAndStopReading()` (now it cannot block forever).
3. Close the pipe/source, wait for the read loop, then `cmd.Wait`.
4. Discard the player; create a new one for a seek.

For `Stop`, the same order. For a long pause, `PauseAndStopReading` avoids
unbounded buffering; on resume, `Play` uses the kept buffer. If the network died
during the pause, the resumed player reaches EOF quickly; classify and recover.

### 7.4 Seek, EOF, and recovery

- Seek: kill old pipeline, wait for the read loop, create a new source at
  `offset`, create a new player, set `baseOffset = offset`, state `Playing`.
  While seeking, show the target position and keep the UI responsive.
- Position shown to the user = `baseOffset + player.Position()`.
- EOF: wrap the source in an `eofReader` that closes a channel on `io.EOF`.
  Auto-advance when EOF is observed and the player is no longer playing, or
  when the known duration is reached. Test both known- and unknown-duration
  tracks.
- Stale URL: if the source produced zero frames, invalidate the cache entry and
  retry once with a fresh URL. Keep the PoC's behavior.
- Prefetch the next track's URL near the end of the current track; do not
  prefetch audio.
- Error taxonomy: `ErrToolMissing`, `ErrResolve`, `ErrStaleStream`,
  `ErrNoAudio`, `ErrFFmpeg`, `ErrNetwork`, `ErrUnavailable`, `ErrCanceled`.
  Map each to a user-facing message.

## 8. UI design

### 8.1 SkinView

- Upload each decoded sheet to `*ebiten.Image` once, after the graphics context
  exists. Never call `NewImageFromImage` per frame.
- Cache `sheet.SubImage(rect)` results per sprite name. Precompute all sprites
  at load time where possible.
- Draw at integer coordinates with nearest filtering. Pixel-art skins look
  wrong with fractional scaling.
- Missing sprite: draw a magenta box with the sprite name. Silent skipping makes
  coordinate bugs invisible.

### 8.2 Immediate-mode widgets

Implement only what the skin needs:

- button: normal/pressed; click fires on release inside the widget.
- toggle: normal/pressed/selected/selected+pressed (4 sprites).
- horizontal slider: background + thumb; drag capture; value 0..1.
- vertical slider: volume/balance/EQ bands.
- scrollbar: thumb, track, wheel.
- list row: hover/selected/current.
- text field: focus, caret, `AppendInputChars`, simple editing.

Use stable IDs (hash of `"window.widget"`), hot/active state, and press/release
semantics. Do not build a full retained widget tree.

### 8.3 Windows

- Internal windows: main (275x116), playlist, equalizer, browser/search,
  settings. Drag by title bar; click to focus; z-order; active/inactive title
  bars.
- Start with a fixed 275x116 main window. In Phase 4B, compute the union of
  visible window rects, resize the OS window to that union, and translate draw
  and mouse coordinates. Recompute only when layout changes, not every frame.
- Shade mode and double-size are Phase 6. Keep all coordinates in 1x logical
  space and scale only at the draw boundary.
- Playlist: use `pledit.bmp` frames for top/bottom/scrollbar; virtualize rows;
  click/shift-click/ctrl-click; double-click to play; wheel and scrollbar.
- EQ: render `eqmain.bmp` chrome and sliders. Visual-only in Phases 4-5; DSP is
  optional Phase 6A.
- Browser/search: a simple panel styled from `pledit` colors (or `gen.bmp` if
  present), with a TTF search field/results and a bitmap-font header. Keep it
  intentionally simpler than the classic windows.
- Debug overlay (F1): FPS/TPS, state, queue index, position, sprite rects, hit
  areas, skin name, missing assets.

### 8.4 Text and scale

- Bitmap font for main-window chrome and ASCII playlist text; TTF fallback for
  non-ASCII. Tinting via `ColorScale` works when glyphs are white.
- Marquee: clip with a subimage of the destination; scroll only when the text
  exceeds the clip width. Do not allocate a new offscreen image every frame.
- For 2x mode, keep logical coordinates at 1x and set the window size to 2x.
  Verify mouse coordinates are logical; if not, divide by the scale.

## 9. Catalog, search, and artwork

### 9.1 yt-dlp catalog (3A)

- Search: `yt-dlp -J --flat-playlist --playlist-end N "ytsearchN:<query>"`.
- Track metadata: `yt-dlp -J --no-playlist <videoID>`.
- Playlist: `yt-dlp -J --flat-playlist --playlist-end 100 <url>`.
- Parse permissively into a narrow struct; do not leak yt-dlp JSON into the UI
  or playback packages.
- Use `id` as the identity. Cache by video ID, not by raw query/URL.
- Cap stdout size; `yt-dlp -J` can be large for playlists.
- Search as you type with a 250-300 ms debounce; cancel the previous request;
  Enter searches immediately. Never spawn one process per keystroke.
- Cache search results briefly in memory. Cache metadata on disk only if it
  measurably helps.

### 9.2 Optional InnerTube (3B)

- If "YTM" means music.youtube.com results, add an InnerTube `WEB_REMIX`
  implementation behind `Catalog`. Study `go_ytmusicapi` and `innertube-go`, but
  wrap them so their types never leak.
- Keep yt-dlp for stream resolution even when InnerTube supplies metadata.
- Do not add account/auth/cookies in v1. If needed, isolate it in a separate
  `Auth` component and never log secrets.

### 9.3 Artwork

- Pick a thumbnail from `thumbnails`; fetch with a 10 s timeout and a real user
  agent.
- Decode JPEG/PNG; add `golang.org/x/image/webp` if the corpus returns WebP.
- Resize to list (64x64) and now-playing (300x300) sizes with
  `golang.org/x/image/draw`.
- Cache original bytes on disk by video ID; keep a small in-memory LRU of
  decoded images. Show a placeholder while loading.
- Never fetch/decode in `Draw`.

## 10. Persistence

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
multiple weeks. The critical path is 0 -> 1 -> 2 -> 3 -> 4 -> 5 -> 7.

| Phase | Outcome | Size |
|-------|---------|------|
| 0 | Clean seams, tests, CLI still works | S |
| 1 | Ebitengine window with a skinned main window and audio | M |
| 2 | Playback core: queue, seek, recovery | L |
| 3 | Catalog: search, metadata, artwork | M-L |
| 4 | Complete classic skin system | L |
| 5 | Full player UX and persistence | L |
| 6 | Fidelity: EQ DSP, visualizer, shade/region (optional) | L |
| 7 | Hardening and v0.1 release | M |

### Phase 0 — Baseline and seams

**Goal:** keep the CLI behavior, make the code testable, prepare for Ebitengine.
No new features.

**Work**

- Remove the built `main`/`winamp-ytm` binaries from the worktree; ignore both
  in `.gitignore`. Keep no binaries in git.
- Move the command to `cmd/ytm-wmp`; `main` becomes flags, dependency checks,
  wiring, and signals only.
- Extract `internal/ytdlp`:
  - `Resolve(ctx, videoID) (StreamInfo, error)`
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

**Verify:** `go test ./...`; `go run ./cmd/ytm-wmp -url <url>` plays; no binary
is tracked; `main` has no business logic.

**Learn:** interfaces owned by the consumer, `exec.CommandContext`, context
cancellation, `io.Reader` contracts, TTL caches, table-driven tests.

### Phase 1 — Walking skeleton: Ebitengine + first skin + audio

**Goal:** open a 275x116 window, draw `main.bmp`, `titlebar.bmp`, and a play
button, and play/pause a track through `ebiten/audio`. This de-risks the project.

**Work**

- Add Ebitengine v2.10.5+ and `golang.org/x/image`; remove `beep`/`oto`.
- Create the single audio context in `main` before `RunGame`.
- Implement `app.Game` (`Update`, `Draw`, `Layout`).
- Implement the initial `internal/skin` loader: `.wsz`/`.zip`/directory,
  lowercase basenames, `main.bmp`/`cbuttons.bmp`/`titlebar.bmp` decode.
- Implement `ui.SkinView` to upload sheets and cache subimages.
- Draw the main background, active title bar, play/pause/stop buttons.
- Immediate-mode press/release for those buttons.
- Play a hardcoded/`-url` track: resolve, ffmpeg `f32le`, `NewPlayerF32`,
  play/pause, clean shutdown.
- Add `-skin path`; fall back to `assets/dev-skin/` (original BMPs).
- F1 debug overlay for sprite rects and hit areas.

**Verify:** window shows the skin; play makes audio; pause stops it; closing
leaves no ffmpeg; broken skin returns an error; F1 works; `go test -race ./...`.

**Learn:** Ebitengine game loop, logical vs. physical pixels, `SubImage`,
input APIs, audio player lifecycle, PCM frame layout.

### Phase 2 — Playback core

**Goal:** a headless engine good enough that the UI can be dumb.

**Work**

- Commands/events and the actor goroutine; atomic `Snapshot`.
- Queue model: `[]Track`, index, history, shuffle bag, repeat off/one/all.
- Async resolve/open with request generations; seek by recreating the pipeline.
- EOF/auto-advance; prefetch next URL; position tracking.
- Pause/stop semantics; volume; stale-URL retry; error taxonomy.
- Fake `Opener`/`Resolver` for tests; integration tests gated by env.
- Wire the app: UI sends commands, drains events, shows state.

**Verify:** a 10-track queue plays through; seek ~1 s; pause/resume has no
restart; killing the network produces a recoverable error; no goroutine leak
after 20 track changes; `go test -race ./...`.

**Learn:** actor/CSP, state machines, backpressure, process lifetime, ffmpeg
seeking, audio clocks, request IDs.

### Phase 3 — Catalog: search, metadata, artwork

**Goal:** stop hardcoding tracks.

**Work (3A, yt-dlp)**

- `Catalog` interface and yt-dlp implementation.
- Search, track metadata, playlist extraction; JSON fixtures.
- Search debounce/cancel/generation; result cache.
- Artwork fetch/decode/resize/disk cache/placeholder.
- Browser window: search field, results list, play/queue actions.
- Optional `cmd/ytmctl` for debugging catalog before the UI is complete.

**Work (3B, optional)**

- InnerTube `WEB_REMIX` search/browse behind the same interface; yt-dlp remains
  the stream resolver. Do this only if YTM-specific results are required.

**Verify:** 10 results with title/artist/duration/thumbnail; selecting plays;
playlist URL imports; repeat search is instant; UI stays responsive; fixtures
pass without network.

**Learn:** JSON decoding, subprocess output, HTTP timeouts, image decoding and
resizing, caching, debouncing.

### Phase 4 — Classic skin system

**Goal:** the distinctive part: full main, playlist, and EQ chrome.

**4A. Parser and main-window renderer**

- Harden the loader (ZIP/dir, names, modern rejection, caps, alpha, optional
  assets).
- Port the full Webamp sprite table.
- Bitmap text, marquee, time digits, stereo indicator, position bar, volume,
  balance, shuffle/repeat, EQ/PL toggles.
- `pledit.txt`/`viscolor.txt`.
- Fuzz the loader; synthetic fixtures; local skin corpus.

**4B. Widgets and window manager**

- Button/toggle/slider/scrollbar/list/text-field widgets.
- Main/playlist/EQ windows, drag, z-order, focus, active/inactive title bars,
  show/hide, snap.
- Dynamic union canvas (start fixed in Phase 1; add here).
- Shade mode/double-size/region masks: Phase 6 unless cheap.

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
| Ebitengine audio + live pipe teardown deadlocks/races | High | Phase 1 spike; kill ffmpeg before `PauseAndStopReading`; bounded reads; repeated shutdown tests; `-race` |
| `PauseAndStopReading` blocks on a stalled read | High | Cancel/kill ffmpeg first; close pipe; test with a fake stalled reader |
| YouTube/yt-dlp changes break playback | High | Cache by video ID; one stale retry; version check; clear update message; keep catalog/resolver boundaries |
| InnerTube API fragility | High | Public yt-dlp path first; wrap third-party clients; yt-dlp fallback for streams |
| Skin incompatibility / BMP bit depths | Medium | Webamp sprite table; corpus-driven fallbacks; per-asset errors; fuzz loader |
| Alpha quirks in 32-bit BMPs | Medium | All-zero alpha -> opaque; test fixtures |
| Text/Unicode in bitmap skins | Medium | Bitmap chrome + TTF fallback; explicit `?` last resort |
| One-window limitation | Medium | Decide early; internal windows + snapping; document; transparent-canvas experiment only after v1 |
| Scope creep (EQ/visualizer/gapless/account) | High | v1 checklist; Phase 6 optional; ship Phase 4/5 first |
| Cross-compilation/Cgo | Medium | Linux first; native CI for Windows/macOS; document deps |
| Legal/ToS | Medium | No bundled skins/content; personal-use docs; don't log/redistribute streams |
| External tools in sandboxes | Medium | Startup checks; configurable paths; Flatpak notes |

## 13. Testing and instrumentation

### Testing pyramid

1. **Pure unit tests:** skin parser, sprite table, text lookup, queue logic,
   state transitions, config serialization. Fast, run in CI.
2. **Fake-process integration:** replace yt-dlp/ffmpeg with Go fakes or tiny
   scripts emitting known JSON/PCM. Tests args, errors, and teardown without
   network.
3. **Opt-in live tests:** real yt-dlp + ffmpeg behind `YTM_LIVE=1`. CI never
   depends on YouTube.
4. **Visual tests:** render a skin to a screenshot with Xvfb where possible;
   otherwise a manual corpus checklist.
5. **Audio tests:** fake source with silence/sine; assert frame counts and state
   transitions, not sound quality.

### Commands

```sh
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
go test -fuzz=FuzzSkinOpen ./internal/skin -fuzztime=30s
YTM_LIVE=1 go test ./internal/catalog/ytdlp -run Live
go build ./...
```

### Instrumentation

- `log/slog` with `-debug`.
- Log spans: resolve, open ffmpeg, first PCM, player start, seek, teardown.
- Ring buffer of ffmpeg stderr per stream.
- Debug overlay: FPS/TPS, state, queue index, position, sprite rects, hit
  areas, missing assets.
- Never log signed URLs, cookies, or auth headers.

### Performance budgets

- 60 FPS; `Draw` under ~5 ms on a normal machine.
- Input latency under one frame.
- Time-to-first-audio under ~2-3 s on a warm cache.
- No per-frame allocations of images or large slices.
- Memory under a few hundred MB in a normal session.

## 14. Order of work and first commits

1. `chore: remove committed binaries and ignore build outputs`
2. `refactor: extract ytdlp resolver and audio pipe from main`
3. `test: cover cache expiry and stale stream retry`
4. `feat: add ebiten game skeleton and load main.bmp`
5. `feat: play a track through ebiten/audio with play/pause`

After that: playback core, then catalog, then the full skin system. A beautiful
UI on a fragile playback engine is the wrong order.

## 15. Open questions to decide

1. **Catalog:** Is `yt-dlp` search (YouTube results) acceptable for v1, or do you
   require true YouTube Music results via InnerTube? Recommendation: ship
   yt-dlp search first behind the `Catalog` interface; add InnerTube in 3B if
   search quality is not good enough.
2. **Account/library:** v1 public catalog only, or cookies/OAuth for liked
   songs/private playlists? Recommendation: public only; separate phase.
3. **Platforms:** Linux-only v1, or Windows/macOS CI from the start?
   Recommendation: Linux first, keep the others buildable; native CI later.
4. **Window model:** accept one OS window with internal Winamp-style windows?
   Recommendation: yes; it is the only sane Ebitengine option for v1.
5. **Skin fidelity:** is main + playlist + EQ chrome enough for v1, with shade,
   double-size, and `region.txt` deferred to Phase 6? Recommendation: yes.
6. **Local files/M3U:** should M3U import support local audio files, or only
   YouTube URLs/IDs? Recommendation: URLs/IDs only in v1; local files are a
   separate source abstraction.
7. **Tool distribution:** require system `yt-dlp`/`ffmpeg`, or bundle/auto-
   download them? Recommendation: system tools with startup checks; document
   install paths. Bundling is a release-time decision.
8. **Module path:** keep `module ytm-wmp`, or switch to the GitHub path (e.g.
   `github.com/roypunga/ytm-wmp`) before imports spread? Recommendation: switch
   now if the repo will be public.
9. **Browser window styling:** classic-skin-colored panel (simple) or a full
   `gen.bmp` generic-window frame (more faithful, more work)? Recommendation:
   start with the simple panel; add `gen.bmp` later.
10. **Clipboard/file dialogs:** skip in v1, or add a small cross-platform
    dependency? Recommendation: skip; Ebitengine has no complete clipboard/file
    dialog API. Use typed paths and a scanned skin folder for now.
