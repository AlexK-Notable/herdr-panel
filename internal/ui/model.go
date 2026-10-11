// Package ui is the panel itself: a Bubble Tea model that draws a spec's
// sections, answers the keyboard and the mouse, edits fields, saves stores and
// runs actions.
package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlexK-Notable/herdr-panel/internal/run"
	"github.com/AlexK-Notable/herdr-panel/internal/store"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

// Options configure a Model.
type Options struct {
	Spec    *spec.Spec
	LoadErr error             // the panel cannot start (a bad spec, a missing view); it shows why
	Context map[string]string // {ctx.*} values
	Via     string            // PANEL_VIA for commands
	Runner  run.Runner        // nil runs commands for real
	Theme   *Theme            // nil for DefaultTheme
	// NoTimers parks every timer, the panel's polling and a view's
	// TickRequest alike, on a clock that moves only when an Advance message
	// says so: a frame then depends only on the input it was given.
	NoTimers bool
	// Stores overrides the store built for each spec store, by name.
	Stores map[string]store.Store
	// Views supply the sections of kind "view", by name.
	Views map[string]View
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
	saveGen  int    // counts saves started; a load older than one is dropped
	applyErr string // saved, but its apply command failed
}

// Model is the panel. It is used through a pointer.
type Model struct {
	opts    Options
	spec    *spec.Spec
	theme   Theme
	runner  run.Runner
	running *atomic.Int64   // commands in flight
	ctx     context.Context //nolint:containedctx // cancelled on quit: kills running commands
	cancel  context.CancelFunc

	stores map[string]*storeState
	lists  map[int]*listState
	texts  map[int]*textState
	busy   map[string]bool // action element ids running now

	width, height int
	clock         time.Duration // the parked clock (NoTimers)
	parked        []parkedTick
	tickSeq       int
	scroll        int
	freeScroll    bool // the user scrolled the body: do not pull it back to the focus
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
	var r run.Runner = run.Exec{}
	if opts.Runner != nil {
		r = opts.Runner
	}
	running := &atomic.Int64{}
	r = counted{r: r, n: running}
	parent := opts.Parent
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	m := &Model{
		opts: opts, spec: opts.Spec, theme: th, runner: r, running: running, ctx: ctx, cancel: cancel,
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
		case spec.FieldsSection, spec.ButtonsSection, spec.ViewSection:
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
	cmds = append(cmds, m.viewFocusChange("")) // a view focused from the start is told
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

// CrashLog is where the crash was written, or "" when the panel has not
// crashed or the log could not be written.
func (m *Model) CrashLog() string {
	if m.crash == nil {
		return ""
	}
	return m.crash.log
}

// Crashed reports whether the panel caught a panic and shows its crash
// screen.
func (m *Model) Crashed() bool { return m.crash != nil }

// Focused returns the id of the focused element.
func (m *Model) Focused() string { return m.focus }

func (m *Model) setStatus(kind int, format string, args ...any) {
	// Messages come from commands: no escape sequence or line break of
	// theirs reaches the terminal.
	text := strings.Join(strings.Fields(ansi.Strip(fmt.Sprintf(format, args...))), " ")
	m.status = status{text: text, kind: kind}
}

// crashLogPath is where a crash is written.
func (m *Model) crashLogPath() string {
	dir := m.opts.CrashDir
	if dir == "" {
		dir = filepath.Join(tempDir(), "herdr-panel")
	}
	return filepath.Join(dir, "crash-"+time.Now().Format("20060102-150405")+".log")
}

// counted counts the commands in flight, for WaitCommands.
type counted struct {
	r run.Runner
	n *atomic.Int64
}

func (c counted) Run(ctx context.Context, req run.Request) run.Result {
	c.n.Add(1)
	defer c.n.Add(-1)
	return c.r.Run(ctx, req)
}

// WaitCommands waits up to d for every command in flight to end, and
// reports whether they did. After Quit they are being killed: a process
// that exits before the kill is sent leaves them running.
func (m *Model) WaitCommands(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for m.running.Load() > 0 {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
	return true
}
