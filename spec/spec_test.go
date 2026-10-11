package spec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestExamplesLoad(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob("../examples/*.toml")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no examples found: %v", err)
	}
	for _, p := range paths {
		s, err := Load(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if s.Title == "" || len(s.Sections) == 0 {
			t.Errorf("%s: loaded empty: %+v", p, s)
		}
	}
}

func TestAutoTitleExampleShape(t *testing.T) {
	t.Parallel()
	s, err := Load("../examples/auto-title.toml")
	if err != nil {
		t.Fatal(err)
	}
	if s.Size.Width.Percent != 70 || s.Save != SaveThenApply {
		t.Errorf("size/save = %+v / %q", s.Size, s.Save)
	}
	st := s.Store("config")
	if st == nil || st.Apply == nil || st.Apply.Argv[0].Raw != "herdr" {
		t.Fatalf("store config = %+v", st)
	}
	f := s.Fields()[0]
	if f.Store != "config" || f.Type != Int || *f.Min != 8 || f.Default != "50" {
		t.Errorf("first field = %+v", f)
	}
	list := s.Sections[2]
	if list.Kind != ListSection || list.Every != 5*time.Second || len(list.RowActions) != 2 {
		t.Errorf("list section = %+v", list)
	}
	if list.Columns[1].Key != "named_by" || list.Columns[1].Width != 12 || list.Columns[0].Title != "Workspace" {
		t.Errorf("columns = %+v", list.Columns)
	}
	for _, f := range s.Fields() {
		if f.Type == Bool && f.Default != "true" && f.Default != "false" {
			t.Errorf("%s: bool default %q", f.Key, f.Default)
		}
	}
}

