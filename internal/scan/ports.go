// Package scan finds things on this machine that are left running.
package scan

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kanywst/sweep/internal/ps"
)

// Listener is a process that holds at least one listening TCP socket.
type Listener struct {
	PID     int       `json:"pid"`
	PPID    int       `json:"ppid"`
	Name    string    `json:"name"`
	Command string    `json:"command"`
	Cwd     string    `json:"cwd"`
	Addrs   []string  `json:"addrs"`
	Started time.Time `json:"started"`
	RSS     int64     `json:"rss_bytes"`
	// Launchers are the npm / pnpm / sh -c processes above this one that
	// exist only to run it. Stopping the listener stops them too.
	Launchers []Process `json:"launchers,omitempty"`
}

// Process is a pid as it was when scanned.
type Process struct {
	PID     int       `json:"pid"`
	Command string    `json:"command"`
	Started time.Time `json:"started"`
}

// Proc returns the listener as a Process.
func (l Listener) Proc() Process { return Process{PID: l.PID, Command: l.Command, Started: l.Started} }

// Targets is the listener followed by its launchers.
func (l Listener) Targets() []Process { return append([]Process{l.Proc()}, l.Launchers...) }

// LauncherPIDs returns the pids of l.Launchers.
func (l Listener) LauncherPIDs() []int {
	out := make([]int, len(l.Launchers))
	for i, p := range l.Launchers {
		out[i] = p.PID
	}
	return out
}

// Group is the listeners started from one directory.
type Group struct {
	Dir       string     `json:"dir"`
	Listeners []Listener `json:"listeners"`
}

// Oldest returns the start time of the longest-running listener in g.
func (g Group) Oldest() time.Time {
	var t time.Time
	for _, l := range g.Listeners {
		if t.IsZero() || l.Started.Before(t) {
			t = l.Started
		}
	}
	return t
}

// Runner runs an external command and returns its stdout.
type Runner = ps.Runner

// ExecRunner runs lsof and ps by absolute path in the C locale.
var ExecRunner Runner = ps.Exec

// PortOptions controls Ports.
type PortOptions struct {
	// All includes processes whose working directory is "/", which are almost
	// always system services rather than something you started.
	All bool
}

