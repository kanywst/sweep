package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/kanywst/sweep/internal/caches"
	"github.com/kanywst/sweep/internal/clones"
	"github.com/kanywst/sweep/internal/scan"
)

// Caches are package-manager and build caches.
type Caches interface {
	Caches(ctx context.Context) []caches.Cache
	Clean(ctx context.Context, c caches.Cache) error
}

// Clones are git working trees whose repository is gone.
type Clones interface {
	Clones(ctx context.Context) ([]clones.Dead, error)
	Trash(path string) error
}

// Machine is everything sweep can look at.
type Machine interface {
	Backend
	Caches
	Clones
}

// Caches finds and measures this machine's caches.
func (System) Caches(ctx context.Context) []caches.Cache {
	return caches.Measure(caches.Find(ctx))
}

// Clean runs a cache's clean command.
func (System) Clean(ctx context.Context, c caches.Cache) error { return caches.Clean(ctx, c) }

// Clones finds dead clones under SWEEP_CLONE_ROOTS (colon-separated) or $HOME,
// four levels deep.
func (System) Clones(ctx context.Context) ([]clones.Dead, error) {
	roots := filepath.SplitList(os.Getenv("SWEEP_CLONE_ROOTS"))
	if len(roots) == 0 {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		roots = []string{h}
	}
	ds, err := clones.Find(ctx, roots, 4)
	return clones.Measure(ds), err
}

// Trash moves path to the trash.
func (System) Trash(path string) error { return clones.Trash(path) }

const (
	tabPorts = iota
	tabCaches
	tabClones
)

var tabNames = []string{"Ports", "Caches", "Clones"}

// app switches between the tabs.
type app struct {
	active int
	ports  model
	caches listTab
	clones listTab
}

func newApp(m Machine) app {
	a := app{ports: newModel(m)}
	a.caches = newListTab(listSpec{
		noun: "cache", nouns: "caches", verb: "Clean", done: "cleaned",
		empty: "no caches from installed tools", how: "runs each tool's own clean command",
		load: func() ([]row, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			var rs []row
			for _, c := range m.Caches(ctx) {
				rs = append(rs, row{key: c.Path, title: c.Name, detail: scan.ShortDir(c.Path), extra: c.CleanCommand(), size: c.Size, val: c})
			}
			return rs, nil
		},
		act: func(r row) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			return m.Clean(ctx, r.val.(caches.Cache))
		},
	})
	a.clones = newListTab(listSpec{
		noun: "dead clone", nouns: "dead clones", verb: "Move to Trash", done: "trashed",
		empty: "no working trees with a missing .git", how: "moves them to the trash, so they can be put back",
		load: func() ([]row, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			ds, err := m.Clones(ctx)
			var rs []row
			for _, d := range ds {
				rs = append(rs, row{key: d.Path, title: filepath.Base(d.Path), detail: scan.ShortDir(filepath.Dir(d.Path)) + "/",
					extra: "gitdir gone: " + scan.ShortDir(d.GitDir), size: d.Size, val: d})
			}
			return rs, err
		},
		act: func(r row) error { return m.Trash(r.key) },
	})
	return a
}

// RunMachine starts the interactive view with every tab.
func RunMachine(ctx context.Context, m Machine) error {
	_, err := tea.NewProgram(newApp(m), tea.WithContext(ctx)).Run()
	return err
}

func (a app) Init() tea.Cmd { return a.ports.Init() }

// busyTyping is true while a key belongs to a text field or a dialog.
func (a app) busyTyping() bool {
	switch a.active {
	case tabPorts:
		return a.ports.mode == modeFilter || a.ports.mode == modeConfirm
	case tabCaches:
		return a.caches.confirm
	default:
		return a.clones.confirm
	}
}

func (a app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg, tea.BackgroundColorMsg:
		var cmds []tea.Cmd
		pm, cmd := a.ports.Update(msg)
		a.ports = pm.(model)
		cmds = append(cmds, cmd)
		a.caches = a.caches.resize(msg, a.ports.st, a.ports.keys)
		a.clones = a.clones.resize(msg, a.ports.st, a.ports.keys)
		return a, tea.Batch(cmds...)
	case spinner.TickMsg:
		var c1, c2, c3 tea.Cmd
		var pm tea.Model
		pm, c1 = a.ports.Update(msg)
		a.ports = pm.(model)
		a.caches, c2 = a.caches.update(msg)
		a.clones, c3 = a.clones.update(msg)
		return a, tea.Batch(c1, c2, c3)
	case listLoadedMsg, listActedMsg:
		var cmd tea.Cmd
		if tabOf(msg) == tabCaches {
			a.caches, cmd = a.caches.update(msg)
		} else {
			a.clones, cmd = a.clones.update(msg)
		}
		return a, cmd
	case tea.KeyPressMsg:
		if !a.busyTyping() {
			switch msg.String() {
			case "tab", "shift+tab", "1", "2", "3":
				return a.switchTab(msg.String())
			}
		}
	}

	var cmd tea.Cmd
	switch a.active {
	case tabPorts:
		var pm tea.Model
		pm, cmd = a.ports.Update(msg)
		a.ports = pm.(model)
	case tabCaches:
		a.caches, cmd = a.caches.update(msg)
		if q, ok := msg.(tea.KeyPressMsg); ok && a.caches.quitKey(q) {
			return a, tea.Quit
		}
	case tabClones:
		a.clones, cmd = a.clones.update(msg)
		if q, ok := msg.(tea.KeyPressMsg); ok && a.clones.quitKey(q) {
			return a, tea.Quit
		}
	}
	return a, cmd
}

func (a app) switchTab(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "tab":
		a.active = (a.active + 1) % len(tabNames)
	case "shift+tab":
		a.active = (a.active + len(tabNames) - 1) % len(tabNames)
	default:
		a.active = int(k[0] - '1')
	}
	switch a.active {
	case tabCaches:
		return a, a.caches.ensureLoaded(tabCaches)
	case tabClones:
		return a, a.clones.ensureLoaded(tabClones)
	}
	return a, nil
}

func (a app) tabBar() string {
	st := a.ports.st
	var parts []string
	for i, n := range tabNames {
		if i == a.active {
			parts = append(parts, st.tab.Render(n))
		} else {
			parts = append(parts, st.faint.Render(n))
		}
	}
	return strings.Join(parts, st.faint.Render("  ·  "))
}

func (a app) View() tea.View {
	a.ports.tabs = a.tabBar()
	switch a.active {
	case tabCaches:
		return a.caches.view(a.tabBar(), a.ports.spinner.View())
	case tabClones:
		return a.clones.view(a.tabBar(), a.ports.spinner.View())
	}
	return a.ports.View()
}

// Keep the compiler honest about what System provides.
var _ Machine = System{}
