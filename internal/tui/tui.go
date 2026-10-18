// Package tui is the interactive view of sweep.
package tui

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kanywst/sweep/internal/format"
	"github.com/kanywst/sweep/internal/proc"
	"github.com/kanywst/sweep/internal/scan"
)

// Backend finds and stops processes. The real one shells out to lsof and ps;
// the demo one works on canned data.
type Backend interface {
	Scan(ctx context.Context) ([]scan.Group, error)
	Stop(targets []proc.Target) []proc.Result
}

// System is the Backend for this machine.
type System struct{ Opt scan.PortOptions }

// Scan lists listening processes.
func (s System) Scan(ctx context.Context) ([]scan.Group, error) {
	return scan.Ports(ctx, scan.ExecRunner, s.Opt)
}

// Stop signals targets, skipping any whose pid was reused since the scan.
func (System) Stop(targets []proc.Target) []proc.Result {
	return proc.Stop(targets, 3*time.Second, proc.SystemLookup)
}

type mode int

const (
	modeScanning mode = iota
	modeList
	modeConfirm
	modeFilter
)

// state is what happened to a listener this session.
type state int

const (
	stateLive state = iota
	stateStopping
	stateStopped
	stateForced // stopped, but only after SIGKILL
	stateFailed
)

type item struct {
	group  int
	l      *scan.Listener
	head   bool
	spacer bool // blank line between directories
}

type (
	scanDoneMsg struct {
		groups []scan.Group
		err    error
	}
	// stoppedMsg reports one listener and its launchers.
	stoppedMsg struct {
		pid     int
		results []proc.Result
	}
	flashDoneMsg struct{ seq int }
)

// tally is what has been swept.
type tally struct {
	procs, forced, failed, launcherFailed int
	rss                                   int64
	ports                                 []string
}

type model struct {
	backend  Backend
	mode     mode
	all      []scan.Group // the last scan
	groups   []scan.Group // all, filtered
	filter   textinput.Model
	anchor   int // start of a range selection, -1 when none
	items    []item
	selected map[int]bool
	states   map[int]state
	errs     map[int]error
	cursor   int
	offset   int
	width    int
	height   int
	now      time.Time
	err      error

	// The current stop batch.
	batch, batchDone int

	swept     tally // this session
	lastBatch tally // the most recent confirm
	flashSeq  int
	flashing  bool
	noAnim    bool
	hint      string // one-off message under the list
	tabs      string // the tab bar, set by app

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
		states:   map[int]state{},
		errs:     map[int]error{},
		dark:     true,
		keys:     defaultKeys(),
		help:     help.New(),
		noAnim:   os.Getenv("SWEEP_NO_ANIM") != "",
		anchor:   -1,
	}
	m.filter = textinput.New()
	m.filter.Prompt = "/"
	m.filter.Placeholder = "command, dir, pid or :port"
	m.applyTheme()
	return m
}

func (m *model) applyTheme() {
	m.st = newStyles(m.dark)
	m.help.Styles = help.DefaultStyles(m.dark)
	m.spinner = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(m.st.p.aqua)))
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

func stopCmd(b Backend, l scan.Listener) tea.Cmd {
	var ts []proc.Target
	for _, p := range l.Targets() {
		ts = append(ts, proc.Target{PID: p.PID, Started: p.Started})
	}
	return func() tea.Msg { return stoppedMsg{pid: l.PID, results: b.Stop(ts)} }
}

