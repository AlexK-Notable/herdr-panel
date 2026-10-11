package ui

import (
	"context"
	"os"
	"strings"
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
	m.lists = nil                                                          // make the next list message panic
	m.Update(rowsMsg{sec: 0})
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
