package ui

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/spec"
)

// selDebounce is how long a selection moved by hand rests before the
// sections following it run their sources, so arrowing through a list runs
// nothing for the rows passed over.
const selDebounce = 150 * time.Millisecond

// selMsg says a moved selection has rested.
type selMsg struct{ list, gen int }

// selSig is what the sections following a list see of it: the selected row's
// id and values. "" is no selection.
func (ls *listState) selSig() string {
	r, ok := ls.selected()
	if !ok {
		return ""
	}
	keys := make([]string, 0, len(r.vals))
	for k := range r.vals {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString(r.id)
	for _, k := range keys {
		b.WriteString("\x00" + k + "\x01" + r.vals[k])
	}
	return b.String()
}

// followers reports whether any section's source follows list id.
func (m *Model) followers(id string) (lists, texts []int) {
	for _, i := range sortedKeys(m.lists) {
		if slices.Contains(m.lists[i].sec.Source.SelLists(), id) {
			lists = append(lists, i)
		}
	}
	for _, i := range sortedKeys(m.texts) {
		if slices.Contains(m.texts[i].sec.Source.SelLists(), id) {
			texts = append(texts, i)
		}
	}
	return lists, texts
}

// followSelections refreshes the sections that follow a list whose selected
// row changed. A first selection, a lost one, or a change brought by the
// list's own refresh is followed at once; a selection moved by hand rests
// selDebounce first.
func (m *Model) followSelections(msg tea.Msg) tea.Cmd {
	_, fromRows := msg.(rowsMsg)
	var cmds []tea.Cmd
	for _, i := range sortedKeys(m.lists) {
		ls := m.lists[i]
		if ls.sec.ID == "" {
			continue
		}
		sig := ls.selSig()
		if sig == ls.followed {
			continue
		}
		first := ls.followed == ""
		ls.followed = sig
		ls.selGen++
		if fromRows || first || sig == "" {
			cmds = append(cmds, m.refreshFollowers(ls.sec.ID))
			continue
		}
		list, gen := i, ls.selGen
		cmds = append(cmds, m.after(selDebounce, "", func(time.Time) tea.Msg { return selMsg{list: list, gen: gen} }))
	}
	return tea.Batch(cmds...)
}

func (m *Model) onSel(msg selMsg) tea.Cmd {
	ls := m.lists[msg.list]
	if ls == nil || ls.selGen != msg.gen {
		return nil // the selection moved again since
	}
	return m.refreshFollowers(ls.sec.ID)
}

func (m *Model) refreshFollowers(id string) tea.Cmd {
	lists, texts := m.followers(id)
	var cmds []tea.Cmd
	for _, i := range lists {
		cmds = append(cmds, m.refreshList(i))
	}
	for _, i := range texts {
		cmds = append(cmds, m.refreshText(i))
	}
	return tea.Batch(cmds...)
}

// selReady reports whether every list a command follows has a selection. A
// section whose list has none shows nothing (its empty text), and runs
// nothing: no selection is not a failure.
func (m *Model) selReady(cmd *spec.Command) bool {
	for _, id := range cmd.SelLists() {
		ls := m.listByID(id)
		if ls == nil {
			return false
		}
		if _, ok := ls.selected(); !ok {
			return false
		}
	}
	return true
}

func (m *Model) listByID(id string) *listState {
	for _, ls := range m.lists {
		if ls.sec.ID == id {
			return ls
		}
	}
	return nil
}

// selValue is {sel.<list>.<column>}: a column of the list's selected row. A
// column the row does not have is empty; no selection is no value.
func (m *Model) selValue(name string) (string, bool) {
	id, col := spec.SelRef(name)
	ls := m.listByID(id)
	if ls == nil {
		return "", false
	}
	r, ok := ls.selected()
	if !ok {
		return "", false
	}
	return r.vals[col], true
}

func sortedKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
