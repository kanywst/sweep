package scan

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const lsofListen = `p100
cnode
f20
n[::1]:5173
f21
n127.0.0.1:5173
p200
cPython
f3
n*:8000
p300
cControlCenter
f10
n*:7000
p700
cnode
f5
n[::1]:3001
p701
cnode
f5
n[::1]:3002
`

const lsofCwd = `p100
fcwd
n/Users/kt/app
p200
fcwd
n/tmp/site
p300
fcwd
n/
p700
fcwd
n/Users/kt/mono
p701
fcwd
n/Users/kt/mono
`

// The whole process table, as `ps -A` returns it.
//
//	400 pnpm dev -> 401 sh -c vite dev -> 100 vite (one chain, safe to stop)
//	600 pnpm -r --parallel dev -> 610 sh -c, 611 sh -c -> 700, 701 (shared)
//	800 tmux server -> 801 zsh -c npm exec vite (tmux has two children)
const psTable = `    1     0   1024 Mon Sep 28 08:00:00 2026     Ss   /sbin/launchd
  100   401  20480 Sat Oct  3 10:00:00 2026     S    node /Users/kt/app/node_modules/.bin/vite dev
  200     1   8192 Thu Oct  1 09:00:00 2026     S    python3 -m http.server 8000
  300     1   4096 Mon Sep 28 08:00:00 2026     S    /System/Library/CoreServices/ControlCenter.app/Contents/MacOS/ControlCenter
  400   800   2048 Sat Oct  3 09:59:59 2026     S    node /opt/homebrew/lib/node_modules/pnpm/bin/pnpm.mjs dev
  401   400   1024 Sat Oct  3 10:00:00 2026     S    sh -c vite dev
  600     1   2048 Sat Oct  3 09:00:00 2026     S    pnpm -r --parallel dev
  610   600   1024 Sat Oct  3 09:00:01 2026     S    sh -c vite
  611   600   1024 Sat Oct  3 09:00:01 2026     S    sh -c tsx watch
  700   610  10240 Sat Oct  3 09:00:02 2026     S    node vite
  701   611  10240 Sat Oct  3 09:00:02 2026     S    node tsx watch
  800     1   2048 Sat Oct  3 09:00:00 2026     Ss   tmux new -s web npm exec vite
  801   800   1024 Sat Oct  3 09:00:00 2026     S    zsh
`

func fakeRunner(t *testing.T) Runner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		joined := name + " " + strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "lsof -nP -iTCP"):
			return []byte(lsofListen), nil
		case strings.HasPrefix(joined, "lsof -a -d cwd"):
			return []byte(lsofCwd), nil
		case strings.HasPrefix(joined, "ps -A"):
			return []byte(psTable), nil
		}
		t.Fatalf("unexpected command %q", joined)
		return nil, fmt.Errorf("unexpected")
	}
}

func byPID(gs []Group) map[int]Listener {
	m := map[int]Listener{}
	for _, g := range gs {
		for _, l := range g.Listeners {
			m[l.PID] = l
		}
	}
	return m
}

func TestPortsGroupsByDirOldestFirst(t *testing.T) {
	gs, err := Ports(context.Background(), fakeRunner(t), PortOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, g := range gs {
		dirs = append(dirs, g.Dir)
	}
	if fmt.Sprint(dirs) != "[/tmp/site /Users/kt/mono /Users/kt/app]" {
		t.Fatalf("dirs = %v (cwd \"/\" hidden, oldest first)", dirs)
	}
	vite := byPID(gs)[100]
	if len(vite.Addrs) != 2 || vite.RSS != 20480*1024 {
		t.Errorf("vite = %+v", vite)
	}
	if !vite.Started.Equal(time.Date(2026, 10, 3, 10, 0, 0, 0, time.Local)) {
		t.Errorf("started = %v", vite.Started)
	}
}

func TestLaunchersStopAtTheirOwnChain(t *testing.T) {
	gs, err := Ports(context.Background(), fakeRunner(t), PortOptions{})
	if err != nil {
		t.Fatal(err)
	}
	m := byPID(gs)
	// sh -c and pnpm dev each have one child; tmux above pnpm has two.
	if got := fmt.Sprint(m[100].LauncherPIDs()); got != "[401 400]" {
		t.Errorf("vite launchers = %s, want [401 400] (never the tmux server)", got)
	}
	// sh -c is private to each server, but the pnpm -r parent is shared.
	if got := fmt.Sprint(m[700].LauncherPIDs()); got != "[610]" {
		t.Errorf("mono vite launchers = %s, want [610], not the shared pnpm", got)
	}
	if len(m[200].Launchers) != 0 {
		t.Errorf("python under launchd: %v", m[200].Launchers)
	}
}

func TestIsLauncher(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want bool
	}{
		{"npm run dev", true},
		{"/usr/local/bin/pnpm dev", true},
		{"node /opt/homebrew/lib/node_modules/pnpm/bin/pnpm.mjs dev", true},
		{"node /usr/local/lib/node_modules/npm/bin/npm-cli.js run dev", true},
		{"sh -c vite dev", true},
		{"/bin/sh -c next dev", true},
		{"bun run dev", true},
		{"bun server.ts", false},
		{"tmux new -s web npm exec vite", false},
		{"nvim /Users/kt/proj/scripts/npm", false},
		{"code --wait /Users/kt/bin/bun", false},
		{"zsh -c npm exec vite", false},
		{"-zsh", false},
		{"node server.js", false},
		{"node /Users/kt/site/node_modules/.bin/next dev", true},
		{"node /Users/kt/site/node_modules/.bin/next start", false},
	} {
		if got := IsLauncher(tc.cmd); got != tc.want {
			t.Errorf("IsLauncher(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func TestPortsAllIncludesRootCwd(t *testing.T) {
	gs, err := Ports(context.Background(), fakeRunner(t), PortOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 4 || gs[0].Dir != "/" {
		t.Fatalf("got %+v, want 4 groups with / first", gs)
	}
}

func TestPortsNoListeners(t *testing.T) {
	exit1 := exec.Command("sh", "-c", "exit 1").Run()
	run := func(context.Context, string, ...string) ([]byte, error) { return nil, exit1 }
	gs, err := Ports(context.Background(), run, PortOptions{})
	if err != nil || len(gs) != 0 {
		t.Fatalf("got %v, %v; want no groups and no error", gs, err)
	}
}

func TestPortsLsofFailureIsAnError(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("lsof not found")
	}
	if _, err := Ports(context.Background(), run, PortOptions{}); err == nil {
		t.Fatal("a missing lsof must not read as all clear")
	}
}
