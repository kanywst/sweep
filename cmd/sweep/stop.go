package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
	in          io.Reader
	interactive bool
	stop        func([]proc.Target) []proc.Result
	scan        func(context.Context) ([]scan.Group, error)
}

// stopCommand stops listeners named by port (":5173") or pid ("41207").
func stopCommand(ctx context.Context, out io.Writer, args []string, o stopOptions) error {
	if len(args) == 0 {
		return errors.New("stop needs a :port or a pid (try sweep help)")
	}
	gs, err := o.scan(ctx)
	if err != nil {
		return err
	}
	ls, err := resolve(gs, args)
	if err != nil {
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
	return stopOptions{
		dryRun:      dryRun,
		yes:         yes,
		in:          os.Stdin,
		interactive: term.IsTerminal(int(os.Stdin.Fd())),
		stop: func(ts []proc.Target) []proc.Result {
			return proc.Stop(ts, 3*time.Second, proc.SystemLookup)
		},
		scan: func(ctx context.Context) ([]scan.Group, error) {
			return scan.Ports(ctx, scan.ExecRunner, scan.PortOptions{All: true})
		},
	}
}
