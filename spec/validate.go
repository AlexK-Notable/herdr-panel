package spec

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ReservedKeys are bound by every panel and cannot be an action's shortcut.
// The vim aliases h, j, k and l are not reserved: a shortcut on one of them
// wins, and the arrow keys still move.
var ReservedKeys = []string{
	"?", "/", "q", "esc", "enter", "space", "tab", "shift+tab",
	"up", "down", "left", "right", "pgup", "pgdown", "home", "end",
	"delete", "backspace",
	"ctrl+c", "ctrl+s", "ctrl+r",
}

// Validate checks a spec built in Go. It first fills in what the TOML decoder
// would: the format version, the save mode, a field's id and label from its
// key, bool values "true"/"false", command timeouts, after = refresh, the id
// column and column titles. It then checks the shapes the decoder checks
// (enums, bounds, non-empty commands) and everything Parse checks. It returns
// an [Errors] value listing every problem; calling it again changes nothing.
//
// A bool Default is stored as written: with TrueValue "1", write Default "1",
// not "true". Unlike the TOML default, a Store built in Go creates its file
// only when Create is set.
func Validate(s *Spec) error {
	if msg, refused := refusedByRequires(s); refused {
		return Errors{{File: s.File, Pos: s.requiresPos, Msg: msg}}
	}
	c := &collector{file: s.File}
	fillDefaults(s)
	checkShapes(s, c)
	validate(s, c)
	sortErrors(c.errs)
	return c.err()
}

func fillDefaults(s *Spec) {
	if s.Version == 0 {
		s.Version = Version
	}
	if s.Save == "" {
		s.Save = SaveThenApply
	}
	cmd := func(c *Command) {
		if c != nil && c.Timeout == 0 {
			c.Timeout = DefaultTimeout
		}
	}
	for _, st := range s.Stores {
		cmd(st.Apply)
	}
	for _, sec := range s.Sections {
		if sec.Kind == "" {
			sec.Kind = FieldsSection
		}
		if sec.IDColumn == "" {
			sec.IDColumn = "id"
		}
		cmd(sec.Source)
		for i := range sec.Columns {
			if sec.Columns[i].Title == "" {
				sec.Columns[i].Title = sec.Columns[i].Key
			}
		}
		for _, a := range slices.Concat(sec.Buttons, sec.RowActions) {
			if a.After == "" {
				a.After = AfterRefresh
			}
			cmd(a.Run)
		}
		for _, f := range sec.Fields {
			if f.ID == "" {
				f.ID = f.Key
			}
			if f.Label == "" {
				f.Label = f.Key
			}
			if f.TrueValue == "" {
				f.TrueValue = "true"
			}
			if f.FalseValue == "" {
				f.FalseValue = "false"
			}
			cmd(f.Test)
		}
	}
}

