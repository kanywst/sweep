package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Up, Down, PageUp, PageDown, Top, Bottom key.Binding
	Toggle, ToggleDir, Range, Clear         key.Binding
	Filter, Open, Copy, Tabs                key.Binding
	Stop, Refresh, Help, Quit, ForceQuit    key.Binding
	Confirm, Cancel                         key.Binding
}

func defaultKeys() keyMap {
	b := func(help, desc string, keys ...string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	}
	return keyMap{
		Up:        b("↑/k", "up", "up", "k"),
		Down:      b("↓/j", "down", "down", "j"),
		PageUp:    b("pgup", "page up", "pgup", "ctrl+u"),
		PageDown:  b("pgdn", "page down", "pgdown", "ctrl+d"),
		Top:       b("g", "top", "g", "home"),
		Bottom:    b("G", "bottom", "G", "end"),
		Toggle:    b("space", "select", "space"),
		ToggleDir: b("a", "select dir", "a"),
		Range:     b("v", "select range", "v"),
		Clear:     b("esc", "clear", "esc"),
		Filter:    b("/", "filter", "/"),
		Open:      b("o", "open dir", "o"),
		Copy:      b("c", "copy start cmd", "c"),
		Tabs:      b("tab", "next tab", "tab"),
		Stop:      b("x", "stop", "x", "d", "delete"),
		Refresh:   b("r", "refresh", "r"),
		Help:      b("?", "more", "?"),
		Quit:      b("q", "quit", "q"),
		ForceQuit: b("ctrl+c", "quit", "ctrl+c"),
		// Not enter: a habitual enter should never stop anything.
		Confirm: b("y", "stop", "y"),
		Cancel:  b("n", "cancel", "n", "esc", "q"),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Toggle, k.ToggleDir, k.Stop, k.Filter, k.Tabs, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Top, k.Bottom},
		{k.Toggle, k.ToggleDir, k.Range, k.Clear},
		{k.Stop, k.Refresh, k.Filter, k.Open, k.Copy},
		{k.Tabs, k.Help, k.Quit},
	}
}

// listKeys is the help for the Caches and Clones tabs.
type listKeys struct {
	k    keyMap
	verb string
}

func (l listKeys) bindings() (toggle, all, act key.Binding) {
	toggle = key.NewBinding(key.WithKeys(l.k.Toggle.Keys()...), key.WithHelp("space", "select"))
	all = key.NewBinding(key.WithKeys(l.k.ToggleDir.Keys()...), key.WithHelp("a", "select all"))
	act = key.NewBinding(key.WithKeys(l.k.Stop.Keys()...), key.WithHelp("x", l.verb))
	return
}

func (l listKeys) ShortHelp() []key.Binding {
	toggle, all, act := l.bindings()
	return []key.Binding{l.k.Up, l.k.Down, toggle, all, act, l.k.Refresh, l.k.Tabs, l.k.Quit}
}

func (l listKeys) FullHelp() [][]key.Binding { return [][]key.Binding{l.ShortHelp()} }