func (m model) busy() bool { return m.mode == modeScanning || m.batchDone < m.batch }

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
		return m, m.spinner.Tick

	case spinner.TickMsg:
		if !m.busy() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case scanDoneMsg:
		m.mode, m.err, m.now = modeList, msg.err, time.Now()
		m.setGroups(msg.groups)
		return m, nil

	case stoppedMsg:
		return m.recordStop(msg)

	case flashDoneMsg:
		if msg.seq == m.flashSeq {
			m.flashing = false
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) recordStop(msg stoppedMsg) (tea.Model, tea.Cmd) {
	m.batchDone++
	delete(m.selected, msg.pid)
	server := msg.results[0]
	for _, r := range msg.results[1:] {
		if r.Err != nil {
			m.lastBatch.launcherFailed++
		}
	}
	if server.Err != nil {
		m.states[msg.pid], m.errs[msg.pid] = stateFailed, server.Err
		m.lastBatch.failed++
		m.swept.failed++
		return m, nil
	}
	m.states[msg.pid] = stateStopped
	if server.Killed {
		m.states[msg.pid] = stateForced
		m.lastBatch.forced++
		m.swept.forced++
	}
	m.lastBatch.procs++
	m.swept.procs++
	if l := m.find(msg.pid); l != nil {
		m.lastBatch.rss += l.RSS
		m.swept.rss += l.RSS
		ports := strings.Fields(format.Ports(l.Addrs))
		m.lastBatch.ports = append(m.lastBatch.ports, ports...)
		m.swept.ports = append(m.swept.ports, ports...)
	}
	if m.noAnim {
		return m, nil
	}
	m.flashSeq++
	m.flashing = true
	seq := m.flashSeq
	return m, tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return flashDoneMsg{seq} })
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.hint = ""
	if key.Matches(msg, m.keys.ForceQuit) {
		return m, tea.Quit
	}
	if m.mode == modeConfirm {
		switch {
		case key.Matches(msg, m.keys.Confirm):
			return m.startStop()
		case key.Matches(msg, m.keys.Cancel):
			m.mode = modeList
		}
		return m, nil
	}
	if m.mode == modeFilter {
		switch msg.String() {
		case "enter":
			m.mode = modeList
			m.filter.Blur()
			return m, nil
		case "esc":
			m.mode = modeList
			m.filter.Blur()
			m.filter.SetValue("")
			m.rebuild()
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.rebuild()
		return m, cmd
	}
	if m.mode != modeList {
		return m, nil
	}
	stopping := m.batchDone < m.batch
	switch {
	case key.Matches(msg, m.keys.Quit):
		if stopping {
			m.hint = "still stopping; quitting now could skip SIGKILL (ctrl+c to quit anyway)"
			return m, nil
		}
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
		switch {
		case m.anchor >= 0:
			m.anchor = -1
		case len(m.selected) > 0:
			clear(m.selected)
		case m.filter.Value() != "":
			m.filter.SetValue("")
			m.rebuild()
		}
	case key.Matches(msg, m.keys.Filter):
		m.mode = modeFilter
		return m, m.filter.Focus()
	case key.Matches(msg, m.keys.Range):
		m.rangeSelect()
	case key.Matches(msg, m.keys.Open):
		if l := m.current(); l != nil && l.Cwd != "" {
			m.hint = "opened " + scan.ShortDir(l.Cwd)
			return m, openDir(l.Cwd)
		}
	case key.Matches(msg, m.keys.Copy):
		if l := m.current(); l != nil {
			c := restartCommand(*l)
			m.hint = "copied: " + c
			return m, tea.SetClipboard(c)
		}
	case key.Matches(msg, m.keys.Stop):
		if !stopping && len(m.targets()) > 0 {
			m.mode = modeConfirm
		}
	case key.Matches(msg, m.keys.Refresh):
		if stopping {
			return m, nil
		}
		m.mode = modeScanning
		return m, tea.Batch(scanCmd(m.backend), m.spinner.Tick)
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		m.clampScroll()
	}
	return m, nil
}

func (m model) startStop() (tea.Model, tea.Cmd) {
	ts := m.targets()
	m.mode = modeList
	m.batch, m.batchDone = len(ts), 0
	m.lastBatch = tally{}
	cmds := []tea.Cmd{m.spinner.Tick}
	for _, l := range ts {
		delete(m.errs, l.PID)
		m.states[l.PID] = stateStopping
		cmds = append(cmds, stopCmd(m.backend, l))
	}
	return m, tea.Batch(cmds...)
}

func (m *model) setGroups(gs []scan.Group) {
	m.all = gs
	alive := map[int]bool{}
	for _, g := range gs {
		for _, l := range g.Listeners {
			alive[l.PID] = true
		}
	}
	for pid := range m.selected {
		if !alive[pid] {
			delete(m.selected, pid)
		}
	}
	// A refresh drops what was swept; failures stay marked.
	for pid, s := range m.states {
		if !alive[pid] || s != stateFailed {
			delete(m.states, pid)
			delete(m.errs, pid)
		}
	}
	m.batch, m.batchDone = 0, 0
	m.lastBatch = tally{}
	m.rebuild()
}

// rebuild lays out the rows for m.all under the current filter.
func (m *model) rebuild() {
	m.groups = filterGroups(m.all, m.filter.Value())
	m.items = m.items[:0]
	for gi := range m.groups {
		if gi > 0 {
			m.items = append(m.items, item{group: gi, spacer: true})
		}
		m.items = append(m.items, item{group: gi, head: true})
		for li := range m.groups[gi].Listeners {
			m.items = append(m.items, item{group: gi, l: &m.groups[gi].Listeners[li]})
		}
	}
	m.cursor = min(m.cursor, max(len(m.items)-1, 0))
	for m.cursor < len(m.items)-1 && (m.items[m.cursor].spacer || m.items[m.cursor].head) {
		m.cursor++
	}
	m.anchor = -1
	m.clampScroll()
}

// filterGroups keeps listeners matching q: ":5173" matches a port, anything
// else is a case-insensitive match on the command, directory or pid.
func filterGroups(gs []scan.Group, q string) []scan.Group {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return gs
	}
	var out []scan.Group
	for _, g := range gs {
		var keep []scan.Listener
		for _, l := range g.Listeners {
			if matches(l, q) {
				keep = append(keep, l)
			}
		}
		if len(keep) > 0 {
			out = append(out, scan.Group{Dir: g.Dir, Listeners: keep})
		}
	}
	return out
}

