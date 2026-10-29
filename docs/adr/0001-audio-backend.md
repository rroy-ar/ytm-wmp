# ADR 0001: Use Ebitengine's audio context with an f32le ffmpeg pipe

- Status: Accepted
- Date: 2026-10-25 (updated after the Phase 1 spike)
- Deciders: project owner
- Related: plan decision D1, Phase 1

## Context

The Phase 0 proof of concept uses `beep` + `oto` for audio. The UI uses
Ebitengine, which creates its own process-wide audio context. Running both
stacks means two audio devices and two event loops fighting over `oto`.

The source is a live ffmpeg stdout pipe. It is not seekable, so changing
position requires a new ffmpeg process. Teardown is the risk: an in-flight read
can hold a lock that shutdown waits on.

## Decision

Phase 1 switches playback to `ebiten/v2/audio`:

- create exactly one 48 kHz context on the first `Update` tick, after
  Ebitengine has started; `main` only constructs the sink;
- have ffmpeg emit `f32le`, stereo, 48 kHz PCM;
- create players with `ctx.NewPlayerF32(source)`;
- pause with `Player.Pause()`, which keeps ffmpeg and the buffered pipe alive
  for an instant resume;
- stop and shut down with `PauseAndStopReading()` (never the deprecated
  `Close()`), after closing the ffmpeg pipe;
- never seek the player: recreate ffmpeg and the player at the target offset;
- tear down in this order: close/cancel the source, then
  `PauseAndStopReading`, then wait for the read loop and process to finish;
- after the reader observes stdout EOF, wait for ffmpeg to exit normally
  instead of killing it in the EOF window; cancellation and explicit stops
  still kill first;
- defer a pause request made while yt-dlp is still resolving until the player
  exists, and clear stale pause intent at the start of a new attempt.

The spike confirmed that creating the context on the first tick works and keeps
device initialization off the startup path. `AudioControl` and `pcmPlayer`
interfaces keep the lifecycle testable without opening a real device.

## Consequences

- One audio device and one event loop, which is the only sane fit for a game
  loop that also renders the UI.
- The PCM format is float32 little-endian; `internal/audio` constants and
  `ffmpegArgs` are the only places that need to know.
- `PauseAndStopReading` can block on a stalled read. Closing/killing ffmpeg
  first is mandatory, not optional; the sink enforces that order.
- Pausing does not stop ffmpeg, so a paused session still holds a network
  connection. That is the price of instant resume in Phase 1; Phase 2 can
  decide whether to stop and re-open on long pauses.
- Seeking is slower than a seekable file, but correct for a network pipe.

## Alternatives considered

- **Keep `beep` and render with Ebitengine.** Lower short-term risk, but two
  audio stacks and two backends to test forever.
- **Use an MP3/Opus decoder.** Adds a seekable-buffer requirement and latency;
  raw PCM from ffmpeg is simpler and already fast.
- **Use `Player.SetPosition`.** Panics on a non-`io.Seeker`; the ffmpeg stdout
  pipe is not seekable.