// checkShapes repeats, for a spec built in Go, the checks the decoder makes
// while reading TOML.
func checkShapes(s *Spec, c *collector) {
	if s.Version != Version {
		c.add(Pos{}, "spec version %d is not a version this herdr-panel reads (it reads %d)", s.Version, Version)
	}
	if strings.TrimSpace(s.Title) == "" {
		c.add(Pos{}, "the spec needs a title")
	}
	if s.Requires != "" && !requiresRe.MatchString(s.Requires) {
		c.add(s.requiresPos, "requires must read \">=X.Y\" or \">=X.Y.Z\", not %q", s.Requires)
	}
	switch s.Save {
	case SaveThenApply, Live:
	default:
		c.add(Pos{}, "save must be %q or %q, not %q", SaveThenApply, Live, s.Save)
	}
	for _, d := range []Dim{s.Size.Width, s.Size.Height} {
		if d.Cells < 0 || d.Cells > 1000 || d.Percent < 0 || d.Percent > 100 || (d.Cells > 0 && d.Percent > 0) {
			c.add(Pos{}, "a size is 1 to 1000 cells or 1 to 100 percent, not both")
		}
	}
	command := func(cmd *Command, what string) {
		if cmd == nil {
			return
		}
		if len(cmd.Argv) == 0 || strings.TrimSpace(cmd.Argv[0].Raw) == "" {
			c.add(cmd.Pos, "%s must name a program", what)
		}
		if cmd.Timeout < time.Second || cmd.Timeout > time.Hour {
			c.add(cmd.Pos, "%s timeout must be 1 s to 1 h, not %s", what, cmd.Timeout)
		}
		for _, a := range cmd.Argv {
			if _, err := a.parsed(); err != nil {
				c.add(cmd.Pos, "%s: %v", what, err)
			}
		}
	}
	for _, st := range s.Stores {
		if !slices.Contains(StoreKinds, st.Kind) {
			c.add(st.Pos, "store %q: kind %q is not supported; this version has %q", st.Name, st.Kind, StoreKinds)
		}
		if strings.TrimSpace(st.Path.Raw) == "" {
			c.add(st.Pos, "store %q needs a path", st.Name)
		} else if _, err := st.Path.parsed(); err != nil {
			c.add(st.Pos, "store %q path: %v", st.Name, err)
		}
		command(st.Apply, fmt.Sprintf("store %q apply", st.Name))
	}
	for _, sec := range s.Sections {
		switch sec.Kind {
		case FieldsSection, ListSection, ButtonsSection, TextSection, ViewSection:
		default:
			c.add(sec.Pos, "section kind must be fields, list, buttons, text or view, not %q", sec.Kind)
		}
		if sec.Every < 0 || sec.Every > 24*time.Hour {
			c.add(sec.Pos, "a refresh interval must be 1 s to 24 h")
		}
		switch {
		case sec.Kind == ListSection && sec.Source == nil:
			c.add(sec.Pos, "a list section needs a source command")
		case sec.Kind == TextSection && sec.Source == nil && sec.Text == "":
			c.add(sec.Pos, "a text section needs text or a source command")
		case sec.Kind == ViewSection && strings.TrimSpace(sec.View) == "":
			c.add(sec.Pos, "a view section needs a view name")
		case sec.Kind == TextSection && sec.Source != nil && sec.Text != "":
			c.add(sec.Pos, "a text section has both text and a source; use one")
		}
		command(sec.Source, "source")
		for _, col := range sec.Columns {
			if strings.TrimSpace(col.Key) == "" {
				c.add(sec.Pos, "a column key must not be empty")
			}
		}
		for _, a := range slices.Concat(sec.Buttons, sec.RowActions) {
			if strings.TrimSpace(a.Label) == "" {
				c.add(a.Pos, "an action needs a label")
			}
			switch a.After {
			case AfterRefresh, AfterClose, AfterNothing:
			default:
				c.add(a.Pos, "after must be refresh, close or none, not %q", a.After)
			}
			if a.Run == nil {
				c.add(a.Pos, "action %q needs a command", a.Label)
			}
			command(a.Run, fmt.Sprintf("action %q", a.Label))
			if _, err := a.Confirm.parsed(); err != nil {
				c.add(a.Pos, "action %q confirm: %v", a.Label, err)
			}
		}
		for _, f := range sec.Fields {
			if strings.TrimSpace(f.Key) == "" {
				c.add(f.Pos, "a field needs a key")
			}
			switch f.Type {
			case Bool, Choice, Int, Duration, Text, Secret:
			default:
				c.add(f.Pos, "field %q: type must be bool, choice, int, duration_s, text or secret, not %q", f.Key, f.Type)
			}
			command(f.Test, fmt.Sprintf("field %q test", f.Key))
		}
	}
}

