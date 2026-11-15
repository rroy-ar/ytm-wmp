package main

import (
	"context"
	"os"
	"testing"

	"ytm-wmp/internal/model"
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

type fakeStartupCatalog struct {
	playlist []model.Track
}

func (f fakeStartupCatalog) Search(context.Context, string, int) ([]model.Track, error) {
	return nil, nil
}
func (f fakeStartupCatalog) Playlist(context.Context, string) ([]model.Track, error) {
	return f.playlist, nil
}
func (f fakeStartupCatalog) Track(context.Context, string) (model.Track, error) {
	return model.Track{}, nil
}

func TestStartupTracksWithQueryAndPlaylist(t *testing.T) {
	cat := fakeStartupCatalog{playlist: []model.Track{{ID: "pl1"}, {ID: "pl2"}}}

	got, err := startupTracks(context.Background(), cat, nil, nil, "PL123", "")
	if err != nil || len(got) != 2 || got[0].ID != "pl1" {
		t.Fatalf("startupTracks playlist = %+v, err=%v", got, err)
	}

	got, err = startupTracks(context.Background(), cat, nil, nil, "", "daft punk")
	if err != nil || len(got) != 0 {
		t.Fatalf("startupTracks query = %+v, err=%v, want empty queue", got, err)
	}
}
