package ui

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/internal/run"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

const listOnly = `spec = 1
title = "L"
[[section]]
kind = "list"
source = { run = ["rows"], every_s = 5 }
columns = ["name"]
`

type fixedRunner struct{ out string }

func (r fixedRunner) Run(context.Context, run.Request) run.Result {
	res := run.Result{Stdout: []byte(r.out)}
	run.Interpret(&res)
	return res
}

func newList(t *testing.T, out string) *Model {
	t.Helper()
	s, err := spec.Parse([]byte(listOnly), "t.toml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{Spec: s, Runner: fixedRunner{out}, NoTimers: true})
}

// A slow source must never pile up overlapping runs.
func TestOnePollInFlight(t *testing.T) {
	t.Parallel()
	m := newList(t, `[]`)
	first := m.refreshList(0)
	if first == nil {
		t.Fatal("no request")
	}
	if second := m.refreshList(0); second != nil {
		t.Error("a second request started while one was out")
	}
}

// An answer to an older request must not overwrite a newer one.
func TestStaleAnswerDropped(t *testing.T) {
	t.Parallel()
	m := newList(t, `[]`)
	ls := m.lists[0]
	ls.seq = 5
	m.onRows(rowsMsg{sec: 0, seq: 4, rows: []row{{id: "old"}}})
	if len(ls.rows) != 0 {
		t.Error("stale rows applied")
	}
	m.onRows(rowsMsg{sec: 0, seq: 5, rows: []row{{id: "new", vals: map[string]string{"name": "new"}}}})
	if len(ls.rows) != 1 || ls.rows[0].id != "new" {
		t.Errorf("rows = %+v", ls.rows)
	}
}

// A refresh asked for while a load is out (after a row action, say) runs
// once that load answers: the load may predate the change.
func TestRefreshDuringALoadRunsAfterIt(t *testing.T) {
	t.Parallel()
	m := newList(t, `[]`)
	first := m.refreshList(0)
	if m.refreshList(0) != nil {
		t.Fatal("two loads at once")
	}
	msg, ok := first().(rowsMsg)
	if !ok {
		t.Fatalf("the load answered %T", first())
	}
	if m.onRows(msg) == nil {
		t.Fatal("the refresh asked for during the load was dropped")
	}
	if m.lists[0].stale || !m.lists[0].loading {
		t.Error("the follow-up refresh did not start")
	}
}

func TestStatusDropsEscapes(t *testing.T) {
	t.Parallel()
	m := newList(t, `[]`)
	m.setStatus(statusErr, "✗ %s", "bad\x1b[2J\x1b]0;title\x07\nnews")
	if m.status.text != "✗ bad news" {
		t.Errorf("status = %q", m.status.text)
	}
}

func TestSelectionFollowsRowIDAcrossRefresh(t *testing.T) {
	t.Parallel()
	m := newList(t, `[]`)
	ls := m.lists[0]
	r := func(id string) row { return row{id: id, vals: map[string]string{"name": id}} }
	ls.seq = 1
	m.onRows(rowsMsg{sec: 0, seq: 1, rows: []row{r("a"), r("b"), r("c")}})
	ls.move(2) // c
	ls.seq = 2
	m.onRows(rowsMsg{sec: 0, seq: 2, rows: []row{r("c"), r("a"), r("b")}})
	if got, _ := ls.selected(); got.id != "c" {
		t.Errorf("selected %q after reorder, want c", got.id)
	}
	ls.seq = 3
	m.onRows(rowsMsg{sec: 0, seq: 3, rows: []row{r("a"), r("b")}})
	if got, _ := ls.selected(); got.id == "" {
		t.Error("selection lost when its row went away")
	}
}

func TestPollingBacksOffOnFailure(t *testing.T) {
	t.Parallel()
	s, _ := spec.Parse([]byte(listOnly), "t.toml", t.TempDir())
	m := New(Options{Spec: s, Runner: fixedRunner{`[]`}})
	ls := m.lists[0]
	ls.seq = 1
	ls.loading = true
	if cmd := m.onRows(rowsMsg{sec: 0, seq: 1, err: "down"}); cmd == nil || ls.failures != 1 {
		t.Fatalf("no retry scheduled (failures %d)", ls.failures)
	}
	if every := ls.sec.Every << min(ls.failures, 4); every != 10*time.Second {
		t.Errorf("backoff %v, want 10s", every)
	}
}

// A panic inside a command becomes the crash screen, with a log, instead of
// a popup that vanishes.
func TestPanicInCommandShowsCrashScreen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, _ := spec.Parse([]byte(listOnly), "t.toml", dir)
	m := New(Options{Spec: s, Runner: panicRunner{}, NoTimers: true, CrashDir: dir})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	msg := m.refreshList(0)()
	m.Update(msg)
	m.View()
	fr := m.Frame()
	if !strings.Contains(fr, "herdr-panel crashed") || !strings.Contains(fr, "panic: boom in a source") {
		t.Fatalf("frame:\n%s", fr)
	}
	if m.crash.log == "" {
		t.Fatal("no crash log")
	}
	data, err := os.ReadFile(m.crash.log)
	if err != nil || !strings.Contains(string(data), "boom in a source") || !strings.Contains(string(data), "goroutine") {
		t.Errorf("crash log: %v\n%s", err, data)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if cmd == nil || m.ExitCode != 1 {
		t.Error("a key did not close the crash screen with exit code 1")
	}
}

type panicRunner struct{}

func (panicRunner) Run(context.Context, run.Request) run.Result { panic("boom in a source") }

func TestPanicInUpdateShowsCrashScreen(t *testing.T) {
	t.Parallel()
	s, _ := spec.Parse([]byte(listOnly), "t.toml", t.TempDir())
	m := New(Options{Spec: s, NoTimers: true, CrashDir: t.TempDir()})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(rowsMsg{sec: 0, seq: m.lists[0].seq, rows: []row{{id: "x"}}}) // nil vals map is fine
	// onRows reaches ls.sec.Every: a panic inside Update, not View.
	m.lists[0].sec = nil
	got, _ := m.Update(rowsMsg{sec: 0, seq: m.lists[0].seq})
	if m.crash == nil {
		t.Fatal("the message did not panic inside Update; the test proves nothing")
	}
	if got != m {
		t.Fatalf("after a panic Update returned %v; Bubble Tea would draw that, not the crash screen", got)
	}
	m.View()
	if !strings.Contains(m.Frame(), "herdr-panel crashed") {
		t.Fatalf("frame:\n%s", m.Frame())
	}
}

func TestQuitCancelsRunningCommands(t *testing.T) {
	t.Parallel()
	s, _ := spec.Parse([]byte(listOnly), "t.toml", t.TempDir())
	m := New(Options{Spec: s, NoTimers: true})
	m.Quit()
	if m.ctx.Err() == nil {
		t.Error("quitting did not cancel the command context")
	}
}

func TestFormatAndParseDuration(t *testing.T) {
	t.Parallel()
	for secs, want := range map[int64]string{0: "0s", 45: "45s", 60: "1m", 90: "1m30s", 3600: "1h", 3725: "1h2m5s"} {
		if got := formatDuration(secs); got != want {
			t.Errorf("formatDuration(%d) = %q, want %q", secs, got, want)
		}
		if back, err := parseDuration(want); err != nil || back != secs {
			t.Errorf("parseDuration(%q) = %d, %v", want, back, err)
		}
	}
	if _, err := parseDuration("1.5s"); err == nil {
		t.Error("accepted a fractional second")
	}
}

const liveSpec = `spec = 1
title = "Live"
save = "live"
[stores.c]
kind = "envfile"
path = "c.env"
create = true
[[section]]
fields = [ { key = "A", type = "int", label = "A" }, { key = "B", type = "int", label = "B" } ]
`

// runAll runs a command and everything it leads to, synchronously.
func runAll(m *Model, cmd tea.Cmd) {
	for queue := []tea.Cmd{cmd}; len(queue) > 0; {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if cmds, _, ok := Unpack(msg); ok {
			queue = append(queue, cmds...)
			continue
		}
		if msg != nil {
			_, next := m.Update(msg)
			queue = append(queue, next)
		}
	}
}

func liveModel(t *testing.T) *Model {
	t.Helper()
	dir := t.TempDir()
	s, err := spec.Parse([]byte(liveSpec), "t.toml", dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Spec: s, NoTimers: true, Runner: fixedRunner{}, CrashDir: dir})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	runAll(m, m.Init())
	return m
}

