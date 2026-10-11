package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlexK-Notable/herdr-panel/spec"
)

// listHeight is how many rows a list shows at once; the rest scroll.
const listHeight = 8

// Minimum is the smallest size the panel draws itself in; below it, one line
// asks for more room.
const (
	MinWidth  = 24
	MinHeight = 6
)

// Element is one clickable thing on screen, as observe reports it.
type Element struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Focused bool   `json:"focused,omitempty"`
}

type frame struct {
	comp     *lipgloss.Compositor
	content  string
	elements []Element
}

func (f *frame) hit(x, y int) string {
	if f == nil || f.comp == nil {
		return ""
	}
	return f.comp.Hit(x, y).ID()
}

// seg is a run of styled text on one line; id makes it clickable.
type seg struct {
	text  string
	id    string
	kind  string
	label string
}

type line []seg

func plain(s string) seg { return seg{text: s} }

// View implements tea.Model. A panic while drawing becomes the crash screen.
func (m *Model) View() (v tea.View) {
	defer func() {
		if r := recover(); r != nil {
			m.crash = m.recordCrash(r)
			v = m.finish(m.crashLines(), nil)
		}
	}()
	return m.finish(m.render())
}

// Frame is the last frame drawn, as plain text.
func (m *Model) Frame() string {
	if m.frame == nil {
		return ""
	}
	return ansi.Strip(m.frame.content)
}

// Elements are the clickable elements of the last frame.
func (m *Model) Elements() []Element {
	if m.frame == nil {
		return nil
	}
	return m.frame.elements
}

// Hit returns the id of the element at a cell of the last frame.
func (m *Model) Hit(x, y int) string { return m.frame.hit(x, y) }

