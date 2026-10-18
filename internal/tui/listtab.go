package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kanywst/sweep/internal/format"
)

// row is one thing a list tab can act on.
type row struct {
	key, title, detail, extra string
	size                      int64
	val                       any
}

// listSpec is what differs between the Caches and Clones tabs.
type listSpec struct {
	noun, nouns string // "cache", "caches"
	verb, done  string // "Clean", "cleaned"
	empty, how  string
	load        func() ([]row, error)
	act         func(row) error
}

type (
	listLoadedMsg struct {
		tab  int
		rows []row
		err  error
	}
	listActedMsg struct {
		tab int
		key string
		err error
	}
)

func tabOf(msg tea.Msg) int {
	switch m := msg.(type) {
	case listLoadedMsg:
		return m.tab
	case listActedMsg:
		return m.tab
	}
	return -1
}

// listTab is a selectable list of sized things, acted on after a confirm.
type listTab struct {
	spec     listSpec
	tab      int
	loaded   bool
	loading  bool
	err      error
	rows     []row
	selected map[string]bool
	states   map[string]state
	errs     map[string]error
	cursor   int
	offset   int
	confirm  bool
	batch    int
	done     int
	freed    int64 // this session
	freedN   int
	width    int
	height   int
	st       styles
	keys     keyMap
	help     help.Model
	spin     spinner.Model
}

func newListTab(s listSpec) listTab {
	return listTab{spec: s, selected: map[string]bool{}, states: map[string]state{}, errs: map[string]error{},
		help: help.New(), spin: spinner.New(spinner.WithSpinner(spinner.MiniDot))}
}

func (t listTab) resize(msg tea.Msg, st styles, keys keyMap) listTab {
	t.st, t.keys = st, keys
	t.help.Styles = help.DefaultStyles(true)
	t.spin.Style = lipgloss.NewStyle().Foreground(st.p.aqua)
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		t.width, t.height = ws.Width, ws.Height
		t.help.SetWidth(ws.Width - 4)
	}
	return t
}

func (t *listTab) ensureLoaded(tab int) tea.Cmd {
	t.tab = tab
	if t.loaded || t.loading {
		return nil
	}
	return t.reload()
}

func (t *listTab) reload() tea.Cmd {
	t.loading = true
	load, tab := t.spec.load, t.tab
	return tea.Batch(t.spin.Tick, func() tea.Msg {
		rs, err := load()
		return listLoadedMsg{tab: tab, rows: rs, err: err}
	})
}

func (t listTab) busy() bool { return t.loading || t.done < t.batch }

func (t listTab) quitKey(k tea.KeyPressMsg) bool {
	return !t.confirm && !t.busy() && key.Matches(k, t.keys.Quit)
}

func (t listTab) update(msg tea.Msg) (listTab, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !t.busy() {
			return t, nil
		}
		var cmd tea.Cmd
		t.spin, cmd = t.spin.Update(msg)
		return t, cmd
	case listLoadedMsg:
		t.loading, t.loaded, t.err = false, true, msg.err
		t.rows = msg.rows
		clear(t.selected)
		clear(t.states)
		t.batch, t.done = 0, 0
		t.cursor = min(t.cursor, max(len(t.rows)-1, 0))
		return t, nil
	case listActedMsg:
		t.done++
		if msg.err != nil {
			t.states[msg.key], t.errs[msg.key] = stateFailed, msg.err
		} else {
			t.states[msg.key] = stateStopped
			for _, r := range t.rows {
				if r.key == msg.key {
					t.freed += r.size
					t.freedN++
				}
			}
		}
		delete(t.selected, msg.key)
		return t, nil
	case tea.KeyPressMsg:
		return t.key(msg)
	}
	return t, nil
}

func (t listTab) live(r row) bool {
	s := t.states[r.key]
	return s == stateLive || s == stateFailed
}

func (t listTab) targets() []row {
	var out []row
	for _, r := range t.rows {
		if t.selected[r.key] && t.live(r) {
			out = append(out, r)
		}
	}
	if len(out) == 0 && len(t.rows) > 0 && t.live(t.rows[t.cursor]) {
		out = append(out, t.rows[t.cursor])
	}
	return out
}

