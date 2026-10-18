package tui

import (
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// palette is a "freshly cleaned" set: aqua for things you can act on, mint
// for what you got back, amber and vermilion for age and danger. Every
// foreground clears WCAG 4.5:1 on its background, and status never relies on
// color alone (each one has a glyph too).
type palette struct {
	fg, muted, faint, bg           color.Color
	aqua, mint, amber, vermilion   color.Color
	cursorBg, badgeFg, modalBorder color.Color
}

func newPalette(dark bool) palette {
	ld := lipgloss.LightDark(dark)
	c := lipgloss.Color
	return palette{
		fg:          ld(c("#1F2328"), c("#E6EDF3")),
		muted:       ld(c("#57606A"), c("#8B949E")),
		faint:       ld(c("#AFB8C1"), c("#484F58")),
		bg:          ld(c("#FFFFFF"), c("#0D1117")),
		aqua:        ld(c("#0E7490"), c("#67E8F9")),
		mint:        ld(c("#15803D"), c("#86EFAC")),
		amber:       ld(c("#B45309"), c("#FCD34D")),
		vermilion:   ld(c("#C2410C"), c("#FF8A65")),
		cursorBg:    ld(c("#E6FAFD"), c("#0F2A30")),
		badgeFg:     ld(c("#FFFFFF"), c("#0D1117")),
		modalBorder: ld(c("#0E7490"), c("#67E8F9")),
	}
}

type styles struct {
	p palette

	app, badge, tab, dim, faint, text, bold lipgloss.Style
	dir, dirParent                          lipgloss.Style
	cursor, cursorBar, check, uncheck       lipgloss.Style
	ageNew, ageDays, ageWeek, ageOld        lipgloss.Style
	port, portPublic, rss                   lipgloss.Style
	freed, freedFlash, selected             lipgloss.Style
	ghost, ok, warn, fail                   lipgloss.Style
	modal, modalTitle, stopBtn, keepBtn     lipgloss.Style
	clearTitle                              lipgloss.Style
}

func newStyles(dark bool) styles {
	p := newPalette(dark)
	s := lipgloss.NewStyle()
	return styles{
		p:          p,
		app:        s.Padding(1, 2),
		badge:      s.Foreground(p.badgeFg).Background(p.mint).Bold(true).Padding(0, 1),
		tab:        s.Foreground(p.fg).Bold(true),
		dim:        s.Foreground(p.muted),
		faint:      s.Foreground(p.faint),
		text:       s.Foreground(p.fg),
		bold:       s.Foreground(p.fg).Bold(true),
		dir:        s.Foreground(p.fg).Bold(true),
		dirParent:  s.Foreground(p.muted),
		cursor:     s.Background(p.cursorBg),
		cursorBar:  s.Foreground(p.aqua),
		check:      s.Foreground(p.aqua).Bold(true),
		uncheck:    s.Foreground(p.faint),
		ageNew:     s.Foreground(p.muted),
		ageDays:    s.Foreground(p.fg),
		ageWeek:    s.Foreground(p.amber),
		ageOld:     s.Foreground(p.vermilion).Bold(true),
		port:       s.Foreground(p.aqua),
		portPublic: s.Foreground(p.amber),
		rss:        s.Foreground(p.muted),
		freed:      s.Foreground(p.mint),
		freedFlash: s.Foreground(p.mint).Bold(true),
		selected:   s.Foreground(p.aqua).Bold(true),
		ghost:      s.Foreground(p.faint).Strikethrough(true).StrikethroughSpaces(false),
		ok:         s.Foreground(p.mint),
		warn:       s.Foreground(p.amber),
		fail:       s.Foreground(p.vermilion).Bold(true),
		modal:      s.Border(lipgloss.RoundedBorder()).BorderForeground(p.modalBorder).Padding(1, 3),
		modalTitle: s.Foreground(p.fg).Bold(true),
		stopBtn:    s.Foreground(p.badgeFg).Background(p.vermilion).Bold(true).Padding(0, 1),
		keepBtn:    s.Foreground(p.fg).Background(p.cursorBg).Padding(0, 1),
		clearTitle: s.Foreground(p.mint).Bold(true),
	}
}

// age shades by how long something has been forgotten. It goes from quiet to
// loud rather than green to red, and the age text itself is always shown.
func (s styles) age(d time.Duration) lipgloss.Style {
	switch {
	case d < 24*time.Hour:
		return s.ageNew
	case d < 7*24*time.Hour:
		return s.ageDays
	case d < 14*24*time.Hour:
		return s.ageWeek
	default:
		return s.ageOld
	}
}

// rule is a mint-to-aqua line.
func (s styles) rule(w int) string {
	cs := lipgloss.Blend1D(w, s.p.mint, s.p.aqua)
	var out strings.Builder
	for _, c := range cs {
		out.WriteString(lipgloss.NewStyle().Foreground(c).Render("━"))
	}
	return out.String()
}
