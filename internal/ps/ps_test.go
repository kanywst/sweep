package ps

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	out := []byte(`  100   401  20480 Sat Oct  3 10:00:00 2026     S+   node /app/node_modules/.bin/vite dev
  555     1      0 Thu Oct  1 09:00:00 2026     Z    (node)
`)
	m, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	p := m[100]
	if p.PPID != 401 || p.RSS != 20480*1024 || p.Stat != "S+" || p.Command != "node /app/node_modules/.bin/vite dev" {
		t.Errorf("got %+v", p)
	}
	if !p.Started.Equal(time.Date(2026, 10, 3, 10, 0, 0, 0, time.Local)) {
		t.Errorf("started = %v", p.Started)
	}
	if !m[555].Zombie() || p.Zombie() {
		t.Error("zombie detection")
	}
}

func TestSame(t *testing.T) {
	a := Proc{PID: 1, Started: time.Unix(100, 0), Command: "vite"}
	if !a.Same(a) {
		t.Error("identical should be same")
	}
	if a.Same(Proc{PID: 1, Started: time.Unix(200, 0), Command: "vite"}) {
		t.Error("recycled pid with a later start must differ")
	}
	if !a.Same(Proc{PID: 1, Started: time.Unix(100, 0), Command: "next-server (v15)"}) {
		t.Error("a retitled process is still the same process")
	}
}

func TestTool(t *testing.T) {
	p, err := Tool("ps")
	if err != nil || p[0] != '/' {
		t.Fatalf("Tool(ps) = %q, %v", p, err)
	}
	if _, err := Tool("nope"); err == nil {
		t.Error("unknown tool should fail")
	}
}
