package spec

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	percentRe  = regexp.MustCompile(`^([1-9][0-9]?|100)%$`)
	requiresRe = regexp.MustCompile(`^>=\s*v?[0-9]+\.[0-9]+(\.[0-9]+)?$`)
)

// obj reads one table and remembers which keys it consumed, so that every
// key left over is reported as unknown, at its own position.
type obj struct {
	t     *table
	c     *collector
	what  string // "the spec", "store \"config\"", ...
	known []string
}

func (o *obj) get(key string) (*value, Pos, bool) {
	o.known = append(o.known, key)
	return o.t.get(key)
}

func (o *obj) want(v *value, p Pos, key string, k kind) bool {
	if v.kind == k {
		return true
	}
	o.c.add(posOr(v.pos, p), "%s must be %s, not %s", key, k, v.kind)
	return false
}

func (o *obj) str(key string) (string, Pos, bool) {
	v, p, ok := o.get(key)
	if !ok || !o.want(v, p, key, kString) {
		return "", p, false
	}
	return v.str, posOr(v.pos, p), true
}

func (o *obj) integer(key string) (int64, Pos, bool) {
	v, p, ok := o.get(key)
	if !ok || !o.want(v, p, key, kInt) {
		return 0, p, false
	}
	return v.i, posOr(v.pos, p), true
}

func (o *obj) boolean(key string) (bool, bool) {
	v, p, ok := o.get(key)
	if !ok || !o.want(v, p, key, kBool) {
		return false, false
	}
	return v.b, true
}

func (o *obj) required(key string) (string, Pos) {
	s, p, ok := o.str(key)
	if _, _, present := o.t.get(key); !present {
		o.c.add(o.t.pos, "%s needs %s", o.what, key)
	} else if ok && strings.TrimSpace(s) == "" {
		o.c.add(p, "%s must not be empty", key)
	}
	return s, p
}

// done reports every key that was not read.
func (o *obj) done() {
	for _, k := range o.t.keys {
		if !slices.Contains(o.known, k) {
			known := slices.Clone(o.known)
			sort.Strings(known)
			o.c.add(o.t.keyPos[k], "unknown key %q in %s (known keys: %s)", k, o.what, strings.Join(known, ", "))
		}
	}
}

func posOr(p, fallback Pos) Pos {
	if p.Line > 0 {
		return p
	}
	return fallback
}

func tableOf(c *collector, v *value, p Pos, what string) *table {
	if v.kind != kTable {
		c.add(posOr(v.pos, p), "%s must be a table, not %s", what, v.kind)
		return nil
	}
	return v.tbl
}

// decode turns the tree into a Spec. It checks shapes and types; Validate
// then checks meaning (references, bounds, placeholders).
func decode(root *table, c *collector) *Spec {
	s := &Spec{Save: SaveThenApply}
	o := &obj{t: root, c: c, what: "the spec"}

	if v, p, ok := o.integer("spec"); ok {
		s.Version = int(v)
		if v != Version {
			c.add(p, "spec = %d is not a version this herdr-panel reads (it reads %d)", v, Version)
		}
	} else if _, _, present := root.get("spec"); !present {
		c.add(Pos{1, 1}, "the spec needs `spec = %d` as its format version", Version)
	}
	if r, rp, ok := o.str("requires"); ok {
		s.Requires, s.requiresPos = r, rp
		if !requiresRe.MatchString(r) {
			c.add(rp, "requires must read \">=X.Y\" or \">=X.Y.Z\", not %q", r)
		}
	}
	s.Title, _ = o.required("title")
	if v, p, ok := o.get("size"); ok {
		if t := tableOf(c, v, p, "size"); t != nil {
			s.Size = decodeSize(t, c)
		}
	}
	if m, p, ok := o.str("save"); ok {
		switch SaveMode(m) {
		case SaveThenApply, Live:
			s.Save = SaveMode(m)
		default:
			c.add(p, "save must be %q or %q, not %q", SaveThenApply, Live, m)
		}
	}
	if v, p, ok := o.get("stores"); ok {
		if t := tableOf(c, v, p, "stores"); t != nil {
			for _, name := range t.keys {
				sv, sp, _ := t.get(name)
				if st := tableOf(c, sv, sp, "stores."+name); st != nil {
					s.Stores = append(s.Stores, decodeStore(name, st, sp, c))
				}
			}
		}
	}
	if v, p, ok := o.get("section"); ok {
		if v.kind != kArray {
			c.add(posOr(v.pos, p), "section must be written as [[section]] tables, not %s", v.kind)
		} else {
			for i, sv := range v.arr {
				if st := tableOf(c, sv, p, "a section"); st != nil {
					s.Sections = append(s.Sections, decodeSection(i, st, c))
				}
			}
		}
	}
	o.done()
	return s
}

