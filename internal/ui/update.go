package ui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/spec"
)

// focusOrder lists the focusable elements in the order tab visits them.
func (m *Model) focusOrder() []string {
	if m.spec == nil {
		return nil
	}
	var ids []string
	for i, sec := range m.spec.Sections {
		switch sec.Kind {
		case spec.FieldsSection:
			for _, f := range sec.Fields {
				ids = append(ids, "field:"+f.ID)
				if f.Type == spec.Secret && f.Test != nil {
					ids = append(ids, "test:"+f.ID)
				}
			}
		case spec.ListSection:
			ids = append(ids, "list:"+strconv.Itoa(i))
			for j := range sec.RowActions {
				ids = append(ids, actionID("rowaction", i, j))
			}
		case spec.ButtonsSection:
			for j := range sec.Buttons {
				ids = append(ids, actionID("button", i, j))
			}
		case spec.ViewSection:
			ids = append(ids, viewPrefix+sec.View)
		case spec.TextSection:
		}
	}
	if m.spec.Save != spec.Live {
		ids = append(ids, "save", "revert")
	}
	return append(ids, "close")
}

func actionID(kind string, sec, j int) string {
	return kind + ":" + strconv.Itoa(sec) + ":" + strconv.Itoa(j)
}

func (m *Model) moveFocus(delta int) {
	order := m.focusOrder()
	if len(order) == 0 {
		return
	}
	i := 0
	for j, id := range order {
		if id == m.focus {
			i = j
			break
		}
	}
	m.focus = order[(i+delta+len(order))%len(order)]
}

// QuitRequest asks the panel to end now, as a signal does: running commands
// are cancelled and nothing is asked.
type QuitRequest struct{}

// Update implements tea.Model. A panic in it becomes the crash screen.
func (m *Model) Update(msg tea.Msg) (model tea.Model, cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			m.crash = m.recordCrash(r)
			// Bubble Tea draws whatever model is returned: after a panic it
			// must still be this one, showing the crash screen.
			model, cmd = m, nil
		}
	}()
	prev, prevPin := m.focus, m.pinKey()
	cmd = m.update(msg)
	if m.pinKey() != prevPin {
		m.freeScroll = false // the focus moved: show it again
	}
	return m, tea.Batch(cmd, m.viewFocusChange(prev), m.followSelections(msg))
}

// pinKey names what the body scroll follows: the focus, and in a list the
// selected row.
func (m *Model) pinKey() string {
	if ls := m.focusedList(); ls != nil {
		return m.focus + "/" + strconv.Itoa(ls.sel)
	}
	return m.focus
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return nil
	case crashMsg:
		m.crash = msg.c
		return nil
	case QuitRequest:
		return m.Quit()
	case viewClose:
		return m.close()
	case viewMsg:
		if r, ok := msg.msg.(TickRequest); ok {
			return m.after(r.D, msg.name, r.Fn)
		}
		return m.toView(msg.name, msg.msg)
	case Advance:
		return m.advanceTo(m.clock + max(0, msg.D))
	case advanceTo:
		return m.advanceTo(msg.at)
	case loadedMsg:
		return m.onLoaded(msg)
	case savedMsg:
		return m.onSaved(msg)
	case appliedMsg:
		return m.onApplied(msg)
	case reloadedForOverwrite:
		return m.onReloadedForOverwrite(msg)
	case actionDoneMsg:
		return m.onActionDone(msg)
	case rowsMsg:
		return m.onRows(msg)
	case textMsg:
		return m.onText(msg)
	case tickMsg:
		return m.onTick(msg)
	case selMsg:
		return m.onSel(msg)
	case tea.PasteMsg:
		if name, v := m.focusedView(); v != nil && m.dialog == nil && m.crash == nil {
			return m.toView(name, msg)
		}
		return m.paste(msg.Content)
	case tea.KeyPressMsg:
		if name, v := m.focusedView(); v != nil && m.dialog == nil && m.crash == nil &&
			!slices.Contains(panelKeys, msg.String()) {
			return m.toView(name, msg)
		}
		return m.key(msg.String())
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return m.click(msg.X, msg.Y)
		}
	case tea.MouseWheelMsg:
		return m.wheel(msg.X, msg.Y, msg.Button == tea.MouseWheelUp)
	}
	return nil
}

func (m *Model) paste(s string) tea.Cmd {
	switch {
	case m.editing != nil:
		m.editing.ed.insert(s)
	case m.filtering() != nil:
		ls := m.filtering()
		ls.filter.insert(s)
		ls.reselect()
	}
	return nil
}

