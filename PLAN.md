# Roadmap: from the current POC to a skinned YouTube Music player

This is a learning-oriented plan. Each phase ends with something you can run and
demo, and each phase introduces one main idea instead of ten at once.

## How to use this plan

- The critical path is **Phase 0 -> 1 -> 2 -> 3**. Do not start the full skin
  system until audio teardown, queueing, and search work.
- Phase 1 is the go/no-go spike: prove Ebitengine + a live ffmpeg pipe + a
  skinned button before committing to the architecture.
- If Ebitengine's audio package fights you, keep `beep`/`oto` for audio and use
  Ebitengine only for rendering. That is a valid fallback; do not spend weeks
  fighting a deadlock-prone integration.
- Phases 4 and 6 are the fun, visible parts. Timebox them and keep a working
  player on `main`.
- Each phase has a "Done when" checklist. Treat it as the exit criterion, not a
  suggestion.

## 1. Where the project is today

The proof of concept in `cmd/winamp-ytm/main.go` already proves the hardest
transport idea:

- `yt-dlp -g` resolves a direct audio stream URL.
- `ffmpeg` turns that URL into a live `s16le`, 48 kHz, stereo PCM pipe.
- `beep`/`oto` plays the pipe with a ~100 ms buffer.
- Stream URLs are cached for 5 hours, with one retry when a cached URL goes
  stale.
- The code is careful about killing ffmpeg and clearing the speaker so a blocked
  pipe does not deadlock shutdown.

Those are good foundations. Keep them.

What is missing:

- No Ebitengine dependency and no real UI.
- No `.wsz` parser at all.
- One hardcoded track, no search, queue, metadata, artwork, or persistence.
- No pause/seek/next/previous.
- Audio is tied to `beep`; Ebitengine needs its own audio context.
- Almost everything lives in `package main`; there are no tests.
- The built `main` binary is untracked; `winamp-ytm` is ignored. Neither belongs
  in the repository.

## 2. Definition of "full player"

Use this as the v1 scope. It is already a lot of work.

- [ ] Search YouTube Music, paste a watch/playlist URL, or import an M3U file.
- [ ] Queue with play, pause, stop, next, previous, seek, volume, shuffle, repeat.
- [ ] Metadata, duration, and artwork for the current track.
- [ ] Classic `.wsz` skin support: main window, playlist, equalizer chrome.
- [ ] Working skinned controls and playlist interaction.
- [ ] Persist skin, volume, queue, playlists, and window positions.
- [ ] Recover from expired stream URLs, network failures, and missing tools with
      actionable errors.
- [ ] Run on Linux first; keep Windows/macOS buildable.

Explicitly out of scope for v1:

- Modern `.wal` skins (XML + Maki). They are a completely different format.
- AVS/MilkDrop visualizations, minibrowser, CD ripping, device sync.
- Video playback.
- True multiple OS windows (see decision D2).
- Account login/library sync. Add it only after the public player works.

### Scope decision gate

After Phase 3, decide:

- **A. Public catalog only (recommended).** Search, playlists, saved local
  playlists, no account.
- **B. Account/library support.** Cookies or OAuth, liked songs, private
  playlists, uploads. This adds authentication, secret storage, and more fragile
  API work. It is a separate project phase, not a detail.

## 3. Target architecture

```text
cmd/ytm-wmp/main.go          flags, dependency checks, ebiten.RunGame
internal/app/                Game loop, wiring, input routing, app state
internal/ui/                 immediate-mode widgets, windows, skin view
internal/ui/screens/         main, playlist, equalizer, browser/settings
internal/skin/               pure .wsz/.zip parser: BMP, sprites, text, config
internal/playback/           actor, queue, state machine, commands/events
internal/audio/              ffmpeg source, PCM reader, ebiten/audio wrapper
internal/catalog/            Catalog interface and domain models
internal/catalog/ytdlp/      search, metadata, playlist extraction
internal/catalog/ytmusic/    optional InnerTube implementation
internal/store/              config, queue, playlists, artwork cache
internal/testutil/           fake commands, synthetic skins, fixtures
```

Keep the dependency direction one-way:

```text
app -> ui -> skin
app -> playback -> catalog
app -> playback -> audio
playback must not import ui
skin must not import ebiten
catalog must not import ui
```

The last two rules are important for testability:

- `internal/skin` works with `image.Image`, `image.Rectangle`, strings, and
  structs. It has no graphics context, so it can be tested and fuzzed normally.
- `internal/catalog` returns domain models, not yt-dlp JSON structs or UI rows.

### Runtime data flow

```text
Ebitengine Update
  ├─ ui.Update()                 mouse/keyboard -> commands
  ├─ drain playback events       state changes, errors, queue changes
  └─ send commands to actor      play/pause/seek/search/...
Ebitengine Draw
  └─ ui.Draw(screen, snapshot)   sprites + text, no blocking work
```

### Playback state machine

```text
Idle -> Resolving -> Buffering -> Playing <-> Paused
                         |
                         +-> Error
Playing -> Ended -> (next track) Resolving
```

