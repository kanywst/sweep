// Package demo is a canned machine for screenshots and the README recording.
package demo

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/kanywst/sweep/internal/proc"
	"github.com/kanywst/sweep/internal/scan"
)

// Backend serves a fixed set of forgotten dev servers and forgets the ones
// you stop.
type Backend struct {
	mu      sync.Mutex
	stopped map[int]bool
	start   time.Time
}

// New returns a demo backend anchored at the current time.
func New() *Backend { return &Backend{stopped: map[int]bool{}, start: time.Now()} }

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

// Stop marks pids as gone.
func (b *Backend) Stop(pids []int) []proc.Result {
	time.Sleep(600 * time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	res := make([]proc.Result, len(pids))
	for i, p := range pids {
		b.stopped[p] = true
		res[i] = proc.Result{PID: p}
	}
	return res
}
