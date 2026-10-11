// Package observe drives a panel without a terminal: it feeds messages to
// the model, runs the commands the model returns until nothing is left, and
// reads the frame and its clickable elements from the same compositor that
// drew them. An agent or a test can then act on a panel by element id.
package observe

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/internal/ui"
)

// maxSteps bounds the messages one action may cause, so a panel that keeps
// asking for more work cannot hang the driver.
const maxSteps = 10000

// Driver runs one panel headlessly.
type Driver struct {
	m      *ui.Model
	Quit   bool
	steps  int
	errors []string
}

// New builds a panel at a size and runs its startup to completion.
// opts.NoTimers is forced on: a frame must depend only on its input.
func New(opts ui.Options, w, h int) *Driver {
	opts.NoTimers = true
	d := &Driver{m: ui.New(opts)}
	d.send(tea.WindowSizeMsg{Width: w, Height: h})
	d.run(d.m.Init())
	d.m.View()
	return d
}

// Model returns the panel.
func (d *Driver) Model() *ui.Model { return d.m }

func (d *Driver) send(msg tea.Msg) {
	_, cmd := d.m.Update(msg)
	d.run(cmd)
}

func (d *Driver) run(cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		d.steps++
		if d.steps > maxSteps {
			d.errors = append(d.errors, "stopped: the panel kept producing work")
			return
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case tea.QuitMsg:
			d.Quit = true
		default:
			_, next := d.m.Update(msg)
			queue = append(queue, next)
		}
	}
}

// Send delivers a message and redraws.
func (d *Driver) Send(msg tea.Msg) {
	d.steps = 0
	d.send(msg)
	d.m.View()
}

// Resize changes the terminal size.
func (d *Driver) Resize(w, h int) { d.Send(tea.WindowSizeMsg{Width: w, Height: h}) }

// Key presses a key named as Bubble Tea names it: "enter", "ctrl+s", "a".
func (d *Driver) Key(name string) error {
	k, err := KeyMsg(name)
	if err != nil {
		return err
	}
	d.Send(k)
	return nil
}

// Type types text, one key per character.
func (d *Driver) Type(s string) {
	for _, r := range s {
		d.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// Paste pastes text the way a terminal with bracketed paste does.
func (d *Driver) Paste(s string) { d.Send(tea.PasteMsg{Content: s}) }

// Click clicks the middle of an element of the current frame.
func (d *Driver) Click(id string) error {
	for _, e := range d.m.Elements() {
		if e.ID == id {
			x, y := e.X+e.W/2, e.Y+e.H/2
			if got := d.m.Hit(x, y); got != id {
				return fmt.Errorf("element %s is covered by %q at %d,%d", id, got, x, y)
			}
			d.Send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			return nil
		}
	}
	return fmt.Errorf("no element %q on screen; there are: %s", id, strings.Join(d.IDs(), " "))
}

// IDs lists the elements on screen.
func (d *Driver) IDs() []string {
	var ids []string
	for _, e := range d.m.Elements() {
		ids = append(ids, e.ID)
	}
	return ids
}

// Errors reports problems the driver itself ran into.
func (d *Driver) Errors() []string { return slices.Clone(d.errors) }

var named = map[string]rune{
	"enter": tea.KeyEnter, "tab": tea.KeyTab, "esc": tea.KeyEscape, "backspace": tea.KeyBackspace,
	"delete": tea.KeyDelete, "up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "space": tea.KeySpace,
}

// KeyMsg builds the key press Bubble Tea would deliver for a key name.
func KeyMsg(name string) (tea.KeyPressMsg, error) {
	var k tea.KeyPressMsg
	parts := strings.Split(name, "+")
	base := parts[len(parts)-1]
	if base == "" && strings.HasSuffix(name, "+") { // "ctrl++"
		base = "+"
	}
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "ctrl":
			k.Mod |= tea.ModCtrl
		case "alt":
			k.Mod |= tea.ModAlt
		case "shift":
			k.Mod |= tea.ModShift
		default:
			return k, fmt.Errorf("unknown modifier %q in key %q", p, name)
		}
	}
	if code, ok := named[base]; ok {
		k.Code = code
		if base == "space" && k.Mod == 0 {
			k.Text = " "
		}
	} else if r, size := utf8.DecodeRuneInString(base); size == len(base) && r != utf8.RuneError {
		k.Code = r
		if k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			k.Text = base
		}
	} else if len(base) >= 2 && base[0] == 'f' {
		var n int
		if _, err := fmt.Sscanf(base, "f%d", &n); err != nil || n < 1 || n > 20 {
			return k, fmt.Errorf("unknown key %q", name)
		}
		k.Code = tea.KeyF1 + rune(n-1)
	} else {
		return k, fmt.Errorf("unknown key %q", name)
	}
	if got := k.String(); got != name {
		return k, fmt.Errorf("key %q would arrive as %q", name, got)
	}
	return k, nil
}
