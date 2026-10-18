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

	"github.com/kanywst/sweep/internal/format"
	"github.com/kanywst/sweep/internal/scan"
	"github.com/kanywst/sweep/internal/tui"
	"github.com/kanywst/sweep/internal/version"
)

const usage = `sweep finds what you left running and helps you stop it.

Usage:
  sweep              open the interactive view
  sweep ports        list processes with listening ports, grouped by directory
  sweep version      print the version

Flags for ports:
  --json             print JSON
  --all              include system processes (working directory "/")
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
	cmd := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("sweep", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	jsonOut := fs.Bool("json", false, "print JSON")
	all := fs.Bool("all", false, "include system processes")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	opt := scan.PortOptions{All: *all}

	switch cmd {
	case "":
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			return printPorts(ctx, out, opt, *jsonOut)
		}
		return tui.Run(ctx, opt)
	case "ports":
		return printPorts(ctx, out, opt, *jsonOut)
	case "version":
		_, err := fmt.Fprintln(out, "sweep", version.String())
		return err
	case "help":
		fmt.Fprint(out, usage)
		return nil
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
