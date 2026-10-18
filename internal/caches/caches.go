// Package caches finds package-manager and build caches and clears them with
// each tool's own command.
package caches

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Cache is one tool's cache directory.
type Cache struct {
	Name  string   `json:"name"`
	Path  string   `json:"path"`
	Size  int64    `json:"size_bytes"`
	Clean []string `json:"clean"` // the command that clears it
}

// CleanCommand is Clean as a shell-like string, for display.
func (c Cache) CleanCommand() string { return strings.Join(c.Clean, " ") }

// spec says where a tool keeps its cache and how to clear it. dir asks the
// tool itself when it can, so a relocated cache is still found.
type spec struct {
	name  string
	tool  string
	dir   func(ctx context.Context) string
	clean []string
}

func toolOutput(ctx context.Context, name string, args ...string) string {
	if _, err := exec.LookPath(name); err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func home(rel ...string) func(context.Context) string {
	return func(context.Context) string {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(append([]string{h}, rel...)...)
	}
}

func specs() []spec {
	yarnDir := home(".cache", "yarn")
	if runtime.GOOS == "darwin" {
		yarnDir = home("Library", "Caches", "Yarn")
	}
	return []spec{
		{"Go build", "go", func(ctx context.Context) string { return toolOutput(ctx, "go", "env", "GOCACHE") }, []string{"go", "clean", "-cache"}},
		{"Go modules", "go", func(ctx context.Context) string { return toolOutput(ctx, "go", "env", "GOMODCACHE") }, []string{"go", "clean", "-modcache"}},
		{"npm", "npm", home(".npm", "_cacache"), []string{"npm", "cache", "clean", "--force"}},
		{"pnpm store", "pnpm", func(ctx context.Context) string { return toolOutput(ctx, "pnpm", "store", "path") }, []string{"pnpm", "store", "prune"}},
		{"yarn", "yarn", yarnDir, []string{"yarn", "cache", "clean"}},
		{"uv", "uv", func(ctx context.Context) string { return toolOutput(ctx, "uv", "cache", "dir") }, []string{"uv", "cache", "clean"}},
		{"pip", "pip3", func(ctx context.Context) string { return toolOutput(ctx, "pip3", "cache", "dir") }, []string{"pip3", "cache", "purge"}},
		{"Homebrew", "brew", func(ctx context.Context) string { return toolOutput(ctx, "brew", "--cache") }, []string{"brew", "cleanup", "--prune=all"}},
	}
}

// Find returns the caches of installed tools that exist on disk, without sizes.
func Find(ctx context.Context) []Cache {
	var out []Cache
	for _, s := range specs() {
		if _, err := exec.LookPath(s.tool); err != nil {
			continue
		}
		dir := s.dir(ctx)
		if dir == "" {
			continue
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		out = append(out, Cache{Name: s.name, Path: dir, Clean: s.clean})
	}
	return out
}

// Measure fills in sizes, one goroutine per cache.
func Measure(cs []Cache) []Cache {
	out := make([]Cache, len(cs))
	var wg sync.WaitGroup
	for i, c := range cs {
		wg.Go(func() {
			c.Size = DirSize(c.Path)
			out[i] = c
		})
	}
	wg.Wait()
	return out
}

// DirSize is the total size of regular files under dir.
func DirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// Clean runs the cache's own clean command.
func Clean(ctx context.Context, c Cache) error {
	cmd := exec.CommandContext(ctx, c.Clean[0], c.Clean[1:]...)
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		if msg != "" {
			return &cleanError{err: err, msg: msg}
		}
		return err
	}
	return nil
}

type cleanError struct {
	err error
	msg string
}

func (e *cleanError) Error() string { return e.msg }
func (e *cleanError) Unwrap() error { return e.err }
