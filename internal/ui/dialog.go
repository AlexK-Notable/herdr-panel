package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/spec"
)

type dialogKind int

const (
	dialogConfirm dialogKind = iota
	dialogPicker
	dialogHelp
)

// dialog is a modal box inside the panel. While one is open, keys and
// clicks go to it alone.
type dialog struct {
	kind     dialogKind
	title    string
	text     string
	buttons  []string
	options  []string // dialogPicker
	cursor   int      // the focused button or option
	scroll   int      // the first text line shown, when the text is too long
	noYN     bool     // y and n do not answer it (its first button is destructive)
	onChoose func(i int) tea.Cmd
	onCancel func() tea.Cmd
}

func (d *dialog) count() int {
	if d.kind == dialogPicker {
		return len(d.options)
	}
	return len(d.buttons)
}

func (m *Model) dialogKey(k string) tea.Cmd {
	d := m.dialog
	if m.tooSmall() && k != "esc" && k != "ctrl+c" {
		return nil // the dialog is not on screen: nothing may answer it blind
	}
	if d.kind == dialogHelp {
		switch k {
		case "up", "k":
			d.scroll--
			return nil
		case "down", "j":
			d.scroll++
			return nil
		case "pgup":
			d.scroll -= 5
			return nil
		case "pgdown":
			d.scroll += 5
			return nil
		}
	}
	switch k {
	case "esc", "q":
		m.dialog = nil
		if d.onCancel != nil {
			return d.onCancel()
		}
		return nil
	case "tab", "right", "down", "j", "l":
		d.cursor = (d.cursor + 1) % max(1, d.count())
	case "shift+tab", "left", "up", "k", "h":
		d.cursor = (d.cursor - 1 + max(1, d.count())) % max(1, d.count())
	case "enter", "space":
		return m.chooseDialog(d.cursor)
	case "y":
		if d.kind == dialogConfirm && !d.noYN {
			return m.chooseDialog(0)
		}
	case "n":
		if d.kind == dialogConfirm && !d.noYN && len(d.buttons) > 1 {
			return m.chooseDialog(len(d.buttons) - 1)
		}
	}
	if d.kind == dialogHelp && k != "tab" && k != "shift+tab" {
		m.dialog = nil // any other key closes help
	}
	return nil
}

func (m *Model) chooseDialog(i int) tea.Cmd {
	d := m.dialog
	m.dialog = nil
	if d.onChoose != nil {
		return d.onChoose(i)
	}
	return nil
}

func (m *Model) openPicker(f *spec.Field) {
	cur, _ := m.fieldValue(f)
	// A choice with no default can also be left out of the file: the
	// program then decides. "not set" comes first, and removes the key.
	var opts []string
	canUnset := unsettable(f)
	if canUnset {
		opts = append(opts, notSet)
	}
	sel := 0
	for _, c := range f.Choices {
		if c.Value == cur {
			sel = len(opts)
		}
		opts = append(opts, c.Label)
	}
	m.dialog = &dialog{
		kind: dialogPicker, title: f.Label, options: opts, cursor: sel,
		onChoose: func(i int) tea.Cmd {
			if canUnset {
				if i == 0 {
					return m.setField(f, "", true)
				}
				i--
			}
			return m.setField(f, f.Choices[i].Value, false)
		},
	}
}

// unsettable reports whether a choice offers "not set": it has no default
// to fall back to, it is not required, and no choice is already "".
func unsettable(f *spec.Field) bool {
	if f.Default != "" || f.Required {
		return false
	}
	for _, c := range f.Choices {
		if c.Value == "" {
			return false
		}
	}
	return true
}

// notSet names the value of a choice left out of the file.
const notSet = "not set"

func (m *Model) openConflict(name string) {
	ss := m.stores[name]
	where := ss.path
	if where == "" {
		where = name
	}
	m.dialog = &dialog{
		kind: dialogConfirm, title: "Changed on disk",
		text:    where + " changed since the panel read it. Your edits have not been written.",
		buttons: []string{"Reload theirs", "Overwrite with mine", "Cancel"},
		noYN:    true, // y would mean "drop my edits"
		onChoose: func(i int) tea.Cmd {
			switch i {
			case 0:
				clear(ss.edits)
				m.setStatus(statusInfo, "reloaded; your edits were dropped")
				return m.loadStore(name)
			case 1:
				// Re-read, then save only our edits on top of their content.
				st, ctx := ss.st, m.ctx
				return m.safe(func() tea.Msg {
					snap, err := st.Load(ctx)
					return reloadedForOverwrite{store: name, snap: snap, err: err}
				})
			}
			m.setStatus(statusInfo, "not saved")
			return nil
		},
	}
}

type reloadedForOverwrite struct {
	store string
	snap  storeSnapshot
	err   error
}

func (m *Model) onReloadedForOverwrite(msg reloadedForOverwrite) tea.Cmd {
	ss := m.stores[msg.store]
	if msg.err != nil {
		m.setStatus(statusErr, "reload failed: %v", msg.err)
		return nil
	}
	ss.snap, ss.loaded, ss.loadErr = msg.snap, true, nil
	return m.saveStore(msg.store, nil)
}

func (m *Model) openDiscard() {
	n := m.Dirty()
	noun := "change"
	if n != 1 {
		noun = "changes"
	}
	m.dialog = &dialog{
		kind: dialogConfirm, title: "Unsaved changes",
		text:    strings.TrimSpace(itoa(n) + " unsaved " + noun + "."),
		buttons: []string{"Save and close", "Discard", "Keep editing"},
		onChoose: func(i int) tea.Cmd {
			switch i {
			case 0:
				return m.save(func() tea.Cmd {
					if m.Dirty() == 0 {
						return m.Quit()
					}
					return nil
				})
			case 1:
				m.revert()
				return m.Quit()
			}
			return nil
		},
	}
}

func (m *Model) openHelp() {
	m.dialog = &dialog{
		kind: dialogHelp, title: "Keys",
		text: strings.Join([]string{
			"tab / shift+tab   next / previous",
			"↑ ↓               move; in a list, the selection",
			"enter / space     change the field, press the button",
			"← →               cycle a choice, step a number",
			"delete            reset a field to its default",
			"/                 filter the focused list",
			"ctrl+s            save",
			"ctrl+r            reload everything",
			"esc / q           close",
			"mouse             click anything; wheel scrolls",
		}, "\n"),
		buttons: []string{"Close"},
	}
}
