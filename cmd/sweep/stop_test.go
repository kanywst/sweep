package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kanywst/sweep/internal/proc"
	"github.com/kanywst/sweep/internal/scan"
)

func fakeStop(stopped *[]int) stopOptions {
	gs := []scan.Group{{Dir: "/w", Listeners: []scan.Listener{
		{PID: 100, Addrs: []string{"[::1]:5173"}, Command: "node /w/node_modules/.bin/vite dev", Cwd: "/w", Started: time.Unix(1, 0),
			Launchers: []scan.Process{{PID: 99, Command: "pnpm dev", Started: time.Unix(1, 0)}}},
		{PID: 200, Addrs: []string{"*:8000"}, Command: "python3 -m http.server", Cwd: "/w", Started: time.Unix(2, 0)},
	}}}
	return stopOptions{
		scan: func(context.Context) ([]scan.Group, error) { return gs, nil },
		stop: func(ts []proc.Target) []proc.Result {
			var rs []proc.Result
			for _, t := range ts {
				*stopped = append(*stopped, t.PID)
				rs = append(rs, proc.Result{PID: t.PID})
			}
			return rs
		},
	}
}

func TestStopByPortWithYes(t *testing.T) {
	var stopped []int
	o := fakeStop(&stopped)
	o.yes = true
	var out bytes.Buffer
	if err := stopCommand(t.Context(), &out, []string{":5173"}, o); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 2 || stopped[0] != 100 || stopped[1] != 99 {
		t.Errorf("stopped %v, want server then launcher", stopped)
	}
	if !strings.Contains(out.String(), "and its launcher pnpm dev") || !strings.Contains(out.String(), "✓ :5173  stopped") {
		t.Errorf("out:\n%s", out.String())
	}
}

func TestStopDryRunStopsNothing(t *testing.T) {
	var stopped []int
	o := fakeStop(&stopped)
	o.dryRun = true
	var out bytes.Buffer
	if err := stopCommand(t.Context(), &out, []string{"200"}, o); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 0 || !strings.Contains(out.String(), "would stop *:8000") {
		t.Errorf("stopped %v, out:\n%s", stopped, out.String())
	}
}

func TestStopAsksAndNeedsYesWithoutTerminal(t *testing.T) {
	var stopped []int
	o := fakeStop(&stopped)
	if err := stopCommand(t.Context(), &bytes.Buffer{}, []string{":8000"}, o); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("non-terminal without --yes: %v", err)
	}
	o.interactive, o.in = true, strings.NewReader("n\n")
	var out bytes.Buffer
	if err := stopCommand(t.Context(), &out, []string{":8000"}, o); err != nil || len(stopped) != 0 || !strings.Contains(out.String(), "kept running") {
		t.Fatalf("answering n: err=%v stopped=%v", err, stopped)
	}
	o.in = strings.NewReader("y\n")
	if err := stopCommand(t.Context(), &bytes.Buffer{}, []string{":8000"}, o); err != nil || len(stopped) != 1 {
		t.Fatalf("answering y: err=%v stopped=%v", err, stopped)
	}
}

func TestStopUnknownTarget(t *testing.T) {
	var stopped []int
	if err := stopCommand(t.Context(), &bytes.Buffer{}, []string{":9999", "12345"}, fakeStop(&stopped)); err == nil ||
		!strings.Contains(err.Error(), "nothing listening for :9999, 12345") {
		t.Fatalf("err = %v", err)
	}
}