Rules:

- Only one goroutine mutates playback state. The UI sends commands over a
  channel and reads snapshots/events.
- Every async request gets a generation/request ID. Drop results that belong to
  an older search, seek, or track.
- Never call `yt-dlp`, `ffmpeg`, HTTP, or disk I/O from `Update` or `Draw`.

## 4. Decisions to make before writing UI code

| ID | Decision | Recommendation | Why |
|----|----------|----------------|-----|
| D1 | Audio backend | Use `ebiten/v2/audio` | One audio device and one event loop; `PauseAndStopReading` (v2.10+) makes pipe teardown safe. Keep an `Opener` interface so it can be replaced. Fallback: keep `beep`/`oto` for audio if the spike fails. |
| D2 | Window model | One OS window containing internal Winamp-style windows | Ebitengine creates one window per process. True Winamp multi-window behavior is not available without a second UI stack. |
| D3 | Catalog source | `yt-dlp` first, optional InnerTube later | `yt-dlp` already handles signatures and format churn. Do not reimplement stream extraction. |
| D4 | Skin parser | Pure Go, no Ebitengine imports | Unit-testable and fuzzable; upload decoded sheets to the GPU once in the UI layer. |
| D5 | UI model | Immediate-mode hit testing over a retained window list | Winamp's widgets are mostly fixed sprites with a few states. A full retained UI framework is over-engineering. |
| D6 | Concurrency | Playback actor + command/event channels | The current process/cancellation logic is already concurrent. Centralizing it avoids races. |

### What ebiten/audio changes

`ebiten/v2/audio` has constraints that affect the design:

- The audio context exists only after the Ebitengine game starts running.
- One context has one sample rate. Use 48 kHz everywhere and let ffmpeg resample.
- Streams must be stereo, 16-bit little-endian or 32-bit float little-endian.
  Prefer `f32le` and `NewPlayerF32` for new code.
- `NewPlayer` asks the source for data on a separate goroutine.
- A player is seekable only if its source is an `io.Seeker`. A pipe is not, and
  `SetPosition` on a non-seeker panics. Do not call it.
- `PauseAndStopReading` stops reads and blocks until an in-flight read finishes,
  so the source can be closed safely. If a read is blocked on a stalled ffmpeg
  pipe, kill/cancel ffmpeg first to make the read return, then pause and close.
- A `Player` does not close its source. Closing the ffmpeg pipe remains your
  responsibility.
- `Player.Close` is deprecated as of v2.10; use `PauseAndStopReading`.

This is the riskiest migration in the project. Spike it in Phase 1 before
building the rest of the UI.

## 5. Phases

Sizes are relative, not calendar promises: S = a few sessions, M = about a week
of focused evenings, L = multiple weeks.

| Phase | Outcome | Size |
|-------|---------|------|
| 0 | Clean seams, tests, CLI still works | S |
| 1 | Ebitengine window with a skinned main window and audio | M |
| 2 | Real playback core: queue, seek, recovery | L |
| 3 | YouTube Music search, metadata, artwork | M-L |
| 4 | Complete classic skin system | L |
| 5 | Full player UX and persistence | L |
| 6 | Fidelity: EQ DSP, visualizer, shade/region (optional chunks) | L |
| 7 | Hardening and v0.1 release | M |

---

### Phase 0 — Baseline and architectural seams

**Goal:** keep the current CLI behavior, but make the code testable and ready for
a UI. No new features.

**Work**

- Remove the untracked `main` binary from the working tree; make sure
  `.gitignore` covers both `main` and `winamp-ytm`.
- Keep one command: `cmd/ytm-wmp`. Move the POC code there.
- Extract `internal/ytdlp`:
  - `Resolve(ctx, input) (streamURL string, err error)`
  - URL cache with injectable clock
  - a command runner interface so tests do not invoke real `yt-dlp`
- Extract `internal/audio`:
  - `Open(ctx, streamURL, offset) (io.ReadCloser, error)`
  - ffmpeg argument construction as a pure function
  - reader that reports read errors and whether any PCM frames arrived
- Add `internal/model.Track` with `ID`, `Title`, `Artist`, `Album`, `Duration`,
  `ThumbnailURL`.
- Main becomes wiring only: flags (`-url`, `-skin`, `-debug`), dependency checks,
  signal handling.
- Add tests:
  - cache expiry and stale retry with a fake opener
  - ffmpeg argument construction
  - "zero frames" error classification
- Write two short ADRs: audio backend, window model.

**Concepts to research**

- Interfaces owned by the consumer; dependency inversion without a framework.
- `os/exec` and `exec.CommandContext`; why `sh -c` is a bug waiting to happen.
- `context.Context` cancellation, `context.WithTimeout`, `context.Cause`.
- `io.Reader` contracts and `bufio.Reader`.
- TTL caches, injectable clocks, table-driven tests.
- `sync.Once`, `sync.Mutex`, and when an actor is simpler.

**Tips**

- Refactor first, change behavior second. The POC's low time-to-first-audio is a
  feature; do not lose it while rearranging code.