func (m *Model) filtering() *listState {
	if id, ok := strings.CutPrefix(m.focus, "list:"); ok {
		if i, err := strconv.Atoi(id); err == nil {
			if ls := m.lists[i]; ls != nil && ls.filter != nil && m.filterActive {
				return ls
			}
		}
	}
	return nil
}

func (m *Model) key(k string) tea.Cmd {
	switch {
	case m.crash != nil:
		m.ExitCode = 1
		return m.Quit()
	case m.spec == nil:
		if k == "q" || k == "esc" || k == "enter" || k == "ctrl+c" {
			return m.Quit()
		}
		m.scrollKey(k)
		return nil
	case k == "ctrl+c":
		return m.Quit() // the escape hatch: no questions
	case m.dialog != nil:
		return m.dialogKey(k)
	case m.editing != nil:
		return m.editKey(k)
	case m.filtering() != nil:
		return m.filterKey(k)
	}
	return m.panelKey(k)
}

// commitEdit ends the edit the way leaving a form field does: the value is
// kept. An invalid value keeps the editor open with its error, so typing is
// never silently lost; ok is then false. An empty secret editor changes
// nothing.
func (m *Model) commitEdit() (tea.Cmd, bool) {
	e := m.editing
	v, err := normalize(e.field, e.ed.String())
	empty := e.field.Type == spec.Secret && e.ed.String() == ""
	if err != nil && !empty {
		e.ed.errMsg = err.Error()
		return nil, false
	}
	m.editing = nil
	if empty {
		return nil, true
	}
	return m.setField(e.field, v, false), true
}

func (m *Model) editKey(k string) tea.Cmd {
	e := m.editing
	switch k {
	case "enter":
		v, err := normalize(e.field, e.ed.String())
		if e.field.Type == spec.Secret && e.ed.String() == "" {
			m.editing = nil // nothing typed: the secret stays as it was
			return nil
		}
		if err != nil {
			e.ed.errMsg = err.Error()
			return nil
		}
		m.editing = nil
		return m.setField(e.field, v, false)
	case "esc":
		m.editing = nil
		return nil
	case "tab", "shift+tab", "up", "down":
		cmd, ok := m.commitEdit()
		if !ok {
			return nil
		}
		if k == "tab" || k == "down" {
			m.moveFocus(1)
		} else {
			m.moveFocus(-1)
		}
		return cmd
	}
	if !e.ed.key(k) {
		if r := []rune(k); len(r) == 1 || k == "space" {
			if k == "space" {
				k = " "
			}
			e.ed.insert(k)
		}
	}
	return nil
}

func (m *Model) filterKey(k string) tea.Cmd {
	ls := m.filtering()
	switch k {
	case "enter", "tab", "down":
		m.filterActive = false
	case "esc":
		ls.filter, m.filterActive = nil, false
	default:
		if !ls.filter.key(k) {
			if r := []rune(k); len(r) == 1 || k == "space" {
				if k == "space" {
					k = " "
				}
				ls.filter.insert(k)
			}
		}
	}
	ls.reselect()
	return nil
}

func (m *Model) panelKey(k string) tea.Cmd {
	if cmd, ok := m.shortcut(k); ok {
		return cmd
	}
	ls := m.focusedList()
	switch k {
	case "q", "esc":
		if ls != nil && ls.filter != nil && k == "esc" {
			ls.filter = nil
			ls.reselect()
			return nil
		}
		return m.close()
	case "?":
		m.openHelp()
	case "ctrl+s":
		return m.save(nil)
	case "ctrl+r":
		m.setStatus(statusInfo, "reloading…")
		return m.refreshAll()
	case "tab":
		m.moveFocus(1)
	case "shift+tab":
		m.moveFocus(-1)
	case "up", "k":
		if ls != nil && ls.sel > 0 {
			ls.move(-1)
		} else {
			m.moveFocus(-1)
		}
	case "down", "j":
		if ls != nil && ls.sel < len(ls.visible())-1 {
			ls.move(1)
		} else {
			m.moveFocus(1)
		}
	case "pgup", "pgdown", "home", "end":
		if ls != nil {
			step := map[string]int{"pgup": -listHeight, "pgdown": listHeight, "home": -1 << 30, "end": 1 << 30}[k]
			ls.move(step)
		} else {
			m.scrollKey(k)
		}
	case "/":
		if ls != nil {
			if ls.filter == nil {
				ls.filter = newEditor("", false, 0)
			}
			m.filterActive = true
		}
	case "enter", "space":
		return m.activate(m.focus)
	case "left", "h", "right", "l":
		if f := m.focusedField(); f != nil {
			delta := 1
			if k == "left" || k == "h" {
				delta = -1
			}
			return m.adjust(f, delta)
		}
	case "delete", "backspace":
		if f := m.focusedField(); f != nil {
			if f.Required {
				m.setStatus(statusErr, "%s is required; it cannot be reset", f.Label)
				return nil
			}
			return m.setField(f, "", true)
		}
	}
	return nil
}

