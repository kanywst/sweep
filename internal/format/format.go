// Package format renders sizes, ages and addresses for people.
package format

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Age renders d as its largest unit: 45s, 12m, 5h, 3d, 6w.
func Age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/24/7))
	}
}

// ParseAge reads an age the way Age writes it (3d, 2w), or any Go duration
// (90m, 1h30m).
func ParseAge(s string) (time.Duration, error) {
	day := 24 * time.Hour
	for suffix, unit := range map[string]time.Duration{"d": day, "w": 7 * day} {
		if n, ok := strings.CutSuffix(s, suffix); ok {
			v, err := strconv.ParseFloat(n, 64)
			if err != nil || v <= 0 {
				return 0, fmt.Errorf("invalid age %q", s)
			}
			return time.Duration(v * float64(unit)), nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid age %q (use 7d, 2w or 12h)", s)
	}
	return d, nil
}

// Bytes renders n with a binary unit: 512B, 12M, 3.4G.
func Bytes(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return fmt.Sprintf("%dB", n)
	case n < k*k:
		return fmt.Sprintf("%dK", n/k)
	case n < k*k*k:
		return fmt.Sprintf("%dM", n/(k*k))
	default:
		return fmt.Sprintf("%.1fG", float64(n)/(k*k*k))
	}
}

// Ports collapses listen addresses to their ports, marking ones bound to
// every interface with "*" because those are reachable from the network.
func Ports(addrs []string) string {
	var out []string
	for _, a := range addrs {
		host, port, ok := strings.CutLast(a, ":")
		if !ok {
			continue
		}
		p := ":" + port
		if host == "*" || host == "0.0.0.0" || host == "[::]" {
			p = "*" + p
		}
		if !slices.Contains(out, p) && !slices.Contains(out, "*"+p) {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

var interpreters = []string{"node", "python", "python3", "bun", "deno", "ruby", "php"}

// Command shortens a process command line to what you would have typed:
// "/opt/homebrew/bin/node /x/node_modules/.bin/../vite/bin/vite.js dev"
// becomes "vite dev".
func Command(cmdline string) string {
	f := strings.Fields(cmdline)
	if len(f) == 0 {
		return cmdline
	}
	f[0] = base(f[0])
	if slices.Contains(interpreters, strings.TrimRight(f[0], "0123456789.")) && len(f) > 1 && strings.Contains(f[1], "/") {
		script := f[1]
		if _, after, ok := strings.Cut(script, "node_modules/"); ok {
			// .../node_modules/.bin/../vite/bin/vite.js -> vite
			rest := strings.TrimPrefix(after, ".bin/")
			rest = strings.TrimPrefix(rest, "../")
			if j := strings.IndexByte(rest, '/'); j > 0 && !strings.HasPrefix(rest, ".pnpm") {
				rest = rest[:j]
			} else {
				rest = base(rest)
			}
			f = append([]string{rest}, f[2:]...)
		} else {
			f = append([]string{f[0], base(script)}, f[2:]...)
		}
	}
	return strings.Join(f, " ")
}

func base(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return p
}
