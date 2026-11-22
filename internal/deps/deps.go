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
	"strconv"
	"strings"
	"time"
)

// DefaultYTDLPMaxAge is the age threshold after which yt-dlp is considered
// stale enough to warrant a startup update hint.
const DefaultYTDLPMaxAge = 180 * 24 * time.Hour

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

// ParseYTDLPVersionDate parses yt-dlp's YYYY.MM.DD[.patch] version string into
// a UTC date. It returns false for unparseable or non-date version strings.
func ParseYTDLPVersionDate(version string) (time.Time, bool) {
	v := strings.TrimSpace(version)
	if v == "" {
		return time.Time{}, false
	}
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return time.Time{}, false
	}
	if len(parts[0]) != 4 || len(parts[1]) != 2 || len(parts[2]) != 2 {
		return time.Time{}, false
	}
	y, errY := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	d, errD := strconv.Atoi(parts[2])
	if errY != nil || errM != nil || errD != nil {
		return time.Time{}, false
	}
	if y < 2020 || m < 1 || m > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != m || t.Day() != d {
		return time.Time{}, false
	}
	return t, true
}

// IsOutdatedYTDLP reports whether version is older than maxAge relative to now.
// When maxAge <= 0, DefaultYTDLPMaxAge is used. Unparseable versions return false.
func IsOutdatedYTDLP(version string, now time.Time, maxAge time.Duration) bool {
	if maxAge <= 0 {
		maxAge = DefaultYTDLPMaxAge
	}
	releaseDate, ok := ParseYTDLPVersionDate(version)
	if !ok {
		return false
	}
	return now.UTC().Sub(releaseDate) > maxAge
}

// OutdatedYTDLPWarning returns an actionable startup warning when yt-dlp is
// older than DefaultYTDLPMaxAge, or "" otherwise.
func OutdatedYTDLPWarning(version string, now time.Time) string {
	if !IsOutdatedYTDLP(version, now, DefaultYTDLPMaxAge) {
		return ""
	}
	return fmt.Sprintf("yt-dlp %s is over 6 months old; run 'yt-dlp -U' or update via your package manager if playback fails", strings.TrimSpace(version))
}