func (t listTab) key(msg tea.KeyPressMsg) (listTab, tea.Cmd) {
	k := t.keys
	if t.confirm {
		switch {
		case key.Matches(msg, k.Confirm):
			t.confirm = false
			return t.start()
		case key.Matches(msg, k.Cancel):
			t.confirm = false
		}
		return t, nil
	}
	if t.busy() {
		return t, nil
	}
	switch {
	case key.Matches(msg, k.Up):
		t.cursor = max(t.cursor-1, 0)
	case key.Matches(msg, k.Down):
		t.cursor = min(t.cursor+1, max(len(t.rows)-1, 0))
	case key.Matches(msg, k.Top):
		t.cursor = 0
	case key.Matches(msg, k.Bottom):
		t.cursor = max(len(t.rows)-1, 0)
	case key.Matches(msg, k.Toggle):
		if len(t.rows) > 0 && t.live(t.rows[t.cursor]) {
			r := t.rows[t.cursor]
			t.selected[r.key] = !t.selected[r.key]
			if !t.selected[r.key] {
				delete(t.selected, r.key)
			}
			t.cursor = min(t.cursor+1, len(t.rows)-1)
		}
	case key.Matches(msg, k.ToggleDir):
		all := true
		for _, r := range t.rows {
			if t.live(r) && !t.selected[r.key] {
				all = false
			}
		}
		for _, r := range t.rows {
			if all {
				delete(t.selected, r.key)
			} else if t.live(r) {
				t.selected[r.key] = true
			}
		}
	case key.Matches(msg, k.Clear):
		clear(t.selected)
	case key.Matches(msg, k.Stop):
		if len(t.targets()) > 0 {
			t.confirm = true
		}
	case key.Matches(msg, k.Refresh):
		return t, t.reload()
	case key.Matches(msg, k.Help):
		t.help.ShowAll = !t.help.ShowAll
	}
	return t, nil
}

// start acts on the targets one after another, so two cache cleans never
// run at once.
func (t listTab) start() (listTab, tea.Cmd) {
	ts := t.targets()
	t.batch, t.done = len(ts), 0
	var acts []tea.Cmd
	for _, r := range ts {
		t.states[r.key] = stateStopping
		act, tab := t.spec.act, t.tab
		acts = append(acts, func() tea.Msg { return listActedMsg{tab: tab, key: r.key, err: act(r)} })
	}
	return t, tea.Batch(t.spin.Tick, tea.Sequence(acts...))
}

// ---- view ----

func (t listTab) view(tabs, _ string) tea.View {
	v := tea.NewView(t.render(tabs))
	v.AltScreen = true
	v.WindowTitle = "sweep · " + t.spec.nouns
	if t.batch > 0 && t.done < t.batch {
		v.ProgressBar = tea.NewProgressBar(tea.ProgressBarDefault, 100*t.done/t.batch)
	}
	return v
}

func (t listTab) totals() (n int, size int64) {
	for _, r := range t.rows {
		if t.live(r) || t.states[r.key] == stateStopping {
			n++
			size += r.size
		}
	}
	return n, size
}

func (t listTab) render(tabs string) string {
	if t.width == 0 {
		return ""
	}
	st, w := t.st, t.width-4
	left := st.badge.Render("sweep") + "  " + tabs
	n, size := t.totals()
	right := st.dim.Render(fmt.Sprintf("%d %s · %s", n, plural(n, t.spec.noun, t.spec.nouns), format.Bytes(size)))
	if t.freedN > 0 {
		right += "   " + st.freed.Render(fmt.Sprintf("%s %d · %s", t.spec.done, t.freedN, format.Bytes(t.freed)))
	}
	header := left + strings.Repeat(" ", max(w-lipgloss.Width(left)-lipgloss.Width(right), 1)) + right

	listH := max(t.height-8-lipgloss.Height(t.help.View(listKeys{t.keys, strings.ToLower(t.spec.verb)})), 3)
	var body string
	switch {
	case t.err != nil && len(t.rows) == 0:
		body = st.fail.Render("✗ " + t.err.Error())
	case t.loading && !t.loaded:
		body = t.spin.View() + st.dim.Render(" measuring…")
	case n == 0 && !t.busy():
		lines := []string{st.clearTitle.Render("✓ All clear"), "", st.rule(24), "", st.dim.Render(t.spec.empty)}
		if t.freedN > 0 {
			lines = append(lines, "", st.freed.Render(fmt.Sprintf("%s %d %s · %s this session", t.spec.done, t.freedN, plural(t.freedN, t.spec.noun, t.spec.nouns), format.Bytes(t.freed))))
		}
		body = lipgloss.Place(w, listH+1, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, lines...))
	default:
		body = st.faint.Render(ansi.Truncate("   "+pad("", wMark)+pad("SIZE", 9)+pad("NAME", 16)+"WHERE", w, "")) + "\n" + t.list(w, listH)
	}

	top := st.app.Render(header + "\n\n" + body)
	var status string
	switch {
	case t.done < t.batch:
		status = t.spin.View() + st.dim.Render(fmt.Sprintf(" %s %d of %d…", strings.ToLower(t.spec.verb), t.done+1, t.batch))
	case len(t.selected) > 0:
		var s int64
		for _, r := range t.targets() {
			s += r.size
		}
		status = st.selected.Render(fmt.Sprintf("%d selected", len(t.selected))) + st.dim.Render(" · "+format.Bytes(s))
	case t.loading:
		status = t.spin.View() + st.dim.Render(" refreshing…")
	}
	footer := lipgloss.NewStyle().Padding(0, 2).Render(ansi.Truncate(status, w, "…") + "\n" + t.help.View(listKeys{t.keys, strings.ToLower(t.spec.verb)}))
	screen := lipgloss.JoinVertical(lipgloss.Left, lipgloss.NewStyle().Height(t.height-lipgloss.Height(footer)).Render(top), footer)
	if t.confirm {
		return t.overlay(screen)
	}
	return screen
}

