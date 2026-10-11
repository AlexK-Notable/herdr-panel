package ui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlexK-Notable/herdr-panel/spec"
)

// View is a section drawn and driven by Go code; the panel package adapts
// its public View to this one.
type View interface {
	Render(width int, focused bool) ViewOutput
	Update(msg tea.Msg) tea.Cmd
}

// ViewOutput is one drawing of a view.
type ViewOutput struct {
	Lines   []string
	Regions []ViewRegion
	Cursor  int // the line kept on screen while the view has focus
}

// ViewRegion is a clickable part of a view's line, in cells.
type ViewRegion struct {
	ID, Label string
	Line, X   int
	W         int
}

// ViewClick tells a view that one of its regions was clicked; X and Y are
// the cell within the region.
type ViewClick struct {
	Region string
	X, Y   int
}

// ViewFocus tells a view that it gained or lost the keyboard focus.
type ViewFocus struct{ Focused bool }

// viewMsg carries a message one of a view's commands returned back to that
// view alone.
type viewMsg struct {
	name string
	msg  tea.Msg
}

// viewClose is a view asking the panel to close (it returned tea.Quit):
// the panel asks about unsaved edits first, as for esc.
type viewClose struct{}

const viewPrefix = "view:"

// panelKeys stay with the panel while a view has focus; every other key goes
// to the view.
var panelKeys = []string{"tab", "shift+tab", "esc", "ctrl+c", "ctrl+s", "ctrl+r"}

func (m *Model) focusedView() (string, View) {
	name, ok := strings.CutPrefix(m.focus, viewPrefix)
	if !ok {
		return "", nil
	}
	return name, m.opts.Views[name]
}

// toView hands a message to a view and wraps the command it returns.
func (m *Model) toView(name string, msg tea.Msg) tea.Cmd {
	v := m.opts.Views[name]
	if v == nil {
		return nil
	}
	return m.wrapView(name, v.Update(msg))
}

// wrapView makes a view's command safe: a panic becomes the crash screen,
// its result goes back to that view, a batch or sequence is unpacked so
// each part is wrapped too, and tea.Quit becomes a close request that asks
// about unsaved edits instead of ending the program on the spot.
func (m *Model) wrapView(name string, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return m.safe(func() tea.Msg { return m.viewResult(name, cmd()) })
}

var cmdType = reflect.TypeFor[tea.Cmd]()

// Unpack returns the commands inside a tea.Batch or tea.Sequence result,
// and whether they must run in order. Bubble Tea's sequence message is not
// exported, so it is recognised by its shape.
func Unpack(msg tea.Msg) (cmds []tea.Cmd, ordered, ok bool) {
	if b, isBatch := msg.(tea.BatchMsg); isBatch {
		return b, false, true
	}
	v := reflect.ValueOf(msg)
	if !v.IsValid() || v.Kind() != reflect.Slice || v.Type().Elem() != cmdType ||
		v.Type().PkgPath() != reflect.TypeFor[tea.BatchMsg]().PkgPath() {
		return nil, false, false
	}
	for i := range v.Len() {
		c, _ := reflect.TypeAssert[tea.Cmd](v.Index(i))
		cmds = append(cmds, c)
	}
	return cmds, true, true
}

func (m *Model) viewResult(name string, msg tea.Msg) tea.Msg {
	switch msg.(type) {
	case nil:
		return nil
	case tea.QuitMsg, tea.InterruptMsg:
		return viewClose{}
	}
	if cmds, ordered, ok := Unpack(msg); ok {
		wrapped := make([]tea.Cmd, 0, len(cmds))
		for _, c := range cmds {
			if c != nil {
				wrapped = append(wrapped, m.wrapView(name, c))
			}
		}
		switch {
		case len(wrapped) == 0:
			return nil
		case ordered:
			return tea.Sequence(wrapped...)()
		}
		return tea.BatchMsg(wrapped)
	}
	if isRuntimeRequest(msg) {
		return msg
	}
	return viewMsg{name: name, msg: msg}
}

// isRuntimeRequest reports whether msg asks Bubble Tea's runtime to do
// something: a window title, a clipboard write, a cursor, raw output, a
// size query. In v2.1 those are Bubble Tea's unexported message types, plus
// RawMsg. Its exported messages (keys, mouse, a window size, suspend) are
// events, and a view that returns one gets it back itself: delivered to the
// panel, an esc would close it and a size would shrink it.
func isRuntimeRequest(msg tea.Msg) bool {
	t := reflect.TypeOf(msg)
	if t.PkgPath() != reflect.TypeFor[tea.BatchMsg]().PkgPath() {
		return false
	}
	if _, raw := msg.(tea.RawMsg); raw {
		return true
	}
	name := t.Name()
	return name != "" && name[0] >= 'a' && name[0] <= 'z'
}

