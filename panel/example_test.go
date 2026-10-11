package panel_test

import (
	"context"
	"fmt"
	"log"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/panel"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

// clock is a View: one line of text and one clickable region.
type clock struct{ ticks int }

type tick struct{}

func (c *clock) Render(in panel.RenderInput) panel.RenderOutput {
	line := fmt.Sprintf("%d ticks  %s", c.ticks, in.Theme.Button.Render("[reset]"))
	if in.Focused {
		line = in.Theme.Focus.Render(line)
	}
	return panel.RenderOutput{
		Lines:   []string{line},
		Regions: []panel.Region{{ID: "reset", Label: "Reset", Line: 0, X: len(strconv.Itoa(c.ticks)) + 8, W: 7}},
	}
}

func (c *clock) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case panel.Click:
		if msg.Region == "reset" {
			c.ticks = 0
		}
	case panel.Focus:
		if msg.Focused {
			return func() tea.Msg { return tick{} } // comes back to this view only
		}
	case tick:
		c.ticks++
	}
	return nil
}

// A spec places a view by name; the program supplies it.
func Example_view() {
	s, err := spec.Parse([]byte(`
spec = 1
title = "Clock"
[[section]]
title = "Ticks"
kind = "view"
view = "clock"
`), "clock.toml", ".")
	if err != nil {
		log.Fatal(err)
	}
	err = panel.Run(context.Background(), s, panel.Options{
		Views: map[string]panel.View{"clock": &clock{}},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// A spec built in Go instead of TOML: Validate fills in the defaults.
func Example_specInGo() {
	s := &spec.Spec{
		Title:  "Settings",
		Stores: []*spec.Store{{Name: "c", Kind: spec.EnvFile, Path: spec.MustTemplate("~/.config/my-plugin/config.env"), Create: true}},
		Sections: []*spec.Section{{Fields: []*spec.Field{
			{Key: "VERBOSE", Type: spec.Bool, Label: "Verbose logging"},
		}}},
	}
	if err := spec.Validate(s); err != nil {
		log.Fatal(err)
	}
	fmt.Println(s.Fields()[0].ID, s.Fields()[0].Store, s.Fields()[0].TrueValue)
	// Output: VERBOSE c true
}