func matches(l scan.Listener, q string) bool {
	if port, ok := strings.CutPrefix(q, ":"); ok {
		for _, a := range l.Addrs {
			if _, p, ok := strings.CutLast(a, ":"); ok && strings.HasPrefix(p, port) {
				return true
			}
		}
		return false
	}
	return strings.Contains(strings.ToLower(format.Command(l.Command)), q) ||
		strings.Contains(strings.ToLower(l.Command), q) ||
		strings.Contains(strings.ToLower(l.Cwd), q) ||
		strconv.Itoa(l.PID) == q
}

// current is the listener under the cursor, if any.
func (m model) current() *scan.Listener {
	if len(m.items) == 0 {
		return nil
	}
	return m.items[m.cursor].l
}

// rangeSelect marks a start row on the first press and selects every live
// row from there to the cursor on the second.
func (m *model) rangeSelect() {
	if m.anchor < 0 {
		m.anchor = m.cursor
		m.hint = "range: move, then press v again"
		return
	}
	lo, hi := min(m.anchor, m.cursor), max(m.anchor, m.cursor)
	for _, it := range m.items[lo : hi+1] {
		if it.l != nil && m.live(*it.l) {
			m.selected[it.l.PID] = true
		}
	}
	m.anchor = -1
}

// restartCommand is how to start l again: its outermost launcher (what you
// typed, e.g. "pnpm dev") or the server itself, run from its directory.
func restartCommand(l scan.Listener) string {
	c := format.Command(l.Command)
	if n := len(l.Launchers); n > 0 {
		c = format.Command(l.Launchers[n-1].Command)
	}
	if l.Cwd == "" {
		return c
	}
	return "cd " + shellQuote(l.Cwd) + " && " + c
}

func shellQuote(s string) string {
	safe := func(r rune) bool {
		return strings.ContainsRune("/.-_~", r) || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
	}
	if s != "" && strings.IndexFunc(s, func(r rune) bool { return !safe(r) }) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func openDir(dir string) tea.Cmd {
	return func() tea.Msg {
		opener := "xdg-open"
		if runtime.GOOS == "darwin" {
			opener = "open"
		}
		_ = exec.Command(opener, dir).Start()
		return nil
	}
}

func (m model) find(pid int) *scan.Listener {
	for _, it := range m.items {
		if it.l != nil && it.l.PID == pid {
			return it.l
		}
	}
	return nil
}

// live reports whether l can be selected: still running, including ones a
// stop failed on.
func (m model) live(l scan.Listener) bool {
	s := m.states[l.PID]
	return s == stateLive || s == stateFailed
}

func (m *model) move(n int) {
	if len(m.items) == 0 {
		return
	}
	m.cursor = max(0, min(len(m.items)-1, m.cursor+n))
	// Never rest on a spacer: keep going the way we were moving.
	for m.items[m.cursor].spacer {
		if n < 0 && m.cursor > 0 {
			m.cursor--
		} else {
			m.cursor++
		}
	}
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
	if it.l != nil && m.live(*it.l) {
		if m.selected[it.l.PID] {
			delete(m.selected, it.l.PID)
		} else {
			m.selected[it.l.PID] = true
		}
	}
	m.move(1)
}

func (m *model) toggleGroup(gi int) {
	if gi < 0 {
		return
	}
	var ls []scan.Listener
	for _, l := range m.groups[gi].Listeners {
		if m.live(l) {
			ls = append(ls, l)
		}
	}
	all := len(ls) > 0 && !slices.ContainsFunc(ls, func(l scan.Listener) bool { return !m.selected[l.PID] })
	for _, l := range ls {
		if all {
			delete(m.selected, l.PID)
		} else {
			m.selected[l.PID] = true
		}
	}
}

// targets are the selected listeners, or the one under the cursor.
func (m model) targets() []scan.Listener {
	var out []scan.Listener
	for _, g := range m.groups {
		for _, l := range g.Listeners {
			if m.selected[l.PID] && m.live(l) {
				out = append(out, l)
			}
		}
	}
	if len(out) == 0 && len(m.items) > 0 {
		if it := m.items[m.cursor]; it.l != nil && m.live(*it.l) {
			out = append(out, *it.l)
		}
	}
	return out
}

// stopSet is every pid the confirm will signal: servers, then launchers.
func (m model) stopSet() []int {
	ts := m.targets()
	var pids []int
	for _, l := range ts {
		pids = append(pids, l.PID)
	}
	for _, l := range ts {
		for _, p := range l.LauncherPIDs() {
			if !slices.Contains(pids, p) {
				pids = append(pids, p)
			}
		}
	}
	return pids
}

// liveCount is what is still listening, counting ones being stopped.
func (m model) liveCount() (n int, rss int64) {
	for _, g := range m.groups {
		for _, l := range g.Listeners {
			if s := m.states[l.PID]; s != stateStopped && s != stateForced {
				n++
				rss += l.RSS
			}
		}
	}
	return n, rss
}
