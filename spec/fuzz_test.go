package spec

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParse checks that no input makes the decoder panic, and that every
// rejection is a spec error (which carries positions), never a bare failure.
func FuzzParse(f *testing.F) {
	paths, _ := filepath.Glob("../examples/*.toml")
	for _, p := range paths {
		if data, err := os.ReadFile(p); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("spec = 1\ntitle = \"T\"\n[[section]]\nkind = \"text\"\ntext = \"x\"\n"))
	f.Add([]byte("a.b = 1\n[a]\n[[a.c]]\n[a.c.d]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := Parse(data, "fuzz.toml", "/tmp")
		if err != nil && !IsSpecError(err) {
			t.Fatalf("not a spec error: %v", err)
		}
		if err == nil && (s == nil || len(s.Sections) == 0) {
			t.Fatalf("accepted a spec with no sections: %q", data)
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
