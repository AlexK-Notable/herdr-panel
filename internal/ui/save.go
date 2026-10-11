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
}

type savedMsg struct {
	store   string
	snap    store.Snapshot
	err     error
	changes map[string]store.Change
	then    func() tea.Cmd // what to do after a successful save (close)
}

type appliedMsg struct {
	store string
	res   run.Result
}

func (m *Model) loadStore(name string) tea.Cmd {
	ss := m.stores[name]
	if ss == nil || ss.st == nil {
		return nil
	}
	st, ctx := ss.st, m.ctx
	return m.safe(func() tea.Msg {
		snap, err := st.Load(ctx)
		return loadedMsg{store: name, snap: snap, err: err}
	})
}

func (m *Model) onLoaded(msg loadedMsg) tea.Cmd {
	ss := m.stores[msg.store]
	if ss == nil {
		return nil
	}
	ss.loaded, ss.loadErr = true, msg.err
	if msg.err == nil {
		ss.snap = msg.snap
		// An edit that now matches the file is no longer an edit.
		for k, c := range ss.edits {
			if v, ok := ss.snap.Values[k]; (c.Unset && !ok) || (!c.Unset && ok && v == c.Value) {
				delete(ss.edits, k)
			}
		}
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

// save writes every store with edits.
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
	for i, name := range names {
		var after func() tea.Cmd
		if i == len(names)-1 {
			after = then
		}
		cmds = append(cmds, m.saveStore(name, after))
	}
	return tea.Batch(cmds...)
}

func (m *Model) saveStore(name string, then func() tea.Cmd) tea.Cmd {
	ss := m.stores[name]
	if ss == nil || len(ss.edits) == 0 || ss.saving {
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
	changes := make(map[string]store.Change, len(ss.edits))
	maps.Copy(changes, ss.edits)
	st, ctx, base := ss.st, m.ctx, ss.snap
	m.setStatus(statusInfo, "saving…")
	return m.safe(func() tea.Msg {
		snap, err := st.Save(ctx, base, changes)
		return savedMsg{store: name, snap: snap, err: err, changes: changes, then: then}
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
		return nil
	case msg.err != nil:
		m.setStatus(statusErr, "save failed: %v", msg.err)
		return nil
	}
	ss.snap, ss.loaded, ss.loadErr = msg.snap, true, nil
	for k, c := range msg.changes {
		if cur, ok := ss.edits[k]; ok && cur == c {
			delete(ss.edits, k) // edited again while saving: keep the newer edit
		}
	}
	var cmds []tea.Cmd
	if ss.def.Apply != nil {
		cmds = append(cmds, m.apply(msg.store))
	} else {
		ss.applyErr = ""
		m.setStatus(statusOK, "✓ saved")
	}
	if msg.then != nil {
		cmds = append(cmds, msg.then())
	}
	return tea.Batch(cmds...)
}

func (m *Model) apply(name string) tea.Cmd {
	ss := m.stores[name]
	argv, err := spec.ExpandArgv(ss.def.Apply.Argv, m.values(nil), m.spec.Dir)
	if err != nil {
		ss.applyErr = err.Error()
		m.setStatus(statusErr, "✗ saved, not applied: %v", err)
		return nil
	}
	m.setStatus(statusInfo, "saved; applying…")
	req := run.Request{Argv: argv, Env: m.commandEnv(nil), Dir: m.spec.Dir, Timeout: ss.def.Apply.Timeout}
	r, ctx := m.runner, m.ctx
	return m.safe(func() tea.Msg { return appliedMsg{store: name, res: r.Run(ctx, req)} })
}

func (m *Model) onApplied(msg appliedMsg) tea.Cmd {
	ss := m.stores[msg.store]
	if ss == nil {
		return nil
	}
	if !msg.res.OK {
		ss.applyErr = msg.res.Message
		m.setStatus(statusErr, "✗ saved, not applied: %s", msg.res.Message)
		return nil
	}
	ss.applyErr = ""
	m.setStatus(statusOK, "✓ saved and applied")
	return nil
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
			env = append(env, "PANEL_FIELD_"+envName(f.ID)+"="+v)
		}
	}
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, "PANEL_ROW_"+envName(k)+"="+row[k])
	}
	return env
}