- Pass commands as argument slices, never as a shell string. Queries and titles
  contain spaces, quotes, and `&`.
- Make the cache clock injectable instead of sleeping in tests.
- Keep ffmpeg stderr in a bounded ring buffer. A raw `bytes.Buffer` is fine for a
  short command, but a long-running stream can grow it forever.
- On shutdown, cancel the context, kill ffmpeg, drain/wait, then close the pipe.
  Log the final ffmpeg stderr only when the exit is unexpected.

**Done when**

- `go test ./...` passes.
- `go run ./cmd/ytm-wmp -url <url>` plays audio.
- There is no logic in `main` beyond flags, checks, and wiring.
- No binaries are tracked.

---

### Phase 1 — Walking skeleton: Ebitengine + first skin + audio

**Goal:** open a 275x116 window, draw `main.bmp`, `titlebar.bmp`, and a play
button, and make the button play a hardcoded track through ebiten/audio. Click
again to pause. This is the vertical slice that de-risks the project.

**Work**

- Add `github.com/hajimehoshi/ebiten/v2` (v2.10+) and
  `golang.org/x/image/bmp`.
- Implement `internal/ui.Game` with `Update`, `Draw`, `Layout`.
- Implement `internal/skin` loader:
  - open `.wsz`, `.zip`, or a directory of loose files
  - normalize names to lowercase basenames, handle backslashes and nested dirs
  - decode `main.bmp`, `cbuttons.bmp`, `titlebar.bmp` first
  - expose a sprite table by name, not by hardcoded draw coordinates
- Implement `internal/ui/skinview`:
  - upload each decoded sheet to `*ebiten.Image` once
  - cache `sheet.SubImage(rect).(*ebiten.Image)` results
- Draw the main background, active title bar, play and pause buttons.
- Add immediate-mode input:
  - hot/active widget IDs
  - press/release semantics, not "click on press"
  - mouse coordinates converted to logical coordinates
- Migrate audio:
  - create the audio context on the first `Update` tick
  - change ffmpeg output to `-f f32le`
  - use `ctx.NewPlayerF32(reader)`
  - play/pause and clean shutdown
  - because ffmpeg already emits raw PCM, you do not need a decoder: just
    forward the pipe bytes to the player
- Add `-skin path`; if no skin is supplied, load a small original dev skin from
  `assets/dev-skin/` (loose BMPs). Do not bundle copyrighted Winamp skins.
- Add a debug key (F1) that draws sprite rectangles and widget hit areas.

**Concepts to research**

- Ebitengine's `Game` interface, `Layout`, logical vs. physical pixels, `GeoM`.
- `ebiten.Image`, `SubImage`, `DrawImageOptions`, `FilterNearest` vs.
  `FilterPixelated`.
- Ebitengine input: `CursorPosition`, `IsMouseButtonPressed`,
  `IsMouseButtonJustPressed`, `AppendInputChars`.
- PCM: sample rate, channels, interleaving, frame size, buffering, underruns.
- Ebitengine audio context/player lifecycle.
- BMP basics: palettes, bit depth, alpha.

**Tips**

- Do not call `audio.NewContext` in `main`; wait until the game is running.
- Do not mix `beep` and `ebiten/audio`. Remove `beep`/`oto` once the spike works,
  or you will have two audio paths and confusing device behavior.
- `ffmpeg` stdout is not seekable. Keep your own playback offset; never call
  `Player.SetPosition` on this source.
- Kill/cancel ffmpeg before `PauseAndStopReading` if the reader might be blocked
  on a stalled network read. Otherwise shutdown can wait forever.
- Cache images. `ebiten.NewImageFromImage` and `NewImage` must not run every
  frame.
- Draw at integer coordinates. At 1x use nearest filtering; only scale with
  `FilterPixelated` if you support non-integer window scales.
- Missing sprites should be obvious: draw a magenta box with the sprite name.
  Silent skipping makes coordinate bugs hard to find.
- Use a fixed logical size and `SetWindowSize` for 2x. Keep all skin coordinates
  in 1x space; scale only at the draw boundary.

**Done when**

- The window shows a skinned main panel.
- Clicking play produces audio; clicking pause stops it.
- Closing the window leaves no ffmpeg process behind.
- A missing or broken skin produces a clear error, not a panic.
- F1 shows a useful debug overlay.

---

### Phase 2 — Playback core

**Goal:** a headless playback engine good enough that the UI can be dumb. Queue,
pause/resume, stop, seek, volume, next/previous, repeat/shuffle, and recovery.

**Work**

- Define `playback.Command` and `playback.Event` types.
- Implement the actor goroutine and the state machine from section 3.
- Queue model:
  - `[]Track`, current index
  - shuffle bag that does not repeat until exhausted
  - repeat off / one / all
  - history for previous
- `audio.Opener`:
  - resolve URL (cached)
  - start ffmpeg at an offset with `-ss`
  - return a PCM reader with `Close`, error, and frame count
- Position tracking:
  - `baseOffset + player.Position()`
  - duration from metadata
  - EOF notifier or duration polling for auto-advance
