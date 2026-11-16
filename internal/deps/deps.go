// Package deps discovers the external tools the player depends on.
//
// The tools are deliberately not bundled: the player only checks that they
// exist, reports their versions for diagnostics, and lets the user install or
// update them through the system package manager (or yt-dlp's self-update).
package deps

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Tool describes an executable found on PATH.
type Tool struct {
	Name    string
	Path    string
	Version string // first line of the version output, or "" if unavailable
}

// MissingError reports a required tool that could not be found.
type MissingError struct {
	Name         string
	OverridePath string
	Err          error
}

func (e *MissingError) Error() string {
	hint := InstallHint(e.Name)
	if e.OverridePath != "" {
		return fmt.Sprintf("required tool %q not found at %q: %v; %s", e.Name, e.OverridePath, e.Err, hint)
	}
	return fmt.Sprintf("required tool %q not found in PATH: %v; %s", e.Name, e.Err, hint)
}

func (e *MissingError) Unwrap() error { return e.Err }

// InstallHint returns an actionable installation URL or guidance for name.
func InstallHint(name string) string {
	switch name {
	case "yt-dlp":
		return "install it from https://github.com/yt-dlp/yt-dlp#installation"
	case "ffmpeg":
		return "install it from https://ffmpeg.org/download.html"
	default:
		return "install it and make sure it is on PATH"
	}
}

// Check looks up name on PATH and asks it for its version.
//
// A missing executable is an error. A tool that exists but cannot report a
// version is still returned: being able to run the binary matters more than
// parsing its version string, and the empty Version tells the caller to show
// "unknown" rather than failing startup.
func Check(ctx context.Context, name string) (Tool, error) {
	return CheckPath(ctx, name, "")
}

// CheckPath resolves overridePath when non-empty (falling back to PATH lookup
// of name when empty) and asks the executable for its version.
func CheckPath(ctx context.Context, name, overridePath string) (Tool, error) {
	target := strings.TrimSpace(overridePath)
	lookup := name
	if target != "" {
		lookup = target
	}
	path, err := exec.LookPath(lookup)
	if err != nil {
		return Tool{}, &MissingError{Name: name, OverridePath: target, Err: err}
	}

	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return Tool{
		Name:    name,
		Path:    path,
		Version: version(versionCtx, path),
	}, nil
}

func version(ctx context.Context, path string) string {
	out, err := exec.CommandContext(ctx, path, versionArgs(path)...).Output()
	if err != nil && len(out) == 0 {
		return ""
	}

	line, _, _ := strings.Cut(string(out), "\n")
	line = strings.TrimSpace(line)
	if len(line) > 200 {
		line = line[:200]
	}
	return line
}

func versionArgs(path string) []string {
	// Normalize both separators so the helper also behaves when inspecting a
	// Windows path from a non-Windows host (and vice versa).
	name := strings.ReplaceAll(path, `\`, "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")

	if name == "ffmpeg" || name == "ffprobe" {
		return []string{"-version"}
	}
	return []string{"--version"}
}

// IsMissing reports whether err is a MissingError.
func IsMissing(err error) bool {
	var missing *MissingError
	return errors.As(err, &missing)
}
