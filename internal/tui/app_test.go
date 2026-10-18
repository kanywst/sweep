package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kanywst/sweep/internal/demo"
)

// drive runs cmds the way the program would, but only for the messages that
// carry results, so tests do not spin on spinner ticks.
func drive(m tea.Model, cmd tea.Cmd) tea.Model {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = drive(m, c)
		}
	case listLoadedMsg, listActedMsg, scanDoneMsg, stoppedMsg:
		m, cmd = m.Update(msg)
		return drive(m, cmd)
	}
	return m
}

func appScreen(m tea.Model) string { return ansi.Strip(m.View().Content) }

func TestTabsCachesCleanAndAllClear(t *testing.T) {
	var m tea.Model = newApp(demo.New())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	m, cmd := m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = drive(m, cmd)
	out := appScreen(m)
	for _, want := range []string{"Caches", "5 caches", "Go build", "go clean -cache", "6.0G"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if out := appScreen(m); !strings.Contains(out, "Clean 5 caches?") || !strings.Contains(out, "own clean command") {
		t.Fatalf("confirm:\n%s", out)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	// The acts run through tea.Sequence; deliver their results directly.
	for _, r := range m.(app).caches.rows {
		m, _ = m.Update(listActedMsg{tab: tabCaches, key: r.key})
	}
	out = appScreen(m)
	if !strings.Contains(out, "cleaned 5") || !strings.Contains(out, "All clear") {
		t.Fatalf("after cleaning:\n%s", out)
	}
}

func TestTabKeysIgnoredWhileFiltering(t *testing.T) {
	var m tea.Model = newApp(demo.New())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	m, _ = m.Update(scanDoneMsg{groups: fixture()})
	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m, _ = m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if m.(app).active != tabPorts {
		t.Fatal("typing 2 into the filter must not switch tabs")
	}
}

func TestClonesTab(t *testing.T) {
	var m tea.Model = newApp(demo.New())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	m, cmd := m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	m = drive(m, cmd)
	out := appScreen(m)
	if !strings.Contains(out, "2 dead clones") || !strings.Contains(out, "gitdir gone") {
		t.Fatalf("clones tab:\n%s", out)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if !strings.Contains(appScreen(m), "Move to Trash 1 dead clone?") {
		t.Fatalf("confirm:\n%s", appScreen(m))
	}
}
