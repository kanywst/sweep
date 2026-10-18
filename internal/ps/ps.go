// Package ps reads the process table through the system ps(1).
package ps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Proc is one row of the process table.
type Proc struct {
	PID     int
	PPID    int
	RSS     int64 // bytes
	Started time.Time
	Stat    string
	Command string
}

// Zombie reports whether the process has exited and is waiting to be reaped.
func (p Proc) Zombie() bool { return strings.HasPrefix(p.Stat, "Z") }

// Same reports whether p is the process that was seen as want. A recycled pid
// has a later start time. The command line is not compared because programs
// such as node rewrite their own title after starting.
func (p Proc) Same(want Proc) bool { return p.Started.Equal(want.Started) }

// Runner runs an external command and returns its stdout.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Exec runs a tool by absolute path with a fixed environment, so neither PATH
// nor the caller's locale can change what runs or how its output looks. This
// matters most under sudo. The C locale also makes ps and lsof escape control
// characters in command lines instead of passing them to the terminal.
func Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := Tool(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	return cmd.Output()
}

var toolPaths = map[string][]string{
	"ps":   {"/bin/ps", "/usr/bin/ps"},
	"lsof": {"/usr/sbin/lsof", "/usr/bin/lsof", "/sbin/lsof", "/bin/lsof"},
}

// ErrNotFound is returned when a required system tool is missing.
var ErrNotFound = errors.New("not found")

// Tool returns the absolute path of a system tool.
func Tool(name string) (string, error) {
	for _, p := range toolPaths[name] {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	hint := ""
	if runtime.GOOS == "linux" {
		hint = " (install the " + map[string]string{"ps": "procps", "lsof": "lsof"}[name] + " package)"
	}
	return "", fmt.Errorf("%s %w%s", name, ErrNotFound, hint)
}

const lstartLayout = "Mon Jan 2 15:04:05 2006"

const columns = "pid=,ppid=,rss=,lstart=,stat=,command="

// Table returns every process on the machine.
func Table(ctx context.Context, run Runner) (map[int]Proc, error) {
	out, err := run(ctx, "ps", "-A", "-o", columns)
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return Parse(out)
}

// Lookup returns the processes among pids that still exist.
func Lookup(ctx context.Context, run Runner, pids []int) (map[int]Proc, error) {
	if len(pids) == 0 {
		return map[int]Proc{}, nil
	}
	s := make([]string, len(pids))
	for i, p := range pids {
		s[i] = strconv.Itoa(p)
	}
	out, err := run(ctx, "ps", "-o", columns, "-p", strings.Join(s, ","))
	if err != nil && len(out) == 0 {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return map[int]Proc{}, nil // ps exits 1 when none of the pids exist
		}
		return nil, fmt.Errorf("ps: %w", err)
	}
	return Parse(out)
}

// Parse reads `ps -o pid=,ppid=,rss=,lstart=,stat=,command=` output.
func Parse(out []byte) (map[int]Proc, error) {
	m := map[int]Proc{}
	for line := range strings.Lines(string(out)) {
		f := strings.Fields(line)
		if len(f) < 10 {
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
		m[pid] = Proc{PID: pid, PPID: ppid, RSS: rss * 1024, Started: started, Stat: f[8], Command: strings.Join(f[9:], " ")}
	}
	return m, nil
}

// Children counts each process's direct children.
func Children(table map[int]Proc) map[int]int {
	n := map[int]int{}
	for _, p := range table {
		n[p.PPID]++
	}
	return n
}