// shortcut runs the action bound to k, if any.
func (m *Model) shortcut(k string) (tea.Cmd, bool) {
	for i, sec := range m.spec.Sections {
		for j, a := range sec.Buttons {
			if a.Key == k {
				m.focus = actionID("button", i, j)
				return m.startAction(m.focus, a, nil), true
			}
		}
		for j, a := range sec.RowActions {
			if a.Key == k {
				return m.runRowAction(i, j), true
			}
		}
	}
	return nil, false
}

func (m *Model) runRowAction(sec, j int) tea.Cmd {
	ls := m.lists[sec]
	a := ls.sec.RowActions[j]
	r, ok := ls.selected()
	if !ok {
		m.setStatus(statusErr, "%s: no row selected", a.Label)
		return nil
	}
	return m.startAction(actionID("rowaction", sec, j), a, r.vals)
}

func (m *Model) close() tea.Cmd {
	if m.Dirty() > 0 {
		m.openDiscard()
		return nil
	}
	return m.Quit()
}

func (m *Model) focusedField() *spec.Field {
	id, ok := strings.CutPrefix(m.focus, "field:")
	if !ok {
		return nil
	}
	return m.field(id)
}

func (m *Model) field(id string) *spec.Field {
	for _, f := range m.spec.Fields() {
		if f.ID == id {
			return f
		}
	}
	return nil
}

func (m *Model) focusedList() *listState {
	id, ok := strings.CutPrefix(m.focus, "list:")
	if !ok {
		return nil
	}
	i, err := strconv.Atoi(id)
	if err != nil {
		return nil
	}
	return m.lists[i]
}

// activate is enter, space or a click on an element.
func (m *Model) activate(id string) tea.Cmd {
	kind, rest, _ := strings.Cut(id, ":")
	switch kind {
	case "field":
		f := m.field(rest)
		if f == nil {
			return nil
		}
		switch f.Type {
		case spec.Bool:
			v, _ := m.fieldValue(f)
			nv := f.TrueValue
			if v == f.TrueValue {
				nv = f.FalseValue
			}
			return m.setField(f, nv, false)
		case spec.Choice:
			m.openPicker(f)
		case spec.Int, spec.Duration, spec.Text, spec.Secret:
			cur, _ := m.fieldValue(f)
			if f.Type == spec.Secret {
				cur = ""
			}
			if f.Type == spec.Duration {
				if n, err := strconv.ParseInt(cur, 10, 64); err == nil {
					cur = formatDuration(n)
				}
			}
			limit := f.MaxLen
			m.editing = &fieldEdit{field: f, ed: newEditor(cur, f.Type == spec.Secret, limit)}
		}
	case "test":
		if f := m.field(rest); f != nil {
			return m.testSecret(f)
		}
	case "button":
		sec, j := splitIdx(rest)
		if sec >= 0 && sec < len(m.spec.Sections) && j < len(m.spec.Sections[sec].Buttons) {
			return m.startAction(id, m.spec.Sections[sec].Buttons[j], nil)
		}
	case "rowaction":
		sec, j := splitIdx(rest)
		if m.lists[sec] != nil && j < len(m.lists[sec].sec.RowActions) {
			return m.runRowAction(sec, j)
		}
	case "save":
		return m.save(nil)
	case "revert":
		m.revert()
	case "close":
		return m.close()
	case "help":
		m.openHelp()
	}
	return nil
}

func splitIdx(s string) (int, int) {
	a, b, _ := strings.Cut(s, ":")
	i, err1 := strconv.Atoi(a)
	j, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil {
		return -1, -1
	}
	return i, j
}

