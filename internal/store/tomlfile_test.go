package store

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

// A config in the shape real plugins ship: documented, commented-out
// defaults, a commented-out table, and a real one.
const viewerLike = `# my-viewer configuration
#
# Every setting is commented out.

# --- Programs ---
# Editor for the e key.
#editor = "code --wait"

# --- Startup ---
#hide_dotfiles = false
#scroll_lines = 3

# --- Keys ---
# Remap keys by intent name.
#[keys]
#refresh = "g"
`

const withTables = `title = "x" # the name
n = 3

[keys]
refresh = "g"

[[hooks]]
run = "a"
`

func edit(t *testing.T, src string, kinds map[string]Kind, changes map[string]Change) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if src != "" {
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f := &TOMLFile{Path: path, Create: true, Kinds: kinds}
	snap, err := f.Load(context.Background())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err := f.Save(context.Background(), snap, changes); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func set(v string) Change { return Change{Value: v} }

var viewerKinds = map[string]Kind{"hide_dotfiles": KindBool, "scroll_lines": KindInt, "n": KindInt}

func TestTOMLCommentedDefaultBecomesTheLine(t *testing.T) {
	t.Parallel()
	got := edit(t, viewerLike, viewerKinds, map[string]Change{
		"editor": set(`nvim "-c" x`), "scroll_lines": set("5"),
	})
	want := strings.Replace(viewerLike, `#editor = "code --wait"`, `editor = "nvim \"-c\" x"`, 1)
	want = strings.Replace(want, "#scroll_lines = 3", "scroll_lines = 5", 1)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A key with no commented default goes above the commented-out [keys]
// header and its comments: uncommenting [keys] later must not swallow it.
func TestTOMLNewKeyStaysAboveACommentedTable(t *testing.T) {
	t.Parallel()
	got := edit(t, viewerLike, nil, map[string]Change{"theme": set("dark")})
	want := strings.Replace(viewerLike, "# --- Keys ---", "theme = \"dark\"\n\n# --- Keys ---", 1)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// The commented `#refresh = "g"` under #[keys] is that table's, not a
	// top-level default.
	got = edit(t, viewerLike, nil, map[string]Change{"refresh": set("r")})
	if !strings.Contains(got, "#refresh = \"g\"") || !strings.Contains(got, "refresh = \"r\"\n\n# --- Keys") {
		t.Errorf("got:\n%s", got)
	}
}

func TestTOMLKeepsTablesCommentsAndOrder(t *testing.T) {
	t.Parallel()
	got := edit(t, withTables, viewerKinds, map[string]Change{
		"title": set("y"), "n": set("4"), "added": set("z"), "refresh": set("top"),
	})
	want := `title = "y" # the name
n = 4
added = "z"
refresh = "top"

[keys]
refresh = "g"

[[hooks]]
run = "a"
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTOMLNewKeyBeforeTheFirstTable(t *testing.T) {
	t.Parallel()
	got := edit(t, "# head\n[keys]\nx = 1\n", nil, map[string]Change{"a": set("1")})
	if want := "a = \"1\"\n\n# head\n[keys]\nx = 1\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTOMLUnsetAndInsertInOneSave(t *testing.T) {
	t.Parallel()
	got := edit(t, "a = 1\nb = 2 # keep\nc = 3\n\n[t]\nx = 1\n", map[string]Kind{"a": KindInt, "c": KindInt, "d": KindInt},
		map[string]Change{"a": {Unset: true}, "c": set("30"), "d": set("4")})
	if want := "b = 2 # keep\nc = 30\nd = 4\n\n[t]\nx = 1\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTOMLLineEnds(t *testing.T) {
	t.Parallel()
	if got := edit(t, "a = 1\r\nb = 2\r\n", map[string]Kind{"a": KindInt}, map[string]Change{"a": set("9")}); got != "a = 9\r\nb = 2\r\n" {
		t.Errorf("CRLF: %q", got)
	}
	if got := edit(t, "a = 1\nb = 2", map[string]Kind{"a": KindInt}, map[string]Change{"a": set("9")}); got != "a = 9\nb = 2" {
		t.Errorf("no final line end, last line kept: %q", got)
	}
	if got := edit(t, "", map[string]Kind{"on": KindBool}, map[string]Change{"on": set("true")}); got != "on = true\n" {
		t.Errorf("new file: %q", got)
	}
}

func TestTOMLStringsRoundTrip(t *testing.T) {
	t.Parallel()
	for _, v := range []string{`quote " and \ back`, "tab\tnew\nline", "ctrl \x01 \x7f", "unicode é ✓", "# not a comment", ""} {
		got := edit(t, "s = 'old' # c\n", nil, map[string]Change{"s": set(v)})
		vals, err := tomlValues([]byte(got))
		if err != nil || vals["s"] != v || !strings.HasSuffix(got, " # c\n") {
			t.Errorf("%q: wrote %q (%v)", v, got, err)
		}
	}
}

func TestTOMLRefusals(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, src string) *TOMLFile {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		return &TOMLFile{Path: p, Kinds: map[string]Kind{"b": KindBool, "n": KindInt}}
	}
	ctx := context.Background()
	f := write("multi.toml", "s = \"\"\"\nline\n\"\"\"\n")
	snap, _ := f.Load(ctx)
	if _, err := f.Save(ctx, snap, map[string]Change{"s": set("x")}); !errors.Is(err, ErrMultiline) {
		t.Errorf("multi-line value: %v", err)
	}
	g := write("kinds.toml", "")
	snap, _ = g.Load(ctx)
	for k, v := range map[string]string{"b": "yes", "n": "1.5", "a.b": "x"} {
		if _, err := g.Save(ctx, snap, map[string]Change{k: set(v)}); err == nil {
			t.Errorf("%s = %q was written", k, v)
		}
	}
	h := write("bad.toml", "a = \n")
	if _, err := h.Load(ctx); err == nil {
		t.Error("invalid TOML loaded")
	}
	m := &TOMLFile{Path: filepath.Join(dir, "missing.toml")}
	snap, _ = m.Load(ctx)
	if _, err := m.Save(ctx, snap, map[string]Change{"a": set("1")}); !errors.Is(err, ErrMissing) {
		t.Errorf("missing file without create: %v", err)
	}
	c := write("conflict.toml", "a = 1\n")
	snap, _ = c.Load(ctx)
	if err := os.WriteFile(c.Path, []byte("a = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Save(ctx, snap, map[string]Change{"a": set("3")}); !errors.Is(err, ErrConflict) {
		t.Errorf("concurrent edit: %v", err)
	}
}

// Load reads top-level scalars only; tables are not fields.
func TestTOMLLoadTopLevelOnly(t *testing.T) {
	t.Parallel()
	vals, err := tomlValues([]byte(withTables + "f = 1.5\nb = true\n"))
	if err != nil {
		t.Fatal(err)
	}
	// f and b follow [[hooks]], so they belong to it.
	if want := map[string]string{"title": "x", "n": "3"}; !maps.Equal(vals, want) {
		t.Errorf("values = %v", vals)
	}
}

// FuzzTOMLSet: any single edit of a parseable file leaves it parseable,
// with the key set and everything else, tables included, unchanged.
func FuzzTOMLSet(f *testing.F) {
	for _, src := range []string{viewerLike, withTables, "", "a = 1\nb = [1,\n 2]\n", "x = { y = 1 }\n# end"} {
		f.Add(src, "a", "v")
	}
	f.Fuzz(func(t *testing.T, src, key, val string) {
		if !bareKey.MatchString(key) {
			return
		}
		var before map[string]any
		if toml.Unmarshal([]byte(src), &before) != nil {
			return
		}
		doc, err := parseTOMLDoc([]byte(src))
		if err != nil {
			return
		}
		lit, err := tomlLiteral(val, KindString)
		if (err != nil) != !utf8.ValidString(val) {
			t.Fatalf("%q: tomlLiteral error %v", val, err)
		}
		if err != nil || doc.set(key, lit) != nil {
			return // multi-line: refused
		}
		var after map[string]any
		if _, isScalar := tomlValues2(before)[key]; !isScalar && before[key] != nil {
			// A [table] of that name: the edit does not parse, and Save's
			// re-parse refuses it (TestTOMLKeyNamingATableIsRefused). An
			// inline table or array was refused by set itself, above.
			return
		}
		if err := toml.Unmarshal(doc.bytes(), &after); err != nil {
			t.Fatalf("set %s in %q: output does not parse: %v\n%s", key, src, err, doc.bytes())
		}
		if after[key] != val {
			t.Fatalf("set %s = %q in %q: got %#v\n%s", key, val, src, after[key], doc.bytes())
		}
		delete(before, key)
		delete(after, key)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("set %s in %q changed other values:\n%v\n%v", key, src, before, after)
		}
	})
}

