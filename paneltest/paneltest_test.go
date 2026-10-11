package paneltest_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

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
	bumps   []int    // bumped messages, in arrival order
	ticks   []time.Time
}

type bumped struct{ by int }

type ticked struct{ at time.Time }

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
		case "t": // poll every second
			return c.tick()
		}
	case bumped:
		c.n += m.by
		c.bumps = append(c.bumps, m.by)
	case ticked:
		c.ticks = append(c.ticks, m.at)
		return c.tick()
	}
	return nil
}

func (c *counter) tick() tea.Cmd {
	return panel.Tick(time.Second, func(t time.Time) tea.Msg { return ticked{t} })
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
		Views: map[string]panel.View{"counter": c},
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
	if got := c.bumps[len(c.bumps)-2:]; !slices.Equal(got, []int{1, 2}) {
		t.Errorf("a sequence arrived as %v, want [1 2]", got)
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

func TestViewTicksFollowTheTestClock(t *testing.T) {
	t.Parallel()
	p, c := counterPanel(t)
	p.Click("view:counter:inc")
	start := time.Now()
	p.Key("t")
	if len(c.ticks) != 0 {
		t.Fatalf("a tick arrived before time moved: %v", c.ticks)
	}
	p.Advance(3500 * time.Millisecond)
	if len(c.ticks) != 3 {
		t.Fatalf("after 3.5 s, %d ticks, want 3", len(c.ticks))
	}
	for i, at := range c.ticks {
		if want := time.Duration(i+1) * time.Second; at.Sub(c.ticks[0].Add(-time.Second)) != want {
			t.Errorf("tick %d at %v, want +%v", i, at, want)
		}
	}
	p.Advance(500 * time.Millisecond) // the re-armed fourth falls due at 4 s
	if len(c.ticks) != 4 {
		t.Errorf("after 4 s, %d ticks, want 4", len(c.ticks))
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("the ticks slept for real: %v", took)
	}
}

func TestViewFocusedFromTheStartIsTold(t *testing.T) {
	t.Parallel()
	c := &counter{}
	src := strings.Replace(withView, "fields = [ { key = \"ON\", type = \"bool\", label = \"Enabled\", default = true } ]\n[[section]]\n", "", 1)
	p := paneltest.New(t, load(t, src), paneltest.Config{
		Views: map[string]panel.View{"counter": c},
	})
	if p.Focused() != "view:counter" || !c.focused {
		t.Errorf("focus %q, view told %v", p.Focused(), c.focused)
	}
}

func TestStoreCreateComesFromTheSpec(t *testing.T) {
	t.Parallel()
	src := strings.Replace(settings, `apply = ["restart-it"]`, "create = false", 1)
	p := paneltest.New(t, load(t, src), paneltest.Config{}) // an empty file stands in for the shipped one
	p.Click("field:ON")
	p.Key("ctrl+s")
	if got := p.StoreText("c"); got != "ON=false\n" {
		t.Fatalf("store = %q", got)
	}
	missing := t.TempDir() + "/gone.env"
	q := paneltest.New(t, load(t, src), paneltest.Config{Stores: map[string]string{"c": missing}})
	q.Click("field:ON")
	q.Key("ctrl+s")
	q.RequireFrame("save failed")
}