// Each fixture is a broken spec and "line:col: text": one of its errors must
// sit at that position and contain the text.
func TestErrorPositions(t *testing.T) {
	t.Parallel()
	head := "spec = 1\ntitle = \"T\"\n[stores.c]\nkind = \"envfile\"\npath = \"x.env\"\n"
	cases := []struct {
		name, src, want string
	}{
		{"no spec version", "title = \"T\"\n[[section]]\ntext = \"x\"\nkind = \"text\"\n", "1:1: the spec needs `spec = 1`"},
		{"wrong version", "spec = 2\ntitle = \"T\"\n", "1:8: spec = 2 is not a version"},
		{"unknown top key", "spec = 1\ntitle = \"T\"\ncolour = \"red\"\n", `3:1: unknown key "colour" in the spec`},
		{"unknown field key", head + "[[section]]\nfields = [\n  { key = \"A\", type = \"bool\", lable = \"x\" },\n]\n", `8:31: unknown key "lable" in field "A"`},
		{"wrong type", head + "[[section]]\nfields = [ { key = \"A\", type = \"int\", min = \"3\" } ]\n", "7:45: min must be an integer, not a string"},
		{"unknown field type", head + "[[section]]\nfields = [ { key = \"A\", type = \"colour\" } ]\n", `7:32: field type must be bool`},
		{"undefined store", head + "[[section]]\nfields = [ { key = \"A\", type = \"bool\", store = \"nope\" } ]\n", `7:12: names store "nope", which is not defined`},
		{"bad env key", head + "[[section]]\nfields = [ { key = \"1A\", type = \"bool\" } ]\n", `7:12: env-file key "1A" must be letters`},
		{"choice default", head + "[[section]]\nfields = [ { key = \"A\", type = \"choice\", choices = [\"x\"], default = \"y\" } ]\n", `7:12: default "y" is not one of the choices`},
		{"min over max", head + "[[section]]\nfields = [ { key = \"A\", type = \"int\", min = 5, max = 1 } ]\n", "7:12: min 5 is greater than max 1"},
		{"secret default", head + "[[section]]\nfields = [ { key = \"A\", type = \"secret\", default = \"hunter2\" } ]\n", "7:12: a secret field cannot have a default"},
		{"pattern", head + "[[section]]\nfields = [ { key = \"A\", type = \"text\", pattern = \"[a-z]+\", default = \"ABC\" } ]\n", `7:12: does not match [a-z]+`},
		{"unclosed placeholder", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [\"x\", \"{row.id\"] } ]\n", "8:41: unclosed {"},
		{"row in button", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [\"x\", \"{row.id}\"] } ]\n", "8:41: cannot use {row.id}"},
		{"unknown field ref", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [\"x\", \"{field.nope}\"] } ]\n", "8:41: refers to {field.nope}"},
		{"unknown ctx", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [\"x\", \"{ctx.window}\"] } ]\n", "8:41: unknown {ctx.window}"},
		{"reserved key", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", key = \"q\", run = [\"x\"] } ]\n", `8:13: key "q" is reserved`},
		{"duplicate key binding", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"A\", key = \"a\", run = [\"x\"] }, { label = \"B\", key = \"a\", run = [\"y\"] } ]\n", `8:54: key "a" is bound twice`},
		{"empty argv", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [] } ]\n", "8:29: run must name a program"},
		{"list without source", head + "[[section]]\nkind = \"list\"\ncolumns = [\"id\"]\n", "6:3: needs source"},
		{"bad size", "spec = 1\ntitle = \"T\"\nsize = { width = \"70\" }\n", `3:18: width must be a number of cells or a percentage`},
		{"duplicate key", "spec = 1\ntitle = \"T\"\ntitle = \"U\"\n", `3:1: key "title" is defined twice`},
		{"duplicate table", head + "[stores.c]\n", `6:9: table [stores.c] is defined twice`},
		{"invalid toml", "spec = 1\ntitle = \"T\n", "2:11: invalid TOML"},
		{"unsupported store kind", "spec = 1\ntitle = \"T\"\n[stores.c]\nkind = \"jsonfile\"\npath = \"x.json\"\n", `4:8: store kind "jsonfile" is not supported`},
		{"save mode", "spec = 1\ntitle = \"T\"\nsave = \"never\"\n", `3:8: save must be`},
		{"dotted toml key", "spec = 1\ntitle = \"T\"\n[stores.c]\nkind = \"tomlfile\"\npath = \"x.toml\"\n[[section]]\nfields = [ { key = \"a.b\", type = \"bool\" } ]\n", `7:12: TOML key "a.b" must be a bare top-level key`},
		{"toml bool values", "spec = 1\ntitle = \"T\"\n[stores.c]\nkind = \"tomlfile\"\npath = \"x.toml\"\n[[section]]\nfields = [ { key = \"on\", type = \"bool\", true_value = \"yes\", false_value = \"no\" } ]\n", `7:12: a bool in a TOML file is true or false`},
		{"requires syntax", "spec = 1\ntitle = \"T\"\nrequires = \"0.3\"\n", `3:12: requires must read`},
		{"secret in a command", head + "[[section]]\nfields = [ { key = \"T\", type = \"secret\" } ]\n[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [\"x\", \"--token={field.T}\"] } ]\n", `10:41: cannot use {field.T}: it is a secret`},
		{"secret in a confirm", head + "[[section]]\nfields = [ { key = \"T\", type = \"secret\" } ]\n[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [\"x\"], confirm = \"Send {field.T}?\" } ]\n", `10:52: cannot use {field.T}: it is a secret`},
		{"program from a row", head + "[[section]]\nkind = \"list\"\nsource = [\"rows\"]\ncolumns = [\"id\"]\nrow_actions = [ { label = \"Go\", run = [\"{row.id}\"] } ]\n", `10:40: the program cannot come from {row.id}`},
		{"field env names clash", head + "[[section]]\nfields = [ { key = \"A\", id = \"a-b\", type = \"bool\" }, { key = \"B\", id = \"a_b\", type = \"bool\" } ]\n", `7:54: would both reach commands as PANEL_FIELD_A_B`},
		{"bad key name", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", key = \"ctrl+A\", run = [\"x\"] } ]\n", `8:13: key "ctrl+A" is not a key name`},
		{"max_len default", head + "[[section]]\nfields = [ { key = \"A\", type = \"text\", max_len = 2, default = \"abc\" } ]\n", `7:12: default: longer than 2 characters`},
		{"placeholder on a bool", head + "[[section]]\nfields = [ { key = \"A\", type = \"bool\", placeholder = \"x\" } ]\n", `7:12: placeholder does not apply to a bool field`},
		{"every_s type", head + "[[section]]\nkind = \"list\"\ncolumns = [\"id\"]\nsource = { run = [\"rows\"], every_s = \"5\" }\n", `9:38: every_s must be an integer`},
		{"header after dotted keys", "spec = 1\ntitle = \"T\"\nstores.c.kind = \"envfile\"\n[stores.c]\n", `4:9: table [stores.c] is defined twice`},
		{"extend an inline table", "spec = 1\ntitle = \"T\"\nsize = { width = 10 }\n[size]\n", `4:2: is defined twice`},
		{"array table after a static array", "spec = 1\ntitle = \"T\"\nsection = []\n[[section]]\n", `4:3: [[section]] conflicts with a value defined earlier`},
		{"table after an array table", "spec = 1\ntitle = \"T\"\n[[section]]\n[section]\n", `4:2: is defined twice`},
		{"view without a name", head + "[[section]]\nkind = \"view\"\n", "6:3: needs view"},
		{"view shown twice", head + "[[section]]\nkind = \"view\"\nview = \"g\"\n[[section]]\nkind = \"view\"\nview = \"g\"\n", `9:3: view "g" is shown twice`},
		{"bad view name", head + "[[section]]\nkind = \"view\"\nview = \"a b\"\n", `6:3: view name "a b" must be`},
		{"table inside a mixed array", "spec = 1\ntitle = \"T\"\na = [1, {b = 1}]\n[a.c]\n", `4:2: "a" is already`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(c.src), "x.toml", "/tmp")
			if err == nil {
				t.Fatalf("no error; want %q", c.want)
			}
			if !IsSpecError(err) {
				t.Fatalf("error is not a spec error: %v", err)
			}
			var line, col int
			if n, _ := fmt.Sscanf(c.want, "%d:%d:", &line, &col); n != 2 {
				t.Fatalf("fixture want %q does not start with line:col:", c.want)
			}
			_, msg, _ := strings.Cut(c.want, ": ")
			var es Errors
			if !errors.As(err, &es) {
				t.Fatalf("err = %v", err)
			}
			for _, e := range es {
				if e.Pos.Line == line && e.Pos.Col == col && strings.Contains(e.Msg, msg) {
					return
				}
			}
			t.Errorf("errors:\n%v\nwant one at %d:%d containing %q", err, line, col, msg)
		})
	}
}

func TestAllErrorsReportedAtOnce(t *testing.T) {
	t.Parallel()
	src := "spec = 1\ntitle = \"T\"\nfoo = 1\nbar = 2\n"
	_, err := Parse([]byte(src), "x.toml", "/tmp")
	var es Errors
	if !errors.As(err, &es) {
		t.Fatalf("err = %v", err)
	}
	// foo, bar, and "no section".
	if len(es) != 3 {
		t.Errorf("got %d errors, want 3:\n%v", len(es), err)
	}
	if es[0].Pos.Line != 1 || es[1].Pos.Line != 3 || es[2].Pos.Line != 4 {
		t.Errorf("not sorted by line:\n%v", err)
	}
}

func TestErrorFormatCarriesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(p, []byte("spec = 1\ntitle = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), p+":2:9: title must be a string") {
		t.Errorf("err = %v", err)
	}
}

func TestDottedKeysAndArrayTables(t *testing.T) {
	t.Parallel()
	src := `spec = 1
title = "T"
stores.c.kind = "envfile"
stores.c.path = "x.env"
[[section]]
fields = [ { key = "A", type = "bool" } ]
[[section]]
title = "Two"
kind = "text"
text = "hi"
`
	s, err := Parse([]byte(src), "x.toml", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Sections) != 2 || s.Sections[1].Text != "hi" || s.Fields()[0].Store != "c" {
		t.Errorf("spec = %+v", s)
	}
}

func goSpec() *Spec {
	return &Spec{
		Title:  "Go",
		Stores: []*Store{{Name: "c", Kind: EnvFile, Path: Template{Raw: "{env.HOME}/x.env"}}},
		Sections: []*Section{
			{Fields: []*Field{
				{Key: "A", Type: Bool, TrueValue: "1", FalseValue: "0", Default: "1"},
				{Key: "T", Type: Secret},
			}},
			{
				Kind: ListSection, Source: &Command{Argv: []Template{{Raw: "rows"}}}, Columns: []Column{{Key: "name"}},
				RowActions: []*Action{{Label: "Open", Key: "o", Run: &Command{Argv: []Template{{Raw: "open"}, {Raw: "{row.id}"}, {Raw: "--a={field.A}"}}}}},
			},
		},
	}
}

func TestValidateSpecBuiltInGo(t *testing.T) {
	t.Parallel()
	s := goSpec()
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
	f := s.Fields()[0]
	if f.Store != "c" || f.ID != "A" || f.Label != "A" || f.Default != "1" {
		t.Errorf("field defaults not filled: %+v", f)
	}
	if s.Version != Version || s.Save != SaveThenApply {
		t.Errorf("version/save = %d/%q", s.Version, s.Save)
	}
	list := s.Sections[1]
	a := list.RowActions[0]
	if s.Sections[0].Kind != FieldsSection || list.IDColumn != "id" || list.Columns[0].Title != "name" ||
		a.After != AfterRefresh || a.Run.Timeout != DefaultTimeout || list.Source.Timeout != DefaultTimeout {
		t.Errorf("section defaults not filled: %+v %+v", list, a)
	}
	// A Template written as a literal expands like a parsed one.
	got, err := ExpandArgv(a.Run.Argv, ValuesFunc(func(ns, n string) (string, bool) { return ns + "." + n, true }), "")
	if err != nil || strings.Join(got, " ") != "open row.id --a=field.A" {
		t.Errorf("argv = %q, %v", got, err)
	}
}

func TestValidateIsIdempotent(t *testing.T) {
	t.Parallel()
	s := goSpec()
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var b strings.Builder
		fmt.Fprintf(&b, "%+v", *s)
		for _, sec := range s.Sections {
			fmt.Fprintf(&b, "%+v", *sec)
		}
		return b.String()
	}
	before := snapshot()
	if err := Validate(s); err != nil {
		t.Fatalf("second Validate: %v", err)
	}
	after := snapshot()
	if before != after {
		t.Errorf("Validate changed the spec the second time:\n%s\n%s", before, after)
	}
}

