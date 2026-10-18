// Package scan finds things on this machine that are left running or left on disk.
package scan

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Listener is a process that holds at least one listening TCP socket.
type Listener struct {
	PID      int       `json:"pid"`
	PPID     int       `json:"ppid"`
	Name     string    `json:"name"`
	Command  string    `json:"command"`
	Cwd      string    `json:"cwd"`
	Addrs    []string  `json:"addrs"`
	Started  time.Time `json:"started"`
	RSS      int64     `json:"rss_bytes"`
	Wrappers []int     `json:"wrapper_pids,omitempty"`
}

// Age is how long the process has been running.
func (l Listener) Age(now time.Time) time.Duration { return now.Sub(l.Started) }

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
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs commands with a C locale so ps output parses the same everywhere.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd.Output()
}

// PortOptions controls Ports.
type PortOptions struct {
	// All includes processes whose working directory is "/", which are almost
	// always system services rather than something you started.
	All bool
}

// Ports lists processes with listening TCP sockets, grouped by working
// directory, oldest group first.
func Ports(ctx context.Context, run Runner, opt PortOptions) ([]Group, error) {
	// lsof exits 1 when it finds nothing; treat empty output as no listeners.
	out, err := run(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pcn")
	if err != nil && len(out) == 0 {
		return nil, nil
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

	procs, err := psInfo(ctx, run, pids)
	if err != nil {
		return nil, err
	}
	var parents []int
	for _, p := range procs {
		parents = append(parents, p.ppid)
	}
	pp, _ := psInfo(ctx, run, parents)
	for k, v := range pp {
		if _, ok := procs[k]; !ok {
			procs[k] = v
		}
	}
	// One more level, for pnpm -> sh -c -> node.
	var grand []int
	for _, p := range pp {
		grand = append(grand, p.ppid)
	}
	gp, _ := psInfo(ctx, run, grand)
	for k, v := range gp {
		if _, ok := procs[k]; !ok {
			procs[k] = v
		}
	}

	var flat []Listener
	for pid, l := range ls {
		p, ok := procs[pid]
		if !ok {
			continue // exited between lsof and ps
		}
		l.PPID, l.Started, l.RSS, l.Command = p.ppid, p.started, p.rss, p.command
		l.Wrappers = wrappers(procs, l.PPID, ls)
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

// wrapperRe matches launcher processes that only exist to run a dev server.
var wrapperRe = regexp.MustCompile(`(^|/)(npm|pnpm|yarn|npx|bun)(\s|$)|pnpm\.mjs|npm exec|^(/bin/)?sh -c `)

// wrappers walks up from ppid while the parent is a launcher that holds no
// socket of its own, so stopping a dev server also stops what started it.
func wrappers(procs map[int]psProc, ppid int, listeners map[int]*Listener) []int {
	var out []int
	for range 3 {
		if ppid <= 1 {
			break
		}
		p, ok := procs[ppid]
		if !ok || !wrapperRe.MatchString(p.command) {
			break
		}
		if _, listening := listeners[ppid]; listening {
			break
		}
		out = append(out, ppid)
		ppid = p.ppid
	}
	return out
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

type psProc struct {
	ppid    int
	rss     int64
	started time.Time
	command string
}

const lstartLayout = "Mon Jan 2 15:04:05 2006"

func psInfo(ctx context.Context, run Runner, pids []int) (map[int]psProc, error) {
	pids = uniq(pids)
	if len(pids) == 0 {
		return map[int]psProc{}, nil
	}
	out, err := run(ctx, "ps", "-o", "pid=,ppid=,rss=,lstart=,command=", "-p", joinInts(pids))
	if err != nil && len(out) == 0 {
		// ps exits 1 if any pid is gone; only fail when nothing came back.
		return map[int]psProc{}, nil
	}
	return parsePs(out)
}

func parsePs(out []byte) (map[int]psProc, error) {
	m := map[int]psProc{}
	for line := range strings.Lines(string(out)) {
		f := strings.Fields(line)
		if len(f) < 9 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		rss, err3 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		started, err := time.ParseInLocation(lstartLayout, strings.Join(f[3:8], " "), time.Local)
		if err != nil {
			return nil, fmt.Errorf("parse ps start time %q: %w", strings.Join(f[3:8], " "), err)
		}
		m[pid] = psProc{ppid: ppid, rss: rss * 1024, started: started, command: strings.Join(f[8:], " ")}
	}
	return m, nil
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

func uniq(xs []int) []int {
	xs = slices.DeleteFunc(slices.Clone(xs), func(x int) bool { return x <= 0 })
	slices.Sort(xs)
	return slices.Compact(xs)
}
