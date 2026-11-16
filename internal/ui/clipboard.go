package ui

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	clipboardTimeout  = 500 * time.Millisecond
	maxClipboardBytes = 4096
)

var errNoClipboardTool = errors.New("no clipboard tool found (install xclip, xsel, or wl-paste)")

// ClipboardFunc reads plain text from the system clipboard.
type ClipboardFunc func(ctx context.Context) (string, error)

func clipboardCandidates(goos, waylandDisplay string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbpaste"}}
	case "windows":
		return [][]string{
			{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard", "-Raw"},
			{"powershell", "-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard", "-Raw"},
		}
	default:
		var cands [][]string
		if waylandDisplay != "" {
			cands = append(cands,
				[]string{"wl-paste", "--no-newline"},
				[]string{"wl-paste", "--no-newline", "--primary"},
			)
		}
		cands = append(cands,
			[]string{"xclip", "-selection", "clipboard", "-o"},
			[]string{"xclip", "-selection", "primary", "-o"},
			[]string{"xsel", "--clipboard", "--output"},
			[]string{"xsel", "--primary", "--output"},
		)
		if waylandDisplay == "" {
			cands = append(cands, []string{"wl-paste", "--no-newline"})
		}
		return cands
	}
}

func readSystemClipboard(parent context.Context) (string, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, clipboardTimeout)
	defer cancel()

	foundTool := false
	for _, cand := range clipboardCandidates(runtime.GOOS, os.Getenv("WAYLAND_DISPLAY")) {
		bin, err := exec.LookPath(cand[0])
		if err != nil {
			continue
		}
		foundTool = true

		out, err := runClipboardCommand(ctx, bin, cand[1:])
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err == nil {
			out = strings.TrimRight(out, "\r\n")
			if out != "" {
				return out, nil
			}
		}
	}

	if !foundTool {
		return "", errNoClipboardTool
	}
	return "", nil
}

func runClipboardCommand(ctx context.Context, bin string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		_ = stdoutPipe.Close()
		return "", err
	}

	raw, readErr := io.ReadAll(io.LimitReader(stdoutPipe, maxClipboardBytes))
	_ = stdoutPipe.Close()
	waitErr := cmd.Wait()
	if readErr != nil {
		return "", readErr
	}
	if waitErr != nil {
		return "", waitErr
	}
	return string(raw), nil
}

func sanitizePasteText(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