// tomlValues2 is tomlValues over an already decoded map.
func tomlValues2(m map[string]any) map[string]bool {
	out := map[string]bool{}
	for k, v := range m {
		switch v.(type) {
		case string, bool, int64, float64:
			out[k] = true
		}
	}
	return out
}

func TestTOMLKeyNamingATableIsRefused(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "c.toml")
	src := "a = 1\n\n[[hooks]]\nrun = \"x\"\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &TOMLFile{Path: path}
	snap, _ := f.Load(context.Background())
	_, err := f.Save(context.Background(), snap, map[string]Change{"hooks": set("y")})
	if err == nil || !strings.Contains(err.Error(), "not written") {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != src {
		t.Errorf("the file changed: %q", data)
	}
}

// The cases the M6/M7 review reproduced, each with its input.
func TestTOMLReviewCases(t *testing.T) {
	t.Parallel()
	ints := map[string]Kind{"b": KindInt, "c": KindInt}
	for _, c := range []struct {
		name, in, key, val, want string
		kinds                    map[string]Kind
	}{
		// A line of another key's multi-line value is value, not a comment.
		{
			"commented default in a multi-line string", "script = \"\"\"\n#editor = old\n\"\"\"\n", "editor", "vim",
			"script = \"\"\"\n#editor = old\n\"\"\"\neditor = \"vim\"\n", nil,
		},
		{
			"in a literal string", "script = '''\n# editor = x\nmore\n'''\n[keys]\n", "editor", "vim",
			"script = '''\n# editor = x\nmore\n'''\neditor = \"vim\"\n[keys]\n", nil,
		},
		{
			"in a dotted key's value", "a.b = \"\"\"\n#editor = x\n\"\"\"\n", "editor", "vim",
			"a.b = \"\"\"\n#editor = x\n\"\"\"\neditor = \"vim\"\n", nil,
		},
		{
			"a header inside a string", "s = \"\"\"\n[x]\n\"\"\"\n#b = 1\n", "b", "5",
			"s = \"\"\"\n[x]\n\"\"\"\nb = 5\n", ints,
		},
		{
			"in a multi-line array", "arr = [\n  1,\n  #editor = 2\n]\n", "editor", "x",
			"arr = [\n  1,\n  #editor = 2\n]\neditor = \"x\"\n", nil,
		},
		// Prose that reads like a setting is not one.
		{
			"prose comment", "# editor = the program e opens\n#editor = \"vim\"\n", "editor", "nano",
			"# editor = the program e opens\neditor = \"nano\"\n", nil,
		},
		// A commented default keeps its own comment.
		{
			"commented default's comment", "#baseline = \"base\" # optional override\n", "baseline", "head",
			"baseline = \"head\" # optional override\n", nil,
		},
		// CRLF without a final line end stays CRLF throughout.
		{"crlf, no final line end", "a = 1\r\nb = 2", "c", "3", "a = 1\r\nb = 2\r\nc = 3\r\n", ints},
		// A byte order mark is kept.
		{"bom", "\ufeffa = 1\n", "c", "3", "\ufeffa = 1\nc = 3\n", ints},
	} {
		if got := edit(t, c.in, c.kinds, map[string]Change{c.key: set(c.val)}); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestTOMLNonScalarValuesAreRefused(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"x = { y = 1 }\n", "x = [1, 2]\n", "x = 1979-05-27\n"} {
		path := filepath.Join(t.TempDir(), "c.toml")
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		f := &TOMLFile{Path: path}
		snap, err := f.Load(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range []Change{set("v"), {Unset: true}} {
			if _, err := f.Save(context.Background(), snap, map[string]Change{"x": c}); !errors.Is(err, ErrNotScalar) {
				t.Errorf("%q, %+v: err = %v", src, c, err)
			}
		}
		if data, _ := os.ReadFile(path); string(data) != src {
			t.Errorf("%q changed to %q", src, data)
		}
	}
}
