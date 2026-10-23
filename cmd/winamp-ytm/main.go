package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
)

// Formato fijo de la salida de ffmpeg: al no depender de la fuente, el altavoz
// se puede inicializar en paralelo con la resolución de la URL de yt-dlp.
const (
	outputSampleRateHz = 48000
	outputChannels     = 2
	outputPrecision    = 2
)

var (
	outputSampleRate = beep.SampleRate(outputSampleRateHz)
	outputFormat     = beep.Format{
		SampleRate:  outputSampleRate,
		NumChannels: outputChannels,
		Precision:   outputPrecision,
	}
	speakerBuffer = outputSampleRate.N(time.Second / 10)
)

// YouTube firma las URLs con una caducidad (~6 h); se reutilizan hasta entonces
// para que repetir un tema no vuelva a invocar a yt-dlp.
const streamURLTTL = 5 * time.Hour

type cachedStream struct {
	url     string
	expires time.Time
}

var streamCache = struct {
	sync.Mutex
	entries map[string]cachedStream
}{entries: make(map[string]cachedStream)}

func getStreamURL(ctx context.Context, queryOrURL string) (string, error) {
	streamCache.Lock()
	cached, ok := streamCache.entries[queryOrURL]
	streamCache.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return cached.url, nil
	}

	// Extrae la URL directa del mejor stream de audio
	cmd := exec.CommandContext(ctx, "yt-dlp",
		"-f", "bestaudio/best",
		"--no-warnings",
		"--no-playlist",
		"-g", queryOrURL,
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("error ejecutando yt-dlp: %w", err)
	}

	streamURL := strings.TrimSpace(string(out))
	if streamURL == "" {
		return "", fmt.Errorf("yt-dlp no devolvió ninguna URL para %q", queryOrURL)
	}

	streamCache.Lock()
	streamCache.entries[queryOrURL] = cachedStream{
		url:     streamURL,
		expires: time.Now().Add(streamURLTTL),
	}
	streamCache.Unlock()

	return streamURL, nil
}

// errStaleStream indica que la URL resuelta no entregó audio, probablemente
// porque la firma caducó o fue rechazada.
var errStaleStream = errors.New("la URL del stream no entregó audio")

func forgetStreamURL(queryOrURL string) {
	streamCache.Lock()
	delete(streamCache.entries, queryOrURL)
	streamCache.Unlock()
}

var (
	initSpeakerOnce sync.Once
	initSpeakerErr  error
)

// ensureSpeaker abre el dispositivo de audio una sola vez; speaker.Init
// reinicia el contexto de oto y con él la latencia de apertura del dispositivo.
func ensureSpeaker() error {
	initSpeakerOnce.Do(func() {
		initSpeakerErr = speaker.Init(outputSampleRate, speakerBuffer)
	})
	return initSpeakerErr
}

// pcmStreamer entrega muestras PCM s16le estéreo leídas de un pipe de ffmpeg.
// Se usa PCM y no MP3 porque los decodificadores de beep necesitan un
// io.Seeker y el stdout de un proceso no lo es.
type pcmStreamer struct {
	cmd    *exec.Cmd
	pipe   io.ReadCloser
	r      *bufio.Reader
	stderr *bytes.Buffer
	url    string
	buf    []byte
	err    error

	started bool
	frames  int

	closeOnce sync.Once
	closeErr  error
}

func (s *pcmStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	width := outputFormat.Width()
	need := len(samples) * width
	if cap(s.buf) < need {
		s.buf = make([]byte, need)
	}
	buf := s.buf[:need]

	read, err := io.ReadFull(s.r, buf)
	for i := 0; i+width <= read; i += width {
		samples[n], _ = outputFormat.DecodeSigned(buf[i:read])
		n++
	}

	s.frames += n
	if n > 0 && !s.started {
		s.started = true
		log.Println("Reproduciendo audio desde la URL:", s.url)
	}

	if err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			s.err = err
		}
		return n, false
	}
	return n, true
}