// viewFocusChange tells views that gained or lost the focus.
func (m *Model) viewFocusChange(prev string) tea.Cmd {
	if prev == m.focus {
		return nil
	}
	var cmds []tea.Cmd
	if name, ok := strings.CutPrefix(prev, viewPrefix); ok {
		cmds = append(cmds, m.toView(name, ViewFocus{Focused: false}))
	}
	if name, ok := strings.CutPrefix(m.focus, viewPrefix); ok {
		cmds = append(cmds, m.toView(name, ViewFocus{Focused: true}))
	}
	return tea.Batch(cmds...)
}

// viewClick handles a click on a region, id "view:<name>:<region>".
func (m *Model) viewClick(rest string, x, y int) tea.Cmd {
	name, region, _ := strings.Cut(rest, ":")
	m.focus = viewPrefix + name
	for _, e := range m.Elements() {
		if e.ID == viewPrefix+rest {
			return m.toView(name, ViewClick{Region: region, X: x - e.X, Y: y - e.Y})
		}
	}
	return m.toView(name, ViewClick{Region: region})
}

// viewLines draws a view section. A zero-width anchor on the cursor line
// lets the panel scroll the focused view into sight.
func (m *Model) viewLines(sec *spec.Section, w int) []line {
	v := m.opts.Views[sec.View]
	if v == nil {
		return []line{{plain(" " + m.theme.Error.Render("✗ no view "+sec.View+" was supplied"))}}
	}
	id := viewPrefix + sec.View
	focused := m.focus == id
	out := v.Render(max(1, w-2), focused)
	if err := checkRegions(out.Regions); err != nil {
		// A programming error in the view: the crash screen names it.
		panic(fmt.Sprintf("view %s: %v", sec.View, err))
	}
	lines := make([]line, 0, len(out.Lines))
	for i, text := range out.Lines {
		l := line{plain(" ")}
		if i == out.Cursor || (i == 0 && (out.Cursor < 0 || out.Cursor >= len(out.Lines))) {
			l = append(l, seg{id: id})
		}
		l = append(l, regionSegs(text, i, sec.View, out.Regions)...)
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		lines = append(lines, line{seg{id: id}})
	}
	return lines
}

// checkRegions enforces what Region.ID promises: letters, digits, _ and -,
// unique within the view. An id with a space would break an observe script
// ("click view:v:a b"); two alike would be one element twice.
func checkRegions(regions []ViewRegion) error {
	seen := map[string]bool{}
	for _, r := range regions {
		if r.ID == "" || strings.IndexFunc(r.ID, notIDRune) >= 0 {
			return fmt.Errorf("region id %q: use letters, digits, _ and -", r.ID)
		}
		if seen[r.ID] {
			return fmt.Errorf("region id %q is used twice", r.ID)
		}
		seen[r.ID] = true
	}
	return nil
}

func notIDRune(c rune) bool {
	switch {
	case c == '_', c == '-', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	}
	return true
}

// regionSegs cuts one line of a view into plain and clickable parts.
func regionSegs(text string, n int, name string, regions []ViewRegion) []seg {
	var rs []ViewRegion
	for _, r := range regions {
		if r.Line == n && r.W > 0 && r.X >= 0 && r.ID != "" {
			rs = append(rs, r)
		}
	}
	slices.SortFunc(rs, func(a, b ViewRegion) int { return a.X - b.X })
	width := ansi.StringWidth(text)
	var segs []seg
	at := 0
	for _, r := range rs {
		if r.X < at || r.X >= width {
			continue // overlaps the previous region, or lies past the text
		}
		end := min(width, r.X+r.W)
		if r.X > at {
			segs = append(segs, plain(ansi.Cut(text, at, r.X)))
		}
		label := r.Label
		if label == "" {
			label = r.ID
		}
		segs = append(segs, seg{text: ansi.Cut(text, r.X, end), id: viewPrefix + name + ":" + r.ID, kind: "view", label: label})
		at = end
	}
	if at < width {
		segs = append(segs, plain(ansi.Cut(text, at, width)))
	}
	return segs
}