- Seek:
  - stop current source
  - start a new ffmpeg at the requested offset
  - create a new player
  - reset base offset
  - show a "seeking" state while it resolves
- Prefetch the next track's stream URL near the end of the current track.
- Error taxonomy: missing tool, resolve failure, stale URL, ffmpeg failure,
  network failure, unsupported track.
- Integration tests gated by an environment variable; unit tests use a fake
  source that emits a sine wave or silence.

**Concepts to research**

- Actor model / CSP: commands in, events out, one owner of mutable state.
- State machines and transition tables.
- Backpressure: what happens when the audio reader is faster than ffmpeg.
- Process lifetime: `Start`, `StdoutPipe`, reading to EOF, `Wait`, zombie
  processes, process groups.
- FFmpeg seeking: `-ss` before `-i` (fast, keyframe-ish) vs. after `-i`
  (accurate, slower).
- Audio clocks: why wall-clock time drifts and why `Player.Position()` is the
  source of truth.
- Request generations and cancellation.

**Tips**

- Write the transition table before the implementation:
  `Idle -> Resolving`, `Playing -> Paused`, `Seeking -> Playing`, etc. Test every
  legal and illegal transition.
- One goroutine owns the queue. Do not let the UI and the audio callback both
  mutate it.
- Tag every async operation with a request ID. A slow search result from three
  keystrokes ago must not replace the current results.
- Do not call `cmd.Wait` before stdout is fully drained. Use a done channel after
  the read loop; otherwise `Wait` can close the pipe under the reader.
- `Pause` is not the same as `Stop`. Decide and document:
  - Pause keeps the process and buffer for instant resume.
  - Stop kills the process and resets position.
- When seeking, kill the old process, wait for the read loop to end, then start
  the new one. Do not reuse the old `audio.Player`.
- Log `resolve_ms`, `first_pcm_ms`, and `play_ms`. Time-to-first-audio is a
  product feature, and Phase 2 is where it can regress.
- Cache stream URLs by video ID, not by query string. A search result and a
  pasted URL should share the same cache entry.
- Test with `go test -race ./...`; the audio reader and the actor are exactly the
  kind of code where races hide.

**Done when**

- A 10-track queue plays through, including next/previous.
- Seek to 50% takes about a second or less on a normal connection.
- Pause/resume does not restart the track or produce a large gap.
- Killing the network mid-track produces an error event and a retry path.
- No goroutine leak after 20 track changes (`go test -race`, plus a manual
  goroutine count check).

---

### Phase 3 — Catalog: YouTube Music search, metadata, artwork

**Goal:** stop hardcoding tracks. Search, select, and play; show title, artist,
duration, and artwork.

Do this in two sub-steps.

**3A. yt-dlp catalog (works end-to-end)**

- `Catalog` interface:
  ```go
  type Catalog interface {
      Search(ctx context.Context, query string, limit int) ([]Track, error)
      Playlist(ctx context.Context, idOrURL string) ([]Track, error)
      Track(ctx context.Context, id string) (Track, error)
  }
  ```
- Search with `yt-dlp -J --flat-playlist "ytsearch10:<query>"`.
- Playlist with `-J --flat-playlist` and `--playlist-end`.
- Full metadata for a selected track with `-J --no-playlist`.
- Parse permissively. yt-dlp's schema changes; keep unknown fields out of the
  domain model.
- Artwork:
  - pick an appropriate thumbnail size
  - fetch with a timeout and a real user agent
  - decode/resize to the UI size
  - cache on disk by video ID
  - show a placeholder while loading

**3B. YTM-specific catalog (recommended before calling it a YTM player)**

- Add an InnerTube-backed implementation behind the same `Catalog` interface.
  Use the `WEB_REMIX` client context so results match music.youtube.com.
- Keep yt-dlp for stream resolution even if InnerTube supplies metadata. Stream
  extraction is the part you do not want to own.
- Evaluate `github.com/JanikSachs/go_ytmusicapi` and
  `github.com/nezbut/innertube-go` as references. If you use one, wrap it so its
  types never leak into the UI or playback packages.
- Support albums, artists, and playlists only after search and track playback
  are solid.

**Concepts to research**

- `encoding/json`, `json.Decoder`, `json.RawMessage`, optional fields.
- yt-dlp's `-J`, `--flat-playlist`, `--no-playlist`, `ytsearch`, `--playlist-end`.
- YouTube Music vs. YouTube search semantics: a video ID is the common key.
- InnerTube: client context, API keys, `WEB_REMIX`, browse/search endpoints.
- HTTP client timeouts, context cancellation, retry with backoff, rate limits.
- Image decoding, resizing (`golang.org/x/image/draw`), and cache design.
- Atomic cache writes.

**Tips**

- Never parse yt-dlp's human-readable output for data. Use `-J` and parse JSON.
- Search results from `--flat-playlist` are incomplete. Resolve the selected
  track lazily; do not run full extraction for all ten results.
- Use the video ID as the identity. URLs contain tracking parameters and can
  expire.
