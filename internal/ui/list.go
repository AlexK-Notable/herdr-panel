package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlexK-Notable/herdr-panel/internal/run"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

type row struct {
	id   string
	vals map[string]string
}

type listState struct {
	sec      *spec.Section
	idx      int
	rows     []row
	loaded   bool
	err      string
	loading  bool
	stale    bool // a refresh was asked for while loading: run it after
	seq      int  // the newest request; older answers are dropped
	tickGen  int
	failures int
	selID    string
	sel      int
	offset   int
	filter   *editor
	followed string // the selection the sections following this list last saw
	selGen   int    // bumped when it moves; a rested selMsg must match
}

type textState struct {
	sec      *spec.Section
	idx      int
	text     string
	loaded   bool
	err      string
	loading  bool
	stale    bool
	seq      int
	tickGen  int
	failures int
}

type rowsMsg struct {
	sec, seq int
	rows     []row
	err      string
}

type textMsg struct {
	sec, seq int
	text     string
	err      string
}

type tickMsg struct {
	sec, gen int
	text     bool
}

// refreshList asks the source for rows, unless a request is already out:
// a slow source never piles up overlapping runs.
func (m *Model) refreshList(i int) tea.Cmd {
	ls := m.lists[i]
	if ls == nil || ls.sec.Source == nil {
		return nil
	}
	if !m.selReady(ls.sec.Source) {
		// Nothing selected to follow: show the empty text, run nothing, and
		// drop any answer still coming for the previous selection.
		ls.rows, ls.loaded, ls.err, ls.loading, ls.stale, ls.failures = nil, true, "", false, false, 0
		ls.seq++
		ls.reselect()
		return nil
	}
	if ls.loading {
		ls.stale = true // what is loading may predate what changed
		return nil
	}
	argv, err := spec.ExpandArgv(ls.sec.Source.Argv, m.values(nil), m.spec.Dir)
	if err != nil {
		ls.err = err.Error()
		return nil
	}
	ls.loading = true
	ls.seq++
	seq, r, ctx := ls.seq, m.runner, m.ctx
	req := run.Request{Argv: argv, Env: m.commandEnv(nil), Dir: m.spec.Dir, Timeout: ls.sec.Source.Timeout}
	idCol := ls.sec.IDColumn
	return m.safe(func() tea.Msg {
		res := r.Run(ctx, req)
		if !res.OK {
			return rowsMsg{sec: i, seq: seq, err: res.Message}
		}
		rows, err := parseRows(res.Stdout, idCol)
		if err != nil {
			return rowsMsg{sec: i, seq: seq, err: err.Error()}
		}
		return rowsMsg{sec: i, seq: seq, rows: rows}
	})
}

func (m *Model) onRows(msg rowsMsg) tea.Cmd {
	ls := m.lists[msg.sec]
	if ls == nil || msg.seq != ls.seq {
		return nil // an answer to a request that a newer one replaced
	}
	ls.loading = false
	if msg.err != "" {
		ls.err = msg.err
		ls.failures++
	} else {
		ls.err, ls.failures, ls.loaded = "", 0, true
		ls.rows = msg.rows
		ls.reselect()
	}
	if ls.stale {
		ls.stale = false
		return m.refreshList(msg.sec)
	}
	return m.scheduleTick(msg.sec, false)
}

// reselect keeps the selection on the same row id after a refresh; a row
// that went away hands the selection to the row now in its place.
func (ls *listState) reselect() {
	vis := ls.visible()
	if len(vis) == 0 {
		ls.sel, ls.selID = 0, ""
		return
	}
	for i, r := range vis {
		if r.id == ls.selID && ls.selID != "" {
			ls.sel = i
			return
		}
	}
	ls.sel = min(ls.sel, len(vis)-1)
	ls.selID = vis[ls.sel].id
}