// validate checks a spec's meaning; both Parse and Validate run it.
func validate(s *Spec, c *collector) {
	if len(s.Sections) == 0 {
		c.add(Pos{1, 1}, "the spec has no [[section]]; a panel needs at least one")
	}

	ids := map[string]*Field{}
	envNames := map[string]string{}
	storeKeys := map[string]Pos{}
	for _, st := range s.Stores {
		if !validName(st.Name, false) {
			c.add(st.Pos, "store name %q must be letters, digits, _ and -", st.Name)
		}
		checkTemplate(c, st.Path, st.Pos, "store path", NSEnv, NSCtx)
	}
	for _, f := range s.Fields() {
		if f.Store == "" && len(s.Stores) == 1 {
			f.Store = s.Stores[0].Name
		}
		validateField(s, f, c)
		if f.ID == "" {
			continue
		}
		if prev, dup := ids[f.ID]; dup {
			c.add(f.Pos, "field id %q is used twice (also at line %d); give one an id", f.ID, prev.Pos.Line)
		} else {
			ids[f.ID] = f
			env := EnvName(f.ID)
			if other, clash := envNames[env]; clash {
				c.add(f.Pos, "field ids %q and %q would both reach commands as PANEL_FIELD_%s; rename one", other, f.ID, env)
			} else {
				envNames[env] = f.ID
			}
		}
		sk := f.Store + "\x00" + f.Key
		if p, dup := storeKeys[sk]; dup && f.Key != "" {
			c.add(f.Pos, "store %q key %q has two fields (also at line %d)", f.Store, f.Key, p.Line)
		} else {
			storeKeys[sk] = f.Pos
		}
	}

	refs := func(t Template, fallback Pos, what string, ns ...string) {
		checkTemplate(c, t, fallback, what, ns...)
		p := posOr(t.pos, fallback)
		for _, ph := range t.Placeholders() {
			if ph[0] == NSSel {
				if l, _ := SelRef(ph[1]); slices.Contains(ns, NSSel) && s.List(l) == nil {
					c.add(p, "%s refers to {sel.%s}, but no list section has id %q", what, ph[1], l)
				}
				continue
			}
			if ph[0] != NSField {
				continue
			}
			switch f, ok := ids[ph[1]]; {
			case !ok:
				c.add(p, "%s refers to {field.%s}, but no field has that id", what, ph[1])
			case f.Type == Secret:
				c.add(p, "%s cannot use {field.%s}: it is a secret, and a secret never reaches a command line or the screen (a test command reads the secret itself)", what, ph[1])
			}
		}
	}
	cmdRefs := func(cmd *Command, what string, ns ...string) {
		if cmd == nil {
			return
		}
		for i, a := range cmd.Argv {
			refs(a, cmd.Pos, what, ns...)
			if i > 0 {
				continue
			}
			for _, ph := range a.Placeholders() {
				if ph[0] == NSRow || ph[0] == NSField || ph[0] == NSSel {
					c.add(posOr(a.pos, cmd.Pos), "%s: the program cannot come from {%s.%s}; pass the value as an argument", what, ph[0], ph[1])
				}
			}
		}
	}
	for _, st := range s.Stores {
		cmdRefs(st.Apply, fmt.Sprintf("store %q apply", st.Name), NSEnv, NSCtx, NSField)
	}
	for _, f := range s.Fields() {
		cmdRefs(f.Test, fmt.Sprintf("field %q test", f.ID), NSEnv, NSCtx, NSField)
	}

	views := map[string]Pos{}
	for _, sec := range s.Sections {
		if sec.Kind != ViewSection || sec.View == "" { // decode reports a missing name
			continue
		}
		switch p, dup := views[sec.View]; {
		case !validName(sec.View, false):
			c.add(sec.Pos, "view name %q must be letters, digits, _ and -", sec.View)
		case dup:
			c.add(sec.Pos, "view %q is shown twice (also at line %d)", sec.View, p.Line)
		default:
			views[sec.View] = sec.Pos
		}
	}

	listIDs := map[string]Pos{}
	for _, sec := range s.Sections {
		if sec.ID == "" {
			continue
		}
		switch p, dup := listIDs[sec.ID]; {
		case !validName(sec.ID, false):
			c.add(sec.Pos, "list id %q must be letters, digits, _ and -", sec.ID)
		case dup:
			c.add(sec.Pos, "list id %q is used twice (also at line %d)", sec.ID, p.Line)
		default:
			listIDs[sec.ID] = sec.Pos
		}
	}
	for _, sec := range s.Sections {
		if sec.Title == "" {
			continue
		}
		t, err := ParseTemplate(sec.Title)
		if err != nil {
			c.add(sec.Pos, "section title %q: %v", sec.Title, err)
			continue
		}
		t.pos = sec.Pos
		refs(t, sec.Pos, fmt.Sprintf("section title %q", sec.Title), NSSel, NSCtx, NSEnv)
	}
	checkSelCycles(s, c)

	keys := map[string]Pos{}
	checkKey := func(a *Action) {
		if a.Key == "" {
			return
		}
		switch p, dup := keys[a.Key]; {
		case !ValidKeyName(a.Key):
			c.add(a.Pos, "key %q is not a key name; write it as Bubble Tea names keys, such as r, R, ctrl+o or f5", a.Key)
		case slices.Contains(ReservedKeys, a.Key):
			c.add(a.Pos, "key %q is reserved by every panel (reserved: %s)", a.Key, strings.Join(ReservedKeys, " "))
		case dup:
			c.add(a.Pos, "key %q is bound twice (also at line %d)", a.Key, p.Line)
		default:
			keys[a.Key] = a.Pos
		}
	}
	for _, sec := range s.Sections {
		switch sec.Kind {
		case ListSection:
			if len(sec.Columns) == 0 {
				c.add(sec.Pos, "a list section needs columns = [\"key\", ...]")
			}
			cmdRefs(sec.Source, "list source", NSEnv, NSCtx, NSField, NSSel)
			for _, a := range sec.RowActions {
				checkKey(a)
				cmdRefs(a.Run, fmt.Sprintf("row action %q", a.Label), NSEnv, NSCtx, NSField, NSRow, NSSel)
				refs(a.Confirm, a.Pos, fmt.Sprintf("row action %q confirm", a.Label), NSEnv, NSCtx, NSField, NSRow, NSSel)
			}
		case ButtonsSection:
			for _, a := range sec.Buttons {
				checkKey(a)
				cmdRefs(a.Run, fmt.Sprintf("button %q", a.Label), NSEnv, NSCtx, NSField, NSSel)
				refs(a.Confirm, a.Pos, fmt.Sprintf("button %q confirm", a.Label), NSEnv, NSCtx, NSField, NSSel)
			}
		case TextSection:
			cmdRefs(sec.Source, "text source", NSEnv, NSCtx, NSField, NSSel)
		case FieldsSection, ViewSection:
		}
	}
}

