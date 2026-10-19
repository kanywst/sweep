// Command sweep finds what you left running and helps you stop it.
package main

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/kanywst/sweep/internal/demo"
	"github.com/kanywst/sweep/internal/format"
	"github.com/kanywst/sweep/internal/scan"
	"github.com/kanywst/sweep/internal/tui"
	"github.com/kanywst/sweep/internal/version"
)

const usage = `sweep finds what you left running and helps you stop it.

Usage:
  sweep              open the interactive view
  sweep ports        list processes with listening ports, grouped by directory
  sweep stop :5173   stop what listens on a port (or give a pid), with its launchers
  sweep stop --older-than 7d
                     stop everything started from a directory in your home (not
                     ~/Library) more than 7 days ago
  sweep version      print the version (also -v, --version)

Flags for ports:
  --json             print JSON
  --all              include system processes (working directory "/")

Flags for stop:
  --older-than AGE   instead of :port or pid: 7d, 2w, 12h
  --dry-run          print what would be stopped and stop nothing
  -y, --yes          do not ask for confirmation (required when not a terminal)
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "sweep:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	// Flags may come before or after the command: take the first bare word as
	// the command and parse everything else as flags.
	// --older-than is the only flag with a value, so it is the only flag that
	// may be followed by a bare word that is not the command.
	cmd := ""
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "--older-than" || a == "-older-than") && i+1 < len(args):
			flags = append(flags, a, args[i+1])
			i++
		case a != "" && a[0] == '-':
			flags = append(flags, a)
		case cmd == "":
			cmd = a
		default:
			rest = append(rest, a)
		}
	}
	fs := flag.NewFlagSet("sweep", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "print JSON")
	all := fs.Bool("all", false, "include system processes")
	dryRun := fs.Bool("dry-run", false, "print what would be stopped")
	olderThan := fs.String("older-than", "", "stop every dev server older than this")
	yes := fs.Bool("yes", false, "stop without asking")
	fs.BoolVar(yes, "y", false, "stop without asking")
	showVersion := fs.Bool("version", false, "print the version")
	fs.BoolVar(showVersion, "v", false, "print the version")
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			cmd = "help"
		} else {
			return fmt.Errorf("%w (try sweep help)", err)
		}
	}
	if len(rest) > 0 && cmd != "stop" {
		return fmt.Errorf("unexpected argument %q (try sweep help)", rest[0])
	}
	if *olderThan != "" && cmd != "stop" {
		return errors.New("--older-than goes with stop (try sweep help)")
	}
	if *showVersion {
		cmd = "version"
	}
	opt := scan.PortOptions{All: *all}

	switch cmd {
	case "":
		if *jsonOut || !term.IsTerminal(int(os.Stdout.Fd())) {
			return printPorts(ctx, out, opt, *jsonOut)
		}
		var m tui.Machine = tui.System{Opt: opt}
		if os.Getenv("SWEEP_DEMO") != "" {
			m = demo.New()
		}
		return tui.RunMachine(ctx, m)
	case "ports":
		return printPorts(ctx, out, opt, *jsonOut)
	case "stop":
		o := systemStopOptions(*dryRun, *yes)
		if *olderThan != "" {
			if len(rest) > 0 {
				return errors.New("give a :port or pid, or --older-than, not both")
			}
			d, err := format.ParseAge(*olderThan)
			if err != nil {
				return err
			}
			o.olderThan = d
		}
		return stopCommand(ctx, out, rest, o)
	case "version":
		_, err := fmt.Fprintln(out, "sweep", version.String())
		return err
	case "help":
		_, err := fmt.Fprint(out, usage)
		return err
	default:
		return fmt.Errorf("unknown command %q (try sweep help)", cmd)
	}
}

func printPorts(ctx context.Context, out io.Writer, opt scan.PortOptions, asJSON bool) error {
	gs, err := scan.Ports(ctx, scan.ExecRunner, opt)
	if err != nil {
		return err
	}
	if asJSON {
		if gs == nil {
			gs = []scan.Group{}
		}
		if err := json.MarshalWrite(out, gs, jsontext.WithIndent("  ")); err != nil {
			return err
		}
		_, err := fmt.Fprintln(out)
		return err
	}
	now := time.Now()
	type row struct{ age, pid, ports, mem, cmd string }
	var rows [][]row
	w := row{}
	for _, g := range gs {
		var rs []row
		for _, l := range g.Listeners {
			r := row{format.Age(now.Sub(l.Started)), strconv.Itoa(l.PID), format.Ports(l.Addrs), format.Bytes(l.RSS), format.Command(l.Command)}
			w = row{maxLen(w.age, r.age), maxLen(w.pid, r.pid), maxLen(w.ports, r.ports), maxLen(w.mem, r.mem), ""}
			rs = append(rs, r)
		}
		rows = append(rows, rs)
	}
	for i, g := range gs {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out, scan.ShortDir(g.Dir))
		for _, r := range rows[i] {
			fmt.Fprintf(out, "  %-*s  %*s  %-*s  %*s  %s\n",
				len(w.age), r.age, len(w.pid), r.pid, len(w.ports), r.ports, len(w.mem), r.mem, r.cmd)
		}
	}
	return nil
}

// maxLen returns whichever of a and b is longer.
func maxLen(a, b string) string {
	if len(b) > len(a) {
		return b
	}
	return a
}
