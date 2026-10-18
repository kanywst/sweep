// Package tui is the interactive view of sweep.
package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kanywst/sweep/internal/format"
	"github.com/kanywst/sweep/internal/proc"
	"github.com/kanywst/sweep/internal/scan"
)

// Backend finds and stops processes. The real one shells out to lsof and ps;
// the demo one works on canned data.
type Backend interface {
	Scan(ctx context.Context) ([]scan.Group, error)
	Stop(pids []int) []proc.Result
}

// System is the Backend for this machine.
type System struct{ Opt scan.PortOptions }

// Scan lists listening processes.
func (s System) Scan(ctx context.Context) ([]scan.Group, error) {
	return scan.Ports(ctx, scan.ExecRunner, s.Opt)
}

// Stop signals pids.
func (System) Stop(pids []int) []proc.Result { return proc.Stop(pids, 3*time.Second) }

// Run starts the interactive view.
func Run(ctx context.Context, b Backend) error {
	_, err := tea.NewProgram(newModel(b), tea.WithContext(ctx)).Run()
	return err
}

type mode int

const (
	modeScanning mode = iota
	modeList
	modeConfirm
	modeStopping
)

type item struct {
	group int // index into groups
	l     *scan.Listener
	head  bool // a directory heading
}

type (
	scanDoneMsg struct {
		groups []scan.Group
		err    error
	}
	stopDoneMsg struct{ results []proc.Result }
)

type model struct {
	backend  Backend
	mode     mode
	groups   []scan.Group
	items    []item
	selected map[int]bool // by pid
	cursor   int
	offset   int
	width    int
	height   int
	now      time.Time
	status   string
	statusOK bool
	err      error

	dark    bool
	st      styles
	keys    keyMap
	help    help.Model
	spinner spinner.Model
}

func newModel(b Backend) model {
	m := model{
		backend:  b,
		selected: map[int]bool{},
		dark:     true,
		keys:     defaultKeys(),
		help:     help.New(),
	}
	m.applyTheme()
	m.spinner = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(m.st.accent)))
	return m
}

func (m *model) applyTheme() {
	m.st = newStyles(m.dark)
	m.help.Styles = help.DefaultStyles(m.dark)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(scanCmd(m.backend), tea.RequestBackgroundColor, m.spinner.Tick)
}

func scanCmd(b Backend) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		gs, err := b.Scan(ctx)
		return scanDoneMsg{gs, err}
	}
}

func stopCmd(b Backend, pids []int) tea.Cmd {
	return func() tea.Msg { return stopDoneMsg{b.Stop(pids)} }
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width - 4)
		m.clampScroll()
		return m, nil

	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.applyTheme()
		m.spinner.Style = lipgloss.NewStyle().Foreground(m.st.accent)
		return m, nil

	case spinner.TickMsg:
		if m.mode != modeScanning && m.mode != modeStopping {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case scanDoneMsg:
		m.mode, m.err, m.now = modeList, msg.err, time.Now()
		m.setGroups(msg.groups)
		return m, nil

	case stopDoneMsg:
		m.status, m.statusOK = summarize(msg.results), true
		for _, r := range msg.results {
			delete(m.selected, r.PID)
			if r.Err != nil {
				m.statusOK = false
			}
		}
		m.mode = modeScanning
		return m, tea.Batch(scanCmd(m.backend), m.spinner.Tick)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.ForceQuit) {
		return m, tea.Quit
	}
	if m.mode == modeConfirm {
		switch {
		case key.Matches(msg, m.keys.Confirm):
			pids := m.stopSet()
			m.mode = modeStopping
			return m, tea.Batch(stopCmd(m.backend, pids), m.spinner.Tick)
		case key.Matches(msg, m.keys.Cancel):
			m.mode = modeList
		}
		return m, nil
	}
	if m.mode != modeList {
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Up):
		m.move(-1)
	case key.Matches(msg, m.keys.Down):
		m.move(1)
	case key.Matches(msg, m.keys.PageUp):
		m.move(-m.listHeight())
	case key.Matches(msg, m.keys.PageDown):
		m.move(m.listHeight())
	case key.Matches(msg, m.keys.Top):
		m.move(-len(m.items))
	case key.Matches(msg, m.keys.Bottom):
		m.move(len(m.items))
	case key.Matches(msg, m.keys.Toggle):
		m.toggle()
	case key.Matches(msg, m.keys.ToggleDir):
		m.toggleGroup(m.currentGroup())
	case key.Matches(msg, m.keys.Clear):
		clear(m.selected)
	case key.Matches(msg, m.keys.Stop):
		if len(m.targets()) > 0 {
			m.mode = modeConfirm
		}
	case key.Matches(msg, m.keys.Refresh):
		m.mode, m.status = modeScanning, ""
		return m, tea.Batch(scanCmd(m.backend), m.spinner.Tick)
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		m.clampScroll()
	}
	return m, nil
}