func (ls *listState) visible() []row {
	if ls.filter == nil || len(ls.filter.value) == 0 {
		return ls.rows
	}
	q := strings.ToLower(ls.filter.String())
	var out []row
	for _, r := range ls.rows {
		for _, c := range ls.sec.Columns {
			if strings.Contains(strings.ToLower(r.vals[c.Key]), q) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

func (ls *listState) selected() (row, bool) {
	vis := ls.visible()
	if ls.sel < 0 || ls.sel >= len(vis) {
		return row{}, false
	}
	return vis[ls.sel], true
}

func (ls *listState) move(delta int) {
	vis := ls.visible()
	if len(vis) == 0 {
		return
	}
	ls.sel = max(0, min(len(vis)-1, ls.sel+delta))
	ls.selID = vis[ls.sel].id
}

// scheduleTick plans the next poll. After failures it waits longer, up to
// sixteen times the interval, so a broken source is not run in a tight loop.
func (m *Model) scheduleTick(sec int, text bool) tea.Cmd {
	var every time.Duration
	var gen *int
	var failures int
	if text {
		ts := m.texts[sec]
		every, gen, failures = ts.sec.Every, &ts.tickGen, ts.failures
	} else {
		ls := m.lists[sec]
		every, gen, failures = ls.sec.Every, &ls.tickGen, ls.failures
	}
	if every <= 0 || m.quitting {
		return nil
	}
	*gen++
	g := *gen
	delay := every << min(failures, 4)
	return m.after(delay, "", func(time.Time) tea.Msg { return tickMsg{sec: sec, gen: g, text: text} })
}

func (m *Model) onTick(msg tickMsg) tea.Cmd {
	if msg.text {
		if ts := m.texts[msg.sec]; ts != nil && ts.tickGen == msg.gen {
			return m.refreshText(msg.sec)
		}
		return nil
	}
	if ls := m.lists[msg.sec]; ls != nil && ls.tickGen == msg.gen {
		return m.refreshList(msg.sec)
	}
	return nil
}

func (m *Model) refreshText(i int) tea.Cmd {
	ts := m.texts[i]
	if ts == nil {
		return nil
	}
	if !m.selReady(ts.sec.Source) {
		ts.text, ts.loaded, ts.err, ts.loading, ts.stale, ts.failures = "", true, "", false, false, 0
		ts.seq++
		return nil
	}
	if ts.loading {
		ts.stale = true
		return nil
	}
	argv, err := spec.ExpandArgv(ts.sec.Source.Argv, m.values(nil), m.spec.Dir)
	if err != nil {
		ts.err = err.Error()
		return nil
	}
	ts.loading = true
	ts.seq++
	seq, r, ctx := ts.seq, m.runner, m.ctx
	req := run.Request{Argv: argv, Env: m.commandEnv(nil), Dir: m.spec.Dir, Timeout: ts.sec.Source.Timeout}
	return m.safe(func() tea.Msg {
		res := r.Run(ctx, req)
		if !res.OK {
			return textMsg{sec: i, seq: seq, err: res.Message}
		}
		return textMsg{sec: i, seq: seq, text: ansi.Strip(string(res.Stdout))}
	})
}

func (m *Model) onText(msg textMsg) tea.Cmd {
	ts := m.texts[msg.sec]
	if ts == nil || msg.seq != ts.seq {
		return nil
	}
	ts.loading = false
	if msg.err != "" {
		ts.err = msg.err
		ts.failures++
	} else {
		ts.err, ts.failures, ts.loaded, ts.text = "", 0, true, msg.text
	}
	if ts.stale {
		ts.stale = false
		return m.refreshText(msg.sec)
	}
	return m.scheduleTick(msg.sec, true)
}

// parseRows reads a source's output: a JSON array of objects, or an object
// {"rows": [...]}. Every value is shown as text.
func parseRows(out []byte, idCol string) ([]row, error) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("the source did not print JSON: %w", err)
	}
	if obj, ok := raw.(map[string]any); ok {
		if okv, has := obj["ok"].(bool); has && !okv {
			msg, _ := obj["message"].(string)
			return nil, fmt.Errorf("the source reported failure: %s", msg)
		}
		raw = obj["rows"]
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, errors.New(`the source must print a JSON array of objects, or {"rows": [...]}`)
	}
	rows := make([]row, 0, len(items))
	for i, it := range items {
		obj, ok := it.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("row %d is not a JSON object", i+1)
		}
		r := row{vals: make(map[string]string, len(obj))}
		for k, v := range obj {
			r.vals[k] = textOf(v)
		}
		r.id = r.vals[idCol]
		if r.id == "" {
			r.id = "#" + strconv.Itoa(i)
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func textOf(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return ansi.Strip(t)
	case bool:
		if t {
			return "yes"
		}
		return "no"
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}
