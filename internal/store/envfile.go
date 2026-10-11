package store

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// An env file is kept as its lines. Reading finds each assignment's key and
// value; writing changes only the lines of keys that were edited, so
// comments, blank lines, order, `export` and quoting survive byte for byte.

type lineKind int

const (
	lineOther     lineKind = iota // blank, comment, or something we keep as is
	lineAssign                    // KEY=value
	lineCommented                 // #KEY=value: a documented default, read as absent
)

type envLine struct {
	kind   lineKind
	raw    string // the line(s) as written, without the final newline
	key    string
	value  string
	export bool
	quote  byte   // ' or " or 0
	suffix string // an inline comment after the value, with its spacing
}

type envDoc struct {
	lines []envLine
	crlf  bool
	// unterminated is the 1-based line where a quoted value never closes,
	// or 0. Such a file cannot be edited safely: a line written after it
	// would be read as part of that value.
	unterminated int
}

var (
	keyRe       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
	commentedRe = regexp.MustCompile(`^#\s*(export\s+)?([A-Za-z_][A-Za-z0-9_]*)=`)
)

func parseEnv(data []byte) *envDoc {
	doc := &envDoc{crlf: bytes.Contains(data, []byte("\r\n"))}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" && len(data) == 0 {
		return doc
	}
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		trim := strings.TrimSpace(raw)
		if m := commentedRe.FindStringSubmatch(trim); m != nil {
			doc.lines = append(doc.lines, envLine{kind: lineCommented, raw: raw, key: m[2]})
			continue
		}
		if trim == "" || strings.HasPrefix(trim, "#") {
			doc.lines = append(doc.lines, envLine{raw: raw})
			continue
		}
		l, consumed, ok := parseAssign(lines[i:])
		if !ok {
			if consumed < 0 && doc.unterminated == 0 {
				doc.unterminated = i + 1
			}
			doc.lines = append(doc.lines, envLine{raw: raw})
			continue
		}
		doc.lines = append(doc.lines, l)
		i += consumed - 1
	}
	return doc
}

// parseAssign reads one assignment starting at lines[0]; a quoted value may
// continue over the following lines. It returns how many lines it used.
func parseAssign(lines []string) (envLine, int, bool) {
	l := envLine{kind: lineAssign}
	s := strings.TrimLeft(lines[0], " \t")
	if rest, ok := strings.CutPrefix(s, "export "); ok {
		l.export = true
		s = strings.TrimLeft(rest, " \t")
	}
	eq := strings.IndexByte(s, '=')
	if eq < 0 {
		return envLine{}, 0, false
	}
	l.key = strings.TrimSpace(s[:eq])
	if !keyRe.MatchString(l.key) {
		return envLine{}, 0, false
	}
	v := strings.TrimLeft(s[eq+1:], " \t")
	used := 1
	switch {
	case strings.HasPrefix(v, `"`) || strings.HasPrefix(v, `'`):
		q := v[0]
		body := v[1:]
		for {
			if end := closingQuote(body, q); end >= 0 {
				l.quote = q
				l.value = unquote(body[:end], q)
				l.suffix = body[end+1:]
				break
			}
			if used >= len(lines) {
				return envLine{}, -1, false // unterminated: keep the line as is
			}
			body += "\n" + lines[used]
			used++
		}
	default:
		if i := strings.Index(v, " #"); i >= 0 {
			l.suffix = v[i:]
			v = v[:i]
		}
		l.value = strings.TrimRight(v, " \t")
		l.suffix = v[len(l.value):] + l.suffix
	}
	l.raw = strings.Join(lines[:used], "\n")
	return l, used, true
}

func closingQuote(s string, q byte) int {
	for i := 0; i < len(s); i++ {
		switch {
		case q == '"' && s[i] == '\\':
			i++
		case s[i] == q:
			return i
		}
	}
	return -1
}

func unquote(s string, q byte) string {
	if q == '\'' {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		default: // \" \\ \$ and anything else: the character itself
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// values returns every assigned key; the last assignment of a key wins, as a
// shell sourcing the file would have it.
func (d *envDoc) values() map[string]string {
	m := map[string]string{}
	for _, l := range d.lines {
		if l.kind == lineAssign {
			m[l.key] = l.value
		}
	}
	return m
}

var plainRe = regexp.MustCompile(`^[A-Za-z0-9_./:@%+,=~-]*$`)

// format writes a value the way a reader of the line expects: as it was
// quoted before when that still works, bare when it is plain, otherwise in
// double quotes with escapes.
func format(v string, prev byte) string {
	switch {
	case prev == '\'' && !strings.ContainsAny(v, "'\n"):
		return "'" + v + "'"
	case prev == 0 && plainRe.MatchString(v):
		return v
	case prev == '"' || !plainRe.MatchString(v):
		r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "$", `\$`, "`", "\\`")
		return `"` + r.Replace(v) + `"`
	}
	return v
}

func quoteOf(formatted string) byte {
	if formatted != "" && (formatted[0] == '"' || formatted[0] == '\'') {
		return formatted[0]
	}
	return 0
}

func (l *envLine) render(v string) string {
	var b strings.Builder
	indent := l.raw[:len(l.raw)-len(strings.TrimLeft(l.raw, " \t"))]
	b.WriteString(indent)
	if l.export {
		b.WriteString("export ")
	}
	fmt.Fprintf(&b, "%s=%s", l.key, format(v, l.quote))
	b.WriteString(l.suffix)
	return b.String()
}

// set changes key to v, in its last assignment; otherwise in place of its
// commented default; otherwise as a new line at the end.
func (d *envDoc) set(key, v string) {
	for i := len(d.lines) - 1; i >= 0; i-- {
		if l := &d.lines[i]; l.kind == lineAssign && l.key == key {
			l.raw, l.value = l.render(v), v
			l.quote = quoteOf(format(v, l.quote))
			return
		}
	}
	nl := envLine{kind: lineAssign, key: key, value: v}
	nl.raw = nl.render(v)
	for i := range d.lines {
		if l := d.lines[i]; l.kind == lineCommented && l.key == key {
			d.lines[i] = nl
			return
		}
	}
	d.lines = append(d.lines, nl)
}

// unset removes every assignment of key, so its default applies again.
func (d *envDoc) unset(key string) {
	out := d.lines[:0]
	for _, l := range d.lines {
		if l.kind != lineAssign || l.key != key {
			out = append(out, l)
		}
	}
	d.lines = out
}

func (d *envDoc) bytes() []byte {
	if len(d.lines) == 0 {
		return nil
	}
	sep := "\n"
	if d.crlf {
		sep = "\r\n"
	}
	var b strings.Builder
	for _, l := range d.lines {
		b.WriteString(strings.ReplaceAll(l.raw, "\n", sep))
		b.WriteString(sep)
	}
	return []byte(b.String())
}
