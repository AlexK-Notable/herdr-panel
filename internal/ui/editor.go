package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// editor is a one-line text input. It is deliberately small and has no
// timers (no blinking cursor), so a frame depends only on the keys pressed:
// observe and golden tests see the same thing every run.
type editor struct {
	value  []rune
	pos    int
	mask   bool // secrets: shown as one * per character
	limit  int  // in characters; 0 for none
	errMsg string
}

func newEditor(v string, mask bool, limit int) *editor {
	r := []rune(v)
	return &editor{value: r, pos: len(r), mask: mask, limit: limit}
}

func (e *editor) String() string { return string(e.value) }

func (e *editor) insert(s string) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	ins := []rune(s)
	if e.limit > 0 && len(e.value)+len(ins) > e.limit {
		ins = ins[:max(0, e.limit-len(e.value))]
	}
	e.value = append(e.value[:e.pos], append(ins, e.value[e.pos:]...)...)
	e.pos += len(ins)
	e.errMsg = ""
}

// key applies an editing key and reports whether it was one.
func (e *editor) key(k string) bool {
	switch k {
	case "left", "ctrl+b":
		e.pos = max(0, e.pos-1)
	case "right", "ctrl+f":
		e.pos = min(len(e.value), e.pos+1)
	case "home", "ctrl+a":
		e.pos = 0
	case "end", "ctrl+e":
		e.pos = len(e.value)
	case "backspace", "ctrl+h":
		if e.pos > 0 {
			e.value = append(e.value[:e.pos-1], e.value[e.pos:]...)
			e.pos--
		}
	case "delete", "ctrl+d":
		if e.pos < len(e.value) {
			e.value = append(e.value[:e.pos], e.value[e.pos+1:]...)
		}
	case "ctrl+u":
		e.value, e.pos = e.value[e.pos:], 0
	case "ctrl+k":
		e.value = e.value[:e.pos]
	case "ctrl+w", "alt+backspace":
		i := e.pos
		for i > 0 && e.value[i-1] == ' ' {
			i--
		}
		for i > 0 && e.value[i-1] != ' ' {
			i--
		}
		e.value = append(e.value[:i], e.value[e.pos:]...)
		e.pos = i
	default:
		return false
	}
	e.errMsg = ""
	return true
}

// view renders the value in width cells with the cursor shown as a reversed
// cell, scrolled so the cursor stays visible.
func (e *editor) view(width int, th Theme) string {
	if width < 2 {
		width = 2
	}
	shown := e.value
	if e.mask {
		shown = []rune(strings.Repeat("*", len(e.value)))
	}
	start := 0
	if e.pos >= width {
		start = e.pos - width + 1
	}
	var b strings.Builder
	cells := 0
	for i := start; i <= len(shown) && cells < width; i++ {
		ch := " "
		if i < len(shown) {
			ch = string(shown[i])
		}
		w := ansi.StringWidth(ch)
		if cells+w > width {
			break
		}
		if i == e.pos {
			b.WriteString(th.Focus.Render(ch))
		} else {
			b.WriteString(ch)
		}
		cells += w
	}
	return b.String()
}
