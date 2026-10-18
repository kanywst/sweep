package clones

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindDeadClones(t *testing.T) {
	root := t.TempDir()
	live := t.TempDir()
	write(t, filepath.Join(root, "work", "dead", ".git"), "gitdir: /nonexistent/sweep-test-gitdir\n")
	write(t, filepath.Join(root, "work", "dead", "main.go"), "package main\n")
	write(t, filepath.Join(root, "work", "alive", ".git"), "gitdir: "+live+"\n")
	if err := os.MkdirAll(filepath.Join(root, "work", "normal", ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "node_modules", "pkg", ".git"), "gitdir: /nonexistent/x\n")
	write(t, filepath.Join(root, "a", "b", "c", "d", "e", "deep", ".git"), "gitdir: /nonexistent/y\n")

	ds, err := Find(t.Context(), []string{root}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Path != filepath.Join(root, "work", "dead") || ds[0].GitDir != "/nonexistent/sweep-test-gitdir" {
		t.Fatalf("got %+v, want only work/dead", ds)
	}
	if Measure(ds)[0].Size == 0 {
		t.Error("size not measured")
	}
}

func TestTrashMovesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(t.TempDir(), "dead")
	write(t, filepath.Join(dir, "f"), "x")
	if err := Trash(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("still there")
	}
	matches, _ := filepath.Glob(filepath.Join(home, "*", "dead"))
	more, _ := filepath.Glob(filepath.Join(home, ".local", "share", "Trash", "files", "dead"))
	if len(matches)+len(more) == 0 {
		t.Fatal("not in the trash")
	}
}
