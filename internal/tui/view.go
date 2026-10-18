package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kanywst/sweep/internal/format"
	"github.com/kanywst/sweep/internal/proc"
	"github.com/kanywst/sweep/internal/scan"
)

// Column widths.
const (
	wMark = 2
	wAge  = 5
	wPID  = 8
	wPort = 16
	wRSS  = 7
)

// chrome is everything around the list: padding, header, gap, column header,
// and the status line above the help.
const chrome = 2 + 1 + 1 + 1 + 2

func (m model) listHeight() int {
	return max(m.height-chrome-lipgloss.Height(m.help.View(m.keys)), 3)
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = m.title()
	if m.batch > 0 && m.batchDone < m.batch {
		state := tea.ProgressBarDefault
		if m.lastBatch.forced > 0 {
			state = tea.ProgressBarWarning
		}
		if m.lastBatch.failed > 0 {
			state = tea.ProgressBarError
		}
		v.ProgressBar = tea.NewProgressBar(state, 100*m.batchDone/m.batch)
	}
	return v
}

func (m model) title() string {
	n, _ := m.liveCount()
	switch {
	case m.batch > 0 && m.batchDone < m.batch:
		return fmt.Sprintf("sweep · stopping %d/%d", m.batchDone, m.batch)
	case m.mode == modeScanning && len(m.items) == 0:
		return "sweep"
	case n == 0:
		return "sweep · all clear"
	default:
		return fmt.Sprintf("sweep · %d listening", n)
	}
}

func (m model) render() string {
	if m.width == 0 {
		return ""
	}
	inner := m.width - 4
	n, _ := m.liveCount()

	var body string
	switch {
	case m.err != nil:
		body = m.st.fail.Render("✗ scan failed: ") + m.st.text.Render(m.err.Error())
	case m.mode == modeScanning && len(m.items) == 0:
		body = m.spinner.View() + m.st.dim.Render(" looking for listening ports…")
	case len(m.items) == 0 && m.filter.Value() != "":
		body = m.st.dim.Render("nothing matches ") + m.st.text.Render(m.filter.Value()) + m.st.faint.Render("   esc to clear")
	case n == 0 && m.batchDone == m.batch:
		body = m.allClear(inner, m.listHeight()+1)
	default:
		body = m.columns(inner) + "\n" + m.list(inner)
	}

	top := m.st.app.Render(m.header(inner) + "\n" + m.filterLine(inner) + "\n" + body)
	footer := lipgloss.NewStyle().Padding(0, 2).Render(m.footer(inner))
	screen := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Height(m.height-lipgloss.Height(footer)).Render(top),
		footer)

	if m.mode == modeConfirm {
		return m.overlay(screen)
	}
	return screen
}

func (m model) header(w int) string {
	tabs := m.tabs
	if tabs == "" {
		tabs = m.st.tab.Render("Ports")
	}
	left := m.st.badge.Render("sweep") + "  " + tabs
	n, rss := m.liveCount()
	right := m.st.dim.Render(fmt.Sprintf("%d listening · %s", n, format.Bytes(rss)))
	if m.swept.procs > 0 {
		freed := fmt.Sprintf("swept %d · %s", m.swept.procs, format.Bytes(m.swept.rss))
		st := m.st.freed
		if m.flashing {
			st = m.st.freedFlash
			freed += " ▲"
		}
		right += m.st.faint.Render("   ") + st.Render(freed)
	}
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// filterLine shows the filter while typing it, or while one is applied.
func (m model) filterLine(w int) string {
	if m.mode != modeFilter && m.filter.Value() == "" {
		return ""
	}
	shown := 0
	for _, it := range m.items {
		if it.l != nil {
			shown++
		}
	}
	line := m.filter.View()
	if m.mode != modeFilter {
		line = m.st.dim.Render("/") + m.st.selected.Render(m.filter.Value())
	}
	return ansi.Truncate(line+m.st.faint.Render(fmt.Sprintf("   %d shown", shown)), w, "…")
}

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
		rows[0] = m.st.faint.Render("     ↑ " + fmt.Sprint(m.offset) + " more")
	}
	if end < len(m.items) {
		rows[len(rows)-1] = m.st.faint.Render("     ↓ " + fmt.Sprint(len(m.items)-end) + " more")
	}
	return strings.Join(rows, "\n")
}

func (m model) row(i, w int) string {
	it := m.items[i]
	if it.spacer {
		return ""
	}
	var line string
	if it.head {
		line = m.groupRow(m.groups[it.group])
	} else {
		line = m.listenerRow(*it.l, w)
	}
	if i == m.cursor {
		return m.st.cursorBar.Render("▌") + m.st.cursor.Width(w-1).Render(ansi.Truncate(line, w-1, ""))
	}
	return " " + ansi.Truncate(line, w-1, "")
}

