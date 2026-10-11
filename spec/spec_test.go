package spec

import (
	"errors"
	"os"
	"path/filepath"
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
	if list.Columns[1].Width != 8 || list.Columns[0].Title != "Workspace" {
		t.Errorf("columns = %+v", list.Columns)
	}
	for _, f := range s.Fields() {
		if f.Type == Bool && f.Default != "true" && f.Default != "false" {
			t.Errorf("%s: bool default %q", f.Key, f.Default)
		}
	}
}

// Each fixture is a broken spec and the "line:col: message" its first error
// must contain. Errors are sorted by position.
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
		{"undefined store", head + "[[section]]\nfields = [ { key = \"A\", type = \"bool\", store = \"nope\" } ]\n", `names store "nope", which is not defined`},
		{"bad env key", head + "[[section]]\nfields = [ { key = \"1A\", type = \"bool\" } ]\n", `env-file key "1A" must be letters`},
		{"choice default", head + "[[section]]\nfields = [ { key = \"A\", type = \"choice\", choices = [\"x\"], default = \"y\" } ]\n", `default "y" is not one of the choices`},
		{"min over max", head + "[[section]]\nfields = [ { key = \"A\", type = \"int\", min = 5, max = 1 } ]\n", "min 5 is greater than max 1"},
		{"secret default", head + "[[section]]\nfields = [ { key = \"A\", type = \"secret\", default = \"hunter2\" } ]\n", "a secret field cannot have a default"},
		{"pattern", head + "[[section]]\nfields = [ { key = \"A\", type = \"text\", pattern = \"[a-z]+\", default = \"ABC\" } ]\n", `does not match [a-z]+`},
		{"unclosed placeholder", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [\"x\", \"{row.id\"] } ]\n", "8:41: unclosed {"},
		{"row in button", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [\"x\", \"{row.id}\"] } ]\n", "cannot use {row.id}"},
		{"unknown field ref", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [\"x\", \"{field.nope}\"] } ]\n", "refers to {field.nope}"},
		{"unknown ctx", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [\"x\", \"{ctx.window}\"] } ]\n", "unknown {ctx.window}"},
		{"reserved key", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", key = \"q\", run = [\"x\"] } ]\n", `key "q" is reserved`},
		{"duplicate key binding", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"A\", key = \"a\", run = [\"x\"] }, { label = \"B\", key = \"a\", run = [\"y\"] } ]\n", `key "a" is bound twice`},
		{"empty argv", head + "[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"Go\", run = [] } ]\n", "run must name a program"},
		{"list without source", head + "[[section]]\nkind = \"list\"\ncolumns = [\"id\"]\n", "needs source"},
		{"bad size", "spec = 1\ntitle = \"T\"\nsize = { width = \"70\" }\n", `3:18: width must be a number of cells or a percentage`},
		{"duplicate key", "spec = 1\ntitle = \"T\"\ntitle = \"U\"\n", `3:1: key "title" is defined twice`},
		{"duplicate table", head + "[stores.c]\n", `6:9: table [stores.c] is defined twice`},
		{"invalid toml", "spec = 1\ntitle = \"T\n", "2:11: invalid TOML"},
		{"unsupported store kind", "spec = 1\ntitle = \"T\"\n[stores.c]\nkind = \"jsonfile\"\npath = \"x.json\"\n", `4:8: store kind "jsonfile" is not supported`},
		{"save mode", "spec = 1\ntitle = \"T\"\nsave = \"never\"\n", `3:8: save must be`},
		{"requires syntax", "spec = 1\ntitle = \"T\"\nrequires = \"0.3\"\n", `3:12: requires must read`},
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
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("errors:\n%v\nwant one containing %q", err, c.want)
			}
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

func TestValidateSpecBuiltInGo(t *testing.T) {
	t.Parallel()
	s := &Spec{
		Title:  "Go",
		Stores: []*Store{{Name: "c", Kind: EnvFile, Path: MustTemplate("x.env")}},
		Sections: []*Section{{Kind: FieldsSection, Fields: []*Field{
			{ID: "a", Key: "A", Type: Bool, TrueValue: "1", FalseValue: "0", Default: "true"},
		}}},
	}
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
	if f := s.Fields()[0]; f.Store != "c" || f.Default != "1" {
		t.Errorf("defaults not filled: %+v", f)
	}
	if s.Version != Version || s.Save != SaveThenApply {
		t.Errorf("version/save = %d/%q", s.Version, s.Save)
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
