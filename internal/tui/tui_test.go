package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kanywst/sweep/internal/proc"
	"github.com/kanywst/sweep/internal/scan"
)

func press(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		m, _ = m.Update(msg)
	}
	return m
}

func fixture() []scan.Group {
	now := time.Now()
	return []scan.Group{
		{Dir: "/tmp/site", Listeners: []scan.Listener{
			{PID: 200, Addrs: []string{"*:8000"}, Started: now.Add(-20 * 24 * time.Hour), RSS: 8 << 20, Command: "python3 -m http.server 8000", Cwd: "/tmp/site"},
		}},
		{Dir: "/work/app", Listeners: []scan.Listener{
			{PID: 100, Addrs: []string{"[::1]:5173"}, Started: now.Add(-2 * time.Hour), RSS: 40 << 20, Command: "node /work/app/node_modules/.bin/vite dev", Cwd: "/work/app", Launchers: []scan.Process{{PID: 401, Command: "sh -c vite dev"}, {PID: 400, Command: "node /opt/homebrew/lib/node_modules/pnpm/bin/pnpm.mjs dev"}}},
			{PID: 101, Addrs: []string{"[::1]:4173"}, Started: now.Add(-time.Hour), RSS: 30 << 20, Command: "node /work/app/node_modules/.bin/vite preview", Cwd: "/work/app"},
		}},
	}
}

func loaded(t *testing.T) tea.Model {
	t.Helper()
	var m tea.Model = newModel(System{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	m, _ = m.Update(scanDoneMsg{groups: fixture()})
	return m
}

func screen(m tea.Model) string { return ansi.Strip(m.View().Content) }

func TestListRendersGroupsAndRows(t *testing.T) {
	out := screen(loaded(t))
	for _, want := range []string{"sweep", "Ports", "3 listening · 78M", "/tmp/site", "/work/app", "*:8000", "vite dev", "2w"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen missing %q:\n%s", want, out)
		}
	}
}

func TestEmptyState(t *testing.T) {
	var m tea.Model = newModel(System{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m, _ = m.Update(scanDoneMsg{})
	if out := screen(m); !strings.Contains(out, "All clear") || !strings.Contains(out, "nothing you started is listening") {
		t.Errorf("no empty state:\n%s", out)
	}
}

func TestSelectAndConfirmIncludesLaunchers(t *testing.T) {
	m := loaded(t)
	// cursor starts on the first row; move to the vite row and select it.
	m = press(m, "j", "j", "space")
	mm := m.(model)
	if !mm.selected[100] || len(mm.selected) != 1 {
		t.Fatalf("selected = %v, want only 100", mm.selected)
	}
	m = press(m, "x")
	mm = m.(model)
	if mm.mode != modeConfirm {
		t.Fatalf("mode = %v, want confirm", mm.mode)
	}
	if got := mm.stopSet(); !slices.Equal(got, []int{100, 401, 400}) {
		t.Errorf("stopSet = %v, want [100 401 400]", got)
	}
	out := screen(m)
	for _, want := range []string{"Stop 1 process?", "↳ pnpm dev", "2 launchers that started them will stop too", "y  Stop 1", "esc  Keep running"} {
		if !strings.Contains(out, want) {
			t.Errorf("modal missing %q:\n%s", want, out)
		}
	}
	m = press(m, "n")
	if m.(model).mode != modeList {
		t.Error("n should cancel")
	}
}

func TestToggleDirSelectsWholeGroup(t *testing.T) {
	m := press(loaded(t), "j", "j", "a")
	sel := m.(model).selected
	if !sel[100] || !sel[101] || sel[200] {
		t.Fatalf("selected = %v, want 100 and 101", sel)
	}
	m = press(m, "a")
	if len(m.(model).selected) != 0 {
		t.Error("second a should clear the group")
	}
}

func TestStopWithoutSelectionTargetsCursorRow(t *testing.T) {
	m := press(loaded(t), "x") // cursor on the python row
	mm := m.(model)
	if mm.mode != modeConfirm || !slices.Equal(mm.stopSet(), []int{200}) {
		t.Fatalf("mode=%v stopSet=%v", mm.mode, mm.stopSet())
	}
}

func TestEnterDoesNotConfirm(t *testing.T) {
	m := press(loaded(t), "x")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.(model).mode != modeConfirm {
		t.Fatal("enter must not confirm a stop")
	}
}

func TestStopMarksRowsAndTallies(t *testing.T) {
	m := press(loaded(t), "j", "j", "a", "x", "y")
	mm := m.(model)
	if mm.batch != 2 || mm.states[100] != stateStopping || mm.states[101] != stateStopping {
		t.Fatalf("batch=%d states=%v", mm.batch, mm.states)
	}
	if !strings.Contains(m.View().WindowTitle, "stopping 0/2") || m.View().ProgressBar == nil {
		t.Errorf("title=%q progress=%v", m.View().WindowTitle, m.View().ProgressBar)
	}
	m, _ = m.Update(stoppedMsg{pid: 100, results: []proc.Result{{PID: 100}, {PID: 401}, {PID: 400}}})
	m, _ = m.Update(stoppedMsg{pid: 101, results: []proc.Result{{PID: 101, Killed: true}}})
	mm = m.(model)
	if mm.states[100] != stateStopped || mm.states[101] != stateForced {
		t.Fatalf("states = %v", mm.states)
	}
	out := screen(m)
	for _, want := range []string{"swept 2 processes", "freed :5173 :4173", "70M", "1 needed SIGKILL", "stopped (SIGKILL)", "swept 2 · 70M"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen missing %q:\n%s", want, out)
		}
	}
	if m.View().ProgressBar != nil {
		t.Error("progress bar should clear when the batch is done")
	}
	if got := m.View().WindowTitle; got != "sweep · 1 listening" {
		t.Errorf("title = %q", got)
	}
}

func TestFailedStopStaysAfterRefresh(t *testing.T) {
	m := press(loaded(t), "x", "y")
	m, _ = m.Update(stoppedMsg{pid: 200, results: []proc.Result{{PID: 200, Err: errors.New("operation not permitted")}}})
	if out := screen(m); !strings.Contains(out, "failed: operation not permitted") || !strings.Contains(out, "✗ 1 failed") {
		t.Fatalf("no failure shown:\n%s", out)
	}
	m, _ = m.Update(scanDoneMsg{groups: fixture()})
	if m.(model).states[200] != stateFailed {
		t.Error("a failure should survive a refresh")
	}
}

func TestAllClearAfterSweepingEverything(t *testing.T) {
	m := loaded(t)
	m, _ = m.Update(scanDoneMsg{groups: nil})
	mm := m.(model)
	mm.swept = tally{procs: 3, rss: 78 << 20, ports: []string{"*:8000", ":5173", ":4173"}}
	out := screen(mm)
	for _, want := range []string{"All clear", "swept 3 processes · 78M this session", "freed *:8000 :5173 :4173"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if mm.View().WindowTitle != "sweep · all clear" {
		t.Errorf("title = %q", mm.View().WindowTitle)
	}
}