func (s *pcmStreamer) Err() error {
	return s.err
}

func (s *pcmStreamer) Close() error {
	s.closeOnce.Do(func() {
		_ = s.pipe.Close()
		s.closeErr = s.cmd.Wait()
	})
	return s.closeErr
}

func openStream(ctx context.Context, streamURL string) (*pcmStreamer, error) {
	ffmpegCmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin",
		"-loglevel", "error",
		// Sin nobuffer, ffmpeg retiene varios bloques de entrada antes de
		// emitir el primer fragmento por stdout.
		"-fflags", "+nobuffer",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
		"-i", streamURL,
		"-vn",
		"-f", "s16le",
		"-ac", strconv.Itoa(outputChannels),
		"-ar", strconv.Itoa(outputSampleRateHz),
		"-",
	)

	stdout, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("error creando el pipe de ffmpeg: %w", err)
	}

	stderr := &bytes.Buffer{}
	ffmpegCmd.Stderr = stderr

	if err := ffmpegCmd.Start(); err != nil {
		_ = stdout.Close()
		return nil, fmt.Errorf("error iniciando ffmpeg: %w", err)
	}

	return &pcmStreamer{
		cmd:    ffmpegCmd,
		pipe:   stdout,
		r:      bufio.NewReaderSize(stdout, 64*1024),
		stderr: stderr,
		url:    streamURL,
	}, nil
}

func playStream(ctx context.Context, streamURL string) error {
	if err := ensureSpeaker(); err != nil {
		return fmt.Errorf("error inicializando el altavoz: %w", err)
	}

	// Se reproduce el pipe en streaming: el audio suena con los primeros
	// fragmentos en lugar de esperar a que termine la descarga completa.
	stream, err := openStream(ctx, streamURL)
	if err != nil {
		return err
	}
	defer stream.Close()

	done := make(chan struct{})
	speaker.Play(beep.Seq(stream, beep.Callback(func() {
		close(done)
	})))

	select {
	case <-ctx.Done():
		// Cerrar el pipe desbloquea la lectura de ffmpeg y evita que Clear
		// espere al mutex del mixer retenido por esa lectura.
		_ = stream.Close()
		speaker.Clear()
	case <-done:
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("error leyendo el audio de ffmpeg: %w", err)
	}

	if stream.frames == 0 {
		if msg := strings.TrimSpace(stream.stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", errStaleStream, msg)
		}
		return errStaleStream
	}

	// Una salida anticipada de ffmpeg (p. ej. corte de red) termina la
	// reproducción sin error de lectura; se reporta con su stderr.
	if err := stream.Close(); err != nil {
		if msg := strings.TrimSpace(stream.stderr.String()); msg != "" {
			return fmt.Errorf("ffmpeg terminó con error: %w (%s)", err, msg)
		}
		return fmt.Errorf("ffmpeg terminó con error: %w", err)
	}

	return nil
}

func play(ctx context.Context, queryOrURL string) error {
	streamURL, err := getStreamURL(ctx, queryOrURL)
	if err != nil {
		return err
	}

	err = playStream(ctx, streamURL)
	if !errors.Is(err, errStaleStream) || ctx.Err() != nil {
		return err
	}

	// La URL cacheada pudo caducar antes de lo previsto: se descarta y se
	// reintenta una vez con una URL recién resuelta.
	forgetStreamURL(queryOrURL)
	if streamURL, err = getStreamURL(ctx, queryOrURL); err != nil {
		return err
	}
	return playStream(ctx, streamURL)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	testUrl := "https://youtu.be/0wRYvsfhsR8?si=MU_r1XyksGYQp-F5"

	// El altavoz no depende de la URL, así que se inicializa en paralelo
	// mientras yt-dlp resuelve el stream.
	go func() { _ = ensureSpeaker() }()

	err := play(ctx, testUrl)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("Error reproduciendo el stream: %v", err)
	}
}
