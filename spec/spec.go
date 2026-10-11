// Package spec defines the herdr-panel spec format: a TOML file describing a
// panel's stores, settings fields, lists, buttons and the commands behind them.
//
// Load reads and validates a spec. Every problem is reported with its
// file:line:col, all at once, as an [Errors] value. A spec can also be built in
// Go and checked with [Validate].
//
// A spec runs commands, so a spec is code: see docs/security.md.
package spec

import (
	"slices"
	"time"
)

// Version is the spec format version this package reads.
const Version = 1

// SaveMode says when edited values are written to their store.
type SaveMode string

const (
	// SaveThenApply writes all edits on Save, then runs each changed store's
	// apply command. It is the default.
	SaveThenApply SaveMode = "save_then_apply"
	// Live writes each edit as it is made, then applies it.
	Live SaveMode = "live"
)

// Spec is a validated panel description.
type Spec struct {
	File     string // the path it was loaded from, or "" when built in Go
	Dir      string // relative paths and programs resolve against this
	Version  int
	Requires string // a minimum herdr-panel version, ">=0.3"; "" for none
	Title    string
	Size     Size
	Save     SaveMode
	Stores   []*Store // in file order
	Sections []*Section

	requiresPos Pos
}

// Store returns the store with this name, or nil.
func (s *Spec) Store(name string) *Store {
	for _, st := range s.Stores {
		if st.Name == name {
			return st
		}
	}
	return nil
}

// Fields returns every field in every section, in order.
func (s *Spec) Fields() []*Field {
	var out []*Field
	for _, sec := range s.Sections {
		out = append(out, sec.Fields...)
	}
	return out
}

// Dim is one popup dimension: a number of cells or a percentage of the
// terminal. The zero value means "not set".
type Dim struct {
	Cells   int
	Percent int
}

// IsZero reports whether the dimension is unset.
func (d Dim) IsZero() bool { return d.Cells == 0 && d.Percent == 0 }

// Size is the popup size an opener asks herdr for. herdr's own popups take
// either form.
type Size struct {
	Width, Height Dim
}

// StoreKind names a store implementation.
type StoreKind string

// The store kinds.
const (
	// EnvFile is a KEY=value file: comments, order, quoting and `export`
	// are kept.
	EnvFile StoreKind = "envfile"
	// TOMLFile is a TOML file whose top-level keys hold the fields: only
	// edited lines change, so comments, tables and order are kept.
	TOMLFile StoreKind = "tomlfile"
)

// StoreKinds are the kinds this version reads.
var StoreKinds = []StoreKind{EnvFile, TOMLFile}

// Store is where field values live.
type Store struct {
	Name   string
	Kind   StoreKind
	Path   Template // expanded with {env.*} and {ctx.*}; relative to Dir
	Create bool     // create the file on first save when it does not exist
	Apply  *Command // run after a save that changed this store; may be nil
	Pos    Pos
}

// FieldType is a field's kind of value.
type FieldType string

// The field types.
const (
	Bool     FieldType = "bool"
	Choice   FieldType = "choice"
	Int      FieldType = "int"
	Duration FieldType = "duration_s" // whole seconds, shown as 1m30s
	Text     FieldType = "text"
	Secret   FieldType = "secret" // never shown; only set or unset
)

// Field is one editable value in a store.
type Field struct {
	ID          string // unique in the spec; {field.<ID>} refers to it
	Store       string
	Key         string
	Type        FieldType
	Label       string
	Help        string
	Default     string // the value shown when the store has none, as stored
	Required    bool   // an empty value blocks Save
	Choices     []ChoiceItem
	Min, Max    *int64
	Pattern     string // a regular expression a text value must match in full
	MaxLen      int
	Placeholder string
	TrueValue   string // what a bool stores; "true" by default
	FalseValue  string // "false" by default
	Test        *Command
	Pos         Pos
}

// ChoiceItem is one value a choice field can take.
type ChoiceItem struct {
	Value string
	Label string
}

// SectionKind is what a section shows.
type SectionKind string

// The section kinds.
const (
	FieldsSection  SectionKind = "fields"
	ListSection    SectionKind = "list"
	ButtonsSection SectionKind = "buttons"
	TextSection    SectionKind = "text"
	// ViewSection is drawn by Go code the panel's host supplies under the
	// section's View name (see panel.Options.Views). Experimental.
	ViewSection SectionKind = "view"
)

// Section is one titled part of the panel.
type Section struct {
	// Title may use {sel.*}, {ctx.*} and {env.*} placeholders (see
	// ExpandTitle); {{ and }} are literal braces.
	Title string
	Kind  SectionKind
	// ID names a list section, so other sections can follow its selected row
	// with {sel.<id>.<column>}. Only lists have one; it may be empty.
	ID string

	Fields []*Field // FieldsSection

	Source     *Command // ListSection and TextSection: prints rows or text
	Every      time.Duration
	Columns    []Column
	IDColumn   string
	Empty      string // shown when a list has no rows
	RowActions []*Action

	Buttons []*Action // ButtonsSection

	Text string // TextSection, when there is no source

	View string // ViewSection: the name the host supplies a View under

	Pos Pos
}

// Column is one column of a list.
type Column struct {
	Key   string
	Title string
	Width int // 0 shares the remaining width
}

// After says what a panel does when an action succeeds.
type After string

// The After values.
const (
	AfterRefresh After = "refresh" // reload lists and stores
	AfterClose   After = "close"   // close the panel
	AfterNothing After = "none"
	// AfterCloseThenRun closes the panel first and then runs the command,
	// detached, once the panel has exited; its result is never shown. It is
	// for an action that opens something the open panel would be in the way
	// of, such as another popup.
	AfterCloseThenRun After = "close_then_run"
)

// Action is a button or a row action.
type Action struct {
	Label   string
	Key     string // a shortcut, as Bubble Tea names keys; "" for none
	Run     *Command
	Confirm Template // a question asked in the panel before running; "" for none
	After   After
	Pos     Pos
}

// Command is an argument vector run without a shell.
type Command struct {
	Argv    []Template
	Timeout time.Duration
	Pos     Pos
}

// DefaultTimeout bounds a command whose spec gives none.
const DefaultTimeout = 10 * time.Second

// ExpandTitle is the section's title with its placeholders filled in. A value
// that is missing, such as {sel.*} while nothing is selected, shows as "…";
// a title that is not a valid template shows as written.
func (s *Section) ExpandTitle(v Values) string {
	t, err := ParseTemplate(s.Title)
	if err != nil {
		return s.Title
	}
	out, err := t.Expand(ValuesFunc(func(ns, name string) (string, bool) {
		if v != nil {
			if val, ok := v.Lookup(ns, name); ok {
				return val, true
			}
		}
		return "…", true
	}))
	if err != nil {
		return s.Title
	}
	return out
}

// List returns the list section with the given id, or nil.
func (s *Spec) List(id string) *Section {
	for _, sec := range s.Sections {
		if sec.Kind == ListSection && sec.ID != "" && sec.ID == id {
			return sec
		}
	}
	return nil
}

// SelLists are the list ids a command's arguments follow with {sel.*}.
func (c *Command) SelLists() []string {
	if c == nil {
		return nil
	}
	var out []string
	for _, a := range c.Argv {
		for _, ph := range a.Placeholders() {
			if ph[0] != NSSel {
				continue
			}
			if l, _ := SelRef(ph[1]); !slices.Contains(out, l) {
				out = append(out, l)
			}
		}
	}
	return out
}
