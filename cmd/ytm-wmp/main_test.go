package main

import (
	"os"
	"testing"
)

func TestChooseSkinPrefersExplicitPath(t *testing.T) {
	const explicit = "/tmp/my-skin.wsz"
	if got := chooseSkin(explicit); got != explicit {
		t.Fatalf("chooseSkin(%q) = %q, want explicit path", explicit, got)
	}
}

func TestChooseSkinFindsDevSkinFromRepoRoot(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatal(err)
		}
	}()

	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	if got := chooseSkin(""); got != devSkinPath {
		t.Fatalf("chooseSkin(\"\") = %q, want %q", got, devSkinPath)
	}
}