func TestValidateChecksGoShapes(t *testing.T) {
	t.Parallel()
	s := goSpec()
	s.Save = "sometimes"
	s.Sections[0].Fields[0].Type = "colour"
	s.Sections[1].Kind = ListSection
	s.Sections[1].Source = nil
	s.Sections[1].RowActions[0].After = "explode"
	s.Sections[1].RowActions[0].Run.Timeout = 2 * time.Hour
	s.Stores[0].Path = Template{Raw: "{env.HOME"}
	err := Validate(s)
	for _, want := range []string{
		`save must be`, `type must be bool`, `a list section needs a source command`,
		`after must be refresh, close or none, not "explode"`, `timeout must be 1 s to 1 h`, `store "c" path: unclosed {`,
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want an error containing %q; got:\n%v", want, err)
		}
	}
}

func TestSecretsNeverReachCommands(t *testing.T) {
	t.Parallel()
	s := goSpec()
	s.Sections[1].RowActions[0].Run.Argv = append(s.Sections[1].RowActions[0].Run.Argv, Template{Raw: "{field.T}"})
	err := Validate(s)
	if err == nil || !strings.Contains(err.Error(), "cannot use {field.T}: it is a secret") {
		t.Errorf("err = %v", err)
	}
}

func TestKeyNames(t *testing.T) {
	t.Parallel()
	for k, want := range map[string]bool{
		"r": true, "R": true, "?": true, "ctrl+o": true, "alt+x": true, "f5": true, "f20": true, "ctrl++": true,
		"shift+tab": true, "ctrl+shift+up": true, "pgdown": true,
		"": false, "ctrl+O": false, "shift+a": false, "f0": false, "f21": false, "super+x": false, "ctrl+ctrl+x": false,
		"rr": false, "Enter": false, " ": false,
	} {
		if got := ValidKeyName(k); got != want {
			t.Errorf("ValidKeyName(%q) = %v, want %v", k, got, want)
		}
	}
}