- Keep catalog errors separate from playback errors. "No results" and "stream
  blocked" are different UX.
- Do not log cookies, authorization headers, or signed stream URLs.
- Cache thumbnails and metadata; do not re-run yt-dlp for a track you already
  saw. A small disk cache makes the app feel much faster.
- Respect rate limits. Add a small delay/backoff between search requests; do not
  fire one process per keystroke.
- Test the decoder with saved JSON fixtures. Do not make the test suite depend on
  YouTube being reachable.
- Add a tiny CLI harness (`cmd/ytmctl search "query"`) if you want to exercise the
  catalog before the browser window exists. It is also a good debugging tool.

**Done when**

- Searching returns ten results with title, artist, duration, and thumbnail.
- Selecting a result plays it.
- A playlist URL imports its tracks.
- A second search for the same query is effectively instant from cache.
- The UI remains responsive during all of this.

---

### Phase 4 — The classic skin system

This is the largest and most distinctive phase. There is no official,
authoritative spec for classic skins; the practical spec is "what Winamp 2.x and
Webamp accept." Build against a corpus, not against a blog post. Split this phase
into sub-milestones so you always have a working player.

**4A. Parser and main-window renderer**

- Harden the loader:
  - `.wsz`, `.zip`, or directory
  - case-insensitive basenames
  - nested folders, duplicate names, `__MACOSX`, `.DS_Store`
  - detect modern skins (`skin.xml`) and reject them with a clear message
  - cap uncompressed size and image dimensions (zip bomb protection)
- Decode BMP with `golang.org/x/image/bmp`. It supports 8, 24, and 32 bpp only.
  Older skins may use 1-, 4-, or 16-bpp BMPs; if your corpus hits one, write a
  small fallback decoder for those cases.
- Handle 32-bit BMP alpha carefully: many classic skins store alpha as zero and
  expect opaque pixels. If the entire image has zero alpha, treat it as opaque.
- Build the complete sprite table from Webamp's `skinSprites.ts`.
- Render the whole main window: background, title bar, transport, shuffle/repeat,
  EQ/playlist toggles, position bar, volume, balance, time digits, playback
  indicator, mono/stereo.
- Bitmap text:
  - `text.bmp` is a 5x6 glyph grid, 31 columns x 3 rows
  - map characters with a lookup table like Webamp's `FONT_LOOKUP`
  - unsupported characters need a fallback (TTF or `?`)
  - implement the scrolling title marquee inside a clipped region
- Parse `pledit.txt` for playlist colors and `viscolor.txt` for visualizer
  colors.

**4B. Widgets and window manager**

- Widgets: button (normal/pressed/selected/selected+pressed), toggle, horizontal
  slider, vertical slider, scrollbar, list row, text field.
- Window manager:
  - main, playlist, equalizer
  - drag by title bar
  - z-order and focus
  - active/inactive title bars
  - show/hide via EQ/playlist buttons
  - snap windows together
- Shade mode for main, EQ, and playlist.
- Double-size mode: keep logical coordinates at 1x, scale the whole canvas at
  draw time, and convert mouse coordinates back to logical space.
- `region.txt` support is optional. Parse `[Normal]`,
  `[WindowShade]`, `[Equalizer]`, and `[EqualizerWS]`; use a precomputed alpha
  mask + shader, or defer it until the rectangular windows are perfect.

**4C. Playlist**

- Virtualized list: only draw visible rows.
- Selection, current-track highlight, double-click to play.
- Scrolling with wheel, scrollbar, keyboard.
- Add/remove/reorder, clear, crop/invert selection eventually.
- M3U import/export.
- Long titles: clip and ellipsize or marquee, depending on the area.

**4D. Equalizer chrome**

- Render the EQ window and its 10 bands + preamp.
- Persist settings.
- Audio DSP is Phase 6; the UI can be visual-only first.

**Concepts to research**

- ZIP format, `archive/zip`, path normalization, zip bombs.
- BMP format: headers, palettes, bit depth, row padding, alpha.
- Sprite atlases, source rectangles, tiling, 9-slice drawing.
- Immediate-mode UI: hot/active state, press/release, drag capture.
- Hit testing, z-order, clipping, coordinate transforms.
- Bitmap fonts, glyph lookup, text measurement, marquee clipping.
- Window shade/double-size modes and DPI scaling.
- Polygon regions and point-in-polygon tests (for `region.txt`).

**Tips**

- Use Webamp's `skinSprites.ts` as the canonical coordinate table. Tutorials and
  blog posts disagree on coordinates and dimensions; Webamp is a working
  implementation. Its sprite table is MIT-licensed.
- In the base skin, `titlebar.bmp` is wider than 275 px because it contains the
  clutter bar. Do not assume every sheet is exactly the window size.
- A `.wsz` file is just a ZIP. Do not trust the extension; some are named `.zip`,
  and some ZIPs contain a top-level folder.
- Validate per asset, not per skin. A missing optional `eq_ex.bmp` should not
  prevent the main window from loading.
