package paneltest_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/panel"
	"github.com/AlexK-Notable/herdr-panel/paneltest"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

const settings = `spec = 1
title = "Settings"
[stores.c]
kind = "envfile"
path = "/must/never/be/written.env"
apply = ["restart-it"]
[[section]]
fields = [
  { key = "ON", type = "bool", label = "Enabled", default = true },
  { key = "N", type = "int", label = "Count", min = 1, max = 9, default = 3 },
]
`

func load(t *testing.T, src string) *spec.Spec {
	t.Helper()
	s, err := spec.Parse([]byte(src), "t.toml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDriveSettings(t *testing.T) {
	t.Parallel()
	p := paneltest.New(t, load(t, settings), paneltest.Config{})
	p.Click("field:ON")
	p.RequireFrame("[ ] off • changed")
	if p.Dirty() != 1 {
		t.Errorf("dirty = %d", p.Dirty())
	}
	p.Key("ctrl+s")
	p.RequireFrame("✓ saved and applied")
	if got := p.StoreText("c"); got != "ON=false\n" {
		t.Errorf("store = %q", got)
	}
	if strings.HasPrefix(p.StorePath("c"), "/must/never") {
		t.Errorf("the test used the spec's real path %s", p.StorePath("c"))
	}
	if calls := p.Calls(); len(calls) != 1 || calls[0].Argv[0] != "restart-it" ||
		!slices.Contains(calls[0].Env, "PANEL_FIELD_ON=false") {
		t.Errorf("calls = %+v", calls)
	}
}

func TestStubAnswersAction(t *testing.T) {
	t.Parallel()
	p := paneltest.New(t, load(t, settings), paneltest.Config{
		Stubs: []paneltest.Stub{{Match: []string{"restart-it"}, Stderr: "not running", Exit: 1}},
	})
	p.Click("field:ON")
	p.Key("ctrl+s")
	p.RequireFrame("✗ saved, not applied: not running")
}

// counter is a View with two clickable regions and a keyboard.
type counter struct {
	n       int
	focused bool
	got     []string // what Update received, for the tests
}

type bumped struct{ by int }

func (c *counter) Render(in panel.RenderInput) panel.RenderOutput {
	mark := " "
	if in.Focused {
		mark = ">"
	}
	return panel.RenderOutput{
		Lines: []string{fmt.Sprintf("%s count %d", mark, c.n), "[-] [+]"},
		Regions: []panel.Region{
			{ID: "dec", Label: "minus", Line: 1, X: 0, W: 3},
			{ID: "inc", Label: "plus", Line: 1, X: 4, W: 3},
		},
	}
}

func (c *counter) Update(msg tea.Msg) tea.Cmd {
	c.got = append(c.got, fmt.Sprintf("%T", msg))
	switch m := msg.(type) {
	case panel.Click:
		if m.Region == "inc" {
			c.n++
		} else {
			c.n--
		}
	case panel.Focus:
		c.focused = m.Focused
	case tea.KeyPressMsg:
		switch m.String() {
		case "+":
			c.n++
		case "b": // a batch: both parts arrive
			return tea.Batch(bump(10), bump(100))
		case "s": // a sequence
			return tea.Sequence(bump(1), bump(2))
		case "x":
			return tea.Quit
		case "p":
			return func() tea.Msg { panic("view command exploded") }
		}
	case bumped:
		c.n += m.by
	}
	return nil
}

func bump(by int) tea.Cmd { return func() tea.Msg { return bumped{by} } }

const withView = `spec = 1
title = "Counter"
[stores.c]
kind = "envfile"
path = "c.env"
[[section]]
fields = [ { key = "ON", type = "bool", label = "Enabled", default = true } ]
[[section]]
title = "Count"
kind = "view"
view = "counter"
`

func counterPanel(t *testing.T) (*paneltest.Panel, *counter) {
	t.Helper()
	c := &counter{}
	p := paneltest.New(t, load(t, withView), paneltest.Config{
		Options: panel.Options{Views: map[string]panel.View{"counter": c}},
	})
	return p, c
}

func TestViewRegionsAreElements(t *testing.T) {
	t.Parallel()
	p, c := counterPanel(t)
	e, ok := p.Element("view:counter:inc")
	if !ok || e.Kind != "view" || e.Label != "plus" || e.W != 3 {
		t.Fatalf("element = %+v %v; all: %+v", e, ok, p.Elements())
	}
	p.Click("view:counter:inc")
	p.Click("view:counter:inc")
	p.Click("view:counter:dec")
	if c.n != 1 {
		t.Errorf("n = %d", c.n)
	}
	p.RequireFrame("> count 1") // a click focuses the view
	if p.Focused() != "view:counter" {
		t.Errorf("focus = %q", p.Focused())
	}
	p.Golden()
}

func TestViewKeysAndFocus(t *testing.T) {
	t.Parallel()
	p, c := counterPanel(t)
	p.Key("tab") // from the field to the view
	if !c.focused || p.Focused() != "view:counter" {
		t.Fatalf("focus = %q, view focused %v", p.Focused(), c.focused)
	}
	p.Key("+", "+", "q", "?") // q and ? belong to the view while it has focus
	if c.n != 2 || p.Closed() {
		t.Errorf("n = %d, closed = %v", c.n, p.Closed())
	}
	if strings.Contains(p.Frame(), "Keys") {
		t.Error("? opened the panel's help instead of reaching the view")
	}
	p.Key("tab")
	if c.focused {
		t.Error("the view was not told it lost the focus")
	}
}

func TestViewCommandsComeBack(t *testing.T) {
	t.Parallel()
	p, c := counterPanel(t)
	p.Click("view:counter:inc")
	p.Key("b")
	if c.n != 111 {
		t.Errorf("after a batch n = %d, want 111", c.n)
	}
	p.Key("s")
	if c.n != 114 {
		t.Errorf("after a sequence n = %d, want 114", c.n)
	}
	i := slices.Index(c.got, "paneltest_test.bumped")
	if i < 0 {
		t.Errorf("the view never received its own message: %v", c.got)
	}
}

func TestViewQuitAsksAboutUnsavedEdits(t *testing.T) {
	t.Parallel()
	p, _ := counterPanel(t)
	p.Click("field:ON")
	p.Click("view:counter:inc")
	p.Key("x") // the view returns tea.Quit
	if p.Closed() {
		t.Fatal("tea.Quit from a view closed the panel past an unsaved edit")
	}
	p.RequireFrame("Unsaved changes")

	q, _ := counterPanel(t)
	q.Click("view:counter:inc")
	q.Key("x")
	if !q.Closed() {
		t.Error("tea.Quit with nothing unsaved did not close")
	}
}

func TestPanelKeysStayWithThePanel(t *testing.T) {
	t.Parallel()
	p, c := counterPanel(t)
	p.Click("view:counter:inc")
	p.Key("esc")
	if !p.Closed() {
		t.Error("esc in a focused view did not close the panel")
	}
	if slices.Contains(c.got, "tea.KeyPressMsg") {
		t.Errorf("a panel key reached the view: %v", c.got)
	}
}
