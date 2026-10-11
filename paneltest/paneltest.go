// Package paneltest drives a panel in a Go test, without a terminal: by
// element id, the way a user would, with every command answered by a stub
// instead of being run.
//
//	p := paneltest.New(t, s, paneltest.Config{
//		Stubs: []paneltest.Stub{{Match: []string{"my-tool", "rows"}, Stdout: `[{"id":"a"}]`}},
//	})
//	p.Click("field:VERBOSE")
//	p.Key("ctrl+s")
//	p.RequireFrame("✓ saved")
//	p.Golden()
//
// Element ids are the ones `herdr-panel observe` reports: "field:<id>",
// "list:<section>", "row:<section>:<row>", "button:<section>:<n>",
// "rowaction:<section>:<n>", "view:<name>:<region>", "save", "close" and,
// while a dialog is open, "dialog:<n>".
package paneltest

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/internal/bridge"
	"github.com/AlexK-Notable/herdr-panel/internal/observe"
	"github.com/AlexK-Notable/herdr-panel/internal/store"
	"github.com/AlexK-Notable/herdr-panel/panel"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

// Config sets up a panel under test.
type Config struct {
	// Width and Height are the terminal size; 80x24 when zero.
	Width, Height int
	// Views supply the spec's sections of kind "view", by name.
	Views map[string]panel.View
	// Theme styles the panel; nil uses panel.DefaultTheme.
	Theme *panel.Theme
	// Context supplies {ctx.*} values. nil means no herdr context, so a test
	// does not depend on the environment it runs in.
	Context map[string]string
	// Via is passed to every command as PANEL_VIA.
	Via string
	// Stubs answer commands whose argv starts with Match. A command no stub
	// matches succeeds with no output. Nothing is ever run.
	Stubs []Stub
	// Stores maps a store name to the file the test uses for it. A store
	// left out gets a path in t.TempDir(), never the spec's: with no file
	// when the spec lets the panel create it, else with an empty one, as the
	// plugin would ship. The spec's create setting applies either way.
	Stores map[string]string
}

// Stub is a canned answer for a command. The first element of Match
// matches by base name when it has no slash: "tool" matches "/bin/tool".
type Stub struct {
	Match          []string
	Stdout, Stderr string
	Exit           int
}

// Call is a command the panel asked to run: its arguments and the PANEL_*
// variables it would have received.
type Call struct {
	Argv []string
	Env  []string
}

// Element is a clickable element of the current frame, in cells from the
// top-left corner.
type Element struct {
	ID      string
	Kind    string // field, button, row, view, ...
	Label   string
	X, Y    int
	W, H    int
	Focused bool
}

// Panel is a panel under test. Its methods fail the test on error.
type Panel struct {
	t      testing.TB
	d      *observe.Driver
	sb     *observe.Sandbox
	stores map[string]string
}

// New builds a panel from a validated spec and runs its startup: stores are
// loaded and list sources answered before New returns. Timers are off, so
// a frame depends only on what the test did.
func New(tb testing.TB, s *spec.Spec, cfg Config) *Panel {
	tb.Helper()
	opts := panel.Options{Views: cfg.Views, Theme: cfg.Theme, Context: cfg.Context, Via: cfg.Via, CrashDir: tb.TempDir()}
	if opts.Context == nil {
		opts.Context = map[string]string{}
	}
	uopts, err := bridge.UIOptions(s, opts)
	if err != nil {
		tb.Fatalf("paneltest: %v", err)
	}
	stubs := &observe.Stubs{}
	for _, st := range cfg.Stubs {
		stubs.Commands = append(stubs.Commands, observe.Stub{Match: st.Match, Stdout: st.Stdout, Stderr: st.Stderr, Exit: st.Exit})
	}
	sb := &observe.Sandbox{Stubs: stubs}
	uopts.Runner = sb
	p := &Panel{t: tb, sb: sb, stores: map[string]string{}}
	uopts.Stores = map[string]store.Store{}
	dir := tb.TempDir()
	for _, st := range s.Stores {
		path, ok := cfg.Stores[st.Name]
		if !ok {
			path = filepath.Join(dir, st.Name+".env")
			if !st.Create {
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					tb.Fatalf("paneltest: %v", err)
				}
			}
		}
		p.stores[st.Name] = path
		uopts.Stores[st.Name] = &store.EnvFile{Path: path, Create: st.Create}
	}
	w, h := cfg.Width, cfg.Height
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	p.d = observe.New(uopts, w, h)
	p.check()
	return p
}

func (p *Panel) check() {
	p.t.Helper()
	if errs := p.d.Errors(); len(errs) > 0 {
		p.t.Fatalf("paneltest: %s", strings.Join(errs, "; "))
	}
	if p.d.Model().Crashed() {
		p.t.Fatalf("paneltest: the panel crashed:\n%s", p.Frame())
	}
}