- Keep a `Placeholder` sprite for missing assets and a debug overlay to show
  which rect is being drawn.
- Draw at integer positions. Pixel-art skins look wrong the moment you introduce
  fractional scaling.
- Clip with `dst.SubImage(rect).(*ebiten.Image)` rather than drawing outside the
  region. Do not allocate a new offscreen image every frame.
- Text is the hardest part of skin fidelity: `text.bmp` has no Unicode, and the
  playlist uses a different font. Use bitmap glyphs for chrome and a small
  embedded TTF for lists/search, then make the two visually consistent.
- Build a skin torture corpus: base skin, an 8-bit skin, a 24-bit skin, a nested
  ZIP, a skin missing optional files, a skin with unusual dimensions, and a
  modern `.wal` renamed to `.wsz`. Run all of them after every parser change.
- Fuzz the loader early. It parses untrusted ZIPs and images.

**Done when**

- Ten diverse classic skins render correctly.
- All main-window controls work.
- The playlist opens, scrolls, selects, and plays.
- The EQ window opens and its controls persist.
- Missing assets fall back gracefully; a bad skin never panics.
- The debug overlay can identify every sprite and hit area.

---

### Phase 5 — Full player UX

**Goal:** make it a real application, not a skinned demo.

**Work**

- Browser/search window, styled from the skin:
  - search field with focus, caret, IME, and clipboard
  - tabs for songs/albums/artists/playlists
  - results list with artwork, title, artist, duration
  - album/artist/playlist detail views
  - queue panel
- Main window integration:
  - scrolling title
  - elapsed/remaining time toggle
  - bitrate/sample-rate display (from `ffprobe` or metadata)
  - stereo indicator
  - error/toast area
- Queue UX:
  - reorder, remove, clear
  - shuffle/repeat controls
  - history
  - "play next" / "add to queue"
- Settings:
  - skin picker and skin folder
  - yt-dlp/ffmpeg paths
  - cache location and size
  - cookies/browser auth (optional)
  - audio buffer and scale
- Persistence:
  - config, queue, local playlists, volume, skin, window layout
  - versioned JSON schema
  - atomic writes (temp file + rename)
  - debounce writes; never write every frame
- Keyboard shortcuts:
  - space play/pause
  - arrows seek/volume
  - `/` focus search
  - Enter play top result
  - Esc close/cancel
- Optional: MPRIS on Linux, media keys, tray icon.

**Concepts to research**

- Text input and IME: `ebiten.AppendInputChars`, key repeat, composition.
- Focus management and keyboard event routing.
- Debouncing and cancellation of in-flight searches.
- List virtualization and scroll physics.
- Config directories (`os.UserConfigDir`), file permissions, atomic rename.
- Schema versioning and migration.
- Clipboard APIs (Ebitengine does not provide a full clipboard; use a small
  cross-platform package if you need paste).

**Tips**

- Search as you type with a 250–300 ms debounce, but make Enter search
  immediately. Cancel the previous request.
- Keep one focus owner. If the search field is focused, space types a space; it
  does not pause playback.
- Do not persist signed stream URLs. Persist video IDs and re-resolve on load.
- Use an embedded TTF for search results and playlist rows; bitmap fonts will
  fail on non-Latin titles. Fall back to `?` only as a last resort.
- Save on change with a debounce; save again on exit. Never let a crash corrupt
  the config: write a temp file, `fsync` if you care, then rename.
- Put user-facing errors in the UI, not only in the log: "yt-dlp not found",
  "This track is unavailable", "Stream expired, retrying".
- Add a "reload yt-dlp" hint in the error message. This project depends on an
  external tool that changes often.

**Done when**

- Cold start restores the last skin, volume, queue, and window layout.
- You can search, play, queue, skip, seek, and restart entirely with the mouse
  and keyboard.
- A missing dependency produces an actionable message.
- The app survives a full evening of use without a terminal.

---

### Phase 6 — Fidelity and sound (optional chunks)

Treat these as independent upgrades. Each one should be shippable on its own.

**6A. Equalizer DSP**

- 10 peaking biquad filters + preamp, implemented as an `io.Reader` between
  ffmpeg and the Ebitengine player.
- Use the RBJ audio EQ cookbook formulas.
- Float32 processing, block-based, no allocations in the hot path.
- Handle clipping/limiting; add a bypass.
- Test with a sine sweep and compare the measured response.

**6B. Spectrum analyzer / oscilloscope**

- Tap PCM with `io.TeeReader` into a ring buffer.
- Window the samples (Hann), run an FFT, map bins logarithmically to ~19 bars.
- Use `viscolor.txt` for colors; add peak-hold and falloff.
- Keep the visualizer off the audio goroutine; analyze a copy.

**6C. Shade modes, double-size, region masks**

- Shade mode for main/EQ/playlist with alternate sprites and hit rects.
- Double-size with nearest-neighbor scaling and correct mouse mapping.
- `region.txt` masks and point-in-polygon hit testing.

**6D. Skin browser and cursors**

