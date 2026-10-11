package ui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/internal/observe"
	"github.com/AlexK-Notable/herdr-panel/internal/ui"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

const viewSpec = `spec = 1
title = "V"
[[section]]
kind = "view"
view = "v"
[[section]]
kind = "buttons"
buttons = [ { label = "Go", run = ["go-thing"] } ]
`

// boom is a view that panics where it is told to.
type boom struct{ where string }

func (b boom) Render(int, bool) ui.ViewOutput {
	if b.where == "render" {
		panic("render exploded")
	}
	return ui.ViewOutput{
		Lines:   []string{"\x1b[1mbold\x1b[0m [a] [b]", "wide 世界 [c]"},
		Regions: []ui.ViewRegion{{ID: "a", Line: 0, X: 5, W: 3}, {ID: "b", Line: 0, X: 9, W: 3}, {ID: "c", Line: 1, X: 10, W: 3}, {ID: "off", Line: 0, X: 40, W: 2}},
	}
}

func (b boom) Update(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(ui.ViewClick); !ok {
		return nil
	}
	switch b.where {
	case "update":
		panic("update exploded")
	case "cmd":
		return func() tea.Msg { panic("command exploded") }
	}
	return nil
}

func viewDriver(t *testing.T, v ui.View) *observe.Driver {
	t.Helper()
	s, err := spec.Parse([]byte(viewSpec), "v.toml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return observe.New(ui.Options{Spec: s, Runner: &observe.Sandbox{}, CrashDir: t.TempDir(), Views: map[string]ui.View{"v": v}}, 60, 16)
}

func TestViewPanicsShowTheCrashScreen(t *testing.T) {
	t.Parallel()
	for _, where := range []string{"render", "update", "cmd"} {
		d := viewDriver(t, boom{where})
		if where != "render" {
			if err := d.Click("view:v:a"); err != nil {
				t.Fatal(err)
			}
		}
		if !d.Model().Crashed() || !strings.Contains(d.Model().Frame(), "exploded") {
			t.Errorf("%s: no crash screen:\n%s", where, d.Model().Frame())
		}
	}
}

func TestViewRegionsHitThemselves(t *testing.T) {
	t.Parallel()
	d := viewDriver(t, boom{})
	got := map[string]bool{}
	for _, e := range d.Model().Elements() {
		got[e.ID] = true
		if hit := d.Model().Hit(e.X+e.W/2, e.Y); hit != e.ID {
			t.Errorf("centre of %s hits %q", e.ID, hit)
		}
	}
	// "off" lies past the end of its line and is dropped; styles and wide
	// characters do not shift the cells.
	if !got["view:v:a"] || !got["view:v:b"] || !got["view:v:c"] || got["view:v:off"] {
		t.Errorf("elements = %v", d.IDs())
	}
	for _, e := range d.Model().Elements() {
		if e.ID == "view:v:c" && e.X != 11 { // one cell of indent, then 10
			t.Errorf("region c at x %d, want 11", e.X)
		}
	}
}

func TestMissingViewIsShownNotCrashed(t *testing.T) {
	t.Parallel()
	s, err := spec.Parse([]byte(viewSpec), "v.toml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := observe.New(ui.Options{Spec: s, Runner: &observe.Sandbox{}}, 60, 16)
	if !strings.Contains(d.Model().Frame(), "no view v was supplied") {
		t.Errorf("frame:\n%s", d.Model().Frame())
	}
}
