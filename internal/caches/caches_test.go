package caches

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b"), make([]byte, 24), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DirSize(dir); got != 1024 {
		t.Errorf("DirSize = %d, want 1024", got)
	}
}

func TestMeasureKeepsOrder(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(b, "x"), make([]byte, 10), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Measure([]Cache{{Name: "a", Path: a}, {Name: "b", Path: b}})
	if got[0].Name != "a" || got[0].Size != 0 || got[1].Name != "b" || got[1].Size != 10 {
		t.Errorf("got %+v", got)
	}
}

func TestFindOnlyExistingDirs(t *testing.T) {
	for _, c := range Find(t.Context()) {
		if fi, err := os.Stat(c.Path); err != nil || !fi.IsDir() {
			t.Errorf("%s: %s is not a directory", c.Name, c.Path)
		}
		if len(c.Clean) == 0 {
			t.Errorf("%s has no clean command", c.Name)
		}
	}
}
