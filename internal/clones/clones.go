// Package clones finds git working trees whose repository is gone.
package clones

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kanywst/sweep/internal/caches"
)

// Dead is a working tree whose .git file points at a git directory that no
// longer exists: a --separate-git-dir clone after its temp dir was wiped, or a
// worktree whose main repository was deleted. git cannot use it any more.
type Dead struct {
	Path     string    `json:"path"`
	GitDir   string    `json:"gitdir"`
	Size     int64     `json:"size_bytes"`
	Modified time.Time `json:"modified"`
}

// skip are directories never descended into.
var skip = map[string]bool{
	"node_modules": true, "Library": true, "vendor": true, ".Trash": true,
	"Applications": true, "Movies": true, "Music": true, "Pictures": true,
}

// Find walks roots up to depth levels deep and returns dead working trees.
// Hidden directories are skipped, as are the usual heavy ones.
func Find(ctx context.Context, roots []string, depth int) ([]Dead, error) {
	var out []Dead
	for _, root := range roots {
		base := strings.Count(filepath.Clean(root), string(filepath.Separator))
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil || !d.IsDir() {
				return nil
			}
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || skip[name]) {
				return fs.SkipDir
			}
			if strings.Count(path, string(filepath.Separator))-base > depth {
				return fs.SkipDir
			}
			if dd, ok := dead(path); ok {
				out = append(out, dd)
				return fs.SkipDir
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return out, err
		}
	}
	return out, nil
}

// dead reports whether dir has a .git file whose gitdir is missing.
func dead(dir string) (Dead, bool) {
	gitFile := filepath.Join(dir, ".git")
	fi, err := os.Lstat(gitFile)
	if err != nil || !fi.Mode().IsRegular() {
		return Dead{}, false
	}
	b, err := os.ReadFile(gitFile) //nolint:gosec // the .git file of a directory we walked
	if err != nil {
		return Dead{}, false
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return Dead{}, false
	}
	target = strings.TrimSpace(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) { //nolint:gosec // only checks that it exists
		return Dead{}, false
	}
	info, _ := os.Stat(dir)
	d := Dead{Path: dir, GitDir: target}
	if info != nil {
		d.Modified = info.ModTime()
	}
	return d, true
}

// Measure fills in sizes.
func Measure(ds []Dead) []Dead {
	for i := range ds {
		ds[i].Size = caches.DirSize(ds[i].Path)
	}
	return ds
}

// Trash moves path to the user's trash so it can be restored.
func Trash(path string) error {
	h, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	name := filepath.Base(path)
	var dir string
	switch runtime.GOOS {
	case "darwin":
		dir = filepath.Join(h, ".Trash")
	default:
		// XDG trash: the file goes in files/, its origin in info/.
		base := filepath.Join(h, ".local", "share", "Trash")
		dir = filepath.Join(base, "files")
		if err := os.MkdirAll(filepath.Join(base, "info"), 0o700); err != nil {
			return err
		}
		defer func() {
			info := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", path, time.Now().Format("2006-01-02T15:04:05"))
			_ = os.WriteFile(filepath.Join(base, "info", name+".trashinfo"), []byte(info), 0o600)
		}()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	dst := filepath.Join(dir, name)
	for i := 2; ; i++ {
		if _, err := os.Lstat(dst); errors.Is(err, fs.ErrNotExist) {
			break
		}
		dst = filepath.Join(dir, fmt.Sprintf("%s %d", name, i))
	}
	return os.Rename(path, dst)
}
