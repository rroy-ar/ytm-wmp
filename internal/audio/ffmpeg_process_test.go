package audio

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests use a tiny shell script in place of ffmpeg. They exercise the
// real os/exec lifecycle, which is where the EOF/kill race lives.
func TestSourceClosesCleanlyAfterEOF(t *testing.T) {
	src := openScript(t, "dd if=/dev/zero bs=8 count=1 2>/dev/null\nexit 0\n")
	defer src.Close()

	frame := make([]byte, FrameSize)
	if _, err := io.ReadFull(src, frame); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if _, err := src.Read(frame); !errors.Is(err, io.EOF) {
		t.Fatalf("second read = %v, want io.EOF", err)
	}

	closeErr := src.Close()
	if closeErr != nil {
		t.Fatalf("Close after EOF = %v, want nil (no kill race)", closeErr)
	}
	if err := EndError(src, closeErr); err != nil {
		t.Fatalf("EndError = %v, want nil after a normal track end", err)
	}
}

func TestSourceStillReportsNonzeroExitAfterEOF(t *testing.T) {
	src := openScript(t, "dd if=/dev/zero bs=8 count=1 2>/dev/null\nexit 7\n")
	defer src.Close()

	frame := make([]byte, FrameSize)
	if _, err := io.ReadFull(src, frame); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if _, err := src.Read(frame); !errors.Is(err, io.EOF) {
		t.Fatalf("second read = %v, want io.EOF", err)
	}

	closeErr := src.Close()
	if closeErr == nil {
		t.Fatal("Close = nil, want a process error for exit 7")
	}
	var procErr *ProcessError
	if !errors.As(closeErr, &procErr) {
		t.Fatalf("Close = %T, want *ProcessError", closeErr)
	}
	err := EndError(src, closeErr)
	if err == nil || !strings.Contains(err.Error(), "ffmpeg failed") {
		t.Fatalf("EndError = %v, want ffmpeg failure", err)
	}
}

func TestSourceKillsOnExplicitClose(t *testing.T) {
	src := openScript(t, "exec sleep 60\n")

	err := src.Close()
	if err == nil {
		t.Fatal("Close = nil, want a killed-process error")
	}
	var procErr *ProcessError
	if !errors.As(err, &procErr) {
		t.Fatalf("Close = %v, want a killed-process error", err)
	}
}

func openScript(t *testing.T, body string) Source {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake ffmpeg is not available on Windows")
	}

	script := filepath.Join(t.TempDir(), "fake-ffmpeg")
	content := "#!/bin/sh\n" + body
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}

	src, err := (Opener{FFmpeg: script}).Open(context.Background(), "https://example.invalid/stream", 0)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	return src
}
