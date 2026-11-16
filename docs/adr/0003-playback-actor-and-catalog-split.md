# ADR 0003: Playback Actor and Catalog / Stream-Resolver Split

- Status: Accepted
- Date: 2026-11-15
- Deciders: project owner
- Related: plan decisions D3, D6, D10, Phases 2 & 3

## Context

Phase 0 and Phase 1 started with a synchronous single-track `playback.Player`
and coupled YouTube target parsing inside `internal/ytdlp`. As multi-track
queues, shuffle/repeat, seek-by-reopen, next-track URL prefetching, live search,
playlist expansion, and artwork caching were added in Phases 2 and 3, two
architectural boundaries were required:

1. **Concurrency and queue ownership**: the UI loop (`Update()` / `Draw()` at
   60 TPS) and asynchronous resolve/ffmpeg/catalog workers cannot lock or mutate
   shared playback or queue state concurrently.
2. **Metadata catalog vs. stream resolution**: searching YouTube/YTM, loading
   playlists, and enriching track metadata have different data shapes, caching
   policies, and backend options (such as a future pure-Go InnerTube `WEB_REMIX`
   client in Phase 3B) than resolving short-lived signed audio stream URLs via
   `yt-dlp -g`.

## Decision

1. **Single playback actor (`internal/playback`)**:
   - `playback.Controller` owns the queue (`history`, Fisher-Yates shuffle bag
     with immediate-repeat avoidance on refill, and `RepeatOff`/`RepeatAll`/
     `RepeatOne`), explicit state transition table, active worker generation
     counter (`gen`), and prefetch state inside a single actor goroutine (`Run`).
   - All external interaction happens through sealed `Command` values (`Send`,
     including `Play`, `PlayIndex`, `Pause`, `Resume`, `Stop`, `Next`, `Prev`,
     `Seek`, `SetVolume`, `SetQueue`, `Enqueue`, `UpdateTrack`, `SetShuffle`,
     `SetRepeat`), non-blocking `Event` notifications (`Events`), and a lock-free
     `atomic.Pointer[snapshotState]` read via `Snapshot()`.
   - `Snapshot.Queue` is published as an immutable slice per queue mutation and
     shared read-only across `Snapshot()` calls without per-frame cloning.

2. **Split `internal/catalog` from `internal/ytdlp` via `internal/model`**:
   - `internal/model` owns shared domain types (`Track`, `StreamInfo`) and pure
     YouTube ID helpers (`VideoID`, `IsVideoID`, `NormalizeTrackID`,
     `DefaultThumbnailURL`) with stdlib-only dependencies.
   - `internal/ytdlp` focuses exclusively on resolving playable stream URLs
     (`yt-dlp -g`), bounded subprocess output (`DefaultMaxStdoutBytes`), and
     the 5-hour video-ID URL cache.
   - `internal/catalog` owns the `Catalog` interface (`Search`, `Playlist`,
     `Track`), `IsPlaylistTarget`, and the debounced, generation-tagged
     `Searcher`, implemented in Phase 3A by `internal/catalog/ytdlp` (`yt-dlp -J`)
     and extensible in Phase 3B by `internal/catalog/ytmusic` without touching
     playback or stream resolution.
   - `internal/playback` never imports `internal/catalog` or `internal/ui`; UI
     background enrichment resolves metadata through `catalog.Catalog` and
     merges it into the active queue non-destructively via `playback.UpdateTrack`.

3. **Pure-Go artwork pipeline (`internal/artwork`)**:
   - `artwork.Cache` fetches, decodes (JPEG/PNG/WebP), resizes (`64x64` list
     and `300x300` now-playing), and caches thumbnails on disk and in an
     in-memory LRU off the UI thread.
   - `internal/ui` keeps `Draw()` pure, bounds background catalog/artwork
     workers with a semaphore, uploads `64x64` textures eagerly inside `Update()`,
     uploads `300x300` textures lazily when the preview modal opens, and
     deterministically calls `Deallocate()` on eviction and shutdown.

## Consequences

- `internal/playback`, `internal/catalog`, `internal/artwork`, and `internal/ui`
  have strictly one-way dependencies and can be tested offline with fakes under
  `go test -race`.
- Swapping or adding a catalog backend (e.g., Phase 3B InnerTube `WEB_REMIX`)
  requires implementing only `catalog.Catalog` without changing `playback.Controller`,
  `internal/ytdlp`, or UI browser logic.
- Keeping stream resolution in `internal/ytdlp` isolates YouTube signature/format
  churn from catalog search and metadata logic.

## Alternatives considered

- **Mutex-protected shared player struct.** Rejected because async worker
  cancellation, stale-generation filtering, and UI polling would require
  scattered locking and invite deadlocks with `EbitenSink` teardown.
- **Placing `Search`/`Playlist` inside `internal/ytdlp`.** Rejected because
  stream URL resolution and catalog browsing evolve independently and Phase 3B
  needs an HTTP catalog implementation that still uses `yt-dlp -g` for streams.