func (m *model) setGroups(gs []scan.Group) {
	m.groups = gs
	m.items = m.items[:0]
	alive := map[int]bool{}
	for gi := range gs {
		m.items = append(m.items, item{group: gi, head: true})
		for li := range gs[gi].Listeners {
			l := &gs[gi].Listeners[li]
			alive[l.PID] = true
			m.items = append(m.items, item{group: gi, l: l})
		}
	}
	for pid := range m.selected {
		if !alive[pid] {
			delete(m.selected, pid)
		}
	}
	m.cursor = min(m.cursor, max(len(m.items)-1, 0))
	if len(m.items) > 1 && m.items[m.cursor].head && m.cursor == 0 {
		m.cursor = 1
	}
	m.clampScroll()
}

func (m *model) move(n int) {
	if len(m.items) == 0 {
		return
	}
	m.cursor = max(0, min(len(m.items)-1, m.cursor+n))
	m.clampScroll()
}

func (m *model) clampScroll() {
	h := m.listHeight()
	if h <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = max(0, min(m.offset, max(len(m.items)-h, 0)))
}

func (m model) currentGroup() int {
	if len(m.items) == 0 {
		return -1
	}
	return m.items[m.cursor].group
}

func (m *model) toggle() {
	if len(m.items) == 0 {
		return
	}
	it := m.items[m.cursor]
	if it.head {
		m.toggleGroup(it.group)
		return
	}
	if m.selected[it.l.PID] {
		delete(m.selected, it.l.PID)
	} else {
		m.selected[it.l.PID] = true
	}
	m.move(1)
}

func (m *model) toggleGroup(gi int) {
	if gi < 0 {
		return
	}
	ls := m.groups[gi].Listeners
	all := !slices.ContainsFunc(ls, func(l scan.Listener) bool { return !m.selected[l.PID] })
	for _, l := range ls {
		if all {
			delete(m.selected, l.PID)
		} else {
			m.selected[l.PID] = true
		}
	}
}

// targets are the selected listeners, or the one under the cursor if none are selected.
func (m model) targets() []scan.Listener {
	var out []scan.Listener
	for _, g := range m.groups {
		for _, l := range g.Listeners {
			if m.selected[l.PID] {
				out = append(out, l)
			}
		}
	}
	if len(out) == 0 && len(m.items) > 0 && !m.items[m.cursor].head {
		out = append(out, *m.items[m.cursor].l)
	}
	return out
}

// stopSet is every pid to signal: the servers, then the launchers above them.
func (m model) stopSet() []int {
	var pids, wrappers []int
	for _, l := range m.targets() {
		pids = append(pids, l.PID)
		wrappers = append(wrappers, l.Wrappers...)
	}
	for _, w := range wrappers {
		if !slices.Contains(pids, w) {
			pids = append(pids, w)
		}
	}
	return pids
}