func TestVersionIn(t *testing.T) {
	t.Parallel()
	mod := func(v string, replaced bool) *debug.BuildInfo {
		m := debug.Module{Path: modulePath, Version: v}
		if replaced {
			m.Replace = &debug.Module{Path: "../herdr-panel"}
		}
		return &debug.BuildInfo{Main: debug.Module{Path: "example.com/consumer"}, Deps: []*debug.Module{&m}}
	}
	for _, c := range []struct {
		info *debug.BuildInfo
		want string
	}{
		{mod("v0.3.1", false), "v0.3.1"},
		{mod("v0.3.1", true), "devel"}, // a go.work or replace builds local source
		{mod("(devel)", false), "devel"},
		{&debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "(devel)"}}, "devel"},
		{&debug.BuildInfo{Main: debug.Module{Path: "other"}}, "devel"},
	} {
		if got := versionIn(c.info); got != c.want {
			t.Errorf("versionIn(%+v) = %q, want %q", c.info.Deps, got, c.want)
		}
	}
}

// TestRequiresRefusalIsTheOnlyError swaps the package-level hostVersion, so
// it does not run in parallel: parallel tests wait until it has finished.
func TestRequiresRefusalIsTheOnlyError(t *testing.T) { //nolint:paralleltest // mutates hostVersion
	old := hostVersion
	hostVersion = func() string { return "v0.2.0" }
	t.Cleanup(func() { hostVersion = old })

	// A newer spec uses a key this host does not know; only the refusal shows.
	src := "spec = 1\ntitle = \"T\"\nrequires = \">=0.3\"\nshiny = true\n[[section]]\nkind = \"text\"\ntext = \"x\"\n"
	_, err := Parse([]byte(src), "x.toml", "/tmp")
	var es Errors
	if !errors.As(err, &es) || len(es) != 1 || !strings.Contains(es[0].Error(), "x.toml:3:12: this spec requires herdr-panel 0.3 or newer; this is v0.2.0") {
		t.Fatalf("err = %v", err)
	}
	for _, ok := range []string{">=0.2", ">= v0.1.9", ">=0.2.0"} {
		src := "spec = 1\ntitle = \"T\"\nrequires = \"" + ok + "\"\n[[section]]\nkind = \"text\"\ntext = \"x\"\n"
		if _, err := Parse([]byte(src), "x.toml", "/tmp"); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	s := goSpec()
	s.Requires = ">=1.0"
	if err := Validate(s); err == nil || !strings.Contains(err.Error(), "requires herdr-panel 1.0 or newer") {
		t.Errorf("Validate: %v", err)
	}
}

func TestRequires(t *testing.T) {
	t.Parallel()
	// A test binary is a development build, which accepts any requirement.
	src := "spec = 1\ntitle = \"T\"\nrequires = \">=99.0\"\n[[section]]\nkind = \"text\"\ntext = \"x\"\n"
	if _, err := Parse([]byte(src), "x.toml", "/tmp"); err != nil {
		t.Errorf("devel build refused: %v", err)
	}
	a, _ := parseSemver("v0.3.1")
	b, _ := parseSemver("0.10")
	if compareSemver(a, b) >= 0 || compareSemver(b, a) <= 0 || compareSemver(a, a) != 0 {
		t.Error("semver ordering is wrong")
	}
}
