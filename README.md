# ytm-wmp

A lightweight YouTube Music player with classic `.wsz` skin support, written in
Go.

The project is between the proof of concept and the full player. Phase 0
established the package seams; the current CLI still plays one track and has no
UI or skin support yet.

## Requirements

- Go 1.27+ (matching `go.mod`)
- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp#installation) on `PATH`
- [`ffmpeg`](https://ffmpeg.org/download.html) on `PATH`

Both tools are checked at startup and their versions are logged.

## Run

```sh
go run ./cmd/ytm-wmp -url 'https://www.youtube.com/watch?v=0wRYvsfhsR8'
```

`-url` accepts a YouTube video ID, a watch/short URL, or a yt-dlp search query.
It defaults to the proof-of-concept track so a bare `go run ./cmd/ytm-wmp`
still demonstrates playback.

Press Ctrl+C to stop; shutdown kills ffmpeg and releases the audio device.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
go build ./...
```

Layout:

| Path | Responsibility |
|------|----------------|
| `cmd/ytm-wmp` | flags, dependency checks, wiring, signals |
| `internal/model` | shared domain types |
| `internal/ytdlp` | yt-dlp invocation and stream URL cache |
| `internal/audio` | ffmpeg PCM source and audio sink |
| `internal/playback` | resolve → open → play orchestration |
| `internal/deps` | external tool discovery and versions |
| `internal/skin` | `.wsz` parser (Phase 1+) |
| `docs/plan` | delivery plan |
| `docs/adr` | architecture decisions |

Phase 0 keeps `beep`/`oto` as the audio sink. Phase 1 replaces it with
Ebitengine audio; see `docs/adr/0001-audio-backend.md`.

No binaries, signed stream URLs, or copyrighted skins belong in git.