// Click clicks the middle of an element, as a mouse would. It fails if the
// element is not on screen, or something else covers its middle.
func (p *Panel) Click(id string) {
	p.t.Helper()
	if err := p.d.Click(id); err != nil {
		p.t.Fatalf("paneltest: %v", err)
	}
	p.check()
}

// Key presses keys, named as Bubble Tea names them: "enter", "ctrl+s", "a".
func (p *Panel) Key(names ...string) {
	p.t.Helper()
	for _, n := range names {
		if err := p.d.Key(n); err != nil {
			p.t.Fatalf("paneltest: %v", err)
		}
	}
	p.check()
}

// Type types text, one key per character.
func (p *Panel) Type(s string) {
	p.t.Helper()
	p.d.Type(s)
	p.check()
}

// Paste pastes text, as a terminal with bracketed paste does.
func (p *Panel) Paste(s string) {
	p.t.Helper()
	p.d.Paste(s)
	p.check()
}

// Resize changes the terminal size.
func (p *Panel) Resize(w, h int) {
	p.t.Helper()
	p.d.Resize(w, h)
	p.check()
}

// Advance moves the panel's clock forward by d. Time stands still
// otherwise: list polling and a View's panel.Tick fire only here, in the
// order they fall due, including ticks re-armed on the way.
func (p *Panel) Advance(d time.Duration) {
	p.t.Helper()
	p.d.Advance(d)
	p.check()
}

// Send delivers any message, for what the methods above do not cover.
func (p *Panel) Send(msg tea.Msg) {
	p.t.Helper()
	p.d.Send(msg)
	p.check()
}

// Frame is the screen as plain text, one line per row.
func (p *Panel) Frame() string { return p.d.Model().Frame() }

// Elements are the clickable elements on screen.
func (p *Panel) Elements() []Element {
	var out []Element
	for _, e := range p.d.Model().Elements() {
		out = append(out, Element{ID: e.ID, Kind: e.Kind, Label: e.Label, X: e.X, Y: e.Y, W: e.W, H: e.H, Focused: e.Focused})
	}
	return out
}

// Element returns the element with an id, if it is on screen.
func (p *Panel) Element(id string) (Element, bool) {
	i := slices.IndexFunc(p.Elements(), func(e Element) bool { return e.ID == id })
	if i < 0 {
		return Element{}, false
	}
	return p.Elements()[i], true
}

// Focused is the id of the element with the keyboard focus.
func (p *Panel) Focused() string { return p.d.Model().Focused() }

// Status is the status line's text.
func (p *Panel) Status() string { return p.d.Model().Status() }

// Dirty is the number of unsaved edits.
func (p *Panel) Dirty() int { return p.d.Model().Dirty() }

// Closed reports whether the panel has closed itself.
func (p *Panel) Closed() bool { return p.d.Quit }

// Calls are the commands the panel asked to run, in order.
func (p *Panel) Calls() []Call {
	var out []Call
	for _, c := range p.sb.Calls() {
		out = append(out, Call{Argv: c.Argv, Env: c.Env})
	}
	return out
}

// StorePath is the file the test uses for a store.
func (p *Panel) StorePath(name string) string {
	p.t.Helper()
	path, ok := p.stores[name]
	if !ok {
		p.t.Fatalf("paneltest: the spec has no store %q", name)
	}
	return path
}

// StoreText is the content of a store's file, "" when it does not exist.
func (p *Panel) StoreText(name string) string {
	p.t.Helper()
	data, err := os.ReadFile(p.StorePath(name))
	if err != nil && !os.IsNotExist(err) {
		p.t.Fatalf("paneltest: %v", err)
	}
	return string(data)
}

// RequireFrame fails the test unless the frame contains every string.
func (p *Panel) RequireFrame(want ...string) {
	p.t.Helper()
	fr := p.Frame()
	for _, w := range want {
		if !strings.Contains(fr, w) {
			p.t.Fatalf("paneltest: the frame lacks %q:\n%s", w, fr)
		}
	}
}

// Golden compares the frame with testdata/<test name>.golden, and fails the
// test when they differ. With PANELTEST_UPDATE=1 in the environment it
// writes the file instead.
func (p *Panel) Golden() {
	p.t.Helper()
	path := filepath.Join("testdata", strings.ReplaceAll(p.t.Name(), "/", "_")+".golden")
	got := []byte(p.Frame() + "\n")
	if os.Getenv("PANELTEST_UPDATE") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			p.t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			p.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // G304: the test's own golden file
	if err != nil {
		p.t.Fatalf("paneltest: %v (run with PANELTEST_UPDATE=1 to create it)", err)
	}
	if !bytes.Equal(got, want) {
		p.t.Fatalf("paneltest: the frame differs from %s (PANELTEST_UPDATE=1 rewrites it)\n--- want\n%s--- got\n%s", path, want, got)
	}
}