func (t listTab) list(w, h int) string {
	t.offset = max(0, min(t.offset, max(len(t.rows)-h, 0)))
	if t.cursor >= t.offset+h {
		t.offset = t.cursor - h + 1
	}
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	st := t.st
	var lines []string
	for i := t.offset; i < min(t.offset+h, len(t.rows)); i++ {
		r := t.rows[i]
		mark := st.uncheck.Render("○")
		if t.selected[r.key] {
			mark = st.check.Render("●")
		}
		text := st.text
		note := ""
		switch t.states[r.key] {
		case stateStopping:
			mark, text, note = t.spin.View(), st.dim, st.dim.Render("  "+strings.ToLower(t.spec.verb)+"…")
		case stateStopped:
			mark, text, note = st.ok.Render("✓"), st.ghost, st.ok.Render("  "+t.spec.done)
		case stateFailed:
			mark, note = st.fail.Render("✗"), st.fail.Render("  "+t.errs[r.key].Error())
		}
		line := "  " + pad(mark, wMark) + sizeStyle(st, r.size).Render(pad(format.Bytes(r.size), 9)) +
			text.Render(pad(ansi.Truncate(r.title, 15, "…"), 16)) + st.dim.Render(r.detail) + st.faint.Render("  "+r.extra) + note
		if i == t.cursor {
			lines = append(lines, st.cursorBar.Render("▌")+st.cursor.Width(w-1).Render(ansi.Truncate(line, w-1, "")))
		} else {
			lines = append(lines, " "+ansi.Truncate(line, w-1, ""))
		}
	}
	return strings.Join(lines, "\n")
}

func (t listTab) overlay(bg string) string {
	st, ts := t.st, t.targets()
	var size int64
	var b strings.Builder
	b.WriteString(st.modalTitle.Render(fmt.Sprintf("%s %d %s?", t.spec.verb, len(ts), plural(len(ts), t.spec.noun, t.spec.nouns))) + "\n\n")
	for i, r := range ts {
		size += r.size
		if i < max(t.height-14, 2) {
			b.WriteString(st.text.Render(pad(ansi.Truncate(r.title, 15, "…"), 16)) + st.dim.Render(pad(format.Bytes(r.size), 8)) + st.faint.Render(ansi.Truncate(r.extra, 40, "…")) + "\n")
		}
	}
	b.WriteString("\n" + st.dim.Render(fmt.Sprintf("frees about %s · %s", format.Bytes(size), t.spec.how)) + "\n\n")
	b.WriteString(st.stopBtn.Render(fmt.Sprintf("y  %s %d", t.spec.verb, len(ts))) + "  " + st.keepBtn.Render("esc  Keep"))
	box := st.modal.Render(b.String())
	x := max((t.width-lipgloss.Width(box))/2, 0)
	y := max((t.height-lipgloss.Height(box))/2, 0)
	return lipgloss.NewCompositor(lipgloss.NewLayer(st.faint.Render(ansi.Strip(bg))), lipgloss.NewLayer(box).X(x).Y(y).Z(1)).Render()
}

// sizeStyle makes the big ones stand out.
func sizeStyle(st styles, n int64) lipgloss.Style {
	switch {
	case n >= 1<<30:
		return st.ageOld
	case n >= 100<<20:
		return st.ageWeek
	}
	return st.ageDays
}
