package format

import (
	"testing"
	"time"
)

func TestAge(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{3 * 24 * time.Hour, "3d"},
		{23 * 24 * time.Hour, "3w"},
	} {
		if got := Age(tc.d); got != tc.want {
			t.Errorf("Age(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestParseAge(t *testing.T) {
	day := 24 * time.Hour
	for in, want := range map[string]time.Duration{
		"7d": 7 * day, "2w": 14 * day, "1.5d": 36 * time.Hour, "12h": 12 * time.Hour, "90m": 90 * time.Minute,
	} {
		if got, err := ParseAge(in); err != nil || got != want {
			t.Errorf("ParseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "d", "-1d", "0d", "7", "7y", "-5m", "0s"} {
		if _, err := ParseAge(in); err == nil {
			t.Errorf("ParseAge(%q): no error", in)
		}
	}
}

func TestBytes(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{
		{512, "512B"},
		{20 << 10, "20K"},
		{300 << 20, "300M"},
		{3 << 30, "3.0G"},
	} {
		if got := Bytes(tc.n); got != tc.want {
			t.Errorf("Bytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestPorts(t *testing.T) {
	for _, tc := range []struct {
		addrs []string
		want  string
	}{
		{[]string{"[::1]:5173", "127.0.0.1:5173"}, ":5173"},
		{[]string{"*:4321"}, "*:4321"},
		{[]string{"127.0.0.1:8080", "*:9090"}, ":8080 *:9090"},
	} {
		if got := Ports(tc.addrs); got != tc.want {
			t.Errorf("Ports(%v) = %q, want %q", tc.addrs, got, tc.want)
		}
	}
}

func TestCommand(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"/opt/homebrew/bin/node /Users/kt/renai/node_modules/.bin/../vite/bin/vite.js dev", "vite dev"},
		{"/opt/homebrew/bin/node /Users/kt/.npm/_npx/aab/node_modules/.bin/serve -s build -l 4321", "serve -s build -l 4321"},
		{"/Library/Frameworks/Python.app/Contents/MacOS/Python -m http.server 8000", "Python -m http.server 8000"},
		{"/usr/local/bin/python3.12 /Users/kt/app/server.py --port 9000", "python3.12 server.py --port 9000"},
		{"/usr/sbin/mysqld --port=3306", "mysqld --port=3306"},
	} {
		if got := Command(tc.in); got != tc.want {
			t.Errorf("Command(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
