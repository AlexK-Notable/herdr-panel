// Package ui is the panel itself: a Bubble Tea model that draws a spec's
// sections, answers the keyboard and the mouse, edits fields, saves stores and
// runs actions.
package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/internal/run"
	"github.com/AlexK-Notable/herdr-panel/internal/store"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

// Options configure a Model.
type Options struct {
	Spec    *spec.Spec
	LoadErr error             // the spec failed to load; the panel shows why
	Context map[string]string // {ctx.*} values
	Via     string            // PANEL_VIA for commands
	Runner  run.Runner        // nil runs commands for real
	Theme   *Theme            // nil for DefaultTheme
	// NoTimers turns off polling. observe drives refreshes itself, and a
	// frame then depends only on the input it was given.
	NoTimers bool
	// Stores overrides the store built for each spec store, by name.
	Stores map[string]store.Store
	// CrashDir is where a crash log is written; "" for the temp directory.
	CrashDir string
	Parent   context.Context //nolint:containedctx // the panel's lifetime; cancelled on quit
}

type status struct {
	text string
	kind int // statusInfo, statusOK, statusErr
}

const (
	statusInfo = iota
	statusOK
	statusErr
)

type storeState struct {
	def      *spec.Store
	st       store.Store
	path     string
	snap     store.Snapshot
	loaded   bool
	loadErr  error
	edits    map[string]store.Change
	saving   bool
	applyErr string // saved, but its apply command failed
}

// Model is the panel. It is used through a pointer.
type Model struct {
	opts   Options
	spec   *spec.Spec
	theme  Theme
	runner run.Runner
	ctx    context.Context //nolint:containedctx // cancelled on quit: kills running commands
	cancel context.CancelFunc

	stores map[string]*storeState
	lists  map[int]*listState
	texts  map[int]*textState
	busy   map[string]bool // action element ids running now

	width, height int
	scroll        int
	focus         string
	editing       *fieldEdit
	filterActive  bool // keys go to the focused list's filter
	dialog        *dialog
	status        status

	frame *frame // the last frame drawn: what a click refers to

	crash    *crash
	quitting bool
	// ExitCode is set when the panel quits: 0, or 1 after a crash.
	ExitCode int
}

type fieldEdit struct {
	field *spec.Field
	ed    *editor
}

// New builds a panel. It does no I/O; Init starts loading.
func New(opts Options) *Model {
	th := DefaultTheme()
	if opts.Theme != nil {
		th = *opts.Theme
	}
	r := opts.Runner
	if r == nil {
		r = run.Exec{}
	}
	parent := opts.Parent
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	m := &Model{
		opts: opts, spec: opts.Spec, theme: th, runner: r, ctx: ctx, cancel: cancel,
		stores: map[string]*storeState{}, lists: map[int]*listState{}, texts: map[int]*textState{},
		busy: map[string]bool{},
	}
	if m.spec == nil {
		return m
	}
	for _, sd := range m.spec.Stores {
		ss := &storeState{def: sd, edits: map[string]store.Change{}}
		if o, ok := opts.Stores[sd.Name]; ok {
			ss.st = o
		} else if p, err := spec.ExpandPath(sd.Path, m.values(nil), m.spec.Dir); err != nil {
			ss.loadErr = fmt.Errorf("store %s: path %s: %w", sd.Name, sd.Path, err)
		} else {
			ss.path = p
			ss.st = &store.EnvFile{Path: p, Create: sd.Create}
		}
		m.stores[sd.Name] = ss
	}
	for i, sec := range m.spec.Sections {
		switch sec.Kind {
		case spec.ListSection:
			m.lists[i] = &listState{sec: sec, idx: i}
		case spec.TextSection:
			if sec.Source != nil {
				m.texts[i] = &textState{sec: sec, idx: i}
			}
		case spec.FieldsSection, spec.ButtonsSection:
		}
	}
	if order := m.focusOrder(); len(order) > 0 {
		m.focus = order[0]
	}
	return m
}

// Init loads the stores and starts the lists.
func (m *Model) Init() (cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			m.crash = m.recordCrash(r)
			cmd = nil
		}
	}()
	if m.spec == nil {
		return nil
	}
	var cmds []tea.Cmd
	for name := range m.stores {
		cmds = append(cmds, m.loadStore(name))
	}
	for i := range m.lists {
		cmds = append(cmds, m.refreshList(i))
	}
	for i := range m.texts {
		cmds = append(cmds, m.refreshText(i))
	}
	return tea.Batch(cmds...)
}

// Quit ends the panel from outside, as a signal handler does: running
// commands are cancelled and Bubble Tea is told to stop.
func (m *Model) Quit() tea.Cmd {
	m.quitting = true
	m.cancel()
	return tea.Quit
}

// Dirty reports how many edits are unsaved.
func (m *Model) Dirty() int {
	n := 0
	for _, ss := range m.stores {
		n += len(ss.edits)
	}
	return n
}

// Status returns the status line's text, for observe and tests.
func (m *Model) Status() string { return m.status.text }

// Size is the terminal size the panel last drew at.
func (m *Model) Size() (int, int) { return m.width, m.height }

// Focused returns the id of the focused element.
func (m *Model) Focused() string { return m.focus }

func (m *Model) setStatus(kind int, format string, args ...any) {
	m.status = status{text: fmt.Sprintf(format, args...), kind: kind}
}

// crashLogPath is where a crash is written.
func (m *Model) crashLogPath() string {
	dir := m.opts.CrashDir
	if dir == "" {
		dir = filepath.Join(tempDir(), "herdr-panel")
	}
	return filepath.Join(dir, "crash-"+time.Now().Format("20060102-150405")+".log")
}
