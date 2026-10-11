package ui

import (
	"charm.land/lipgloss/v2"
)

// Theme maps the panel's roles to styles. Colours are ANSI palette indices,
// so a panel takes on the user's terminal theme, and every role also has a
// non-colour mark (reverse, bold, a glyph), so meaning never rests on colour.
// NO_COLOR is honoured by Bubble Tea's renderer, which drops colours and keeps
// attributes.
type Theme struct {
	Title       lipgloss.Style
	Section     lipgloss.Style
	Label       lipgloss.Style
	Value       lipgloss.Style
	Muted       lipgloss.Style
	Focus       lipgloss.Style // the focused element
	Selected    lipgloss.Style // the selected row of a list that is not focused
	Dirty       lipgloss.Style
	Error       lipgloss.Style
	OK          lipgloss.Style
	Rule        lipgloss.Style
	Button      lipgloss.Style
	DialogTitle lipgloss.Style
	Dialog      lipgloss.Style
}

// DefaultTheme uses the ANSI 16 palette.
func DefaultTheme() Theme {
	s := lipgloss.NewStyle
	return Theme{
		Title:       s().Bold(true),
		Section:     s().Bold(true).Foreground(lipgloss.Color("4")),
		Label:       s(),
		Value:       s(),
		Muted:       s().Faint(true),
		Focus:       s().Reverse(true),
		Selected:    s().Bold(true),
		Dirty:       s().Foreground(lipgloss.Color("3")),
		Error:       s().Foreground(lipgloss.Color("1")),
		OK:          s().Foreground(lipgloss.Color("2")),
		Rule:        s().Faint(true),
		Button:      s(),
		DialogTitle: s().Bold(true),
		Dialog:      s(),
	}
}
