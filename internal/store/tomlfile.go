package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"

	"github.com/AlexK-Notable/herdr-panel/internal/atomicfile"
)

// Kind is the TOML type a key is written as.
type Kind int

// The kinds a field maps to: bool fields are booleans, int and duration
// fields integers, everything else a string.
const (
	KindString Kind = iota
	KindBool
	KindInt
)

// TOMLFile is a TOML file whose top-level keys hold the fields. Only lines
// of edited keys change: comments, order, tables and every other key stay
// byte for byte. A key written commented out (#key = value) above the first
// table becomes a real line in its place on the first set; a key that is
// nowhere is added before the first table, where TOML requires top-level
// keys to be.
type TOMLFile struct {
	Path   string
	Create bool
	// Kinds gives each key's TOML type; a key left out is a string.
	Kinds map[string]Kind
}

// ErrMultiline means a key to change spans several lines (a multi-line
// string or array); it is left for the user to edit by hand.
var ErrMultiline = errors.New("its value spans several lines; edit it by hand")

// Load implements Store. Only top-level scalars are values; tables and
// arrays are not fields and are ignored.
func (f *TOMLFile) Load(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	data, rev, err := atomicfile.Read(f.Path)
	if err != nil {
		return Snapshot{}, err
	}
	vals, err := tomlValues(data)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%s: %w", f.Path, err)
	}
	return Snapshot{Values: vals, Revision: rev}, nil
}

// bom is the UTF-8 byte order mark some editors put at the start of a file.
const bom = "\ufeff"

func tomlValues(data []byte) (map[string]string, error) {
	data = bytes.TrimPrefix(data, []byte(bom))
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("not valid TOML: %w", err)
	}
	out := map[string]string{}
	for k, v := range m {
		switch t := v.(type) {
		case string:
			out[k] = t
		case bool:
			out[k] = strconv.FormatBool(t)
		case int64:
			out[k] = strconv.FormatInt(t, 10)
		case float64:
			out[k] = strconv.FormatFloat(t, 'g', -1, 64)
		}
	}
	return out, nil
}