func validateField(s *Spec, f *Field, c *collector) {
	st := s.Store(f.Store)
	switch {
	case f.Store == "" && len(s.Stores) == 0:
		c.add(f.Pos, "field %q has no store to live in; add a [stores.<name>] table", f.Key)
	case f.Store == "":
		c.add(f.Pos, "field %q needs store = \"...\" (the spec has %d stores)", f.Key, len(s.Stores))
	case st == nil:
		c.add(f.Pos, "field %q names store %q, which is not defined", f.Key, f.Store)
	case st.Kind == EnvFile && f.Key != "" && !validName(f.Key, true):
		c.add(f.Pos, "env-file key %q must be letters, digits and _, not starting with a digit", f.Key)
	case st.Kind == TOMLFile && f.Key != "" && !bareTOMLKey(f.Key):
		c.add(f.Pos, "TOML key %q must be a bare top-level key: letters, digits, _ and -", f.Key)
	case st.Kind == TOMLFile && f.Type == Bool && (f.TrueValue != "true" || f.FalseValue != "false"):
		c.add(f.Pos, "a bool in a TOML file is true or false; true_value/false_value do not apply")
	}
	if f.ID != "" && !validName(f.ID, false) {
		c.add(f.Pos, "field id %q must be letters, digits, _ and -", f.ID)
	}
	notFor := func(cond bool, what string) {
		if cond {
			c.add(f.Pos, "%s does not apply to a %s field", what, f.Type)
		}
	}
	notFor(len(f.Choices) > 0 && f.Type != Choice, "choices")
	notFor((f.Min != nil || f.Max != nil) && f.Type != Int && f.Type != Duration, "min/max")
	notFor(f.Pattern != "" && f.Type != Text, "pattern")
	notFor(f.MaxLen != 0 && f.Type != Text && f.Type != Secret, "max_len")
	notFor(f.Placeholder != "" && f.Type != Text, "placeholder")
	notFor(f.Test != nil && f.Type != Secret, "test")
	notFor((f.TrueValue != "true" || f.FalseValue != "false") && f.Type != Bool, "true_value/false_value")

	switch f.Type {
	case Bool:
		if f.TrueValue == f.FalseValue {
			c.add(f.Pos, "true_value and false_value must differ")
		}
		if f.Default != "" && f.Default != f.TrueValue && f.Default != f.FalseValue {
			c.add(f.Pos, "default %q is neither %q nor %q", f.Default, f.TrueValue, f.FalseValue)
		}
	case Choice:
		if len(f.Choices) == 0 {
			c.add(f.Pos, "a choice field needs choices = [...]")
		}
		seen := map[string]bool{}
		for _, ch := range f.Choices {
			if seen[ch.Value] {
				c.add(f.Pos, "choice %q is listed twice", ch.Value)
			}
			seen[ch.Value] = true
		}
		if f.Default != "" && !seen[f.Default] && len(f.Choices) > 0 {
			c.add(f.Pos, "default %q is not one of the choices", f.Default)
		}
	case Int, Duration:
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			c.add(f.Pos, "min %d is greater than max %d", *f.Min, *f.Max)
		}
		if f.Default != "" {
			if err := CheckInt(f, f.Default); err != nil {
				c.add(f.Pos, "default: %v", err)
			}
		}
	case Text:
		if f.MaxLen < 0 {
			c.add(f.Pos, "max_len must not be negative")
		}
		if f.Pattern != "" {
			if _, err := regexp.Compile(f.Pattern); err != nil {
				c.add(f.Pos, "pattern does not compile: %v", err)
				break
			}
		}
		if f.Default != "" {
			if err := CheckText(f, f.Default); err != nil {
				c.add(f.Pos, "default: %v", err)
			}
		}
	case Secret:
		if f.Default != "" {
			c.add(f.Pos, "a secret field cannot have a default: the spec file would hold the secret")
		}
	}
}

