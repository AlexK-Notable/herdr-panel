package panel

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/AlexK-Notable/herdr-panel/internal/ui"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

// View is a section drawn and driven by Go code. A spec places it with
//
//	[[section]]
//	kind = "view"
//	view = "<name>"
//
// and the program running the spec supplies it in [Options].Views under that
// name.
//
// Update receives:
//   - [tea.KeyPressMsg] and [tea.PasteMsg] while the view has the focus. The
//     panel keeps tab, shift+tab, esc, ctrl+c, ctrl+s and ctrl+r; every other
//     key, q and ? included, goes to the view.
//   - [Click] when one of its regions is clicked, and [Focus] when it gains
//     or loses the focus.
//   - every message its own commands return, and only those. A command
//     returning a Bubble Tea event (a key, a mouse event, a window size,
//     tea.Suspend) gets it back itself; requests to the runtime (a window
//     title, the clipboard, tea.Raw) go to the runtime. The runtime's
//     answers to such requests (a background colour, the clipboard's
//     content) reach the panel, not the view.
//
// The panel calls Render and Update on one goroutine, never at the same
// time. A command runs on its own goroutine, as in any Bubble Tea program;
// its result comes back through Update. A panic in any of them shows the
// panel's crash screen. A command returning [tea.Quit] asks the panel to
// close: unsaved edits are offered first, as for esc.
//
// Experimental: View may change in a minor release until the v1 freeze.
type View interface {
	Render(RenderInput) RenderOutput
	Update(tea.Msg) tea.Cmd
}

// RenderInput is what a View draws for. Fields may be added.
type RenderInput struct {
	Width   int  // cells available on each line
	Focused bool // the view has the keyboard focus; show it
	Theme   Theme
}

// RenderOutput is one drawing of a View. Fields may be added.
type RenderOutput struct {
	// Lines are drawn in order, one terminal line each. They may carry
	// styles; a line wider than Width is cut.
	Lines []string
	// Regions are the clickable parts.
	Regions []Region
	// Cursor is the line the panel scrolls into sight while the view has
	// the focus.
	Cursor int
}

// Region is a clickable part of one line, in cells from the line's start.
// observe and paneltest know it as "view:<view name>:<ID>".
type Region struct {
	ID    string // unique within the view; letters, digits, _ and -
	Label string // what observe reports; ID when empty
	Line  int
	X, W  int
}

// Click reports a click on a region. X and Y are the cell within it.
type Click struct {
	Region string
	X, Y   int
}

// Focus reports that a View gained or lost the keyboard focus.
type Focus struct{ Focused bool }

// Theme maps the panel's roles to styles. The default uses the 16 ANSI
// colours, so a panel follows the terminal's palette, and every role also
// has a mark that is not a colour, so meaning never rests on colour alone.
// NO_COLOR is honoured: colours are dropped, the other marks kept. A View
// should style with these roles rather than its own colours.
type Theme struct {
	Title       lipgloss.Style
	Section     lipgloss.Style
	Label       lipgloss.Style
	Value       lipgloss.Style
	Muted       lipgloss.Style
	Focus       lipgloss.Style // the focused element
	Selected    lipgloss.Style // the selected row of a list without the focus
	Dirty       lipgloss.Style
	Error       lipgloss.Style
	OK          lipgloss.Style
	Rule        lipgloss.Style
	Button      lipgloss.Style
	DialogTitle lipgloss.Style
	Dialog      lipgloss.Style
}

// DefaultTheme is the theme a panel uses when Options.Theme is nil.
func DefaultTheme() Theme { return Theme(ui.DefaultTheme()) }

// Tick is tea.Tick for a View: fn's message comes back to the View's
// Update after d. Use it, not tea.Tick, for a View that polls: under
// paneltest and observe, time stands still until the test advances it
// (paneltest.Panel.Advance), where tea.Tick would sleep for real. Its command
// only works inside a panel.
func Tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	return func() tea.Msg { return ui.TickRequest{D: d, Fn: fn} }
}

// ErrNoView means a spec has a view section that Options.Views does not
// supply. The herdr-panel binary supplies none.
var ErrNoView = errors.New("no view supplied")

// CheckViews reports, as an error wrapping ErrNoView, the view sections of s
// that views does not supply. Run makes the same check.
func CheckViews(s *spec.Spec, views map[string]View) error {
	var missing []string
	for _, sec := range s.Sections {
		if sec.Kind == spec.ViewSection && views[sec.View] == nil {
			missing = append(missing, sec.View)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: the spec shows view %s, which only a Go program embedding herdr-panel can supply", ErrNoView, strings.Join(missing, ", "))
}

// adapter presents a View to the panel's internals.
type adapter struct {
	v     View
	theme Theme
}

func (a adapter) Render(width int, focused bool) ui.ViewOutput {
	out := a.v.Render(RenderInput{Width: width, Focused: focused, Theme: a.theme})
	regions := make([]ui.ViewRegion, len(out.Regions))
	for i, r := range out.Regions {
		regions[i] = ui.ViewRegion{ID: r.ID, Label: r.Label, Line: r.Line, X: r.X, W: r.W}
	}
	return ui.ViewOutput{Lines: out.Lines, Regions: regions, Cursor: out.Cursor}
}

func (a adapter) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ui.ViewClick:
		msg = Click{Region: m.Region, X: m.X, Y: m.Y}
	case ui.ViewFocus:
		msg = Focus{Focused: m.Focused}
	}
	return a.v.Update(msg)
}

func adaptViews(views map[string]View, theme Theme) map[string]ui.View {
	if len(views) == 0 {
		return nil
	}
	out := make(map[string]ui.View, len(views))
	for name, v := range views {
		if v != nil {
			out[name] = adapter{v: v, theme: theme}
		}
	}
	return out
}
