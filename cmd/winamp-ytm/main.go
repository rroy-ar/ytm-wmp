package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

func getStreamURL(ctx context.Context, queryOrURL string) (string, error) {
	// Extrae la URL directa del mejor stream de audio
	cmd := exec.CommandContext(ctx, "yt-dlp", "-f", "bestaudio", "-g", queryOrURL)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("error ejecutando yt-dlp: %w", err)
	}
	fmt.Print("url: ", strings.TrimSpace(string(out)))
	return strings.TrimSpace(string(out)), nil
}

func playStream(ctx context.Context, streamURL string) error {
	ffmpegCmd := exec.CommandContext(ctx, "ffmpeg",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
		"-i", streamURL,
		"-f", "mp3",
		"-",
	)

	var audioBuf bytes.Buffer
	ffmpegCmd.Stdout = &audioBuf

	if err := ffmpegCmd.Run(); err != nil {
		return fmt.Errorf("error transcodificando con ffmpeg: %w", err)
	}

	// mp3.Decode requiere un io.ReadSeeker para indexar los frames; el pipe de
	// ffmpeg no lo es, así que se decodifica desde el buffer en memoria.
	streamer, format, err := mp3.Decode(io.NopCloser(bytes.NewReader(audioBuf.Bytes())))
	if err != nil {
		return fmt.Errorf("error decodificando audio: %w", err)
	}
	defer streamer.Close()

	err = speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
	if err != nil {
		return fmt.Errorf("error inicializando el altavoz: %w", err)
	}

	log.Println("Reproduciendo audio desde la URL:", streamURL)

	done := make(chan struct{})
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		close(done)
	})))

	select {
	case <-ctx.Done():
	case <-done:
	}

	return nil
}

func main() {
	ctx := context.Background()

	testUrl := "https://youtu.be/0wRYvsfhsR8?si=MU_r1XyksGYQp-F5"
	streamURL, err := getStreamURL(ctx, testUrl)
	if err != nil {
		log.Fatalf("Error obteniendo la URL del stream: %v", err)
	}

	err = playStream(ctx, streamURL)
	if err != nil {
		log.Fatalf("Error reproduciendo el stream: %v", err)
	}
}
