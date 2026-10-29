# ADR 0001: Use Ebitengine's audio context with an f32le ffmpeg pipe

- Status: Accepted
- Date: 2026-10-25
- Deciders: project owner
- Related: plan decision D1, Phase 1

## Context

The Phase 0 proof of concept uses `beep` + `oto` for audio. The UI will use
Ebitengine, which creates its own process-wide audio context. Running both
stacks means two audio devices and two event loops fighting over `oto`.

The source is a live ffmpeg stdout pipe. It is not seekable, so changing
position requires a new ffmpeg process. Teardown is the risk: an in-flight read
can hold a lock that shutdown waits on.

## Decision

Phase 1 switches playback to `ebiten/v2/audio`:

- create exactly one audio context in `main` with `audio.NewContext(48000)`
  before `RunGame`;
- have ffmpeg emit `f32le`, stereo, 48 kHz PCM;
- create players with `ctx.NewPlayerF32(source)`;
- pause with `PauseAndStopReading()` (never `Close()`, deprecated in v2.10);
- never seek the player: recreate ffmpeg and the player at the target offset;
- tear down in this order: cancel/kill ffmpeg, `PauseAndStopReading`, close the
  pipe, wait for the read loop and process, then discard the player.

Phase 0 keeps the `beep` sink behind `internal/audio.Source` so this switch does
not change the resolver, playback orchestration, or CLI.

## Consequences

- One audio device and one event loop, which is the only sane fit for a game
  loop that also renders the UI.
- The PCM format changes from `s16le` to `f32le`; `internal/audio` constants and
  `ffmpegArgs` are the only places that need to know.
- `PauseAndStopReading` can block on a stalled read. Killing ffmpeg and closing
  the pipe first is mandatory, not optional.
- Seeking is slower than a seekable file, but correct for a network pipe.

## Alternatives considered

- **Keep `beep` and render with Ebitengine.** Lower short-term risk, but two
  audio stacks and two backends to test forever.
- **Use an MP3/Opus decoder.** Adds a seekable-buffer requirement and latency;
  raw PCM from ffmpeg is simpler and already fast.
- **Use `Player.SetPosition`.** Panics on a non-`io.Seeker`; the ffmpeg stdout
  pipe is not seekable.