func summarize(rs []proc.Result) string {
	var stopped, forced, failed int
	for _, r := range rs {
		switch {
		case r.Err != nil:
			failed++
		case r.Killed:
			forced++
			stopped++
		default:
			stopped++
		}
	}
	s := fmt.Sprintf("stopped %d", stopped)
	if forced > 0 {
		s += fmt.Sprintf(" (%d forced)", forced)
	}
	if failed > 0 {
		s += fmt.Sprintf(", %d failed", failed)
	}
	return s
}

// ---- view ----

const chrome = 2 + 1 + 1 + 1 + 1 + 2 // padding, header, gap, column header, gap, footer gap

func (m model) listHeight() int {
	return max(m.height-chrome-lipgloss.Height(m.help.View(m.keys)), 3)
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "sweep"
	return v
}

func (m model) render() string {
	if m.width == 0 {
		return ""
	}
	inner := m.width - 4
	var b strings.Builder
	b.WriteString(m.header(inner) + "\n\n")

	switch {
	case m.err != nil:
		b.WriteString(m.st.errS.Render("scan failed: "+m.err.Error()) + "\n")
	case m.mode == modeScanning && len(m.items) == 0:
		b.WriteString(m.spinner.View() + m.st.dim.Render(" looking for listening ports…") + "\n")
	case len(m.items) == 0:
		b.WriteString(m.st.ok.Render("✓ ") + m.st.dim.Render("nothing you started is listening on a port.") + "\n")
	default:
		b.WriteString(m.columns(inner) + "\n")
		b.WriteString(m.list(inner))
	}

	body := m.st.app.Render(b.String())
	footer := lipgloss.NewStyle().Padding(0, 2).Render(m.footer())
	screen := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Height(m.height-lipgloss.Height(footer)).Render(body),
		footer)

	if m.mode == modeConfirm || m.mode == modeStopping {
		return m.overlay(screen)
	}
	return screen
}