func decodeSize(t *table, c *collector) Size {
	o := &obj{t: t, c: c, what: "size"}
	var sz Size
	for _, key := range []string{"width", "height"} {
		v, p, ok := o.get(key)
		if !ok {
			continue
		}
		var d Dim
		switch v.kind {
		case kInt:
			if v.i < 1 || v.i > 1000 {
				c.add(posOr(v.pos, p), "%s must be 1 to 1000 cells, not %d", key, v.i)
			}
			d.Cells = int(v.i)
		case kString:
			if !percentRe.MatchString(v.str) {
				c.add(posOr(v.pos, p), "%s must be a number of cells or a percentage from 1%% to 100%%, such as \"70%%\", not %q", key, v.str)
				break
			}
			d.Percent, _ = strconv.Atoi(strings.TrimSuffix(v.str, "%"))
		default:
			c.add(posOr(v.pos, p), "%s must be a number of cells or a percentage string, not %s", key, v.kind)
		}
		if key == "width" {
			sz.Width = d
		} else {
			sz.Height = d
		}
	}
	o.done()
	return sz
}

func decodeStore(name string, t *table, p Pos, c *collector) *Store {
	o := &obj{t: t, c: c, what: fmt.Sprintf("store %q", name)}
	st := &Store{Name: name, Pos: p, Create: true}
	if k, kp := o.required("kind"); k != "" {
		st.Kind = StoreKind(k)
		if st.Kind != EnvFile {
			c.add(kp, "store kind %q is not supported; this version has %q (json and toml stores are planned)", k, EnvFile)
		}
	}
	if path, pp := o.required("path"); path != "" {
		st.Path = parseTemplate(c, pp, path)
	}
	if b, ok := o.boolean("create"); ok {
		st.Create = b
	}
	if v, vp, ok := o.get("apply"); ok {
		st.Apply = decodeCommand(c, v, vp, "apply", false)
	}
	o.done()
	return st
}

// decodeCommand reads `["prog", "arg"]` or `{ run = [...], timeout_s = 5 }`.
// every reports whether `every_s` may appear (sources only); it is read by
// the caller from the same table.
func decodeCommand(c *collector, v *value, p Pos, what string, every bool) *Command {
	cmd := &Command{Pos: posOr(v.pos, p), Timeout: DefaultTimeout}
	switch v.kind {
	case kArray:
		cmd.Argv = decodeArgv(c, v, p, what)
	case kTable:
		o := &obj{t: v.tbl, c: c, what: what}
		if rv, rp, ok := o.get("run"); ok {
			cmd.Argv = decodeArgv(c, rv, rp, what+".run")
		} else {
			c.add(cmd.Pos, "%s needs run = [\"program\", \"argument\", ...]", what)
		}
		if n, np, ok := o.integer("timeout_s"); ok {
			if n < 1 || n > 3600 {
				c.add(np, "timeout_s must be 1 to 3600, not %d", n)
			}
			cmd.Timeout = time.Duration(n) * time.Second
		}
		if every {
			o.known = append(o.known, "every_s")
		}
		o.done()
	default:
		c.add(cmd.Pos, "%s must be an argument array or a table with run, not %s", what, v.kind)
		return nil
	}
	if len(cmd.Argv) == 0 {
		return nil
	}
	return cmd
}