func (m *Model) finish(lines []line, overlay *overlay) tea.View {
	w, h := max(m.width, 1), max(m.height, 1)
	var b strings.Builder
	f := &frame{}
	var layers []*lipgloss.Layer
	elems := map[string]int{}
	for y := range h {
		var l line
		if y < len(lines) {
			l = lines[y]
		}
		x := 0
		var row strings.Builder
		for _, s := range l {
			if x >= w {
				break
			}
			text := s.text
			sw := lipgloss.Width(text)
			if x+sw > w {
				text = ansi.Truncate(text, w-x, "…")
				sw = lipgloss.Width(text)
			}
			row.WriteString(text)
			if s.id != "" && sw > 0 {
				layers = append(layers, lipgloss.NewLayer(text).ID(s.id).X(x).Y(y).Z(10))
				addElement(f, elems, Element{ID: s.id, Kind: s.kind, Label: s.label, X: x, Y: y, W: sw, H: 1, Focused: s.id == m.focus})
			}
			x += sw
		}
		if x < w {
			row.WriteString(strings.Repeat(" ", w-x))
		}
		b.WriteString(row.String())
		if y < h-1 {
			b.WriteByte('\n')
		}
	}
	base := lipgloss.NewLayer(b.String()).X(0).Y(0).Z(0)
	all := append([]*lipgloss.Layer{base}, layers...)
	if overlay != nil {
		all = append(all, overlay.layers...)
		for _, e := range overlay.elements {
			if e.X < w && e.Y < h { // never report what is off screen
				addElement(f, elems, e)
			}
		}
	}
	f.comp = lipgloss.NewCompositor(all...)
	f.content = crop(f.comp.Render(), w, h)
	m.frame = f
	v := tea.NewView(f.content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// addElement records an element, merging two parts of one id (a field's
// label and value) into one box.
func addElement(f *frame, idx map[string]int, e Element) {
	if i, ok := idx[e.ID]; ok && f.elements[i].Y == e.Y {
		p := &f.elements[i]
		right := max(p.X+p.W, e.X+e.W)
		p.X = min(p.X, e.X)
		p.W = right - p.X
		return
	}
	idx[e.ID] = len(f.elements)
	f.elements = append(f.elements, e)
}

func (m *Model) render() ([]line, *overlay) {
	switch {
	case m.crash != nil:
		return m.crashLines(), nil
	case m.tooSmall():
		return []line{{plain(fmt.Sprintf("make me bigger %dx%d", m.width, m.height))}}, nil
	case m.spec == nil:
		return m.loadErrorLines(), nil
	}
	w := m.width
	header := m.headerLines(w)
	footer := m.footerLines(w)
	body := m.bodyLines(w)
	bodyH := max(1, m.height-len(header)-len(footer))
	m.keepFocusVisible(body, bodyH)
	end := min(len(body), m.scroll+bodyH)
	visible := body[min(m.scroll, len(body)):end]
	lines := append([]line{}, header...)
	lines = append(lines, visible...)
	for len(lines) < m.height-len(footer) {
		lines = append(lines, nil)
	}
	lines = append(lines, footer...)
	var ov *overlay
	if m.dialog != nil {
		ov = m.dialogOverlay()
	}
	return lines, ov
}

func (m *Model) keepFocusVisible(body []line, bodyH int) {
	target := -1
	for i, l := range body {
		for _, s := range l {
			if s.id == m.focus || (strings.HasPrefix(s.id, "row:") && m.isSelectedRow(s.id)) {
				target = i
			}
		}
		if target >= 0 && !strings.HasPrefix(m.focus, "list:") {
			break
		}
	}
	maxScroll := max(0, len(body)-bodyH)
	if target >= 0 && !m.freeScroll {
		if target < m.scroll {
			m.scroll = target
		}
		if target >= m.scroll+bodyH {
			m.scroll = target - bodyH + 1
		}
	}
	m.scroll = max(0, min(m.scroll, maxScroll))
}

func (m *Model) isSelectedRow(id string) bool {
	ls := m.focusedList()
	if ls == nil {
		return false
	}
	sec, j := splitIdx(strings.TrimPrefix(id, "row:"))
	return sec == ls.idx && j == ls.sel
}

func (m *Model) headerLines(w int) []line {
	th := m.theme
	title := th.Title.Render(m.spec.Title)
	var right string
	switch n := m.Dirty(); {
	case n > 0:
		right = th.Dirty.Render(fmt.Sprintf("● %d unsaved", n))
	case m.spec.Save == spec.Live:
		right = th.Muted.Render("saves as you edit")
	}
	gap := max(1, w-1-lipgloss.Width(title)-lipgloss.Width(right))
	return []line{
		{plain(" "), plain(title), plain(strings.Repeat(" ", gap)), plain(right)},
		{plain(th.Rule.Render(strings.Repeat("─", w)))},
	}
}

func (m *Model) footerLines(w int) []line {
	th := m.theme
	var hint line
	switch {
	case m.editing != nil && m.editing.ed.errMsg != "":
		hint = line{plain(" " + th.Error.Render("✗ "+m.editing.ed.errMsg))}
	case m.editing != nil:
		hint = line{plain(" " + th.Muted.Render("enter keeps · esc cancels · tab moves on"))}
	case m.status.text != "":
		st := th.Muted
		switch m.status.kind {
		case statusOK:
			st = th.OK
		case statusErr:
			st = th.Error
		}
		hint = line{plain(" " + st.Render(m.status.text))}
	default:
		if f := m.focusedField(); f != nil && f.Help != "" {
			hint = line{plain(" " + th.Muted.Render(f.Help))}
		}
	}
	buttons := line{plain(" ")}
	add := func(id, label string) {
		buttons = append(buttons, m.button(id, label, "button"), plain(" "))
	}
	if m.showsSave() {
		add("save", "Save ctrl+s")
		add("revert", "Revert")
	}
	add("close", "Close esc")
	used := 0
	for _, s := range buttons {
		used += lipgloss.Width(s.text)
	}
	help := "? help"
	if pad := w - used - len(help) - 1; pad > 0 {
		buttons = append(buttons, plain(strings.Repeat(" ", pad)), seg{text: th.Muted.Render(help), id: "help", kind: "button", label: "help"})
	}
	return []line{{plain(th.Rule.Render(strings.Repeat("─", w)))}, hint, buttons}
}

func (m *Model) button(id, label, kind string) seg {
	text := "[" + label + "]"
	if m.busy[id] {
		text = "[… " + label + "]"
	}
	st := m.theme.Button
	if id == m.focus {
		st = m.theme.Focus
	}
	return seg{text: st.Render(text), id: id, kind: kind, label: label}
}

func (m *Model) bodyLines(w int) []line {
	var out []line
	labelW := 0
	for _, f := range m.spec.Fields() {
		labelW = max(labelW, lipgloss.Width(f.Label))
	}
	labelW = max(8, min(labelW, w*2/5))
	for i, sec := range m.spec.Sections {
		if i > 0 {
			out = append(out, nil)
		}
		if sec.Title != "" {
			out = append(out, line{plain(" " + m.theme.Section.Render(sec.ExpandTitle(m.values(nil))))})
		}
		switch sec.Kind {
		case spec.FieldsSection:
			for _, f := range sec.Fields {
				out = append(out, m.fieldLines(f, labelW, w)...)
			}
		case spec.ListSection:
			out = append(out, m.listLines(i, w)...)
		case spec.ButtonsSection:
			var segs []seg
			for j, a := range sec.Buttons {
				segs = append(segs, m.actionSeg(actionID("button", i, j), a))
			}
			out = append(out, wrapSegs(segs, w)...)
		case spec.TextSection:
			out = append(out, m.textLines(i, sec, w)...)
		case spec.ViewSection:
			out = append(out, m.viewLines(sec, w)...)
		}
	}
	return out
}

func (m *Model) actionSeg(id string, a *spec.Action) seg {
	label := a.Label
	if a.Key != "" {
		label = a.Key + " " + label
	}
	return m.button(id, label, "button")
}

// wrapSegs lays buttons out on as many lines as they need.
func wrapSegs(segs []seg, w int) []line {
	var out []line
	cur := line{plain("   ")}
	x := 3
	for _, s := range segs {
		sw := lipgloss.Width(s.text)
		if x+sw > w && x > 3 {
			out = append(out, cur)
			cur, x = line{plain("   ")}, 3
		}
		cur = append(cur, s, plain(" "))
		x += sw + 1
	}
	if len(segs) > 0 {
		out = append(out, cur)
	}
	return out
}

func (m *Model) marker(id string) string {
	if id == m.focus {
		return "▸ "
	}
	return "  "
}

func (m *Model) fieldLines(f *spec.Field, labelW, w int) []line {
	th := m.theme
	id := "field:" + f.ID
	v, src := m.fieldValue(f)
	label := ansi.Truncate(f.Label, labelW, "…")
	label += strings.Repeat(" ", labelW-lipgloss.Width(label))
	valueW := max(4, w-labelW-8)

	var value string
	switch {
	case m.editing != nil && m.editing.field == f:
		value = m.editing.ed.view(valueW, th)
	default:
		shown := display(f, v)
		switch {
		case shown == "" && f.Placeholder != "":
			shown = th.Muted.Render(f.Placeholder)
		case shown == "":
			shown = th.Muted.Render("(not set)")
		default:
			shown = ansi.Truncate(shown, valueW, "…")
		}
		if id == m.focus {
			value = th.Focus.Render(ansi.Strip(shown))
			if shown == "" {
				value = th.Focus.Render(" ")
			}
		} else {
			value = th.Value.Render(shown)
		}
	}
	l := line{
		plain(" " + m.marker(id)),
		seg{text: th.Label.Render(label), id: id, kind: "field", label: f.Label},
		seg{text: "  ", id: id, kind: "field", label: f.Label},
		seg{text: value, id: id, kind: "field", label: f.Label},
	}
	switch src {
	case fromEdit:
		l = append(l, plain(" "+th.Dirty.Render("• changed")))
	case fromDefault:
		if v != "" && f.Type != spec.Secret {
			l = append(l, plain(" "+th.Muted.Render("(default)")))
		}
	case fromStore:
	}
	if f.Type == spec.Secret && f.Test != nil {
		l = append(l, plain(" "), m.button("test:"+f.ID, "Test", "button"))
	}
	if ss := m.stores[f.Store]; ss != nil && ss.loadErr != nil && f == m.firstFieldOf(f.Store) {
		return []line{l, {plain("     " + th.Error.Render("✗ "+ss.loadErr.Error()))}}
	}
	return []line{l}
}

func (m *Model) firstFieldOf(storeName string) *spec.Field {
	for _, f := range m.spec.Fields() {
		if f.Store == storeName {
			return f
		}
	}
	return nil
}

func (m *Model) listLines(i, w int) []line {
	th := m.theme
	ls := m.lists[i]
	id := "list:" + strconv.Itoa(i)
	widths := columnWidths(ls.sec.Columns, w-6)
	var head strings.Builder
	for j, c := range ls.sec.Columns {
		head.WriteString(pad(c.Title, widths[j]))
		head.WriteString(" ")
	}
	out := []line{{plain(" " + m.marker(id)), seg{text: th.Muted.Render(head.String()), id: id, kind: "list", label: ls.sec.ExpandTitle(m.values(nil))}}}
	if ls.filter != nil {
		text := ls.filter.String()
		if m.filterActive && m.focus == id {
			text = ls.filter.view(max(4, w-12), th)
		}
		out = append(out, line{plain("   " + th.Muted.Render("/ ")), plain(text), plain(th.Muted.Render("  esc clears"))})
	}
	vis := ls.visible()
	switch {
	case ls.err != "":
		out = append(out, line{plain("   " + th.Error.Render("✗ "+ls.err))})
	case !ls.loaded && ls.loading:
		out = append(out, line{plain("   " + th.Muted.Render("loading…"))})
	}
	if len(vis) == 0 && ls.loaded {
		empty := ls.sec.Empty
		if empty == "" {
			empty = "nothing here"
		}
		if ls.filter != nil && len(ls.rows) > 0 {
			empty = "no row matches the filter"
		}
		out = append(out, line{plain("   " + th.Muted.Render(empty))})
	}
	if ls.sel < ls.offset {
		ls.offset = ls.sel
	}
	if ls.sel >= ls.offset+listHeight {
		ls.offset = ls.sel - listHeight + 1
	}
	ls.offset = max(0, min(ls.offset, max(0, len(vis)-listHeight)))
	focused := m.focus == id
	for j := ls.offset; j < min(len(vis), ls.offset+listHeight); j++ {
		r := vis[j]
		var cells strings.Builder
		for k, c := range ls.sec.Columns {
			cells.WriteString(pad(r.vals[c.Key], widths[k]))
			cells.WriteString(" ")
		}
		text := cells.String()
		mark := "  "
		switch {
		case j == ls.sel && focused:
			text, mark = th.Focus.Render(text), "▸ "
		case j == ls.sel:
			text, mark = th.Selected.Render(text), "› "
		}
		label := r.vals[ls.sec.IDColumn]
		if len(ls.sec.Columns) > 0 {
			label = r.vals[ls.sec.Columns[0].Key]
		}
		out = append(out, line{plain(" " + mark), seg{text: text, id: "row:" + strconv.Itoa(i) + ":" + strconv.Itoa(j), kind: "row", label: label}})
	}
	if more := len(vis) - (ls.offset + listHeight); more > 0 {
		out = append(out, line{plain("   " + th.Muted.Render(fmt.Sprintf("… %d more", more)))})
	}
	var segs []seg
	for j, a := range ls.sec.RowActions {
		segs = append(segs, m.actionSeg(actionID("rowaction", i, j), a))
	}
	return append(out, wrapSegs(segs, w)...)
}

func columnWidths(cols []spec.Column, avail int) []int {
	widths := make([]int, len(cols))
	flex := 0
	for i, c := range cols {
		if c.Width > 0 {
			widths[i] = c.Width
			avail -= c.Width + 1
		} else {
			flex++
		}
	}
	for i := range cols {
		if widths[i] == 0 {
			widths[i] = max(4, (avail-flex)/max(1, flex))
		}
	}
	return widths
}

func pad(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

func (m *Model) textLines(i int, sec *spec.Section, w int) []line {
	th := m.theme
	text := sec.Text
	if ts := m.texts[i]; ts != nil {
		switch {
		case ts.err != "":
			return []line{{plain("   " + th.Error.Render("✗ "+ts.err))}}
		case !ts.loaded:
			return []line{{plain("   " + th.Muted.Render("loading…"))}}
		}
		text = ts.text
	}
	// Prose wraps at words: cutting a line would drop its end, which is
	// often the part that matters.
	tw := max(1, w-4)
	var out []line
	for l := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		for wl := range strings.SplitSeq(ansi.Wrap(l, tw, ""), "\n") {
			out = append(out, line{plain("   " + ansi.Truncate(wl, tw, "…"))})
			if len(out) >= 200 {
				return out
			}
		}
	}
	return out
}

func (m *Model) crashLines() []line {
	th := m.theme
	c := m.crash
	out := []line{
		{plain(" " + th.Error.Render("herdr-panel crashed. This is a bug."))},
		nil,
		{plain(" panic: " + c.value)},
		nil,
	}
	if c.log != "" {
		out = append(out, line{plain(" The details are in " + c.log)})
	} else {
		for l := range strings.SplitSeq(c.stack, "\n") {
			out = append(out, line{plain(" " + l)})
		}
	}
	return append(out, nil, line{plain(" " + th.Muted.Render("press any key to close"))})
}

func (m *Model) loadErrorLines() []line {
	th := m.theme
	head := "This panel cannot start:"
	if spec.IsSpecError(m.opts.LoadErr) {
		head = "This panel's spec has problems:"
	}
	out := []line{{plain(" " + th.Error.Render(head))}, nil}
	msg := "no spec"
	if m.opts.LoadErr != nil {
		msg = m.opts.LoadErr.Error()
	}
	for l := range strings.SplitSeq(msg, "\n") {
		out = append(out, line{plain("  " + l)})
	}
	out = append(out, nil, line{plain(" " + th.Muted.Render("press q to close"))})
	if m.scroll > 0 && m.scroll < len(out) {
		out = out[m.scroll:]
	}
	return out
}

// overlay is a dialog drawn above the panel.
type overlay struct {
	layers   []*lipgloss.Layer
	elements []Element
}

func (m *Model) dialogOverlay() *overlay {
	th := m.theme
	d := m.dialog
	boxW := min(m.width-4, 64)
	innerW := boxW - 4
	avail := max(1, m.height-2) // rows inside the border
	more := func(n int, where string) line {
		return line{plain(th.Muted.Render(fmt.Sprintf("… %d more %s", n, where)))}
	}
	lines := []line{{plain(th.DialogTitle.Render(d.title))}, nil}
	var text []line
	if d.text != "" {
		for l := range strings.SplitSeq(lipgloss.NewStyle().Width(innerW).Render(d.text), "\n") {
			text = append(text, line{plain(l)})
		}
	}
	switch d.kind {
	case dialogPicker:
		if len(text) > 0 {
			lines = append(append(lines, text...), nil)
		}
		// The options get what is left, windowed around the cursor.
		sp := spanAt(len(d.options), max(1, avail-len(lines)), d.cursor-(avail-len(lines)-2)/2)
		if sp.above {
			lines = append(lines, more(sp.start, "above"))
		}
		for i := sp.start; i < sp.end; i++ {
			o := d.options[i]
			text, mark := o, "  "
			if i == d.cursor {
				text, mark = th.Focus.Render(o), "▸ "
			}
			lines = append(lines, line{plain(mark), seg{text: text, id: "option:" + strconv.Itoa(i), kind: "option", label: o}})
		}
		if sp.below {
			lines = append(lines, more(len(d.options)-sp.end, "below"))
		}
	default:
		// The text scrolls (help: ↑ ↓ PgUp PgDn); the buttons stay.
		if len(text) > 0 {
			sp := spanAt(len(text), max(1, avail-len(lines)-2), d.scroll)
			d.scroll = sp.start
			if sp.above {
				lines = append(lines, more(sp.start, "above (↑)"))
			}
			lines = append(lines, text[sp.start:sp.end]...)
			if sp.below {
				lines = append(lines, more(len(text)-sp.end, "below (↓)"))
			}
			lines = append(lines, nil)
		}
		var btnLine line
		for i, b := range d.buttons {
			st := th.Button
			if i == d.cursor {
				st = th.Focus
			}
			btnLine = append(btnLine, seg{text: st.Render("[" + b + "]"), id: "dialog:" + strconv.Itoa(i), kind: "dialog-button", label: b}, plain(" "))
		}
		lines = append(lines, btnLine)
	}
	if len(lines) > avail {
		lines = lines[:avail] // only when the panel is barely tall enough
	}
	boxH := len(lines) + 2
	x0 := max(0, (m.width-boxW)/2)
	y0 := max(0, (m.height-boxH)/2)

	ov := &overlay{}
	border := lipgloss.RoundedBorder()
	var box strings.Builder
	box.WriteString(th.Dialog.Render(border.TopLeft + strings.Repeat(border.Top, boxW-2) + border.TopRight))
	for li, l := range lines {
		box.WriteString("\n")
		var row strings.Builder
		x := x0 + 2
		for _, s := range l {
			text := ansi.Truncate(s.text, innerW-(x-x0-2), "…")
			sw := lipgloss.Width(text)
			if s.id != "" && sw > 0 {
				ov.layers = append(ov.layers, lipgloss.NewLayer(text).ID(s.id).X(x).Y(y0+1+li).Z(110))
				ov.elements = append(ov.elements, Element{ID: s.id, Kind: s.kind, Label: s.label, X: x, Y: y0 + 1 + li, W: sw, H: 1, Focused: strings.HasSuffix(s.id, ":"+strconv.Itoa(d.cursor))})
			}
			row.WriteString(text)
			x += sw
		}
		content := row.String()
		box.WriteString(border.Left + " " + content + strings.Repeat(" ", max(0, innerW-lipgloss.Width(content))) + " " + border.Right)
	}
	box.WriteString("\n" + border.BottomLeft + strings.Repeat(border.Bottom, boxW-2) + border.BottomRight)
	ov.layers = append([]*lipgloss.Layer{lipgloss.NewLayer(box.String()).ID("dialog").X(x0).Y(y0).Z(100)}, ov.layers...)
	ov.elements = append([]Element{{ID: "dialog", Kind: "dialog", Label: d.title, X: x0, Y: y0, W: boxW, H: boxH}}, ov.elements...)
	return ov
}

// span is the part of n lines that fits in rows rows, starting near start.
// When lines are hidden, a marker line takes one row at that end.
type span struct {
	start, end   int
	above, below bool
}

func spanAt(n, rows, start int) span {
	if n <= rows {
		return span{end: n}
	}
	if rows < 3 { // no room for markers
		start = min(max(0, start), n-rows)
		return span{start: start, end: start + rows}
	}
	s := span{start: min(max(0, start), n-(rows-1))} // the last page needs only the top marker
	s.above = s.start > 0
	room := rows
	if s.above {
		room--
	}
	if s.start+room >= n {
		s.end = n
	} else {
		s.below, s.end = true, s.start+room-1
	}
	return s
}

func (m *Model) tooSmall() bool { return m.width < MinWidth || m.height < MinHeight }

// crop cuts a rendered frame to w cells by h lines.
func crop(s string, w, h int) string {
	ls := strings.Split(s, "\n")
	if len(ls) > h {
		ls = ls[:h]
	}
	for i, l := range ls {
		if ansi.StringWidth(l) > w {
			ls[i] = ansi.Truncate(l, w, "")
		}
	}
	return strings.Join(ls, "\n")
}