// In live mode, an edit made while the previous save is in flight is saved
// too, once that save lands.
func TestLiveEditDuringSaveIsSaved(t *testing.T) {
	t.Parallel()
	m := liveModel(t)
	a, b := m.spec.Fields()[0], m.spec.Fields()[1]
	first := m.setField(a, "1", false)
	if second := m.setField(b, "2", false); second != nil {
		t.Fatal("a second save started while the first was in flight")
	}
	runAll(m, first)
	data, _ := os.ReadFile(m.stores["c"].path)
	if string(data) != "A=1\nB=2\n" || m.Dirty() != 0 {
		t.Errorf("file %q, %d unsaved", data, m.Dirty())
	}
}

// A load that read the file before a save wrote it, but answers after the
// save, must not put the old values back (and then fake a conflict).
func TestStaleLoadAfterSaveIsDropped(t *testing.T) {
	t.Parallel()
	m := liveModel(t)
	a := m.spec.Fields()[0]
	stale := m.loadStore("c")() // reads the file before the save
	runAll(m, m.setField(a, "7", false))
	m.Update(stale)
	if v, src := m.fieldValue(a); v != "7" || src != fromStore {
		t.Fatalf("after the stale load A = %q (source %d)", v, src)
	}
	runAll(m, m.setField(a, "8", false))
	if m.dialog != nil {
		t.Errorf("the panel's own save looked like a conflict: %q", m.dialog.title)
	}
}