func decodeArgv(c *collector, v *value, p Pos, what string) []Template {
	if v.kind != kArray {
		c.add(posOr(v.pos, p), "%s must be an array of strings, not %s", what, v.kind)
		return nil
	}
	if len(v.arr) == 0 {
		c.add(posOr(v.pos, p), "%s must name a program", what)
		return nil
	}
	argv := make([]Template, 0, len(v.arr))
	for _, a := range v.arr {
		if a.kind != kString {
			c.add(posOr(a.pos, p), "%s must hold strings only, not %s", what, a.kind)
			continue
		}
		argv = append(argv, parseTemplate(c, posOr(a.pos, p), a.str))
	}
	if len(argv) > 0 && strings.TrimSpace(argv[0].Raw) == "" {
		c.add(posOr(v.pos, p), "%s must name a program first", what)
	}
	return argv
}

func parseTemplate(c *collector, p Pos, s string) Template {
	t, err := ParseTemplate(s)
	if err != nil {
		c.add(p, "%v", err)
		return Template{Raw: s, pos: p, parts: []part{}}
	}
	t.pos = p
	return t
}

func decodeSection(i int, t *table, c *collector) *Section {
	what := fmt.Sprintf("section %d", i+1)
	o := &obj{t: t, c: c, what: what}
	sec := &Section{Pos: t.pos, IDColumn: "id"}
	sec.Title, _, _ = o.str("title")
	if sec.Title != "" {
		o.what = fmt.Sprintf("section %q", sec.Title)
		what = o.what
	}
	kind := FieldsSection
	if k, kp, ok := o.str("kind"); ok {
		kind = SectionKind(k)
		switch kind {
		case FieldsSection, ListSection, ButtonsSection, TextSection, ViewSection:
		default:
			c.add(kp, "section kind must be fields, list, buttons, text or view, not %q", k)
			o.known = append(o.known, t.keys...) // its other keys are not the problem
			o.done()
			return sec
		}
	}
	sec.Kind = kind

	switch kind {
	case FieldsSection:
		if v, p, ok := o.get("fields"); ok {
			for j, fv := range arrayOfTables(c, v, p, "fields") {
				sec.Fields = append(sec.Fields, decodeField(fv, c, fmt.Sprintf("field %d of %s", j+1, what)))
			}
		} else {
			c.add(t.pos, "%s has no fields (a fields section needs fields = [...])", what)
		}
	case ListSection:
		decodeSource(o, sec, c, what, true)
		if v, p, ok := o.get("columns"); ok {
			sec.Columns = decodeColumns(c, v, p)
		}
		if s, _, ok := o.str("id_column"); ok {
			sec.IDColumn = s
		}
		if s, _, ok := o.str("empty"); ok {
			sec.Empty = s
		}
		if v, p, ok := o.get("row_actions"); ok {
			for j, av := range arrayOfTables(c, v, p, "row_actions") {
				sec.RowActions = append(sec.RowActions, decodeAction(av, c, fmt.Sprintf("row action %d of %s", j+1, what), AfterRefresh))
			}
		}
	case ButtonsSection:
		if v, p, ok := o.get("buttons"); ok {
			for j, av := range arrayOfTables(c, v, p, "buttons") {
				sec.Buttons = append(sec.Buttons, decodeAction(av, c, fmt.Sprintf("button %d of %s", j+1, what), AfterRefresh))
			}
		} else {
			c.add(t.pos, "%s has no buttons (a buttons section needs buttons = [...])", what)
		}
	case ViewSection:
		sec.View, _ = o.required("view")
	case TextSection:
		if s, _, ok := o.str("text"); ok {
			sec.Text = s
		}
		if _, sp, has := t.get("source"); has {
			if sec.Text != "" {
				c.add(sp, "%s has both text and source; use one", what)
			}
			decodeSource(o, sec, c, what, false)
		} else if sec.Text == "" {
			c.add(t.pos, "%s needs text = \"...\" or a source command", what)
		}
	}
	o.done()
	return sec
}