func (m model) header(w int) string {
	var n int
	var rss int64
	for _, g := range m.groups {
		n += len(g.Listeners)
		for _, l := range g.Listeners {
			rss += l.RSS
		}
	}
	left := m.st.badge.Render("sweep") + "  " + m.st.title.Render("Ports")
	right := m.st.dim.Render(fmt.Sprintf("%d listening · %d dirs · %s", n, len(m.groups), format.Bytes(rss)))
	if len(m.selected) > 0 {
		right = m.st.check.Render(fmt.Sprintf("%d selected", len(m.selected))) + m.st.faint.Render("  ·  ") + right
	}
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// Column widths.
const (
	wMark = 2
	wAge  = 5
	wPID  = 8
	wPort = 16
	wRSS  = 7
)

func (m model) columns(w int) string {
	cols := pad("", wMark) + pad("AGE", wAge) + pad("PID", wPID) + pad("PORT", wPort) + pad("MEM", wRSS) + "COMMAND"
	return m.st.faint.Render(ansi.Truncate("   "+cols, w, ""))
}

func (m model) list(w int) string {
	h := m.listHeight()
	end := min(m.offset+h, len(m.items))
	var rows []string
	for i := m.offset; i < end; i++ {
		rows = append(rows, m.row(i, w))
	}
	for len(rows) < h {
		rows = append(rows, "")
	}
	if m.offset > 0 {
		rows[0] = m.st.faint.Render(pad("", wMark) + "  ↑ more")
	}
	if end < len(m.items) {
		rows[len(rows)-1] = m.st.faint.Render(pad("", wMark) + "  ↓ more")
	}
	return strings.Join(rows, "\n")
}

func (m model) row(i, w int) string {
	it := m.items[i]
	isCursor := i == m.cursor
	var line string
	if it.head {
		g := m.groups[it.group]
		mark := m.groupMark(g)
		oldest := m.now.Sub(g.Oldest())
		line = mark + " " + m.st.dir.Render(scan.ShortDir(g.Dir)) +
			m.st.dirMeta.Render(fmt.Sprintf("  %d · oldest ", len(g.Listeners))) +
			m.st.age(oldest).Render(format.Age(oldest))
	} else {
		l := it.l
		mark := m.st.uncheck.Render("○")
		if m.selected[l.PID] {
			mark = m.st.check.Render("●")
		}
		age := m.now.Sub(l.Started)
		ports := format.Ports(l.Addrs)
		cmdW := max(w-2-wMark-wAge-wPID-wPort-wRSS, 10)
		line = "  " + pad(mark, wMark) +
			m.st.age(age).Render(pad(format.Age(age), wAge)) +
			m.st.pid.Render(pad(fmt.Sprint(l.PID), wPID)) +
			m.st.port.Render(pad(ansi.Truncate(ports, wPort-1, "…"), wPort)) +
			m.st.rss.Render(pad(format.Bytes(l.RSS), wRSS)) +
			m.st.cmd.Render(ansi.Truncate(format.Command(l.Command), cmdW, "…"))
	}
	if isCursor {
		bar := lipgloss.NewStyle().Foreground(m.st.accent).Render("▌")
		line = m.st.cursor.Width(w - 1).Render(ansi.Truncate(line, w-1, ""))
		return bar + line
	}
	return " " + ansi.Truncate(line, w-1, "")
}

func (m model) groupMark(g scan.Group) string {
	n := 0
	for _, l := range g.Listeners {
		if m.selected[l.PID] {
			n++
		}
	}
	switch {
	case n == 0:
		return m.st.uncheck.Render("▸")
	case n == len(g.Listeners):
		return m.st.check.Render("◆")
	default:
		return m.st.check.Render("◇")
	}
}

func (m model) footer() string {
	var status string
	switch {
	case m.mode == modeScanning && len(m.items) > 0:
		status = m.spinner.View() + m.st.dim.Render(" refreshing…")
	case m.status != "" && m.statusOK:
		status = m.st.ok.Render("✓ " + m.status)
	case m.status != "":
		status = m.st.errS.Render("! " + m.status)
	}
	h := m.help.View(m.keys)
	if status == "" {
		return h
	}
	return status + "\n" + h
}

func (m model) overlay(bg string) string {
	ts := m.targets()
	pids := m.stopSet()
	extra := len(pids) - len(ts)

	var b strings.Builder
	if m.mode == modeStopping {
		b.WriteString(m.spinner.View() + m.st.title.Render(fmt.Sprintf(" stopping %d processes…", len(pids))))
	} else {
		title := fmt.Sprintf("Stop %d %s?", len(ts), plural(len(ts), "process", "processes"))
		b.WriteString(m.st.modalTitle.Render(title) + "\n\n")
		show := ts[:min(len(ts), 8)]
		for _, l := range show {
			b.WriteString(m.st.port.Render(pad(format.Ports(l.Addrs), 14)) +
				m.st.cmd.Render(ansi.Truncate(format.Command(l.Command), 36, "…")) +
				m.st.faint.Render("  "+scan.ShortDir(l.Cwd)) + "\n")
		}
		if len(ts) > len(show) {
			b.WriteString(m.st.faint.Render(fmt.Sprintf("… and %d more", len(ts)-len(show))) + "\n")
		}
		if extra > 0 {
			b.WriteString("\n" + m.st.dim.Render(fmt.Sprintf("also stops %d %s that started them (npm, pnpm, sh)",
				extra, plural(extra, "launcher", "launchers"))) + "\n")
		}
		b.WriteString("\n" + m.st.dim.Render("SIGTERM, then SIGKILL after 3s") + "\n\n")
		b.WriteString(m.st.danger.Render("y  stop") + "   " + m.st.dim.Render("n  cancel"))
	}
	box := m.st.modal.Render(b.String())
	x := max((m.width-lipgloss.Width(box))/2, 0)
	y := max((m.height-lipgloss.Height(box))/2, 0)
	dimmed := m.st.faint.Render(ansi.Strip(bg))
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(dimmed),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	).Render()
}

func pad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
