package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPlaceSingleCartridgeAndCollisions(t *testing.T) {
	root := t.TempDir()
	im := New(filepath.Join(root, "staging"), filepath.Join(root, "lib"), "")
	for i := 0; i < 2; i++ {
		write(t, filepath.Join(root, "staging", "r1", "Golden Sun", "Golden Sun (Europe).gba"))
		write(t, filepath.Join(root, "staging", "r1", "Golden Sun", "readme.nfo"))
		res, err := im.Run(context.Background(), Job{Subdir: "r1", Platform: "gba", Title: "Golden Sun", Formats: []string{"gba"}})
		if err != nil {
			t.Fatal(err)
		}
		want := map[int]string{0: "gba/Golden Sun (Europe).gba", 1: "gba/Golden Sun (Europe) (2).gba"}[i]
		if res.Placed != want {
			t.Fatalf("run %d placed %q, want %q", i, res.Placed, want)
		}
		if _, err := os.Stat(filepath.Join(root, "staging", "r1")); !os.IsNotExist(err) {
			t.Fatal("staging folder should be gone")
		}
	}
}

func TestMultiFileDiscGoesToFolder(t *testing.T) {
	root := t.TempDir()
	im := New(filepath.Join(root, "staging"), filepath.Join(root, "lib"), "")
	write(t, filepath.Join(root, "staging", "r2", "CB", "Crash.cue"))
	write(t, filepath.Join(root, "staging", "r2", "CB", "Crash (Track 1).bin"))
	write(t, filepath.Join(root, "staging", "r2", "CB", "Crash (Track 2).bin"))
	res, err := im.Run(context.Background(), Job{Subdir: "r2", Platform: "psx", Title: "Crash Bandicoot", Formats: []string{"chd", "cue", "bin"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Placed != "psx/Crash Bandicoot" {
		t.Fatalf("placed %q", res.Placed)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "lib", "psx", "Crash Bandicoot"))
	if len(entries) != 3 {
		t.Fatalf("want 3 files in the game folder, got %d", len(entries))
	}
}

func TestCartridgePackKeepsBest(t *testing.T) {
	root := t.TempDir()
	im := New(filepath.Join(root, "staging"), filepath.Join(root, "lib"), "")
	write(t, filepath.Join(root, "staging", "r3", "Game (USA).gba"))
	write(t, filepath.Join(root, "staging", "r3", "Game (France).gba"))
	res, err := im.Run(context.Background(), Job{Subdir: "r3", Platform: "gba", Title: "Game", Formats: []string{"gba"},
		Pick: func(names []string) int {
			for i, n := range names {
				if n == "Game (France).gba" {
					return i
				}
			}
			return 0
		}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Placed != "gba/Game (France).gba" {
		t.Fatalf("placed %q", res.Placed)
	}
}

func TestNoGameFile(t *testing.T) {
	root := t.TempDir()
	im := New(filepath.Join(root, "staging"), filepath.Join(root, "lib"), "")
	write(t, filepath.Join(root, "staging", "r4", "info.nfo"))
	if _, err := im.Run(context.Background(), Job{Subdir: "r4", Platform: "gba", Title: "X", Formats: []string{"gba"}}); err == nil {
		t.Fatal("want an error when no game file is present")
	}
}

func TestEnsureFolderWrapsASingleFile(t *testing.T) {
	root := t.TempDir()
	im := New(filepath.Join(root, "staging"), filepath.Join(root, "lib"), "")
	write(t, filepath.Join(root, "lib", "ps2", "Ico.iso"))
	got, err := im.EnsureFolder("ps2/Ico.iso")
	if err != nil || got != "ps2/Ico" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "lib", "ps2", "Ico", "Ico.iso")); err != nil {
		t.Fatal("file not moved into the folder")
	}
	// Already a folder: unchanged.
	if again, _ := im.EnsureFolder("ps2/Ico"); again != "ps2/Ico" {
		t.Fatalf("folder changed to %q", again)
	}
}