// slowStop answers a cancel only after a while, as a process group does
// while it is being killed.
type slowStop struct{ stopped *atomic.Bool }

func (s slowStop) Run(ctx context.Context, _ run.Request) run.Result {
	<-ctx.Done()
	time.Sleep(20 * time.Millisecond)
	s.stopped.Store(true)
	return run.Result{}
}

func TestWaitCommandsOutlastsTheKill(t *testing.T) {
	t.Parallel()
	s, _ := spec.Parse([]byte(listOnly), "t.toml", t.TempDir())
	stopped := &atomic.Bool{}
	m := New(Options{Spec: s, Runner: slowStop{stopped}, NoTimers: true})
	cmd := m.refreshList(0)
	go cmd()
	for m.running.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	m.Quit()
	if !m.WaitCommands(time.Second) || !stopped.Load() {
		t.Fatal("WaitCommands returned before the command ended")
	}
	if !m.WaitCommands(0) {
		t.Error("nothing is running, yet WaitCommands timed out")
	}
}

// An untitled text shows nothing while its source has not answered; a
// titled one shows its title and "loading…".
func TestTextLoadingState(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		title string
		line3 string // the first line under the header
		has   string // in the frame
		not   string // not in the frame
	}{
		{"", "General", "General", "loading"},
		{`title = "Notes"`, "Notes", "loading…", ""},
	} {
		src := "spec = 1\ntitle = \"T\"\n[[section]]\n" + c.title + "\nkind = \"text\"\nsource = [\"say\"]\n" +
			"[[section]]\ntitle = \"General\"\nkind = \"text\"\ntext = \"x\"\n"
		s, err := spec.Parse([]byte(src), "t.toml", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		m := New(Options{Spec: s, Runner: fixedRunner{"late"}, NoTimers: true})
		m.width, m.height = 60, 12
		if m.texts[0] == nil || m.texts[0].loaded {
			t.Fatal("the source has already answered: this checks nothing")
		}
		m.View()
		fr := m.Frame()
		lines := strings.Split(fr, "\n")
		if !strings.Contains(lines[2], c.line3) || !strings.Contains(fr, c.has) || (c.not != "" && strings.Contains(fr, c.not)) {
			t.Errorf("title %q: line 3 = %q, want %q, and %q shown:\n%s", c.title, lines[2], c.line3, c.has, fr)
		}
	}
}