func decodeSource(o *obj, sec *Section, c *collector, what string, required bool) {
	v, p, ok := o.get("source")
	if !ok {
		if required {
			c.add(o.t.pos, "%s needs source = [\"program\", ...] printing its rows as JSON", what)
		}
		return
	}
	sec.Source = decodeCommand(c, v, p, "source", true)
	// every_s may sit beside source or inside its table, not both.
	if n, np, ok := o.integer("every_s"); ok {
		sec.Every = everyOf(c, n, np)
	}
	if v.kind == kTable {
		if ev, ep, ok := v.tbl.get("every_s"); ok {
			switch {
			case sec.Every > 0:
				c.add(posOr(ev.pos, ep), "every_s is given twice, beside source and inside it")
			case ev.kind != kInt:
				c.add(posOr(ev.pos, ep), "every_s must be an integer, not %s", ev.kind)
			default:
				sec.Every = everyOf(c, ev.i, posOr(ev.pos, ep))
			}
		}
	}
}

func everyOf(c *collector, n int64, p Pos) time.Duration {
	if n < 1 || n > 86400 {
		c.add(p, "every_s must be 1 to 86400, not %d", n)
	}
	return time.Duration(n) * time.Second
}

func arrayOfTables(c *collector, v *value, p Pos, what string) []*table {
	if v.kind != kArray {
		c.add(posOr(v.pos, p), "%s must be an array of tables, not %s", what, v.kind)
		return nil
	}
	out := make([]*table, 0, len(v.arr))
	for _, e := range v.arr {
		if e.kind != kTable {
			c.add(posOr(e.pos, p), "each entry of %s must be a table, not %s", what, e.kind)
			continue
		}
		out = append(out, e.tbl)
	}
	return out
}

func decodeColumns(c *collector, v *value, p Pos) []Column {
	if v.kind != kArray {
		c.add(posOr(v.pos, p), "columns must be an array, not %s", v.kind)
		return nil
	}
	cols := make([]Column, 0, len(v.arr))
	for _, e := range v.arr {
		switch e.kind {
		case kString:
			if strings.TrimSpace(e.str) == "" {
				c.add(posOr(e.pos, p), "a column key must not be empty")
				continue
			}
			cols = append(cols, Column{Key: e.str, Title: e.str})
		case kTable:
			o := &obj{t: e.tbl, c: c, what: "a column"}
			col := Column{}
			col.Key, _ = o.required("key")
			col.Title = col.Key
			if s, _, ok := o.str("title"); ok {
				col.Title = s
			}
			if n, np, ok := o.integer("width"); ok {
				if n < 1 || n > 500 {
					c.add(np, "column width must be 1 to 500, not %d", n)
				}
				col.Width = int(n)
			}
			o.done()
			cols = append(cols, col)
		default:
			c.add(posOr(e.pos, p), "a column must be a key string or a table, not %s", e.kind)
		}
	}
	return cols
}

func decodeAction(t *table, c *collector, what string, after After) *Action {
	o := &obj{t: t, c: c, what: what}
	a := &Action{Pos: t.pos, After: after}
	a.Label, _ = o.required("label")
	if s, _, ok := o.str("key"); ok {
		a.Key = s
	}
	if v, p, ok := o.get("run"); ok {
		a.Run = &Command{Pos: posOr(v.pos, p), Timeout: DefaultTimeout, Argv: decodeArgv(c, v, p, "run")}
	} else {
		c.add(t.pos, "%s needs run = [\"program\", ...]", what)
	}
	if n, np, ok := o.integer("timeout_s"); ok && a.Run != nil {
		if n < 1 || n > 3600 {
			c.add(np, "timeout_s must be 1 to 3600, not %d", n)
		}
		a.Run.Timeout = time.Duration(n) * time.Second
	}
	if s, sp, ok := o.str("confirm"); ok {
		a.Confirm = parseTemplate(c, sp, s)
	}
	if s, sp, ok := o.str("after"); ok {
		switch After(s) {
		case AfterRefresh, AfterClose, AfterNothing:
			a.After = After(s)
		default:
			c.add(sp, "after must be refresh, close or none, not %q", s)
		}
	}
	o.done()
	return a
}

