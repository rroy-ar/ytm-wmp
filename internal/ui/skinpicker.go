package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	maxSkinPathBytes   = 4096
	maxPickerErrBytes  = 2048
	skinPickerWinTitle = "Select Classic Winamp Skin"
)

var errNoSkinPickerTool = errors.New("no file picker tool found (install zenity or kdialog)")

// SkinPickerFunc opens an OS file dialog and returns the selected skin path
// (.wsz, .zip, or directory), or ("", nil) when the user cancels.
type SkinPickerFunc func(ctx context.Context, startDir string) (string, error)

func skinPickerCandidates(goos, desktopEnv, startDir string) [][]string {
	switch goos {
	case "darwin":
		if startDir != "" {
			return [][]string{{
				"osascript",
				"-e", "on run argv",
				"-e", `POSIX path of (choose file with prompt "Select Classic Winamp Skin (.wsz / .zip)" default location POSIX file (item 1 of argv))`,
				"-e", "end run",
				"--", startDir,
			}}
		}
		return [][]string{{
			"osascript",
			"-e", `POSIX path of (choose file with prompt "Select Classic Winamp Skin (.wsz / .zip)")`,
		}}
	case "windows":
		var initLine string
		if startDir != "" {
			escaped := strings.ReplaceAll(startDir, "'", "''")
			initLine = fmt.Sprintf("$d.InitialDirectory = '%s'; ", escaped)
		}
		script := "Add-Type -AssemblyName System.Windows.Forms; " +
			"$d = New-Object System.Windows.Forms.OpenFileDialog; " +
			"$d.Title = '" + skinPickerWinTitle + "'; " +
			"$d.Filter = 'Winamp Skins (*.wsz;*.zip)|*.wsz;*.zip|All files (*.*)|*.*'; " +
			initLine +
			"if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($d.FileName) }"
		return [][]string{
			{"powershell.exe", "-NoProfile", "-NonInteractive", "-Sta", "-Command", script},
			{"powershell", "-NoProfile", "-NonInteractive", "-Sta", "-Command", script},
		}
	default:
		dirArg := ""
		if startDir != "" {
			dirArg = filepath.Clean(startDir) + "/"
		}
		zenityArgs := []string{
			"--file-selection",
			"--title=" + skinPickerWinTitle,
			"--file-filter=Winamp skins (*.wsz *.zip) | *.wsz *.zip",
			"--file-filter=All files | *",
		}
		if dirArg != "" {
			zenityArgs = append(zenityArgs, "--filename="+dirArg)
		}

		yadArgs := []string{
			"--file",
			"--title=" + skinPickerWinTitle,
			"--file-filter=Winamp skins (*.wsz *.zip) | *.wsz *.zip",
			"--file-filter=All files | *",
		}
		if dirArg != "" {
			yadArgs = append(yadArgs, "--filename="+dirArg)
		}

		kdialogStart := "."
		if startDir != "" {
			kdialogStart = filepath.Clean(startDir)
		}

		zenityCmd := append([]string{"zenity"}, zenityArgs...)
		yadCmd := append([]string{"yad"}, yadArgs...)
		qarmaCmd := append([]string{"qarma"}, zenityArgs...)
		kdialogCmd := []string{"kdialog", "--title", skinPickerWinTitle, "--getopenfilename", kdialogStart, "*.wsz *.zip|Winamp skins (*.wsz *.zip)"}

		if strings.Contains(strings.ToUpper(desktopEnv), "KDE") {
			return [][]string{kdialogCmd, zenityCmd, yadCmd, qarmaCmd}
		}
		return [][]string{zenityCmd, yadCmd, qarmaCmd, kdialogCmd}
	}
}

func pickSkinFile(parent context.Context, startDir string) (string, error) {
	if parent == nil {
		parent = context.Background()
	}

	cands := skinPickerCandidates(runtime.GOOS, os.Getenv("XDG_CURRENT_DESKTOP"), startDir)
	for _, cand := range cands {
		bin, err := exec.LookPath(cand[0])
		if err != nil {
			continue
		}
		out, cancelled, err := runSkinPickerCommand(parent, bin, cand[1:])
		if parent.Err() != nil {
			return "", parent.Err()
		}
		if cancelled {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return strings.TrimRight(out, "\r\n"), nil
	}
	return "", errNoSkinPickerTool
}

func runSkinPickerCommand(ctx context.Context, bin string, args []string) (path string, cancelled bool, err error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", false, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", false, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		return "", false, err
	}

	rawOut, readErr := io.ReadAll(io.LimitReader(stdoutPipe, maxSkinPathBytes))
	rawErr, _ := io.ReadAll(io.LimitReader(stderrPipe, maxPickerErrBytes))
	_ = stdoutPipe.Close()
	_ = stderrPipe.Close()
	waitErr := cmd.Wait()

	if readErr != nil {
		return "", false, readErr
	}
	if waitErr == nil {
		return string(rawOut), false, nil
	}

	stderrText := strings.TrimSpace(string(rawErr))
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) && exitErr.ExitCode() == 1 {
		lower := strings.ToLower(stderrText)
		if !strings.Contains(lower, "cannot open display") && !strings.Contains(lower, "could not connect") {
			return "", true, nil
		}
	}
	if stderrText != "" {
		return "", false, fmt.Errorf("skin picker (%s): %w: %s", filepath.Base(bin), waitErr, stderrText)
	}
	return "", false, fmt.Errorf("skin picker (%s): %w", filepath.Base(bin), waitErr)
}

func defaultSkinStartDir(skinPath string) string {
	if skinPath != "" {
		if info, err := os.Stat(skinPath); err == nil {
			dir := skinPath
			if !info.IsDir() {
				dir = filepath.Dir(skinPath)
			}
			if filepath.Base(filepath.Clean(dir)) != "dev-skin" {
				if abs, err := filepath.Abs(dir); err == nil {
					return abs
				}
				return dir
			}
		}
	}
	if info, err := os.Stat("skins"); err == nil && info.IsDir() {
		if abs, err := filepath.Abs("skins"); err == nil {
			return abs
		}
		return "skins"
	}
	return ""
}