// EnvName is a field id or row column as it appears in a command's
// environment: PANEL_FIELD_<EnvName(id)>, PANEL_ROW_<EnvName(column)>.
// Letters are upper-cased and anything other than a letter or digit becomes _.
func EnvName(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return '_'
	}, s)
}

var namedKeys = []string{
	"enter", "tab", "esc", "backspace", "delete", "insert", "space",
	"up", "down", "left", "right", "home", "end", "pgup", "pgdown",
}

// ValidKeyName reports whether k is a key as Bubble Tea names it: a printable
// character ("r", "R", "?"), a named key ("enter", "f5"), or either after
// ctrl+, alt+ or shift+. After ctrl+ or alt+ a letter is lower case, and
// shift+ never precedes a letter (shift+a arrives as A).
func ValidKeyName(k string) bool {
	parts := strings.Split(k, "+")
	base, mods := parts[len(parts)-1], parts[:len(parts)-1]
	if base == "" && strings.HasSuffix(k, "++") { // ctrl++
		base, mods = "+", parts[:len(parts)-2]
	}
	seen := map[string]bool{}
	for _, m := range mods {
		if (m != "ctrl" && m != "alt" && m != "shift") || seen[m] {
			return false
		}
		seen[m] = true
	}
	r := []rune(base)
	switch {
	case slices.Contains(namedKeys, base):
		return true
	case len(base) >= 2 && base[0] == 'f':
		n, err := strconv.Atoi(base[1:])
		return err == nil && n >= 1 && n <= 20
	case len(r) != 1:
		return false
	}
	ch := r[0]
	switch {
	case ch <= ' ' || ch == 0x7f:
		return false
	case seen["shift"] && ch >= 'a' && ch <= 'z':
		return false
	case (seen["ctrl"] || seen["alt"]) && ch >= 'A' && ch <= 'Z':
		return false
	}
	return true
}

