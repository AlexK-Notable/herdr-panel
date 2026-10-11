package store

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joho/godotenv"
)

// fixtures cover what real env files hold. None uses $: godotenv expands
// variables, which a store must not do.
var fixtures = map[string]string{
	"plain":     "A=1\nB=two\n",
	"comments":  "# head\n\nA=1 # trailing\n  # indented\nB=x\n",
	"export":    "export A=1\nexport B=\"two words\"\n",
	"quotes":    "A='single quoted'\nB=\"double \\\"esc\\\" \\n line\"\nC=\"\"\n",
	"spacing":   "A = spaced\nB=  lead\n",
	"commented": "#A=default\nB=1\n# C=documented\n",
	"multiline": "A=\"line one\nline two\"\nB=after\n",
	"crlf":      "A=1\r\nB=2\r\n",
	"no-eol":    "A=1\nB=2",
	"dup":       "A=1\nA=2\n",
	"empty":     "",
}

// The store must read every fixture the way godotenv does: godotenv is the
// common reader of these files, used here only as an oracle.
func TestParseAgreesWithGodotenv(t *testing.T) {
	t.Parallel()
	for name, src := range fixtures {
		want, err := godotenv.Unmarshal(src)
		if err != nil {
			t.Fatalf("%s: godotenv: %v", name, err)
		}
		got := parseEnv([]byte(src)).values()
		if !maps.Equal(got, want) {
			t.Errorf("%s:\n got  %q\n want %q", name, got, want)
		}
	}
}

func TestUntouchedRoundTripIsByteIdentical(t *testing.T) {
	t.Parallel()
	for name, src := range fixtures {
		if got := string(parseEnv([]byte(src)).bytes()); got != src && (name != "no-eol" || got != src+"\n") {
			t.Errorf("%s: %q became %q", name, src, got)
		}
	}
}

func TestSetChangesOnlyItsLine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src, key, val, want string
	}{
		{"plain", "# c\nA=1\nB=2\n", "A", "9", "# c\nA=9\nB=2\n"},
		{"keeps export and comment", "export A=1 # note\n", "A", "2", "export A=2 # note\n"},
		{"keeps single quotes", "A='x'\n", "A", "y z", "A='y z'\n"},
		{"needs quotes", "A=1\n", "A", "two words", "A=\"two words\"\n"},
		{"escapes", "A=1\n", "A", "say \"hi\"\n$HOME", "A=\"say \\\"hi\\\"\\n\\$HOME\"\n"},
		{"commented default becomes real", "# about A\n#A=5\nB=1\n", "A", "7", "# about A\nA=7\nB=1\n"},
		{"appends", "B=1\n", "A", "1", "B=1\nA=1\n"},
		{"appends without eol", "B=1", "A", "1", "B=1\nA=1\n"},
		{"last duplicate wins", "A=1\nA=2\n", "A", "3", "A=1\nA=3\n"},
		{"crlf kept", "A=1\r\nB=2\r\n", "A", "3", "A=3\r\nB=2\r\n"},
		{"multiline replaced", "A=\"one\ntwo\"\nB=1\n", "A", "x", "A=\"x\"\nB=1\n"},
		{"indent kept", "  A=1\n", "A", "2", "  A=2\n"},
		{"new file", "", "A", "1", "A=1\n"},
	}
	for _, c := range cases {
		d := parseEnv([]byte(c.src))
		d.set(c.key, c.val)
		got := string(d.bytes())
		if got != c.want {
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, c.want)
		}
		// Whatever we write must read back as what was set, by both readers.
		if v := parseEnv([]byte(got)).values()[c.key]; v != c.val {
			t.Errorf("%s: reads back %q", c.name, v)
		}
		if !strings.Contains(c.val, "$") {
			if m, err := godotenv.Unmarshal(got); err != nil || m[c.key] != c.val {
				t.Errorf("%s: godotenv reads back %q (%v)", c.name, m[c.key], err)
			}
		}
	}
}

func TestUnset(t *testing.T) {
	t.Parallel()
	d := parseEnv([]byte("# keep\nA=1\nB=2\nA=3\n"))
	d.unset("A")
	if got := string(d.bytes()); got != "# keep\nB=2\n" {
		t.Errorf("got %q", got)
	}
}

func TestEnvFileStoreSaveAndConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "conf", "c.env")
	st := &EnvFile{Path: p, Create: true}

	snap, err := st.Load(ctx)
	if err != nil || len(snap.Values) != 0 || snap.Revision.Exists() {
		t.Fatalf("load of a missing file: %+v %v", snap, err)
	}
	snap, err = st.Save(ctx, snap, map[string]Change{"A": {Value: "1"}, "B": {Value: "x y"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "A=1\nB=\"x y\"\n" {
		t.Errorf("file = %q", got)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("new file mode %v", fi.Mode().Perm())
	}

	// Someone else edits the file: our next save must not overwrite it.
	if err := os.WriteFile(p, []byte("A=1\nB=\"x y\"\nC=theirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(ctx, snap, map[string]Change{"A": {Value: "2"}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	// The "overwrite" path: load again and re-apply only our change.
	fresh, err := st.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(ctx, fresh, map[string]Change{"A": {Value: "2"}, "B": {Unset: true}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "A=2\nC=theirs\n" {
		t.Errorf("file = %q", got)
	}
}

func TestEnvFileStoreWithoutCreate(t *testing.T) {
	t.Parallel()
	st := &EnvFile{Path: filepath.Join(t.TempDir(), "c.env")}
	snap, _ := st.Load(context.Background())
	if _, err := st.Save(context.Background(), snap, map[string]Change{"A": {Value: "1"}}); !errors.Is(err, ErrMissing) {
		t.Errorf("err = %v, want ErrMissing", err)
	}
}

// FuzzEnvRoundTrip: any value set must read back exactly, and a file
// nothing was set in must come back unchanged.
func FuzzEnvRoundTrip(f *testing.F) {
	for _, src := range fixtures {
		f.Add(src, "plain value")
	}
	f.Add("A=1\n", "with \"quotes\" and 'single' and \\ and $x and\nnewline")
	f.Fuzz(func(t *testing.T, src, val string) {
		if strings.ContainsRune(val, '\r') || strings.ContainsRune(src, '\r') {
			return // a bare CR is not a line ending this format keeps
		}
		d := parseEnv([]byte(src))
		if d.unterminated > 0 {
			return // Save refuses these files; see TestUnterminatedQuoteRefusesSave
		}
		before := d.values()
		d.set("FUZZ_KEY", val)
		out := d.bytes()
		after := parseEnv(out).values()
		if after["FUZZ_KEY"] != val {
			t.Fatalf("set %q, read back %q from %q", val, after["FUZZ_KEY"], out)
		}
		for k, v := range before {
			if k != "FUZZ_KEY" && after[k] != v {
				t.Fatalf("setting FUZZ_KEY changed %s: %q -> %q in %q", k, v, after[k], out)
			}
		}
	})
}

func TestUnterminatedQuoteRefusesSave(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "c.env")
	if err := os.WriteFile(p, []byte("B=1\nA=\"open\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := &EnvFile{Path: p}
	snap, err := st.Load(context.Background())
	if err != nil || snap.Values["B"] != "1" {
		t.Fatalf("load: %+v %v", snap, err)
	}
	_, err = st.Save(context.Background(), snap, map[string]Change{"C": {Value: "x"}})
	if !errors.Is(err, ErrUnterminated) || !strings.Contains(err.Error(), "c.env:2:") {
		t.Errorf("err = %v, want ErrUnterminated at line 2", err)
	}
}
