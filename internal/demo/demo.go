// Package demo is a canned machine for screenshots and the README recording.
package demo

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/kanywst/sweep/internal/caches"
	"github.com/kanywst/sweep/internal/clones"

	"github.com/kanywst/sweep/internal/proc"
	"github.com/kanywst/sweep/internal/scan"
)

// Backend serves a fixed set of forgotten dev servers and forgets the ones
// you stop.
type Backend struct {
	mu      sync.Mutex
	stopped map[int]bool
	cleaned map[string]bool
	start   time.Time
}

// New returns a demo backend anchored at the current time.
func New() *Backend {
	return &Backend{stopped: map[int]bool{}, cleaned: map[string]bool{}, start: time.Now()}
}

type entry struct {
	dir       string
	pid       int
	addr      string
	age       time.Duration
	mem       int64
	cmd       string
	launchers []scan.Process
}

const day = 24 * time.Hour

var world = []entry{
	{"~/work/shop", 41207, "[::1]:5173", 23 * day, 182 << 20, "node node_modules/.bin/vite dev", []scan.Process{{PID: 41205, Command: "sh -c vite"}, {PID: 41203, Command: "pnpm dev"}}},
	{"~/work/shop", 41388, "[::1]:4173", 23 * day, 96 << 20, "node node_modules/.bin/vite preview", []scan.Process{{PID: 41386, Command: "pnpm preview"}}},
	{"~/work/shop", 52011, "*:6006", 19 * day, 410 << 20, "node node_modules/.bin/storybook dev -p 6006", []scan.Process{{PID: 52009, Command: "npm run storybook"}}},
	{"~/blog", 38842, "*:4321", 16 * day, 74 << 20, "node node_modules/.bin/astro dev --host", []scan.Process{{PID: 38840, Command: "npm run dev"}}},
	{"~/scratch/site", 27115, "*:8000", 12 * day, 21 << 20, "python3 -m http.server 8000", nil},
	{"~/work/api", 63390, "127.0.0.1:8080", 4 * day, 58 << 20, "air", nil},
	{"~/work/api", 63402, "127.0.0.1:5432", 4 * day, 132 << 20, "postgres -D .pgdata", nil},
	{"~/docs", 70211, "[::1]:3000", 2 * time.Hour, 140 << 20, "node node_modules/.bin/next dev", []scan.Process{{PID: 70209, Command: "pnpm dev"}}},
}

// Scan returns the processes not yet stopped.
func (b *Backend) Scan(context.Context) ([]scan.Group, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	time.Sleep(350 * time.Millisecond) // let the spinner show, as a real scan would
	var gs []scan.Group
	for _, e := range world {
		if b.stopped[e.pid] {
			continue
		}
		l := scan.Listener{
			PID: e.pid, PPID: 1, Name: "node", Command: e.cmd, Cwd: e.dir,
			Addrs: []string{e.addr}, Started: b.start.Add(-e.age), RSS: e.mem, Launchers: e.launchers,
		}
		i := slices.IndexFunc(gs, func(g scan.Group) bool { return g.Dir == e.dir })
		if i < 0 {
			gs = append(gs, scan.Group{Dir: e.dir})
			i = len(gs) - 1
		}
		gs[i].Listeners = append(gs[i].Listeners, l)
	}
	return gs, nil
}

// Stop marks the targets as gone.
func (b *Backend) Stop(targets []proc.Target) []proc.Result {
	time.Sleep(600 * time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	res := make([]proc.Result, len(targets))
	for i, t := range targets {
		b.stopped[t.PID] = true
		res[i] = proc.Result{PID: t.PID}
	}
	return res
}

var demoCaches = []caches.Cache{
	{Name: "Go build", Path: "~/Library/Caches/go-build", Size: 6 << 30, Clean: []string{"go", "clean", "-cache"}},
	{Name: "Homebrew", Path: "~/Library/Caches/Homebrew", Size: 3500 << 20, Clean: []string{"brew", "cleanup", "--prune=all"}},
	{Name: "uv", Path: "~/.cache/uv", Size: 3400 << 20, Clean: []string{"uv", "cache", "clean"}},
	{Name: "Go modules", Path: "~/go/pkg/mod", Size: 750 << 20, Clean: []string{"go", "clean", "-modcache"}},
	{Name: "npm", Path: "~/.npm/_cacache", Size: 716 << 20, Clean: []string{"npm", "cache", "clean", "--force"}},
}

var demoClones = []clones.Dead{
	{Path: "~/oss/kong", GitDir: "/tmp/kong-gitdir", Size: 23 << 20},
	{Path: "~/scratch/mcp-go", GitDir: "/tmp/mcp-go-gitdir", Size: 4 << 20},
}

// Caches returns canned caches that have not been cleaned.
func (b *Backend) Caches(context.Context) []caches.Cache {
	time.Sleep(500 * time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []caches.Cache
	for _, c := range demoCaches {
		if !b.cleaned[c.Path] {
			out = append(out, c)
		}
	}
	return out
}

// Clean pretends to run the clean command.
func (b *Backend) Clean(_ context.Context, c caches.Cache) error {
	time.Sleep(700 * time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleaned[c.Path] = true
	return nil
}

// Clones returns canned dead clones that have not been trashed.
func (b *Backend) Clones(context.Context) ([]clones.Dead, error) {
	time.Sleep(400 * time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []clones.Dead
	for _, d := range demoClones {
		if !b.cleaned[d.Path] {
			out = append(out, d)
		}
	}
	return out, nil
}

// Trash pretends to move path to the trash.
func (b *Backend) Trash(path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if path == "" {
		return errors.New("empty path")
	}
	b.cleaned[path] = true
	return nil
}
