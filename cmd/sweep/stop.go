package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/kanywst/sweep/internal/format"
	"github.com/kanywst/sweep/internal/proc"
	"github.com/kanywst/sweep/internal/scan"
)

type stopOptions struct {
	dryRun, yes bool
	// olderThan, when set, picks every dev server started longer ago than
	// this instead of the listeners named in args.
	olderThan   time.Duration
	home        string
	in          io.Reader
	interactive bool
	stop        func([]proc.Target) []proc.Result
	scan        func(context.Context, scan.PortOptions) ([]scan.Group, error)
}

// stopCommand stops listeners named by port (":5173") or pid ("41207"), or
// with o.olderThan every dev server older than that.
func stopCommand(ctx context.Context, out io.Writer, args []string, o stopOptions) error {
	if len(args) == 0 && o.olderThan == 0 {
		return errors.New("stop needs a :port, a pid or --older-than (try sweep help)")
	}
	// A port or pid can name anything, but --older-than only picks from what
	// sweep ports lists, never a system process.
	gs, err := o.scan(ctx, scan.PortOptions{All: o.olderThan == 0})
	if err != nil {
		return err
	}
	var ls []scan.Listener
	if o.olderThan > 0 {
		ls = olderThan(gs, time.Now().Add(-o.olderThan), o.home)
		if len(ls) == 0 {
			fmt.Fprintf(out, "nothing started more than %s ago\n", format.Age(o.olderThan))
			return nil
		}
	} else if ls, err = resolve(gs, args); err != nil {
		return err
	}

	verb := "stop"
	if o.dryRun {
		verb = "would stop"
	}
	for _, l := range ls {
		fmt.Fprintf(out, "%s %s  %s  (pid %d, %s)\n", verb, format.Ports(l.Addrs), format.Command(l.Command), l.PID, scan.ShortDir(l.Cwd))
		for _, p := range l.Launchers {
			fmt.Fprintf(out, "  and its launcher %s (pid %d)\n", format.Command(p.Command), p.PID)
		}
	}
	if o.dryRun {
		return nil
	}
	if !o.yes {
		if !o.interactive {
			return errors.New("not a terminal: pass --yes to stop without asking")
		}
		fmt.Fprintf(out, "Stop %d? [y/N] ", len(ls))
		ans, _ := bufio.NewReader(o.in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(ans)); a != "y" && a != "yes" {
			fmt.Fprintln(out, "kept running")
			return nil
		}
	}

	failed := 0
	for _, l := range ls {
		var ts []proc.Target
		for _, p := range l.Targets() {
			ts = append(ts, proc.Target{PID: p.PID, Started: p.Started})
		}
		res := o.stop(ts)
		switch r := res[0]; {
		case r.Err != nil:
			failed++
			fmt.Fprintf(out, "✗ %s  %v\n", format.Ports(l.Addrs), r.Err)
		case r.Killed:
			fmt.Fprintf(out, "✓ %s  stopped (needed SIGKILL)\n", format.Ports(l.Addrs))
		default:
			fmt.Fprintf(out, "✓ %s  stopped\n", format.Ports(l.Addrs))
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d not stopped", failed)
	}
	return nil
}

// olderThan returns the listeners known to have started before cutoff from a
// project directory under home, in scan order. Apps (~/Library/...) and
// services such as Homebrew's mysqld also listen, but are not dev servers you
// left behind, so a bulk stop leaves them alone.
func olderThan(gs []scan.Group, cutoff time.Time, home string) []scan.Listener {
	var out []scan.Listener
	for _, g := range gs {
		if home == "" || !inside(g.Dir, home) || inside(g.Dir, filepath.Join(home, "Library")) {
			continue
		}
		for _, l := range g.Listeners {
			// A zero start time means ps did not report the pid; its age is
			// unknown, so it is not old.
			if !l.Started.IsZero() && l.Started.Before(cutoff) {
				out = append(out, l)
			}
		}
	}
	return out
}

// inside reports whether dir is root or below it.
func inside(dir, root string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// resolve maps ":port" and "pid" arguments to listeners, in argument order.
func resolve(gs []scan.Group, args []string) ([]scan.Listener, error) {
	var out []scan.Listener
	seen := map[int]bool{}
	var missing []string
	for _, a := range args {
		found := false
		for _, g := range gs {
			for _, l := range g.Listeners {
				if !matchesArg(l, a) || seen[l.PID] {
					continue
				}
				seen[l.PID], found = true, true
				out = append(out, l)
			}
		}
		if !found {
			missing = append(missing, a)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("nothing listening for %s", strings.Join(missing, ", "))
	}
	return out, nil
}

func matchesArg(l scan.Listener, a string) bool {
	if port, ok := strings.CutPrefix(a, ":"); ok {
		for _, addr := range l.Addrs {
			if _, p, ok := strings.CutLast(addr, ":"); ok && p == port {
				return true
			}
		}
		return false
	}
	pid, err := strconv.Atoi(a)
	return err == nil && pid == l.PID
}

func systemStopOptions(dryRun, yes bool) stopOptions {
	home, _ := os.UserHomeDir()
	return stopOptions{
		home:        home,
		dryRun:      dryRun,
		yes:         yes,
		in:          os.Stdin,
		interactive: term.IsTerminal(int(os.Stdin.Fd())),
		stop: func(ts []proc.Target) []proc.Result {
			return proc.Stop(ts, 3*time.Second, proc.SystemLookup)
		},
		scan: func(ctx context.Context, opt scan.PortOptions) ([]scan.Group, error) {
			return scan.Ports(ctx, scan.ExecRunner, opt)
		},
	}
}