- Scan skin folders, show previews, support drag-and-drop.
- Reject `.wal` with an explanation.
- `.cur` custom cursors: parse or skip; hidden system cursor + drawn cursor is
  the fallback.

**6E. Gapless playback / MPRIS / media keys**

- Gapless requires prebuffering the next source and coordinating players; it is
  a deep rabbit hole. Only do it if the rest is solid.
- MPRIS on Linux via D-Bus is a nice desktop integration and a good systems
  programming exercise.

**Concepts to research**

- Biquad filters, transfer functions, the RBJ cookbook, clipping.
- FFT, windowing, dB scaling, logarithmic frequency bins.
- Ring buffers and lock-free single-producer/single-consumer patterns.
- Alpha masks, polygon rasterization, custom Kage shaders.
- D-Bus/MPRIS and platform-specific build tags.

**Tips**

- DSP in float32; convert back to the player's expected format at the boundary.
- Do not allocate slices inside `Read`. Preallocate per stream.
- Visualizers are eye candy. If they stutter, drop frames, not audio.
- Region masks are rare. Do them after the rectangular windows are perfect.
- Double-size changes hit testing; test at both scales with the debug overlay.

**Done when**

- Each chunk has a visible/audible demo and does not regress the base player.

---

### Phase 7 — Hardening and v0.1 release

**Goal:** make it installable and hard to crash.

**Work**

- Error paths and timeouts:
  - missing/old yt-dlp
  - ffmpeg failure
  - offline/rate-limited
  - age-restricted/members-only
  - expired URL retry
- Performance:
  - `pprof` CPU and heap profiles
  - frame-time budget (16.6 ms at 60 FPS)
  - allocation check in `Draw`
  - startup/time-to-first-audio measurements
- Tests:
  - unit tests for parser, queue, state machine
  - fake-process integration tests
  - corpus smoke test
  - `go test -race`
  - fuzz `skin.Open` and BMP fallback
- Packaging:
  - Linux binary first
  - Windows/macOS builds in CI on native runners
  - Windows GUI build with `-ldflags -H=windowsgui`
  - desktop entry/icon on Linux
- Docs:
  - README with install and troubleshooting
  - architecture notes/ADRs
  - legal note: yt-dlp/YouTube ToS, no bundled skins or content
- Release:
  - tag v0.1.0
  - include a smoke-test checklist
  - keep yt-dlp update instructions prominent

**Concepts to research**

- Go fuzzing (`go test -fuzz`), `testing.F`, corpus files.
- `runtime/pprof`, `net/http/pprof`, benchmarks.
- Cross-compilation limits: Ebitengine/oto often needs Cgo; macOS builds usually
  require a macOS runner.
- CI matrices, release archives, checksums, licensing.
- Process permission/sandbox issues (Flatpak/Snap) for yt-dlp/ffmpeg.

**Tips**

- Fuzz the ZIP/BMP parser before release. A malformed skin must not take down the
  player.
- Cap decompressed sizes and image dimensions; reject path traversal.
- Do not commit binaries, skins, cookies, or cache files.
- Test on a clean machine or container without yt-dlp installed to see the
  first-run experience.
- Ebitengine cannot be cross-compiled everywhere from Linux because of Cgo;
  build macOS on macOS in CI.
- Keep a `CHANGELOG.md`; this project will break when YouTube changes.

**Done when**

- v0.1.0 builds on at least Linux and one other OS.
- A new user can install dependencies, launch, load a skin, search, and play.
- The 20-skin smoke test passes.
- Known limitations are documented.

## 6. Cross-cutting practices

### Testing pyramid

1. **Pure unit tests**: skin parser, sprite table, text lookup, queue logic,
   state transitions, config serialization. These should be fast and run in CI.
2. **Fake-process integration tests**: replace `yt-dlp`/`ffmpeg` with small shell
   scripts or Go test helpers that emit known JSON/PCM. This tests argument
   construction and error handling without the network.
3. **Opt-in live tests**: real yt-dlp + ffmpeg behind an environment variable.
   Never make CI depend on YouTube.
4. **Visual tests**: render a skin to a screenshot and compare. Ebitengine needs
   a graphics context; use Xvfb on Linux or run these manually.
5. **Audio tests**: hard to assert automatically. Use a sine-wave fake source,
   measure that reads return the expected number of frames, and test state
   transitions separately.

### Instrumentation

- Use `log/slog` with a `-debug` flag.
- Log spans: resolve, open ffmpeg, first PCM, player start, seek.
- Keep a ring buffer of ffmpeg stderr per stream.
- Add a debug overlay: FPS, TPS, current state, queue index, sprite rects,
  hit areas, source position.
- Never log full URLs with signatures or cookies.

### Performance budgets

- 60 FPS; `Draw` under ~5 ms on a normal machine.
- UI input latency under one frame.
- Time-to-first-audio under ~2–3 seconds on a warm cache.
- No per-frame allocations of images or large slices.
- Memory under a few hundred MB for a normal session.

### Skin corpus

- Keep copyrighted skins out of git.
- Use a local `skins/` directory (gitignored) and a few synthetic fixtures in
  `testdata/`.
