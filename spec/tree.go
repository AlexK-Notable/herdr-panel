package spec

import (
	"errors"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// The spec is decoded in two steps: TOML into this tree, which keeps the
// position of every key and value, then the tree into typed values (decode.go).
// The tree is what lets every error, including unknown keys and wrong types,
// say where it is.

type kind int

const (
	kString kind = iota + 1
	kBool
	kInt
	kFloat
	kArray
	kTable
)

func (k kind) String() string {
	switch k {
	case kString:
		return "a string"
	case kBool:
		return "a boolean"
	case kInt:
		return "an integer"
	case kFloat:
		return "a float"
	case kArray:
		return "an array"
	case kTable:
		return "a table"
	}
	return "an unsupported value"
}

type value struct {
	kind kind
	pos  Pos
	str  string
	b    bool
	i    int64
	f    float64
	arr  []*value
	tbl  *table
}

type table struct {
	pos     Pos
	keys    []string // in file order
	vals    map[string]*value
	keyPos  map[string]Pos
	defined bool // by a [header] or as an inline table, so it cannot be reopened
	inline  bool // inline tables are closed: nothing may be added afterwards
	dotted  bool // made by a dotted key (a.b = 1); TOML forbids a [header] for it
	isArray bool // an element of an [[array of tables]]
}

func newTable(p Pos) *table {
	return &table{pos: p, vals: map[string]*value{}, keyPos: map[string]Pos{}}
}

func (t *table) get(key string) (*value, Pos, bool) {
	v, ok := t.vals[key]
	return v, t.keyPos[key], ok
}

func (t *table) set(key string, p Pos, v *value) {
	t.keys = append(t.keys, key)
	t.vals[key] = v
	t.keyPos[key] = p
}

type treeBuilder struct {
	p    *unstable.Parser
	c    *collector
	root *table
	cur  *table
}

func parseTree(data []byte, c *collector) *table {
	b := &treeBuilder{p: &unstable.Parser{}, c: c, root: newTable(Pos{1, 1})}
	b.root.defined = true
	b.cur = b.root
	b.p.Reset(data)
	for b.p.NextExpression() {
		b.expression(b.p.Expression())
	}
	if err := b.p.Error(); err != nil {
		var perr *unstable.ParserError
		pos := Pos{}
		if errors.As(err, &perr) {
			pos = b.highlightPos(perr.Highlight)
		}
		msg := err.Error()
		if perr != nil {
			msg = perr.Message
		}
		c.add(pos, "invalid TOML: %s", msg)
		return nil
	}
	return b.root
}

// highlightPos locates a parser error. Range panics when the highlight is not
// a slice of the input, which go-toml does not promise, so that case simply
// has no position.
func (b *treeBuilder) highlightPos(h []byte) (p Pos) {
	if len(h) == 0 {
		return Pos{}
	}
	defer func() {
		if recover() != nil {
			p = Pos{}
		}
	}()
	sh := b.p.Shape(b.p.Range(h))
	return Pos{sh.Start.Line, sh.Start.Column}
}

func (b *treeBuilder) pos(n *unstable.Node) Pos {
	if n == nil || n.Raw.Length == 0 {
		return Pos{}
	}
	sh := b.p.Shape(n.Raw)
	return Pos{sh.Start.Line, sh.Start.Column}
}

type keyPart struct {
	name string
	pos  Pos
}

func (b *treeBuilder) keyParts(n *unstable.Node) []keyPart {
	var parts []keyPart
	it := n.Key()
	for it.Next() {
		k := it.Node()
		parts = append(parts, keyPart{string(k.Data), b.pos(k)})
	}
	return parts
}

func dotted(parts []keyPart) string {
	names := make([]string, len(parts))
	for i, p := range parts {
		names[i] = p.name
	}
	return strings.Join(names, ".")
}

func (b *treeBuilder) expression(n *unstable.Node) {
	switch n.Kind {
	case unstable.KeyValue:
		b.keyValue(b.cur, n)
	case unstable.Table:
		parts := b.keyParts(n)
		t := b.descend(b.root, parts[:len(parts)-1])
		if t == nil {
			b.cur = newTable(Pos{}) // keep going; the error is recorded
			return
		}
		last := parts[len(parts)-1]
		v, _, ok := t.get(last.name)
		switch {
		case !ok:
			nt := newTable(last.pos)
			nt.defined = true
			t.set(last.name, last.pos, &value{kind: kTable, pos: last.pos, tbl: nt})
			b.cur = nt
		case v.kind == kTable && !v.tbl.defined && !v.tbl.inline && !v.tbl.dotted:
			v.tbl.defined = true
			v.tbl.pos = last.pos
			b.cur = v.tbl
		default:
			b.c.add(last.pos, "table [%s] is defined twice", dotted(parts))
			b.cur = newTable(Pos{})
		}
	case unstable.ArrayTable:
		parts := b.keyParts(n)
		t := b.descend(b.root, parts[:len(parts)-1])
		if t == nil {
			b.cur = newTable(Pos{})
			return
		}
		last := parts[len(parts)-1]
		nt := newTable(last.pos)
		nt.defined, nt.isArray = true, true
		v, _, ok := t.get(last.name)
		switch {
		case !ok:
			t.set(last.name, last.pos, &value{kind: kArray, pos: last.pos, arr: []*value{{kind: kTable, pos: last.pos, tbl: nt}}})
		case isArrayOfTables(v):
			v.arr = append(v.arr, &value{kind: kTable, pos: last.pos, tbl: nt})
		default:
			b.c.add(last.pos, "[[%s]] conflicts with a value defined earlier", dotted(parts))
		}
		b.cur = nt
	default:
		// Comments and whitespace carry nothing a spec needs.
	}
}

// isArrayOfTables reports whether v was made by [[headers]], as opposed to a
// static array, which may hold anything and cannot be extended.
func isArrayOfTables(v *value) bool {
	if v.kind != kArray || len(v.arr) == 0 {
		return false
	}
	last := v.arr[len(v.arr)-1]
	return last.kind == kTable && last.tbl.isArray
}

// descend walks a header's leading keys from t, creating implicit tables and
// entering the last element of an array of tables, as TOML specifies.
func (b *treeBuilder) descend(t *table, parts []keyPart) *table {
	for _, part := range parts {
		v, _, ok := t.get(part.name)
		switch {
		case !ok:
			nt := newTable(part.pos)
			t.set(part.name, part.pos, &value{kind: kTable, pos: part.pos, tbl: nt})
			t = nt
		case v.kind == kTable && !v.tbl.inline:
			t = v.tbl
		case isArrayOfTables(v):
			t = v.arr[len(v.arr)-1].tbl
		default:
			b.c.add(part.pos, "%q is already %s, not a table", part.name, v.kind)
			return nil
		}
	}
	return t
}

func (b *treeBuilder) keyValue(t *table, n *unstable.Node) {
	parts := b.keyParts(n)
	// a.b.c = v defines the intermediate tables a and a.b inside t.
	for _, part := range parts[:len(parts)-1] {
		v, _, ok := t.get(part.name)
		switch {
		case !ok:
			nt := newTable(part.pos)
			nt.dotted = true
			t.set(part.name, part.pos, &value{kind: kTable, pos: part.pos, tbl: nt})
			t = nt
		case v.kind == kTable && !v.tbl.inline && (v.tbl.dotted || !v.tbl.defined):
			t = v.tbl
		default:
			b.c.add(part.pos, "key %q is defined twice", dotted(parts))
			return
		}
	}
	last := parts[len(parts)-1]
	if _, _, ok := t.get(last.name); ok {
		b.c.add(last.pos, "key %q is defined twice", dotted(parts))
		return
	}
	if v := b.value(n.Value()); v != nil {
		t.set(last.name, last.pos, v)
	}
}

func (b *treeBuilder) value(n *unstable.Node) *value {
	p := b.pos(n)
	switch n.Kind {
	case unstable.String:
		return &value{kind: kString, pos: p, str: string(n.Data)}
	case unstable.Bool:
		return &value{kind: kBool, pos: p, b: string(n.Data) == "true"}
	case unstable.Integer:
		i, err := parseInt(string(n.Data))
		if err != nil {
			b.c.add(p, "integer %s is out of range", n.Data)
			return nil
		}
		return &value{kind: kInt, pos: p, i: i}
	case unstable.Float:
		f, err := strconv.ParseFloat(strings.ReplaceAll(string(n.Data), "_", ""), 64)
		if err != nil {
			b.c.add(p, "float %s cannot be read", n.Data)
			return nil
		}
		return &value{kind: kFloat, pos: p, f: f}
	case unstable.Array:
		arr := &value{kind: kArray, pos: p}
		it := n.Children()
		for it.Next() {
			if v := b.value(it.Node()); v != nil {
				if arr.pos.Line == 0 {
					arr.pos = v.pos
				}
				arr.arr = append(arr.arr, v)
			}
		}
		return arr
	case unstable.InlineTable:
		t := newTable(p)
		t.defined, t.inline = true, true
		it := n.Children()
		for it.Next() {
			if c := it.Node(); c.Kind == unstable.KeyValue {
				b.keyValue(t, c)
			}
		}
		return &value{kind: kTable, pos: p, tbl: t}
	default:
		b.c.add(p, "dates and times are not used in a spec")
		return nil
	}
}

func parseInt(s string) (int64, error) {
	s = strings.ReplaceAll(s, "_", "")
	switch {
	case strings.HasPrefix(s, "0x"):
		return strconv.ParseInt(s[2:], 16, 64)
	case strings.HasPrefix(s, "0o"):
		return strconv.ParseInt(s[2:], 8, 64)
	case strings.HasPrefix(s, "0b"):
		return strconv.ParseInt(s[2:], 2, 64)
	}
	return strconv.ParseInt(s, 10, 64)
}
