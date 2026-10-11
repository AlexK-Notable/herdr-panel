package spec

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ReservedKeys are bound by every panel and cannot be an action's shortcut.
var ReservedKeys = []string{
	"?", "/", "q", "esc", "enter", "space", "tab", "shift+tab",
	"up", "down", "left", "right", "pgup", "pgdown", "home", "end",
	"ctrl+c", "ctrl+s", "ctrl+r",
}

// Validate fills in defaults (a field's store when the spec has only one, a
// bool default written as true or false) and checks the spec's meaning:
// references, ids, bounds, defaults and which placeholders each place may
// use. It returns an [Errors] value listing every problem.
func Validate(s *Spec) error {
	c := &collector{file: s.File}
	validate(s, c)
	sortErrors(c.errs)
	return c.err()
}

func validate(s *Spec, c *collector) {
	if s.Version != Version && s.Version != 0 {
		c.add(Pos{}, "spec version %d is not %d", s.Version, Version)
	}
	if s.Version == 0 && s.File == "" {
		s.Version = Version // built in Go without one
	}
	if s.Save == "" {
		s.Save = SaveThenApply
	}
	checkRequires(s, c)
	if len(s.Sections) == 0 {
		c.add(Pos{1, 1}, "the spec has no [[section]]; a panel needs at least one")
	}

	ids := map[string]*Field{}
	storeKeys := map[string]Pos{}
	for _, st := range s.Stores {
		if !validName(st.Name, false) {
			c.add(st.Pos, "store name %q must be letters, digits, _ and -", st.Name)
		}
		checkTemplate(c, st.Pos, st.Path, "store path", NSEnv, NSCtx)
	}
	for _, f := range s.Fields() {
		if f.Store == "" && len(s.Stores) == 1 {
			f.Store = s.Stores[0].Name
		}
		validateField(s, f, c)
		if f.ID != "" {
			if prev, dup := ids[f.ID]; dup {
				c.add(f.Pos, "field id %q is used twice (also at line %d); give one an id", f.ID, prev.Pos.Line)
			} else {
				ids[f.ID] = f
			}
		}
		sk := f.Store + "\x00" + f.Key
		if p, dup := storeKeys[sk]; dup && f.Key != "" {
			c.add(f.Pos, "store %q key %q has two fields (also at line %d)", f.Store, f.Key, p.Line)
		} else {
			storeKeys[sk] = f.Pos
		}
	}

	refs := func(p Pos, t Template, what string, ns ...string) {
		checkTemplate(c, p, t, what, ns...)
		for _, ph := range t.Placeholders() {
			if ph[0] == NSField {
				if _, ok := ids[ph[1]]; !ok {
					c.add(p, "%s refers to {field.%s}, but no field has that id", what, ph[1])
				}
			}
		}
	}
	cmdRefs := func(cmd *Command, what string, ns ...string) {
		if cmd == nil {
			return
		}
		for _, a := range cmd.Argv {
			refs(cmd.Pos, a, what, ns...)
		}
	}
	for _, st := range s.Stores {
		cmdRefs(st.Apply, fmt.Sprintf("store %q apply", st.Name), NSEnv, NSCtx, NSField)
	}
	for _, f := range s.Fields() {
		cmdRefs(f.Test, fmt.Sprintf("field %q test", f.ID), NSEnv, NSCtx, NSField)
	}

	keys := map[string]Pos{}
	checkKey := func(a *Action) {
		if a.Key == "" {
			return
		}
		if slices.Contains(ReservedKeys, a.Key) {
			c.add(a.Pos, "key %q is reserved by every panel (reserved: %s)", a.Key, strings.Join(ReservedKeys, " "))
		} else if p, dup := keys[a.Key]; dup {
			c.add(a.Pos, "key %q is bound twice (also at line %d)", a.Key, p.Line)
		} else {
			keys[a.Key] = a.Pos
		}
	}
	for _, sec := range s.Sections {
		switch sec.Kind {
		case ListSection:
			if sec.Source == nil {
				c.add(sec.Pos, "a list section needs a source command")
			}
			if len(sec.Columns) == 0 {
				c.add(sec.Pos, "a list section needs columns = [\"key\", ...]")
			}
			cmdRefs(sec.Source, "list source", NSEnv, NSCtx, NSField)
			for _, a := range sec.RowActions {
				checkKey(a)
				cmdRefs(a.Run, fmt.Sprintf("row action %q", a.Label), NSEnv, NSCtx, NSField, NSRow)
				refs(a.Pos, a.Confirm, fmt.Sprintf("row action %q confirm", a.Label), NSEnv, NSCtx, NSField, NSRow)
			}
		case ButtonsSection:
			for _, a := range sec.Buttons {
				checkKey(a)
				cmdRefs(a.Run, fmt.Sprintf("button %q", a.Label), NSEnv, NSCtx, NSField)
				refs(a.Pos, a.Confirm, fmt.Sprintf("button %q confirm", a.Label), NSEnv, NSCtx, NSField)
			}
		case TextSection:
			cmdRefs(sec.Source, "text source", NSEnv, NSCtx, NSField)
		case FieldsSection:
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
	notFor(f.Test != nil && f.Type != Secret, "test")
	notFor((f.TrueValue != "true" || f.FalseValue != "false") && f.Type != Bool, "true_value/false_value")

	switch f.Type {
	case Bool:
		switch f.Default {
		case "true":
			f.Default = f.TrueValue
		case "false":
			f.Default = f.FalseValue
		}
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
		if f.Pattern != "" {
			if _, err := regexp.Compile(f.Pattern); err != nil {
				c.add(f.Pos, "pattern does not compile: %v", err)
			} else if f.Default != "" {
				if err := CheckText(f, f.Default); err != nil {
					c.add(f.Pos, "default: %v", err)
				}
			}
		}
	case Secret:
		if f.Default != "" {
			c.add(f.Pos, "a secret field cannot have a default: the spec file would hold the secret")
		}
	}
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

func checkTemplate(c *collector, p Pos, t Template, what string, allowed ...string) {
	for _, ph := range t.Placeholders() {
		ns, name := ph[0], ph[1]
		switch {
		case !slices.Contains([]string{NSRow, NSField, NSCtx, NSEnv}, ns):
			c.add(p, "%s: unknown placeholder namespace {%s.%s} (use row, field, ctx or env)", what, ns, name)
		case !slices.Contains(allowed, ns):
			c.add(p, "%s cannot use {%s.%s} (allowed here: %s)", what, ns, name, strings.Join(allowed, ", "))
		case ns == NSCtx && !slices.Contains(CtxNames, name):
			c.add(p, "%s: unknown {ctx.%s} (known: %s)", what, name, strings.Join(CtxNames, ", "))
		}
	}
}

func checkRequires(s *Spec, c *collector) {
	if s.Requires == "" {
		return
	}
	want, ok := strings.CutPrefix(strings.TrimSpace(s.Requires), ">=")
	if !ok {
		c.add(s.requiresPos, "requires must read \">=X.Y\" or \">=X.Y.Z\", not %q", s.Requires)
		return
	}
	wv, err := parseSemver(strings.TrimSpace(want))
	if err != nil {
		c.add(s.requiresPos, "requires: %v", err)
		return
	}
	host := HostVersion()
	hv, err := parseSemver(host)
	if err != nil {
		return // a development build accepts every spec
	}
	if compareSemver(hv, wv) < 0 {
		c.add(s.requiresPos, "this spec requires herdr-panel %s or newer; this is %s", want, host)
	}
}