// adjust is left and right on a field: cycle a choice, step a number.
func (m *Model) adjust(f *spec.Field, delta int) tea.Cmd {
	cur, _ := m.fieldValue(f)
	switch f.Type {
	case spec.Choice:
		if len(f.Choices) == 0 {
			return nil
		}
		i := -1
		for j, c := range f.Choices {
			if c.Value == cur {
				i = j
			}
		}
		if unsettable(f) {
			// Not set is a stop of its own, before the first choice.
			n := len(f.Choices) + 1
			i = (i + 1 + delta + n) % n
			if i == 0 {
				return m.setField(f, "", true)
			}
			return m.setField(f, f.Choices[i-1].Value, false)
		}
		i = (i + delta + len(f.Choices)) % len(f.Choices)
		return m.setField(f, f.Choices[i].Value, false)
	case spec.Bool:
		return m.activate("field:" + f.ID)
	case spec.Int, spec.Duration:
		n, err := strconv.ParseInt(strings.TrimSpace(cur), 10, 64)
		if err != nil {
			n = 0
		}
		step := int64(1)
		if f.Type == spec.Duration {
			step = 10
			if n >= 600 {
				step = 60
			}
		}
		n += int64(delta) * step
		if f.Min != nil && n < *f.Min {
			n = *f.Min
		}
		if f.Max != nil && n > *f.Max {
			n = *f.Max
		}
		return m.setField(f, strconv.FormatInt(n, 10), false)
	case spec.Text, spec.Secret:
	}
	return nil
}

func (m *Model) click(x, y int) tea.Cmd {
	if m.crash != nil {
		m.ExitCode = 1
		return m.Quit()
	}
	if m.frame == nil {
		return nil
	}
	id := m.frame.hit(x, y)
	if m.dialog != nil {
		// The dialog is modal: a click outside it is swallowed.
		switch {
		case strings.HasPrefix(id, "dialog:"):
			i, _ := strconv.Atoi(strings.TrimPrefix(id, "dialog:"))
			return m.chooseDialog(i)
		case strings.HasPrefix(id, "option:"):
			i, _ := strconv.Atoi(strings.TrimPrefix(id, "option:"))
			return m.chooseDialog(i)
		}
		return nil
	}
	var committed tea.Cmd
	if m.editing != nil && id != "field:"+m.editing.field.ID {
		// A click elsewhere leaves the field as tab does: the value is kept,
		// and an invalid one keeps the editor open and swallows the click.
		cmd, ok := m.commitEdit()
		if !ok {
			return nil
		}
		committed = cmd
	}
	return tea.Batch(committed, m.clickTarget(id, x, y))
}

// clickTarget acts on a click on the element id.
func (m *Model) clickTarget(id string, x, y int) tea.Cmd {
	switch kind, rest, _ := strings.Cut(id, ":"); kind {
	case "":
		return nil
	case "row":
		sec, j := splitIdx(rest)
		if ls := m.lists[sec]; ls != nil {
			m.focus = "list:" + strconv.Itoa(sec)
			ls.move(j - ls.sel)
		}
		return nil
	case "list":
		m.focus = id
		return nil
	case "view":
		return m.viewClick(rest, x, y)
	case "field":
		if m.editing != nil {
			return nil // already editing this one
		}
	}
	if id != "help" {
		m.focus = id
	}
	return m.activate(id)
}

func (m *Model) wheel(x, y int, up bool) tea.Cmd {
	delta := 1
	if up {
		delta = -1
	}
	if d := m.dialog; d != nil {
		switch d.kind {
		case dialogHelp:
			d.scroll += delta
		case dialogPicker:
			d.cursor = min(max(0, d.cursor+delta), len(d.options)-1)
		case dialogConfirm: // its buttons sit on one line: nothing to scroll
		}
		return nil // the body behind a dialog does not move
	}
	if m.frame != nil {
		id := m.frame.hit(x, y)
		if kind, rest, _ := strings.Cut(id, ":"); kind == "row" || kind == "list" {
			first, _, _ := strings.Cut(rest, ":")
			sec, _ := strconv.Atoi(first)
			if ls := m.lists[sec]; ls != nil {
				ls.move(delta)
				return nil
			}
		}
	}
	m.scroll = max(0, m.scroll+delta)
	m.freeScroll = true
	return nil
}

func (m *Model) scrollKey(k string) {
	switch k {
	case "up", "k":
		m.scroll--
	case "down", "j":
		m.scroll++
	case "pgup":
		m.scroll -= max(1, m.height/2)
	case "pgdown":
		m.scroll += max(1, m.height/2)
	case "home":
		m.scroll = 0
	case "end":
		m.scroll = 1 << 20
	}
	m.scroll = max(0, m.scroll)
	m.freeScroll = true
}
