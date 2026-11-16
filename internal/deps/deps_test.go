package deps

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckFindsToolAndVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}

	dir := t.TempDir()
	writeTool(t, dir, "fake-tool", "#!/bin/sh\necho 1.2.3\n")
	t.Setenv("PATH", dir)

	tool, err := Check(context.Background(), "fake-tool")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if tool.Name != "fake-tool" {
		t.Fatalf("Name = %q, want fake-tool", tool.Name)
	}
	if want := filepath.Join(dir, "fake-tool"); tool.Path != want {
		t.Fatalf("Path = %q, want %q", tool.Path, want)
	}
	if tool.Version != "1.2.3" {
		t.Fatalf("Version = %q, want 1.2.3", tool.Version)
	}
}

func TestCheckUsesFfmpegVersionFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}

	dir := t.TempDir()
	writeTool(t, dir, "ffmpeg", "#!/bin/sh\necho \"$@\"\n")
	t.Setenv("PATH", dir)

	tool, err := Check(context.Background(), "ffmpeg")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if tool.Version != "-version" {
		t.Fatalf("Version = %q, want ffmpeg invocation with -version", tool.Version)
	}
}

func TestCheckMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := Check(context.Background(), "definitely-not-a-real-tool")
	if err == nil {
		t.Fatal("Check = nil, want missing tool error")
	}
	if !IsMissing(err) {
		t.Fatalf("IsMissing(%v) = false, want true", err)
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Fatalf("error = %q, want actionable PATH message", err)
	}
}

func TestCheckPathOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}
	// Empty PATH ensures lookup only succeeds via explicit override path.
	t.Setenv("PATH", t.TempDir())

	toolDir := t.TempDir()
	customPath := filepath.Join(toolDir, "custom-yt-dlp")
	writeTool(t, toolDir, "custom-yt-dlp", "#!/bin/sh\necho 2026.11.01\n")

	tool, err := CheckPath(context.Background(), "yt-dlp", customPath)
	if err != nil {
		t.Fatalf("CheckPath valid = %v", err)
	}
	if tool.Path != customPath || tool.Version != "2026.11.01" {
		t.Fatalf("CheckPath = %+v, want path %q version 2026.11.01", tool, customPath)
	}

	_, err = CheckPath(context.Background(), "yt-dlp", filepath.Join(toolDir, "missing-bin"))
	if !IsMissing(err) || !strings.Contains(err.Error(), "missing-bin") {
		t.Fatalf("CheckPath missing err = %v, want MissingError mentioning override", err)
	}
}

func TestVersionArgs(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/usr/bin/yt-dlp", "--version"},
		{"/usr/bin/ffmpeg", "-version"},
		{`C:\tools\ffmpeg.exe`, "-version"},
		{`C:\tools\yt-dlp.exe`, "--version"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			args := versionArgs(tt.path)
			if len(args) != 1 || args[0] != tt.want {
				t.Fatalf("versionArgs(%q) = %q, want [%q]", tt.path, args, tt.want)
			}
		})
	}
}

func writeTool(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
