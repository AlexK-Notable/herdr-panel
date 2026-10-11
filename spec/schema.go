package spec

import "encoding/json"

// JSONSchema returns a JSON Schema (draft 2020-12) for the spec format, for
// editors (taplo, VS Code's Even Better TOML) and agents. It describes shape
// and types; references between fields, stores and placeholders are checked
// by Validate, which a schema cannot express.
func JSONSchema() []byte {
	str := jsObj("type", "string")
	nonEmpty := jsObj("type", "string", "minLength", 1)
	integer := jsObj("type", "integer")
	boolean := jsObj("type", "boolean")
	argv := jsObj("type", "array", "minItems", 1, "items", str,
		"description", "A program and its arguments, run without a shell. Each element may hold placeholders.")
	command := jsObj("oneOf", []any{argv, closed(map[string]any{
		"run":       argv,
		"timeout_s": jsObj("type", "integer", "minimum", 1, "maximum", 3600),
	}, "run")})
	source := jsObj("oneOf", []any{argv, closed(map[string]any{
		"run":       argv,
		"timeout_s": jsObj("type", "integer", "minimum", 1, "maximum", 3600),
		"every_s":   jsObj("type", "integer", "minimum", 1, "maximum", 86400),
	}, "run")})
	dim := jsObj("oneOf", []any{
		jsObj("type", "integer", "minimum", 1, "maximum", 1000),
		jsObj("type", "string", "pattern", `^([1-9][0-9]?|100)%$`),
	})
	scalar := jsObj("type", []any{"string", "integer", "boolean", "number"})
	action := closed(map[string]any{
		"label":     nonEmpty,
		"key":       str,
		"run":       argv,
		"timeout_s": jsObj("type", "integer", "minimum", 1, "maximum", 3600),
		"confirm":   str,
		"after":     jsObj("enum", []any{"refresh", "close", "none"}),
	}, "label", "run")
	field := closed(map[string]any{
		"key":         nonEmpty,
		"id":          str,
		"store":       str,
		"type":        jsObj("enum", []any{"bool", "choice", "int", "duration_s", "text", "secret"}),
		"label":       str,
		"help":        str,
		"required":    boolean,
		"default":     scalar,
		"choices":     jsObj("type", "array", "items", jsObj("oneOf", []any{str, closed(map[string]any{"value": scalar, "label": str}, "value")})),
		"min":         integer,
		"max":         integer,
		"pattern":     str,
		"max_len":     jsObj("type", "integer", "minimum", 1),
		"placeholder": str,
		"true_value":  str,
		"false_value": str,
		"test":        command,
	}, "key", "type")
	column := jsObj("oneOf", []any{str, closed(map[string]any{
		"key": nonEmpty, "title": str, "width": jsObj("type", "integer", "minimum", 1, "maximum", 500),
	}, "key")})
	section := closed(map[string]any{
		"title":       str,
		"kind":        jsObj("enum", []any{"fields", "list", "buttons", "text", "view"}),
		"fields":      jsObj("type", "array", "items", field),
		"source":      source,
		"every_s":     jsObj("type", "integer", "minimum", 1, "maximum", 86400),
		"columns":     jsObj("type", "array", "items", column),
		"id_column":   str,
		"empty":       str,
		"row_actions": jsObj("type", "array", "items", action),
		"buttons":     jsObj("type", "array", "items", action),
		"text":        str,
		"view":        nonEmpty,
	})
	store := closed(map[string]any{
		"kind":   jsObj("enum", []any{"envfile", "tomlfile"}),
		"path":   nonEmpty,
		"create": boolean,
		"apply":  command,
	}, "kind", "path")
	root := closed(map[string]any{
		"spec":     jsObj("const", Version),
		"requires": jsObj("type", "string", "pattern", `^>=\s*v?[0-9]+\.[0-9]+(\.[0-9]+)?$`),
		"title":    nonEmpty,
		"size":     closed(map[string]any{"width": dim, "height": dim}),
		"save":     jsObj("enum", []any{"save_then_apply", "live"}),
		"stores":   jsObj("type", "object", "additionalProperties", store),
		"section":  jsObj("type", "array", "minItems", 1, "items", section),
	}, "spec", "title", "section")
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$id"] = "https://github.com/AlexK-Notable/herdr-panel/schema/spec-v1.json"
	root["title"] = "herdr-panel spec, version 1"
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		panic(err) // the schema is built from literals; this cannot fail
	}
	return append(out, '\n')
}

func jsObj(kv ...any) map[string]any {
	m := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1] //nolint:forcetypeassert // keys are literals
	}
	return m
}

func closed(props map[string]any, required ...string) map[string]any {
	m := jsObj("type", "object", "properties", props, "additionalProperties", false)
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}
