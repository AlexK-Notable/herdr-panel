package ui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TickRequest asks for Fn's message after D. A view returns it from a
// command (panel.Tick builds it). The panel turns it into a timer; with
// Options.NoTimers it parks it instead, until a driver sends Advance.
type TickRequest struct {
	D  time.Duration
	Fn func(time.Time) tea.Msg
}

// Advance moves the parked clock of a panel with Options.NoTimers forward
// by D, delivering in order every parked tick that falls due, including
// ticks re-armed on the way.
type Advance struct{ D time.Duration }

type advanceTo struct{ at time.Duration }

type parkedTick struct {
	due  time.Duration
	seq  int    // ties keep the order they were asked in
	view string // the view it goes to; "" for the panel's own
	fn   func(time.Time) tea.Msg
}

// ClockEpoch is the time a parked clock starts at.
var ClockEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// after delivers fn's message after d: to the view named, or to the panel.
func (m *Model) after(d time.Duration, view string, fn func(time.Time) tea.Msg) tea.Cmd {
	if fn == nil {
		return nil
	}
	if !m.opts.NoTimers {
		return tea.Tick(d, func(t time.Time) tea.Msg {
			if view != "" {
				return viewMsg{name: view, msg: fn(t)}
			}
			return fn(t)
		})
	}
	m.tickSeq++
	m.parked = append(m.parked, parkedTick{due: m.clock + max(0, d), seq: m.tickSeq, view: view, fn: fn})
	return nil
}

// advanceTo delivers the earliest parked tick due by at, then asks for the
// next one: whatever that tick re-arms is parked before the next is picked.
func (m *Model) advanceTo(at time.Duration) tea.Cmd {
	i := -1
	for j, p := range m.parked {
		if p.due <= at && (i < 0 || p.due < m.parked[i].due || p.due == m.parked[i].due && p.seq < m.parked[i].seq) {
			i = j
		}
	}
	if i < 0 {
		m.clock = max(m.clock, at)
		return nil
	}
	p := m.parked[i]
	m.parked = slices.Delete(m.parked, i, i+1)
	m.clock = p.due
	msg := p.fn(ClockEpoch.Add(p.due))
	var cmd tea.Cmd
	if p.view != "" {
		cmd = m.toView(p.view, msg)
	} else {
		cmd = m.update(msg)
	}
	return tea.Batch(cmd, func() tea.Msg { return advanceTo{at: at} })
}