- Record which skins pass/fail and why in a small table.
- Re-run the corpus after every parser change.

### ADRs

Write short decision records for:

- audio backend and seek strategy
- single-window model
- catalog interface and yt-dlp vs. InnerTube
- skin parser boundaries
- persistence schema

This is more valuable than long comments because it captures *why*.

## 7. Risks and unknowns

| Risk | Impact | Mitigation |
|------|--------|------------|
| Ebitengine audio + live pipe teardown deadlocks or races | High | Spike in Phase 1; kill ffmpeg before `PauseAndStopReading`; test shutdown repeatedly |
| One-window limitation disappoints the Winamp feel | Medium | Decide D2 early; internal windows and snapping; transparent-canvas experiment only after v1 |
| yt-dlp/YouTube changes break playback | High | Pin nothing forever; version-check and update path; clear errors; keep catalog/stream boundaries |
| InnerTube auth/API fragility | High | Public catalog first; wrap third-party clients; keep yt-dlp fallback |
| Skin incompatibility | Medium | Corpus-driven development; per-asset fallbacks; debug overlay; fuzz loader |
| Text/Unicode in bitmap skins | Medium | Bitmap chrome + embedded TTF for lists; explicit fallback |
| Scope creep (EQ, visualizer, gapless, account) | High | v1 checklist; ship Phase 4/5 before Phase 6; optional chunks |
| Legal/ToS | Medium | Personal-use docs; no bundled content or skins; don't redistribute streams |
| External tool availability in sandboxes | Medium | Startup checks; configurable paths; document Flatpak implications |

## 8. Suggested first five commits

1. `chore: remove committed binaries and ignore build outputs`
2. `refactor: extract ytdlp resolver and audio pipe from main`
3. `test: cover cache expiry and stale stream retry`
4. `feat: add ebiten game skeleton and load main.bmp`
5. `feat: play hardcoded track through ebiten/audio with play/pause`

After that, build the queue and catalog before adding more skin widgets. A
beautiful UI on top of a fragile playback engine is the wrong order.

## 9. Resources

### Winamp skins

- Winamp Skinning Tutorial (WACUP archive):
  <https://raw.githubusercontent.com/WACUP/Winamp-Skinning-Archive/master/Classic%20Skins/Winamp_skinning_tutorial_1_5_0.pdf>
- Webamp sprite table (canonical coordinates, MIT):
  <https://github.com/captbaritone/webamp/blob/master/packages/webamp/js/skinSprites.ts>
- Winamp Skin Museum (corpus and previews):
  <https://skins.webamp.org/>
- Skin config files (`pledit.txt`, `viscolor.txt`, `region.txt`):
  <https://winampskins.neocities.org/config>
- File format overview:
  <http://fileformats.archiveteam.org/wiki/Winamp_Skin>
- Webamp source (a working reference implementation):
  <https://github.com/captbaritone/webamp>

### Ebitengine

- Cheat sheet:
  <https://ebitengine.org/en/documents/cheatsheet.html>
- Audio package:
  <https://pkg.go.dev/github.com/hajimehoshi/ebiten/v2/audio>
- Performance tips:
  <https://ebitengine.org/en/documents/performancetips.html>
- Examples:
  <https://github.com/hajimehoshi/ebiten/tree/main/examples>
- FAQ (Cgo/cross-compilation, Windows console):
  <https://ebitengine.org/en/documents/faq.html>

### Go and media

- `os/exec`:
  <https://pkg.go.dev/os/exec>
- Go pipelines and cancellation:
  <https://go.dev/blog/pipelines>
- `archive/zip`:
  <https://pkg.go.dev/archive/zip>
- `golang.org/x/image/bmp` (8/24/32 bpp only):
  <https://pkg.go.dev/golang.org/x/image/bmp>
- FFmpeg seeking:
  <https://trac.ffmpeg.org/wiki/Seeking>
- yt-dlp options:
  <https://github.com/yt-dlp/yt-dlp#usage-and-options>
- RBJ audio EQ cookbook:
  <https://www.w3.org/TR/audio-eq-cookbook/>

### YouTube Music clients to study

- `go_ytmusicapi`:
  <https://github.com/JanikSachs/go_ytmusicapi>
- `innertube-go`:
  <https://github.com/nezbut/innertube-go>
- Python reference implementation:
  <https://github.com/sigma67/ytmusicapi>

Read these for their request shapes and parsers. Pin a version if you depend on
one, and wrap it behind your own interface.

## 10. The short version

If you only remember one order, use this:

1. Make the current audio path clean and tested.
2. Prove Ebitengine + one skin + play/pause.
3. Build the playback actor and queue.
4. Add search/metadata/artwork.
5. Build the skin widget/window system.
6. Add the browser and persistence.
7. Add EQ/visualizer/shade only when the player is already usable.
8. Harden, fuzz, package, release.

The project is roughly two products in one: a streaming audio engine and a
classic skin renderer. Keep them behind clean interfaces, demo each phase, and do
not let the skin work start before audio teardown is reliable.