// Save implements Store.
func (f *TOMLFile) Save(ctx context.Context, base Snapshot, changes map[string]Change) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	data, rev, err := atomicfile.Read(f.Path)
	if err != nil {
		return Snapshot{}, err
	}
	if rev != base.Revision {
		return Snapshot{}, fmt.Errorf("%s: %w", f.Path, ErrConflict)
	}
	if !rev.Exists() && !f.Create {
		return Snapshot{}, fmt.Errorf("%s: %w", f.Path, ErrMissing)
	}
	doc, err := parseTOMLDoc(data)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%s: %w", f.Path, err)
	}
	for _, key := range sortedKeys(changes) {
		c := changes[key]
		if !bareKey.MatchString(key) {
			return Snapshot{}, fmt.Errorf("%s: key %q is not a bare TOML key", f.Path, key)
		}
		if c.Unset {
			err = doc.unset(key)
		} else {
			var lit string
			if lit, err = tomlLiteral(c.Value, f.Kinds[key]); err == nil {
				err = doc.set(key, lit)
			}
		}
		if err != nil {
			return Snapshot{}, fmt.Errorf("%s: %s: %w", f.Path, key, err)
		}
	}
	out := doc.bytes()
	vals, err := tomlValues(out)
	if err != nil {
		// The editor must never write a file that does not parse.
		return Snapshot{}, fmt.Errorf("%s: the edited file would not parse, so it was not written: %w", f.Path, err)
	}
	newRev, err := atomicfile.Write(f.Path, out, rev, atomicfile.Options{MakeDirs: f.Create})
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Values: vals, Revision: newRev}, nil
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// tomlLiteral writes v as a TOML value of kind k.
func tomlLiteral(v string, k Kind) (string, error) {
	switch k {
	case KindBool:
		if v != "true" && v != "false" {
			return "", fmt.Errorf("%q is not true or false", v)
		}
		return v, nil
	case KindInt:
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return "", fmt.Errorf("%q is not a whole number", v)
		}
		return v, nil
	case KindString:
	}
	if !utf8.ValidString(v) {
		return "", errors.New("the value is not valid UTF-8, which a TOML file must be")
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range v {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}

// tomlDoc is a TOML file as lines, with where each top-level key is.
type tomlDoc struct {
	lines      []string
	crlf       bool // CRLF line ends
	eol        bool // the file ended with a line end
	origLast   string
	firstTable int                      // index of the first table header line, or len(lines)
	topEnd     int                      // the first header, real or commented out, or len(lines)
	keys       map[string]lineSpan      // top-level keys with a single-part name
	kinds      map[string]unstable.Kind // the kind of each one's value
	spans      []lineSpan               // every key-value above the first table, dotted ones too
	bom        bool
}

// ErrNotScalar means a key to change holds a table, an array or a date: a
// field cannot show it, so it is left for the user to edit by hand.
var ErrNotScalar = errors.New("its value is a table, an array or a date; edit it by hand")

type lineSpan struct{ first, last int } // line indexes, inclusive

// A table header, real or commented out (#[keys]): commented defaults below
// one belong to that table, not to the top level.
var headerLine = regexp.MustCompile(`^\s*#?\s*\[`)

func parseTOMLDoc(data []byte) (*tomlDoc, error) {
	// Lines keep their own "\r": a file can mix line ends, and a multi-line
	// string's value depends on which it holds.
	text, hadBOM := strings.CutPrefix(string(data), bom) // kept, and written back
	data = []byte(text)
	d := &tomlDoc{
		crlf: strings.Contains(text, "\r\n"), bom: hadBOM,
		keys: map[string]lineSpan{}, kinds: map[string]unstable.Kind{},
	}
	d.eol = len(text) == 0 || strings.HasSuffix(text, "\n")
	if text = strings.TrimSuffix(text, "\n"); text != "" || len(data) > 1 {
		d.lines = strings.Split(text, "\n")
		d.origLast = d.lines[len(d.lines)-1]
	}
	d.firstTable = len(d.lines)
	p := &unstable.Parser{}
	p.Reset(data) // its line numbers are the indexes into d.lines
	for p.NextExpression() {
		e := p.Expression()
		first, last, ok := nodeLines(p, e)
		if !ok {
			continue
		}
		switch e.Kind {
		case unstable.Table, unstable.ArrayTable:
			d.firstTable = min(d.firstTable, first)
		case unstable.KeyValue:
			if first >= d.firstTable {
				continue
			}
			d.spans = append(d.spans, lineSpan{first, last})
			var parts []string
			for key := e.Key(); key.Next(); {
				parts = append(parts, string(key.Node().Data))
			}
			if len(parts) == 1 {
				d.keys[parts[0]] = lineSpan{first, last}
				d.kinds[parts[0]] = e.Value().Kind
			}
		default: // comments are not kept by this parser
		}
	}
	if err := p.Error(); err != nil {
		return nil, fmt.Errorf("not valid TOML: %w", err)
	}
	d.topEnd = d.firstTable
	for i := range d.firstTable {
		if !d.inValue(i) && headerLine.MatchString(d.lines[i]) {
			d.topEnd = i
			break
		}
	}
	return d, nil
}

// nodeLines is the first and last line (0-based) a node's tokens occupy.
// Containers (key-values, arrays, inline tables) have no bytes of their
// own, so their extent is that of the tokens inside them.
func nodeLines(p *unstable.Parser, n *unstable.Node) (first, last int, ok bool) {
	first, last = 1<<30, -1
	var walk func(n *unstable.Node)
	walk = func(n *unstable.Node) {
		if n == nil {
			return
		}
		if n.Raw.Length > 0 {
			sh := p.Shape(n.Raw)
			first, last = min(first, sh.Start.Line-1), max(last, sh.End.Line-1)
		}
		// A key-value's children are its value, then its key parts; a
		// table's are its key parts.
		for it := n.Children(); it.Next(); {
			walk(it.Node())
		}
	}
	walk(n)
	return first, last, last >= 0
}

// set gives key the literal lit: on its own line when it has one, else on
// its commented default above the first table, else after the last
// top-level key, else at the top of the file before any table.
func (d *tomlDoc) set(key, lit string) error {
	line := key + " = " + lit
	if s, ok := d.keys[key]; ok {
		if !scalar(d.kinds[key]) {
			return ErrNotScalar
		}
		if s.first != s.last {
			return ErrMultiline
		}
		old, cr := cutCR(d.lines[s.first])
		d.lines[s.first] = line + trailingComment(old) + cr
		return nil
	}
	for i := range d.topEnd {
		if d.inValue(i) {
			continue // a line of another key's value, not a comment
		}
		text, cr := cutCR(d.lines[i])
		if k, comment, ok := commentedKey(text); ok && k == key {
			d.lines[i] = line + comment + cr
			d.keys[key] = lineSpan{i, i}
			d.kinds[key] = unstable.String
			d.spans = append(d.spans, lineSpan{i, i})
			return nil
		}
	}
	cr := ""
	if d.crlf {
		cr = "\r"
	}
	var at int
	add := []string{line + cr}
	last := -1
	for _, s := range d.spans {
		last = max(last, s.last)
	}
	switch {
	case last >= 0:
		at = last + 1
	case d.topEnd < len(d.lines):
		// Above the first header, real or commented out, and above the
		// comment block that introduces it, with a blank line between.
		at = d.topEnd
		for at > 0 && strings.HasPrefix(strings.TrimSpace(d.lines[at-1]), "#") {
			at--
		}
		add = append(add, cr)
	default:
		at = len(d.lines)
	}
	if at == len(d.lines) && at > 0 && !d.eol && d.crlf && !strings.HasSuffix(d.lines[at-1], "\r") {
		d.lines[at-1] += "\r" // the old last line gets its own line end, the file's way
	}
	d.insert(at, add)
	d.keys[key] = lineSpan{at, at}
	d.kinds[key] = unstable.String
	d.spans = append(d.spans, lineSpan{at, at})
	return nil
}

// inValue reports whether line i belongs to a key-value: such a line is
// value, never a comment or a header, whatever it looks like.
func (d *tomlDoc) inValue(i int) bool {
	for _, s := range d.spans {
		if i >= s.first && i <= s.last {
			return true
		}
	}
	return false
}

// commentedKey reads a commented-out setting, `#key = value # why`: the
// text after the # must parse as one single-line key-value with a bare key
// and a scalar value. Prose such as `# editor = the program e opens` does not.
func commentedKey(line string) (key, comment string, ok bool) {
	body, found := strings.CutPrefix(strings.TrimLeft(line, " \t"), "#")
	if !found {
		return "", "", false
	}
	body = strings.TrimLeft(body, " \t")
	p := &unstable.Parser{}
	p.Reset([]byte(body))
	if !p.NextExpression() {
		return "", "", false
	}
	e := p.Expression()
	if e.Kind != unstable.KeyValue || !scalar(e.Value().Kind) {
		return "", "", false
	}
	var parts []string
	for k := e.Key(); k.Next(); {
		parts = append(parts, string(k.Node().Data))
	}
	if p.NextExpression() || p.Error() != nil || len(parts) != 1 || strings.Contains(body, "\n") {
		return "", "", false
	}
	return parts[0], trailingComment(body), true
}

// scalar reports whether a value kind is one a field edits.
func scalar(k unstable.Kind) bool {
	switch k {
	case unstable.String, unstable.Bool, unstable.Integer, unstable.Float:
		return true
	default:
		return false
	}
}

// insert puts lines at index at, moving every later span down.
func (d *tomlDoc) insert(at int, lines []string) {
	d.lines = append(d.lines[:at], append(lines, d.lines[at:]...)...)
	d.shift(at, len(lines))
}

// unset removes key's lines; a key the file does not set is left alone.
func (d *tomlDoc) unset(key string) error {
	s, ok := d.keys[key]
	if !ok {
		return nil
	}
	if !scalar(d.kinds[key]) {
		return ErrNotScalar
	}
	d.lines = append(d.lines[:s.first], d.lines[s.last+1:]...)
	delete(d.keys, key)
	delete(d.kinds, key)
	d.spans = slices.DeleteFunc(d.spans, func(sp lineSpan) bool { return sp == s })
	d.shift(s.first, -(s.last - s.first + 1))
	return nil
}

func (d *tomlDoc) shift(at, n int) {
	for k, s := range d.keys {
		if s.first >= at {
			d.keys[k] = lineSpan{s.first + n, s.last + n}
		}
	}
	for i, s := range d.spans {
		if s.first >= at {
			d.spans[i] = lineSpan{s.first + n, s.last + n}
		}
	}
	if d.firstTable >= at {
		d.firstTable += n
	}
	if d.topEnd >= at {
		d.topEnd += n
	}
}

// trailingComment keeps a comment after a single-line value: `n = 3 # why`.
// The key-value's own extent ends with its value, so whatever follows it is
// whitespace and the comment; a # inside a string is inside that extent.
func trailingComment(line string) string {
	p := &unstable.Parser{}
	p.Reset([]byte(line))
	if !p.NextExpression() {
		return ""
	}
	e := p.Expression()
	if e.Kind != unstable.KeyValue || e.Raw.Length == 0 {
		return ""
	}
	end := int(e.Raw.Offset + e.Raw.Length)
	if end > len(line) {
		return ""
	}
	rest := line[end:]
	if !strings.HasPrefix(strings.TrimLeft(rest, " \t"), "#") {
		return ""
	}
	return rest
}

// cutCR splits a line's "\r", if any, from its text.
func cutCR(line string) (text, cr string) {
	if t, ok := strings.CutSuffix(line, "\r"); ok {
		return t, "\r"
	}
	return line, ""
}

// bytes is the file again, with its own line ends. A file that lacked a
// final line end gets one only when its last line changed.
func (d *tomlDoc) bytes() []byte {
	if len(d.lines) == 0 {
		return nil
	}
	out := strings.Join(d.lines, "\n")
	if last := d.lines[len(d.lines)-1]; !d.eol && last != d.origLast {
		// The last line changed in a file without a final line end: end it,
		// and the file's own way.
		if _, cr := cutCR(last); cr == "" && d.crlf {
			out += "\r"
		}
		out += "\n"
	} else if d.eol {
		out += "\n"
	}
	if d.bom {
		out = bom + out
	}
	return []byte(out)
}
