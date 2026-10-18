package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Up, Down, PageUp, PageDown, Top, Bottom key.Binding
	Toggle, ToggleDir, Clear                key.Binding
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
		Clear:     b("esc", "clear", "esc"),
		Stop:      b("x", "stop", "x", "d", "delete"),
		Refresh:   b("r", "refresh", "r"),
		Help:      b("?", "more", "?"),
		Quit:      b("q", "quit", "q"),
		ForceQuit: b("ctrl+c", "quit", "ctrl+c"),
		Confirm:   b("y", "stop", "y", "enter"),
		Cancel:    b("n", "cancel", "n", "esc", "q"),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Toggle, k.ToggleDir, k.Stop, k.Refresh, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Top, k.Bottom},
		{k.Toggle, k.ToggleDir, k.Clear},
		{k.Stop, k.Refresh, k.Help, k.Quit},
	}
}