// CheckInt reports whether s is a valid value for an int or duration field.
func CheckInt(f *Field, s string) error {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a whole number", s)
	}
	if f.Min != nil && n < *f.Min {
		return fmt.Errorf("%d is below the minimum %d", n, *f.Min)
	}
	if f.Max != nil && n > *f.Max {
		return fmt.Errorf("%d is above the maximum %d", n, *f.Max)
	}
	return nil
}

// CheckText reports whether s is a valid value for a text field.
func CheckText(f *Field, s string) error {
	if f.MaxLen > 0 && len([]rune(s)) > f.MaxLen {
		return fmt.Errorf("longer than %d characters", f.MaxLen)
	}
	if f.Pattern != "" {
		re, err := regexp.Compile(`^(?:` + f.Pattern + `)$`)
		if err != nil {
			return fmt.Errorf("pattern: %w", err)
		}
		if !re.MatchString(s) {
			return fmt.Errorf("%q does not match %s", s, f.Pattern)
		}
	}
	return nil
}

func checkTemplate(c *collector, t Template, fallback Pos, what string, allowed ...string) {
	p := posOr(t.pos, fallback)
	for _, ph := range t.Placeholders() {
		ns, name := ph[0], ph[1]
		switch {
		case !slices.Contains([]string{NSRow, NSField, NSSel, NSCtx, NSEnv}, ns):
			c.add(p, "%s: unknown placeholder namespace {%s.%s} (use row, field, sel, ctx or env)", what, ns, name)
		case !slices.Contains(allowed, ns):
			c.add(p, "%s cannot use {%s.%s} (allowed here: %s)", what, ns, name, strings.Join(allowed, ", "))
		case ns == NSCtx && !slices.Contains(CtxNames, name):
			c.add(p, "%s: unknown {ctx.%s} (known: %s)", what, name, strings.Join(CtxNames, ", "))
		}
	}
}

// hostVersion is HostVersion; tests replace it.
var hostVersion = HostVersion

// refusedByRequires reports whether the spec asks for a newer herdr-panel
// than this one. A refused spec gets that one error and no others: its other
// "errors" are most likely keys the newer version added.
func refusedByRequires(s *Spec) (string, bool) {
	want, ok := strings.CutPrefix(strings.TrimSpace(s.Requires), ">=")
	if !ok {
		return "", false
	}
	want = strings.TrimSpace(want)
	wv, err := parseSemver(want)
	if err != nil {
		return "", false
	}
	host := hostVersion()
	hv, err := parseSemver(host)
	if err != nil {
		return "", false // a development build accepts every spec
	}
	if compareSemver(hv, wv) < 0 {
		return fmt.Sprintf("this spec requires herdr-panel %s or newer; this is %s", want, host), true
	}
	return "", false
}

// bareTOMLKey reports whether k is a bare TOML key, the only kind a
// tomlfile field edits: no dots, no quotes.
func bareTOMLKey(k string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		switch {
		case r == '_', r == '-', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// checkSelCycles refuses lists whose sources follow each other's selection
// in a circle (a list following its own selection included): each would
// refresh the next for ever.
func checkSelCycles(s *Spec, c *collector) {
	deps := map[string][]string{}
	for _, sec := range s.Sections {
		if sec.Kind == ListSection && sec.ID != "" {
			deps[sec.ID] = sec.Source.SelLists()
		}
	}
	const (
		unseen = iota
		visiting
		done
	)
	state := map[string]int{}
	reported := map[string]bool{}
	var visit func(id string, path []string)
	visit = func(id string, path []string) {
		switch state[id] {
		case done:
			return
		case visiting:
			i := slices.Index(path, id)
			cycle := append(slices.Clone(path[i:]), id)
			if !reported[id] {
				reported[id] = true
				c.add(s.List(id).Pos, "list %q follows its own selection through {sel.*} (%s); a list's source cannot depend on itself", id, strings.Join(cycle, " -> "))
			}
			return
		}
		state[id] = visiting
		for _, d := range deps[id] {
			if _, ok := deps[d]; ok {
				visit(d, append(path, id))
			}
		}
		state[id] = done
	}
	ids := make([]string, 0, len(deps))
	for id := range deps {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		visit(id, nil)
	}
}
