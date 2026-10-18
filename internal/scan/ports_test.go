package scan

import (
	"context"
	"fmt"
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
`

// 400 is pnpm, 401 is "sh -c" under it, 100 is the vite node under that.
const psOut = `  100   401  20480 Sat Oct  3 10:00:00 2026     node /Users/kt/app/node_modules/.bin/vite dev
  200     1   8192 Thu Oct  1 09:00:00 2026     python3 -m http.server 8000
  300     1   4096 Mon Sep 28 08:00:00 2026     /System/Library/CoreServices/ControlCenter.app/Contents/MacOS/ControlCenter
  401   400   1024 Sat Oct  3 10:00:00 2026     sh -c vite dev
  400     1   2048 Sat Oct  3 09:59:59 2026     node /opt/homebrew/lib/node_modules/pnpm/bin/pnpm.mjs dev
`

func fakeRunner(t *testing.T) Runner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		joined := name + " " + strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "lsof -nP -iTCP"):
			return []byte(lsofListen), nil
		case strings.HasPrefix(joined, "lsof -a -d cwd"):
			return []byte(lsofCwd), nil
		case name == "ps":
			return []byte(psOut), nil
		}
		t.Fatalf("unexpected command %q", joined)
		return nil, fmt.Errorf("unexpected")
	}
}

func TestPortsGroupsByDirOldestFirst(t *testing.T) {
	gs, err := Ports(context.Background(), fakeRunner(t), PortOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 2 {
		t.Fatalf("got %d groups, want 2 (cwd \"/\" hidden): %+v", len(gs), gs)
	}
	if gs[0].Dir != "/tmp/site" || gs[1].Dir != "/Users/kt/app" {
		t.Fatalf("order = %q, %q; want /tmp/site first (older)", gs[0].Dir, gs[1].Dir)
	}
	vite := gs[1].Listeners[0]
	if len(vite.Addrs) != 2 {
		t.Errorf("addrs = %v, want both sockets", vite.Addrs)
	}
	if vite.RSS != 20480*1024 {
		t.Errorf("rss = %d", vite.RSS)
	}
	want := time.Date(2026, 10, 3, 10, 0, 0, 0, time.Local)
	if !vite.Started.Equal(want) {
		t.Errorf("started = %v, want %v", vite.Started, want)
	}
	if fmt.Sprint(vite.Wrappers) != "[401 400]" {
		t.Errorf("wrappers = %v, want [401 400]", vite.Wrappers)
	}
	if len(gs[0].Listeners[0].Wrappers) != 0 {
		t.Errorf("python under launchd should have no wrappers, got %v", gs[0].Listeners[0].Wrappers)
	}
}

func TestPortsAllIncludesRootCwd(t *testing.T) {
	gs, err := Ports(context.Background(), fakeRunner(t), PortOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 3 || gs[0].Dir != "/" {
		t.Fatalf("got %+v, want 3 groups with / first", gs)
	}
}

func TestPortsNoListeners(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) { return nil, fmt.Errorf("exit status 1") }
	gs, err := Ports(context.Background(), run, PortOptions{})
	if err != nil || len(gs) != 0 {
		t.Fatalf("got %v, %v; want no groups and no error", gs, err)
	}
}
