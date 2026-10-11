package spec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaFile = "../schema/spec-v1.json"

// The committed schema is what editors fetch; it must match the code.
// Regenerate with: go run ./cmd/herdr-panel schema > schema/spec-v1.json
func TestCommittedSchemaIsCurrent(t *testing.T) {
	t.Parallel()
	got, err := os.ReadFile(schemaFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, JSONSchema()) {
		t.Errorf("%s is stale; regenerate it with `go run ./cmd/herdr-panel schema > schema/spec-v1.json`", schemaFile)
	}
}

func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(JSONSchema()))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("spec-v1.json", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("spec-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func tomlAsJSON(t *testing.T, data []byte) any {
	t.Helper()
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	j, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestExamplesMatchSchema(t *testing.T) {
	t.Parallel()
	sch := compileSchema(t)
	paths, _ := filepath.Glob("../examples/*.toml")
	if len(paths) == 0 {
		t.Fatal("no examples")
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := sch.Validate(tomlAsJSON(t, data)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

// The schema must reject what Parse rejects for shape: a check that passed
// everything would prove nothing.
func TestSchemaRejectsBadShapes(t *testing.T) {
	t.Parallel()
	sch := compileSchema(t)
	bad := []string{
		"spec = 1\ntitle = \"T\"\ncolour = 1\n[[section]]\ntext = \"x\"\n",
		"spec = 2\ntitle = \"T\"\n[[section]]\ntext = \"x\"\n",
		"spec = 1\ntitle = \"T\"\n[[section]]\nfields = [ { key = \"A\", type = \"colour\" } ]\n",
		"spec = 1\ntitle = \"T\"\n[[section]]\nkind = \"buttons\"\nbuttons = [ { label = \"x\", run = [] } ]\n",
		"spec = 1\ntitle = \"T\"\nsize = { width = \"70\" }\n[[section]]\ntext = \"x\"\n",
	}
	for _, src := range bad {
		if err := sch.Validate(tomlAsJSON(t, []byte(src))); err == nil {
			t.Errorf("schema accepted:\n%s", src)
		}
	}
}
