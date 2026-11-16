# AGENTS.md

Guidelines for coding agents working in `ytm-wmp`.

## Repository Structure

| Path | Responsibility |
|------|----------------|
| `cmd/ytm-wmp/` | Desktop player entrypoint: flags (`-url`, `-skin`, `-catalog`, `-query`, `-playlist`, `-debug`), `deps.Check`, wiring, signal handling, `ebiten.RunGame` |
| `cmd/ytmctl/` | Terminal debug CLI for `search`, `track`, `playlist`, and `artwork` with `-catalog ytmusic\|ytdlp` |
| `internal/model/` | Shared domain types (`Track`, `StreamInfo`) and YouTube ID helpers (`VideoID`, `IsVideoID`, `NormalizeTrackID`, `DefaultThumbnailURL`) |
| `internal/deps/` | Startup discovery and version checks for `yt-dlp` and `ffmpeg` |
| `internal/ytdlp/` | Stream URL resolution (`-g`) and 5-hour URL cache |
| `internal/catalog/` | `Catalog` interface, `IsPlaylistTarget`, and debounced, generation-tagged async `Searcher` |
| `internal/catalog/ytdlp/` | `yt-dlp -J` catalog implementation (search, track metadata, playlists), stdout limits, and in-memory caches |
| `internal/catalog/ytmusic/` | Pure-Go YouTube Music InnerTube `WEB_REMIX` catalog implementation (`search`, `next`, `browse`) and in-memory caches |
| `internal/artwork/` | Pure-Go thumbnail fetch, JPEG/PNG/WebP decode, `64x64`/`300x300` bilinear resize, atomic disk cache, in-memory LRU, and `Placeholder` |
| `internal/audio/` | `ffmpeg` `f32le` 48 kHz stereo PCM `Source` (`Opener`) and `ebiten/v2/audio` sink (`EbitenSink`) |
| `internal/playback/` | Headless actor `Controller`, explicit state transition table, queue (history, shuffle bag, repeat), seek-by-reopen, prefetch, and error taxonomy |
| `internal/skin/` | Pure-Go `.wsz`/`.zip`/directory skin loader, security/size caps, `skin.xml` modern-skin rejection, alpha fix, and canonical sprite table |
| `internal/ui/` | Ebitengine `Game`, immediate-mode hit testing (`Pointer`), fixed/browser layout, search/browser panel, debug overlay |
| `internal/ui/skinview/` | One-time GPU sheet upload (`ebiten.NewImageFromImage`) and `SubImage` cache with magenta placeholders |
| `assets/dev-skin/` | Original classic-dimension BMPs used when `-skin` is omitted |
| `docs/plan/` | Active execution plan and project status |
| `docs/adr/` | Architecture Decision Records |

### Architectural Boundaries & Invariants

- **One-way dependencies**:
  - `internal/skin`, `internal/artwork`, and `internal/catalog/...` must **not** import `ebiten`, `internal/ui`, `internal/playback`, or `internal/ytdlp`.
  - `internal/ytdlp` imports only `internal/model` + stdlib (never `internal/audio`).
  - `internal/playback` must **not** import `internal/ui` or `internal/catalog`.
  - `cmd/*` wires packages together and holds no playback or UI business logic.
- **Keep `Draw()` pure**: never invoke `yt-dlp`, `ffmpeg`, HTTP, disk I/O, or `ebiten.NewImage` / `ebiten.NewImageFromImage` inside `Draw()`. Background goroutines perform I/O and decoding; GPU texture uploads and audio context initialization happen inside `Update()`.
- **Single playback state owner**: `playback.Controller` owns queue and playback state in one actor goroutine. The UI interacts exclusively via `Command`, `Event`, and atomic `Snapshot`.
- **Clean subprocess teardown**: always close/kill the `ffmpeg` source before `PauseAndStopReading()`, wait for normal exit after stdout EOF instead of killing during the EOF window, and never log signed stream URLs, cookies, or auth headers.
- **Test with fakes, gate live tests**: unit tests must pass offline without `yt-dlp`, `ffmpeg`, or network access; live tests stay behind `YTM_LIVE=1` or `YTM_WMP_LIVE_TEST=1`.

## Documentation

- [`docs/plan/winamp-ytm-player.md`](docs/plan/winamp-ytm-player.md) is the **single source of truth** for architecture, design decisions, phase status, and upcoming work. (Root `PLAN.md` is historical brainstorming.)
- [`docs/adr/`](docs/adr/) records key architectural decisions (e.g., `0001-audio-backend.md`, `0002-single-window-model.md`).
- [`README.md`](README.md) documents prerequisites, run commands, flags, controls, repo layout, what has been completed, and what is next.

## Workflow Expectations

> **IMPORTANT — COMMIT EARLY AND OFTEN:** Do **not** save up a giant uncommitted diff until the end of a task. As soon as each small, logical, passing unit of work is complete (and verified), commit it right away before moving on to the next piece.

1. **Commit early and often (mandatory)**:
   - Break work into small, logical, passing commits as each piece lands—never accumulate large multi-step diffs across unrelated changes.
   - Run the verification checks below and commit immediately after each cohesive change (implementation + its tests, or doc update).
   - Use concise conventional commit messages (e.g., `feat(skin): ...`, `fix(audio): ...`, `test(ui): ...`, `docs(plan): ...`).
2. **Update docs whenever work is done**:
   - After completing a feature, flag, phase, or architectural shift, update `docs/plan/winamp-ytm-player.md` (status summary, package layout, phase outcome, and commit trail) and `README.md` (flags, controls, layout, done/next status).
   - Add or update an ADR in `docs/adr/` when changing concurrency, audio, rendering, or persistence architecture.
3. **Verify before every commit**:
   ```sh
   gofmt -l .
   go vet ./...
   go build ./...
   go test ./...
   go test -race ./...
   ```
   (Building and testing `internal/ui` on Linux requires X11/OpenGL dev headers listed in `README.md`.)
4. **House rules**: never commit compiled binaries, signed stream URLs, cookies/credentials, or copyrighted `.wsz` skins. Keep local test skins inside a gitignored `skins/` directory.
