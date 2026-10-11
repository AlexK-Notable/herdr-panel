package ui

import (
	"errors"
	"maps"
	"os"
	"sort"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/internal/run"
	"github.com/AlexK-Notable/herdr-panel/internal/store"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

type loadedMsg struct {
	store string
	snap  store.Snapshot
	err   error
	// gen is the store's save generation when the load started, and
	// duringSave whether a save was then in flight: a load that may have
	// read the file before a save wrote it must not replace that save's
	// snapshot.
	gen        int
	duringSave bool
}

type savedMsg struct {
	store   string
	snap    store.Snapshot
	err     error
	changes map[string]store.Change
	after   *afterSave
}

type appliedMsg struct {
	store string
	res   run.Result
	after *afterSave
}

// afterSave is something to do (close the panel) once every store of one
// save request is saved and applied. Any failure cancels it, so the panel
// stays open on the failure; it never closes over "saved, not applied".
type afterSave struct {
	remaining int
	failed    bool
	then      func() tea.Cmd
}

// done counts one store as finished.
func (a *afterSave) done(ok bool) tea.Cmd {
	if a == nil {
		return nil
	}
	a.remaining--
	if !ok {
		a.failed = true
	}
	if a.remaining == 0 && !a.failed {
		return a.then()
	}
	return nil
}

func (m *Model) loadStore(name string) tea.Cmd {
	ss := m.stores[name]
	if ss == nil || ss.st == nil {
		return nil
	}
	st, ctx, gen, during := ss.st, m.ctx, ss.saveGen, ss.saving
	return m.safe(func() tea.Msg {
		snap, err := st.Load(ctx)
		return loadedMsg{store: name, snap: snap, err: err, gen: gen, duringSave: during}
	})
}

func (m *Model) onLoaded(msg loadedMsg) tea.Cmd {
	ss := m.stores[msg.store]
	if ss == nil {
		return nil
	}
	if ss.loaded && (msg.gen != ss.saveGen || msg.duringSave) {
		return nil // older than a save: the save's snapshot is newer
	}
	first := !ss.loaded
	ss.loaded, ss.loadErr = true, msg.err
	if msg.err != nil {
		return nil
	}
	ss.snap = msg.snap
	// An edit that now matches the file is no longer an edit.
	for k, c := range ss.edits {
		if v, ok := ss.snap.Values[k]; (c.Unset && !ok) || (!c.Unset && ok && v == c.Value) {
			delete(ss.edits, k)
		}
	}
	if first && m.spec.Save == spec.Live && len(ss.edits) > 0 {
		return m.saveStore(msg.store, nil) // edits made while it loaded
	}
	return nil
}

// fieldValue is a field's current value and where it comes from.
func (m *Model) fieldValue(f *spec.Field) (string, valueSource) {
	ss := m.stores[f.Store]
	if ss == nil {
		return f.Default, fromDefault
	}
	if c, ok := ss.edits[f.Key]; ok {
		if c.Unset {
			return f.Default, fromDefault
		}
		return c.Value, fromEdit
	}
	if v, ok := ss.snap.Values[f.Key]; ok {
		return v, fromStore
	}
	return f.Default, fromDefault
}

type valueSource int

const (
	fromDefault valueSource = iota
	fromStore
	fromEdit
)

// setField records an edit; in live mode it is saved at once.
func (m *Model) setField(f *spec.Field, v string, unset bool) tea.Cmd {
	ss := m.stores[f.Store]
	if ss == nil {
		return nil
	}
	if !unset && v == "" && emptyMeansUnset(f, ss.def) {
		unset = true
	}
	cur, inFile := ss.snap.Values[f.Key]
	switch {
	case unset && !inFile, !unset && inFile && cur == v:
		delete(ss.edits, f.Key)
	case unset:
		ss.edits[f.Key] = store.Change{Unset: true}
	default:
		ss.edits[f.Key] = store.Change{Value: v}
	}
	if m.spec.Save == spec.Live {
		return m.saveStore(f.Store, nil)
	}
	return nil
}

// emptyMeansUnset reports whether clearing a field removes its key rather
// than writing an empty value: always for a number, which "" is not, and for
// any field in a TOML file, whose programs tell an absent key (use the
// default, or $EDITOR) from an empty one. A choice's "" is a choice.
func emptyMeansUnset(f *spec.Field, sd *spec.Store) bool {
	switch f.Type {
	case spec.Choice:
		return false
	case spec.Int, spec.Duration:
		return true
	case spec.Bool, spec.Text, spec.Secret:
	}
	return sd != nil && sd.Kind == spec.TOMLFile
}

// save writes every store with edits. then, if set, runs once all of them
// are saved and applied; any failure cancels it.
func (m *Model) save(then func() tea.Cmd) tea.Cmd {
	var cmds []tea.Cmd
	names := make([]string, 0, len(m.stores))
	for name, ss := range m.stores {
		if len(ss.edits) > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		m.setStatus(statusInfo, "nothing to save")
		if then != nil {
			return then()
		}
		return nil
	}
	var after *afterSave
	if then != nil {
		after = &afterSave{remaining: len(names), then: then}
	}
	for _, name := range names {
		cmd := m.saveStore(name, after)
		if cmd == nil {
			after.done(false) // it could not start; the status says why
		}
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m *Model) saveStore(name string, after *afterSave) tea.Cmd {
	ss := m.stores[name]
	if ss == nil || len(ss.edits) == 0 {
		return nil
	}
	if ss.saving {
		m.setStatus(statusInfo, "still saving %s; try again in a moment", name)
		return nil
	}
	if ss.st == nil || (ss.loadErr != nil && !errors.Is(ss.loadErr, os.ErrNotExist)) {
		m.setStatus(statusErr, "cannot save %s: %v", name, ss.loadErr)
		return nil
	}
	if !ss.loaded {
		m.setStatus(statusErr, "cannot save %s yet: still loading", name)
		return nil
	}
	ss.saving = true
	ss.saveGen++
	changes := make(map[string]store.Change, len(ss.edits))
	maps.Copy(changes, ss.edits)
	st, ctx, base := ss.st, m.ctx, ss.snap
	m.setStatus(statusInfo, "saving…")
	return m.safe(func() tea.Msg {
		snap, err := st.Save(ctx, base, changes)
		return savedMsg{store: name, snap: snap, err: err, changes: changes, after: after}
	})
}

func (m *Model) onSaved(msg savedMsg) tea.Cmd {
	ss := m.stores[msg.store]
	if ss == nil {
		return nil
	}
	ss.saving = false
	switch {
	case errors.Is(msg.err, store.ErrConflict):
		m.openConflict(msg.store)
		return msg.after.done(false)
	case msg.err != nil:
		m.setStatus(statusErr, "save failed: %v", msg.err)
		return msg.after.done(false)
	}
	ss.snap, ss.loaded, ss.loadErr = msg.snap, true, nil
	for k, c := range msg.changes {
		if cur, ok := ss.edits[k]; ok && cur == c {
			delete(ss.edits, k) // edited again while saving: keep the newer edit
		}
	}
	var cmds []tea.Cmd
	if ss.def.Apply != nil {
		// The close, if any, waits for the apply: closing first would
		// cancel it before it ran.
		cmds = append(cmds, m.apply(msg.store, msg.after))
	} else {
		ss.applyErr = ""
		m.setStatus(statusOK, "✓ saved")
		cmds = append(cmds, msg.after.done(true))
	}
	if m.spec.Save == spec.Live && len(ss.edits) > 0 {
		cmds = append(cmds, m.saveStore(msg.store, nil)) // edited again while saving
	}
	return tea.Batch(cmds...)
}

func (m *Model) apply(name string, after *afterSave) tea.Cmd {
	ss := m.stores[name]
	argv, err := spec.ExpandArgv(ss.def.Apply.Argv, m.values(nil), m.spec.Dir)
	if err != nil {
		ss.applyErr = err.Error()
		m.setStatus(statusErr, "✗ saved, not applied: %v", err)
		return after.done(false)
	}
	m.setStatus(statusInfo, "saved; applying…")
	req := run.Request{Argv: argv, Env: m.commandEnv(nil), Dir: m.spec.Dir, Timeout: ss.def.Apply.Timeout}
	r, ctx := m.runner, m.ctx
	return m.safe(func() tea.Msg { return appliedMsg{store: name, res: r.Run(ctx, req), after: after} })
}

func (m *Model) onApplied(msg appliedMsg) tea.Cmd {
	ss := m.stores[msg.store]
	if ss == nil {
		return nil
	}
	if !msg.res.OK {
		ss.applyErr = msg.res.Message
		m.setStatus(statusErr, "✗ saved, not applied: %s", msg.res.Message)
		return msg.after.done(false)
	}
	ss.applyErr = ""
	m.setStatus(statusOK, "✓ saved and applied")
	return msg.after.done(true)
}

// revert drops every unsaved edit.
func (m *Model) revert() {
	for _, ss := range m.stores {
		clear(ss.edits)
	}
	m.setStatus(statusInfo, "edits discarded")
}

// refreshAll reloads stores and every list and text section.
func (m *Model) refreshAll() tea.Cmd {
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

// values supplies placeholders: ctx, env, the fields' current values (never
// a secret) and the given row.
func (m *Model) values(row map[string]string) spec.Values {
	return spec.ValuesFunc(func(ns, name string) (string, bool) {
		switch ns {
		case spec.NSCtx:
			if name == "spec_dir" && m.spec != nil {
				return m.spec.Dir, true
			}
			v, ok := m.opts.Context[name]
			if !ok && name == "cwd" {
				if wd, err := os.Getwd(); err == nil {
					return wd, true
				}
			}
			return v, ok
		case spec.NSEnv:
			return os.LookupEnv(name)
		case spec.NSRow:
			v, ok := row[name]
			return v, ok
		case spec.NSField:
			for _, f := range m.spec.Fields() {
				if f.ID == name && f.Type != spec.Secret {
					v, _ := m.fieldValue(f)
					return v, true
				}
			}
		}
		return "", false
	})
}

// commandEnv is what every command gets on top of the inherited environment.
func (m *Model) commandEnv(row map[string]string) []string {
	env := []string{"PANEL_VIA=" + m.opts.Via}
	if m.spec != nil {
		for _, f := range m.spec.Fields() {
			if f.Type == spec.Secret {
				continue
			}
			v, _ := m.fieldValue(f)
			env = append(env, "PANEL_FIELD_"+spec.EnvName(f.ID)+"="+v)
		}
	}
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, "PANEL_ROW_"+spec.EnvName(k)+"="+row[k])
	}
	return env
}