func decodeField(t *table, c *collector, what string) *Field {
	o := &obj{t: t, c: c, what: what}
	f := &Field{Pos: t.pos, TrueValue: "true", FalseValue: "false"}
	f.Key, _ = o.required("key")
	if f.Key != "" {
		o.what = fmt.Sprintf("field %q", f.Key)
	}
	if s, ip, ok := o.str("id"); ok {
		if strings.TrimSpace(s) == "" {
			c.add(ip, "id must not be empty; leave it out to use the key")
		}
		f.ID = s
	} else {
		f.ID = f.Key
	}
	if s, _, ok := o.str("store"); ok {
		f.Store = s
	}
	typ, tp := o.required("type")
	f.Type = FieldType(typ)
	switch f.Type {
	case Bool, Choice, Int, Duration, Text, Secret, "":
	default:
		c.add(tp, "field type must be bool, choice, int, duration_s, text or secret, not %q", typ)
	}
	f.Label = f.Key
	if s, _, ok := o.str("label"); ok {
		f.Label = s
	}
	if s, _, ok := o.str("help"); ok {
		f.Help = s
	}
	if b, ok := o.boolean("required"); ok {
		f.Required = b
	}
	var boolDefault *bool
	if v, p, ok := o.get("default"); ok {
		f.Default = scalarString(c, v, p, "default")
		if v.kind == kBool {
			boolDefault = &v.b
		}
	}
	if v, p, ok := o.get("choices"); ok {
		f.Choices = decodeChoices(c, v, p)
	}
	if n, _, ok := o.integer("min"); ok {
		f.Min = &n
	}
	if n, _, ok := o.integer("max"); ok {
		f.Max = &n
	}
	if s, _, ok := o.str("pattern"); ok {
		f.Pattern = s
	}
	if n, np, ok := o.integer("max_len"); ok {
		if n < 1 {
			c.add(np, "max_len must be at least 1, not %d", n)
		}
		f.MaxLen = int(n)
	}
	if s, _, ok := o.str("placeholder"); ok {
		f.Placeholder = s
	}
	if s, _, ok := o.str("true_value"); ok {
		f.TrueValue = s
	}
	if s, _, ok := o.str("false_value"); ok {
		f.FalseValue = s
	}
	if v, p, ok := o.get("test"); ok {
		f.Test = decodeCommand(c, v, p, "test", false)
	}
	// default = true means "on", whatever on is written as in the file.
	if boolDefault != nil && f.Type == Bool {
		f.Default = f.FalseValue
		if *boolDefault {
			f.Default = f.TrueValue
		}
	}
	o.done()
	return f
}

// scalarString reads a default as the string a store would hold.
func scalarString(c *collector, v *value, p Pos, what string) string {
	switch v.kind {
	case kString:
		return v.str
	case kInt:
		return strconv.FormatInt(v.i, 10)
	case kBool:
		return strconv.FormatBool(v.b)
	case kFloat:
		return strconv.FormatFloat(v.f, 'g', -1, 64)
	default:
		c.add(posOr(v.pos, p), "%s must be a string, number or boolean, not %s", what, v.kind)
		return ""
	}
}

func decodeChoices(c *collector, v *value, p Pos) []ChoiceItem {
	if v.kind != kArray {
		c.add(posOr(v.pos, p), "choices must be an array, not %s", v.kind)
		return nil
	}
	out := make([]ChoiceItem, 0, len(v.arr))
	for _, e := range v.arr {
		switch e.kind {
		case kString:
			out = append(out, ChoiceItem{Value: e.str, Label: e.str})
		case kTable:
			o := &obj{t: e.tbl, c: c, what: "a choice"}
			ch := ChoiceItem{}
			if vv, vp, ok := o.get("value"); ok {
				ch.Value = scalarString(c, vv, vp, "value")
			} else {
				c.add(e.tbl.pos, "a choice needs value")
			}
			ch.Label = ch.Value
			if s, _, ok := o.str("label"); ok {
				ch.Label = s
			}
			o.done()
			out = append(out, ch)
		default:
			c.add(posOr(e.pos, p), "a choice must be a string or { value, label }, not %s", e.kind)
		}
	}
	return out
}