func (m model) groupRow(g scan.Group) string {
	live, sel := 0, 0
	for _, l := range g.Listeners {
		if m.live(l) {
			live++
			if m.selected[l.PID] {
				sel++
			}
		}
	}
	var mark string
	switch {
	case live == 0:
		mark = m.st.ok.Render("✓")
	case sel == 0:
		mark = m.st.uncheck.Render("▸")
	case sel == live:
		mark = m.st.check.Render("◆")
	default:
		mark = m.st.check.Render("◇")
	}
	parent, name := splitDir(scan.ShortDir(g.Dir))
	head := mark + " " + m.st.dirParent.Render(parent) + m.st.dir.Render(name)
	if live == 0 {
		return head + m.st.ok.Render("  swept")
	}
	oldest := m.now.Sub(g.Oldest())
	return head + m.st.dim.Render(fmt.Sprintf("  %d · oldest ", live)) + m.st.age(oldest).Render(format.Age(oldest))
}

func (m model) listenerRow(l scan.Listener, w int) string {
	age := m.now.Sub(l.Started)
	ports := format.Ports(l.Addrs)
	cmdW := max(w-2-wMark-wAge-wPID-wPort-wRSS, 10)
	cmd := ansi.Truncate(format.Command(l.Command), cmdW, "…")

	st := m.states[l.PID]
	if st != stateLive && st != stateFailed {
		var mark, note string
		switch st {
		case stateStopping:
			mark, note = m.spinner.View(), m.st.dim.Render("  stopping…")
		case stateStopped:
			mark, note = m.st.ok.Render("✓"), m.st.ok.Render("  stopped")
		case stateForced:
			mark, note = m.st.ok.Render("✓"), m.st.warn.Render("  stopped (SIGKILL)")
		}
		ghost := m.st.ghost
		if st == stateStopping {
			ghost = m.st.dim
		}
		return "  " + pad(mark, wMark) +
			ghost.Render(pad(format.Age(age), wAge)+pad(fmt.Sprint(l.PID), wPID)+pad(ports, wPort)+pad(format.Bytes(l.RSS), wRSS)+cmd) + note
	}

	mark := m.st.uncheck.Render("○")
	note := ""
	if st == stateFailed {
		mark, note = m.st.fail.Render("✗"), m.st.fail.Render("  "+shortErr(m.errs[l.PID]))
	}
	if m.selected[l.PID] {
		mark = m.st.check.Render("●")
	}
	portSt := m.st.port
	if strings.Contains(ports, "*") {
		portSt = m.st.portPublic
	}
	return "  " + pad(mark, wMark) +
		m.st.age(age).Render(pad(format.Age(age), wAge)) +
		m.st.dim.Render(pad(fmt.Sprint(l.PID), wPID)) +
		portSt.Render(pad(ansi.Truncate(ports, wPort-1, "…"), wPort)) +
		m.st.rss.Render(pad(format.Bytes(l.RSS), wRSS)) +
		m.st.text.Render(cmd) + note
}

func (m model) footer(w int) string {
	var status string
	switch {
	case m.hint != "":
		status = m.st.warn.Render(m.hint)
	case m.batch > 0 && m.batchDone < m.batch:
		status = m.spinner.View() + m.st.dim.Render(fmt.Sprintf(" stopping %d of %d…", m.batchDone+1, m.batch))
	case m.lastBatch.procs > 0 || m.lastBatch.failed > 0:
		status = m.summary(m.lastBatch)
	case m.mode == modeScanning:
		status = m.spinner.View() + m.st.dim.Render(" refreshing…")
	default:
		if ts := m.targets(); len(m.selected) > 0 {
			var rss int64
			var ports []string
			for _, l := range ts {
				rss += l.RSS
				ports = append(ports, format.Ports(l.Addrs))
			}
			status = m.st.selected.Render(fmt.Sprintf("%d selected", len(ts))) +
				m.st.dim.Render(" · "+strings.Join(ports, " ")+" · "+format.Bytes(rss))
		}
	}
	return ansi.Truncate(status, w, "…") + "\n" + m.help.View(m.keys)
}

