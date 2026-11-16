package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSkinPickerCandidates(t *testing.T) {
	t.Run("darwin without startDir", func(t *testing.T) {
		got := skinPickerCandidates("darwin", "", "")
		want := [][]string{{
			"osascript",
			"-e", `POSIX path of (choose file with prompt "Select Classic Winamp Skin (.wsz / .zip)")`,
		}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})

	t.Run("darwin with startDir", func(t *testing.T) {
		got := skinPickerCandidates("darwin", "", "/Users/test/skins")
		if len(got) != 1 || got[0][0] != "osascript" || got[0][len(got[0])-1] != "/Users/test/skins" {
			t.Fatalf("unexpected darwin candidates: %#v", got)
		}
	})

	t.Run("windows escapes quotes", func(t *testing.T) {
		got := skinPickerCandidates("windows", "", `C:\O'Brien\skins`)
		if len(got) != 2 || got[0][0] != "powershell.exe" || got[1][0] != "powershell" {
			t.Fatalf("unexpected windows candidates: %#v", got)
		}
		script := got[0][len(got[0])-1]
		if !strings.Contains(script, `C:\O''Brien\skins`) {
			t.Fatalf("script did not escape single quote: %q", script)
		}
	})

	t.Run("linux default prefers zenity before kdialog", func(t *testing.T) {
		got := skinPickerCandidates("linux", "GNOME", "/tmp/skins")
		if len(got) != 4 {
			t.Fatalf("len = %d, want 4", len(got))
		}
		if got[0][0] != "zenity" || got[1][0] != "yad" || got[2][0] != "qarma" || got[3][0] != "kdialog" {
			t.Fatalf("unexpected order: %q, %q, %q, %q", got[0][0], got[1][0], got[2][0], got[3][0])
		}
		if !strings.Contains(strings.Join(got[0], " "), "--filename=/tmp/skins/") {
			t.Fatalf("zenity args missing --filename: %v", got[0])
		}
	})

	t.Run("linux KDE prefers kdialog first", func(t *testing.T) {
		got := skinPickerCandidates("linux", "KDE", "/tmp/skins")
		if len(got) != 4 || got[0][0] != "kdialog" {
			t.Fatalf("unexpected KDE candidates: %#v", got)
		}
		if got[0][4] != "/tmp/skins" {
			t.Fatalf("kdialog start dir = %q, want /tmp/skins", got[0][4])
		}
	})
}

func TestDefaultSkinStartDir(t *testing.T) {
	tmp := t.TempDir()
	wszFile := filepath.Join(tmp, "cool.wsz")
	if err := os.WriteFile(wszFile, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultSkinStartDir(wszFile); got != tmp {
		t.Fatalf("defaultSkinStartDir(file) = %q, want %q", got, tmp)
	}

	customDir := filepath.Join(tmp, "custom-skin")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := defaultSkinStartDir(customDir); got != customDir {
		t.Fatalf("defaultSkinStartDir(customDir) = %q, want %q", got, customDir)
	}

	devDir := filepath.Join(tmp, "dev-skin")
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Without a local ./skins dir, dev-skin falls back to "".
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	if got := defaultSkinStartDir(devDir); got != "" {
		t.Fatalf("defaultSkinStartDir(dev-skin without skins/) = %q, want empty", got)
	}

	skinsDir := filepath.Join(tmp, "skins")
	if err := os.MkdirAll(skinsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := defaultSkinStartDir(devDir); got != skinsDir {
		t.Fatalf("defaultSkinStartDir(dev-skin with skins/) = %q, want %q", got, skinsDir)
	}
}

func TestRunSkinPickerCommand(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("GO_WANT_SKIN_PICKER_HELPER", "1")

	t.Run("selected path", func(t *testing.T) {
		t.Setenv("SKIN_PICKER_MODE", "ok")
		got, cancelled, err := runSkinPickerCommand(context.Background(), exe, []string{"-test.run=TestSkinPickerHelperProcess"})
		if err != nil || cancelled || got != "/tmp/selected.wsz\n" {
			t.Fatalf("got (%q, %v, %v), want (/tmp/selected.wsz\\n, false, nil)", got, cancelled, err)
		}
	})

	t.Run("user cancelled exit code 1", func(t *testing.T) {
		t.Setenv("SKIN_PICKER_MODE", "cancel")
		got, cancelled, err := runSkinPickerCommand(context.Background(), exe, []string{"-test.run=TestSkinPickerHelperProcess"})
		if err != nil || !cancelled || got != "" {
			t.Fatalf("got (%q, %v, %v), want (\"\", true, nil)", got, cancelled, err)
		}
	})

	t.Run("display failure exit code 1", func(t *testing.T) {
		t.Setenv("SKIN_PICKER_MODE", "nodisplay")
		_, cancelled, err := runSkinPickerCommand(context.Background(), exe, []string{"-test.run=TestSkinPickerHelperProcess"})
		if err == nil || cancelled {
			t.Fatalf("got (cancelled=%v, err=%v), want non-nil error and cancelled=false", cancelled, err)
		}
	})

	t.Run("unexpected exit code 2", func(t *testing.T) {
		t.Setenv("SKIN_PICKER_MODE", "fail")
		_, cancelled, err := runSkinPickerCommand(context.Background(), exe, []string{"-test.run=TestSkinPickerHelperProcess"})
		if err == nil || cancelled {
			t.Fatalf("got (cancelled=%v, err=%v), want non-nil error and cancelled=false", cancelled, err)
		}
	})
}

func TestSkinPickerHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_SKIN_PICKER_HELPER") != "1" {
		return
	}
	switch os.Getenv("SKIN_PICKER_MODE") {
	case "ok":
		fmt.Println("/tmp/selected.wsz")
		os.Exit(0)
	case "cancel":
		os.Exit(1)
	case "nodisplay":
		fmt.Fprintln(os.Stderr, "Gtk-WARNING **: cannot open display")
		os.Exit(1)
	case "fail":
		fmt.Fprintln(os.Stderr, "unexpected failure")
		os.Exit(2)
	}
	os.Exit(0)
}
