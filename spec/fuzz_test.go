package spec

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParse checks that no input makes the decoder panic, that every
// rejection is a spec error (which carries positions), never a bare failure,
// and that the published JSON Schema accepts every spec Parse accepts: an
// editor must never flag a spec the panel runs.
func FuzzParse(f *testing.F) {
	paths, _ := filepath.Glob("../examples/*.toml")
	for _, p := range paths {
		if data, err := os.ReadFile(p); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("spec = 1\ntitle = \"T\"\n[[section]]\nkind = \"text\"\ntext = \"x\"\n"))
	f.Add([]byte("a.b = 1\n[a]\n[[a.c]]\n[a.c.d]\n"))
	f.Add([]byte("a = [1, {b = 1}]\n[a.c]\n"))
	f.Add([]byte("x = [[1, 2], [\"a\"], {y = [{}]}]\n"))
	f.Add([]byte("spec = 1\ntitle = \"T\"\n[stores.c]\nkind = \"envfile\"\npath = \"c.env\"\n[[section]]\nfields = [ { key = \"A\", type = \"bool\", default = true } ]\n"))
	sch := compileSchema(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := Parse(data, "fuzz.toml", "/tmp")
		if err != nil && !IsSpecError(err) {
			t.Fatalf("not a spec error: %v", err)
		}
		if err == nil && (s == nil || len(s.Sections) == 0) {
			t.Fatalf("accepted a spec with no sections: %q", data)
		}
		if err == nil {
			if verr := sch.Validate(tomlAsJSON(t, data)); verr != nil {
				t.Fatalf("Parse accepted what the schema rejects: %v\n%q", verr, data)
			}
		}
	})
}

func FuzzTemplate(f *testing.F) {
	for _, s := range []string{"{row.id}", "{{x}}", "{env.A:-b}", "a{b.c}d"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		tpl, err := ParseTemplate(s)
		if err != nil {
			return
		}
		vals := ValuesFunc(func(string, string) (string, bool) { return "v", true })
		if _, err := tpl.Expand(vals); err != nil {
			t.Fatalf("parsed template %q failed to expand: %v", s, err)
		}
	})
}