func (m model) summary(t tally) string {
	var b strings.Builder
	if t.procs > 0 {
		b.WriteString(m.st.ok.Render(fmt.Sprintf("✓ swept %d %s", t.procs, plural(t.procs, "process", "processes"))))
		if len(t.ports) > 0 {
			b.WriteString(m.st.dim.Render(" · freed ") + m.st.port.Render(strings.Join(t.ports, " ")))
		}
		b.WriteString(m.st.dim.Render(" · " + format.Bytes(t.rss)))
		if t.forced > 0 {
			b.WriteString(m.st.warn.Render(fmt.Sprintf(" · %d needed SIGKILL", t.forced)))
		}
	}
	if t.failed > 0 {
		if b.Len() > 0 {
			b.WriteString("  ")
		}
		b.WriteString(m.st.fail.Render(fmt.Sprintf("✗ %d failed", t.failed)))
	}
	if t.launcherFailed > 0 {
		b.WriteString(m.st.warn.Render(fmt.Sprintf(" · %d %s not stopped", t.launcherFailed, plural(t.launcherFailed, "launcher", "launchers"))))
	}
	return b.String() + m.st.faint.Render("   r to refresh")
}

func (m model) allClear(w, h int) string {
	lines := []string{
		m.st.clearTitle.Render("✓ All clear"),
		"",
		m.st.rule(24),
		"",
		m.st.dim.Render("nothing you started is listening on a port"),
	}
	if m.swept.procs > 0 {
		lines = append(lines, "",
			m.st.freed.Render(fmt.Sprintf("swept %d %s · %s this session",
				m.swept.procs, plural(m.swept.procs, "process", "processes"), format.Bytes(m.swept.rss))))
		if len(m.swept.ports) > 0 {
			lines = append(lines, m.st.dim.Render("freed ")+m.st.port.Render(strings.Join(m.swept.ports, " ")))
		}
	}
	card := lipgloss.JoinVertical(lipgloss.Center, lines...)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, card)
}

func (m model) overlay(bg string) string {
	ts := m.targets()
	var b strings.Builder
	b.WriteString(m.st.modalTitle.Render(fmt.Sprintf("Stop %d %s?", len(ts), plural(len(ts), "process", "processes"))) + "\n\n")

	// Fit the list to the terminal: the title, footer lines and border take ~12.
	budget, shown := max(m.height-14, 2), 0
	for _, l := range ts {
		need := 1 + len(l.Launchers)
		if shown > 0 && budget < need {
			break
		}
		budget -= need
		shown++
	}
	show := ts[:shown]
	for _, l := range show {
		ports := format.Ports(l.Addrs)
		portSt := m.st.port
		if strings.Contains(ports, "*") {
			portSt = m.st.portPublic
		}
		b.WriteString(portSt.Render(pad(ansi.Truncate(ports, 13, "…"), 14)) +
			m.st.text.Render(pad(ansi.Truncate(format.Command(l.Command), 30, "…"), 32)) +
			m.st.dim.Render(ansi.Truncate(scan.ShortDir(l.Cwd), 24, "…")) + "\n")
		for _, p := range l.Launchers {
			b.WriteString(m.st.faint.Render(pad("", 14)+"↳ "+ansi.Truncate(format.Command(p.Command), 28, "…")) + "\n")
		}
	}
	if len(ts) > len(show) {
		b.WriteString(m.st.faint.Render(fmt.Sprintf("… and %d more", len(ts)-len(show))) + "\n")
	}
	if extra := len(m.stopSet()) - len(ts); extra > 0 {
		b.WriteString("\n" + m.st.dim.Render(fmt.Sprintf("↳ %d %s that started them will stop too",
			extra, plural(extra, "launcher", "launchers"))) + "\n")
	}
	b.WriteString(m.st.faint.Render("SIGTERM, then SIGKILL after 3s") + "\n\n")
	b.WriteString(m.st.stopBtn.Render(fmt.Sprintf("y  Stop %d", len(ts))) + "  " + m.st.keepBtn.Render("esc  Keep running"))

	box := m.st.modal.Render(b.String())
	x := max((m.width-lipgloss.Width(box))/2, 0)
	y := max((m.height-lipgloss.Height(box))/2, 0)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(m.st.faint.Render(ansi.Strip(bg))),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	).Render()
}

// splitDir splits "~/work/shop" into "~/work/" and "shop".
func splitDir(d string) (string, string) {
	if i := strings.LastIndexByte(d, '/'); i >= 0 && i < len(d)-1 {
		return d[:i+1], d[i+1:]
	}
	return "", d
}

func shortErr(err error) string {
	switch {
	case err == nil:
		return "failed"
	case errors.Is(err, proc.ErrChanged):
		return "skipped: pid reused, press r"
	}
	return "failed: " + err.Error()
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