// Ports lists processes with listening TCP sockets, grouped by working
// directory, oldest group first.
func Ports(ctx context.Context, run Runner, opt PortOptions) ([]Group, error) {
	out, err := run(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pcn")
	if errors.Is(err, ps.ErrNotFound) {
		return nil, err
	}
	if err != nil {
		// lsof exits 1 with no output when nothing matches; anything else is
		// a real failure and must not read as "all clear".
		if ee, ok := errors.AsType[*exec.ExitError](err); !ok || ee.ExitCode() != 1 || len(out) != 0 {
			return nil, fmt.Errorf("lsof: %w", err)
		}
	}
	ls := parseLsofListen(out)
	if len(ls) == 0 {
		return nil, nil
	}
	pids := slices.Sorted(maps.Keys(ls))

	if out, err := run(ctx, "lsof", "-a", "-d", "cwd", "-p", joinInts(pids), "-F", "pn"); err == nil || len(out) > 0 {
		for pid, cwd := range parseLsofCwd(out) {
			if l, ok := ls[pid]; ok {
				l.Cwd = cwd
			}
		}
	}

	table, err := ps.Table(ctx, run)
	if err != nil {
		return nil, err
	}
	children := ps.Children(table)

	var flat []Listener
	for pid, l := range ls {
		p, ok := table[pid]
		if !ok {
			continue // exited between lsof and ps
		}
		l.PPID, l.Started, l.RSS, l.Command = p.PPID, p.Started, p.RSS, p.Command
		l.Launchers = launchers(table, children, pid, ls)
		if !opt.All && (l.Cwd == "" || l.Cwd == "/") {
			continue
		}
		flat = append(flat, *l)
	}
	return group(flat), nil
}

func group(ls []Listener) []Group {
	byDir := map[string][]Listener{}
	for _, l := range ls {
		byDir[l.Cwd] = append(byDir[l.Cwd], l)
	}
	gs := make([]Group, 0, len(byDir))
	for dir, members := range byDir {
		slices.SortFunc(members, func(a, b Listener) int { return a.Started.Compare(b.Started) })
		gs = append(gs, Group{Dir: dir, Listeners: members})
	}
	slices.SortFunc(gs, func(a, b Group) int {
		return cmp.Or(a.Oldest().Compare(b.Oldest()), cmp.Compare(a.Dir, b.Dir))
	})
	return gs
}

// launchers walks up from pid while each parent is a launcher that does
// nothing but run the process below it: it must look like npm / pnpm / yarn /
// npx / bun run / sh -c by program name, have exactly one child, and hold no
// socket. A parent with a second child (pnpm -r, a tmux server, a shell
// running two jobs) is never stopped, because that would take the others down.
func launchers(table map[int]ps.Proc, children map[int]int, pid int, listeners map[int]*Listener) []Process {
	var out []Process
	cur := table[pid]
	for range 3 {
		parent, ok := table[cur.PPID]
		if !ok || parent.PID <= 1 || children[parent.PID] != 1 || !IsLauncher(parent.Command) {
			break
		}
		if _, listening := listeners[parent.PID]; listening {
			break
		}
		out = append(out, Process{PID: parent.PID, Command: parent.Command, Started: parent.Started})
		cur = parent
	}
	return out
}

var (
	launcherBins    = []string{"npm", "pnpm", "yarn", "npx", "pnpx"}
	launcherScripts = []string{"npm-cli.js", "npx-cli.js", "pnpm.cjs", "pnpm.mjs", "pnpm.js", "yarn.js", "yarn.cjs"}
)

// IsLauncher reports whether cmdline is a package-manager or shell process
// whose job is to start another program. It looks only at the program and
// the script it runs, never at arbitrary arguments, so "tmux new npm exec x"
// or "nvim scripts/npm" do not count.
func IsLauncher(cmdline string) bool {
	f := strings.Fields(cmdline)
	if len(f) == 0 {
		return false
	}
	bin := filepath.Base(f[0])
	switch {
	case slices.Contains(launcherBins, bin):
		return true
	case bin == "bun":
		return len(f) > 1 && (f[1] == "run" || f[1] == "x")
	case bin == "sh":
		return len(f) > 1 && f[1] == "-c"
	case strings.HasPrefix(bin, "node"):
		if len(f) < 2 {
			return false
		}
		script := filepath.Base(f[1])
		return slices.Contains(launcherScripts, script) || supervisesServer(script, f[2:])
	}
	return false
}

// supervisesServer reports dev commands that fork the real server as a child
// and do nothing else. `next dev` neither restarts nor exits when its child is
// killed (next 16 dist/cli/next-dev.js), so it would be left behind.
func supervisesServer(script string, args []string) bool {
	return script == "next" && len(args) > 0 && args[0] == "dev"
}

func parseLsofListen(out []byte) map[int]*Listener {
	ls := map[int]*Listener{}
	var cur *Listener
	for line := range strings.Lines(string(out)) {
		line = strings.TrimRight(line, "\n")
		if line == "" {
			continue
		}
		v := line[1:]
		switch line[0] {
		case 'p':
			pid, err := strconv.Atoi(v)
			if err != nil {
				cur = nil
				continue
			}
			if ls[pid] == nil {
				ls[pid] = &Listener{PID: pid}
			}
			cur = ls[pid]
		case 'c':
			if cur != nil {
				cur.Name = v
			}
		case 'n':
			if cur != nil && !slices.Contains(cur.Addrs, v) {
				cur.Addrs = append(cur.Addrs, v)
			}
		}
	}
	return ls
}

func parseLsofCwd(out []byte) map[int]string {
	m := map[int]string{}
	pid := 0
	for line := range strings.Lines(string(out)) {
		line = strings.TrimRight(line, "\n")
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			if pid != 0 {
				m[pid] = line[1:]
			}
		}
	}
	return m
}

// ShortDir replaces the home directory prefix with "~".
func ShortDir(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return dir
	}
	if dir == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return dir
}

func joinInts(xs []int) string {
	s := make([]string, len(xs))
	for i, x := range xs {
		s[i] = strconv.Itoa(x)
	}
	return strings.Join(s, ",")
}
