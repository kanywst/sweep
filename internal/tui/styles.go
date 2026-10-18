package tui

import (
	"image/color"
	"time"

	"charm.land/lipgloss/v2"
)

type styles struct {
	app, badge, title, dim, faint         lipgloss.Style
	dir, dirMeta                          lipgloss.Style
	cursor, check, uncheck                lipgloss.Style
	ageFresh, ageDays, ageWeeks, ageStale lipgloss.Style
	pid, port, rss, cmd                   lipgloss.Style
	modal, modalTitle, danger, ok, errS   lipgloss.Style
	accent                                color.Color
}

func newStyles(dark bool) styles {
	ld := lipgloss.LightDark(dark)
	var (
		fg      = ld(lipgloss.Color("#1F2328"), lipgloss.Color("#E6EDF3"))
		muted   = ld(lipgloss.Color("#656D76"), lipgloss.Color("#8B949E"))
		faint   = ld(lipgloss.Color("#AFB8C1"), lipgloss.Color("#484F58"))
		accent  = ld(lipgloss.Color("#0969DA"), lipgloss.Color("#58A6FF"))
		cursor  = ld(lipgloss.Color("#DDF4FF"), lipgloss.Color("#1C2D41"))
		green   = ld(lipgloss.Color("#1A7F37"), lipgloss.Color("#3FB950"))
		yellow  = ld(lipgloss.Color("#9A6700"), lipgloss.Color("#D29922"))
		orange  = ld(lipgloss.Color("#BC4C00"), lipgloss.Color("#F0883E"))
		red     = ld(lipgloss.Color("#CF222E"), lipgloss.Color("#F85149"))
		purple  = ld(lipgloss.Color("#8250DF"), lipgloss.Color("#BC8CFF"))
		badgeFg = lipgloss.Color("#FFFFFF")
	)
	s := lipgloss.NewStyle()
	return styles{
		app:        s.Padding(1, 2),
		badge:      s.Foreground(badgeFg).Background(accent).Bold(true).Padding(0, 1),
		title:      s.Foreground(fg).Bold(true),
		dim:        s.Foreground(muted),
		faint:      s.Foreground(faint),
		dir:        s.Foreground(purple).Bold(true),
		dirMeta:    s.Foreground(muted),
		cursor:     s.Background(cursor),
		check:      s.Foreground(accent).Bold(true),
		uncheck:    s.Foreground(faint),
		ageFresh:   s.Foreground(green),
		ageDays:    s.Foreground(yellow),
		ageWeeks:   s.Foreground(orange),
		ageStale:   s.Foreground(red).Bold(true),
		pid:        s.Foreground(muted),
		port:       s.Foreground(accent),
		rss:        s.Foreground(muted),
		cmd:        s.Foreground(fg),
		modal:      s.Border(lipgloss.RoundedBorder()).BorderForeground(red).Padding(1, 3),
		modalTitle: s.Foreground(red).Bold(true),
		danger:     s.Foreground(badgeFg).Background(red).Bold(true).Padding(0, 1),
		ok:         s.Foreground(green),
		errS:       s.Foreground(red),
		accent:     accent,
	}
}

func (s styles) age(d time.Duration) lipgloss.Style {
	switch {
	case d < 24*time.Hour:
		return s.ageFresh
	case d < 7*24*time.Hour:
		return s.ageDays
	case d < 14*24*time.Hour:
		return s.ageWeeks
	default:
		return s.ageStale
	}
}
