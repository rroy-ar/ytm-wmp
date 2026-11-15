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

func TestInitialTracks(t *testing.T) {
	got := initialTracks(nil, nil)
	if len(got) != 1 || got[0].ID != defaultTarget {
		t.Fatalf("initialTracks(nil, nil) = %+v, want [%s]", got, defaultTarget)
	}

	got = initialTracks([]string{"id1", "id2"}, nil)
	if len(got) != 2 || got[0].ID != "id1" || got[1].ID != "id2" {
		t.Fatalf("initialTracks(flags, nil) = %+v", got)
	}

	got = initialTracks([]string{"id1"}, []string{"", "id2"})
	if len(got) != 2 || got[0].ID != "id1" || got[1].ID != "id2" {
		t.Fatalf("initialTracks(flags, args) = %+v", got)
	}
}
