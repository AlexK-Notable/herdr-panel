package spec

import (
	"fmt"
	"strings"
)

// Pos is a position in a spec file. Line and Col start at 1; the zero value
// means "no position", for errors about the file as a whole.
type Pos struct {
	Line, Col int
}

// Error is one problem in a spec, at a place in its file.
type Error struct {
	File string
	Pos  Pos
	Msg  string
}

func (e *Error) Error() string {
	switch {
	case e.Pos.Line > 0 && e.File != "":
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Pos.Line, e.Pos.Col, e.Msg)
	case e.Pos.Line > 0:
		return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Col, e.Msg)
	case e.File != "":
		return e.File + ": " + e.Msg
	}
	return e.Msg
}

// Errors is every problem found in one spec, in file order. Load and
// Validate return it rather than stopping at the first problem, so an editor
// or an agent can fix them all in one pass.
type Errors []*Error

func (es Errors) Error() string {
	lines := make([]string, len(es))
	for i, e := range es {
		lines[i] = e.Error()
	}
	return strings.Join(lines, "\n")
}

// Unwrap lets errors.As find each *Error.
func (es Errors) Unwrap() []error {
	out := make([]error, len(es))
	for i, e := range es {
		out[i] = e
	}
	return out
}

// collector gathers errors for one file.
type collector struct {
	file string
	errs Errors
}

func (c *collector) add(p Pos, format string, args ...any) {
	c.errs = append(c.errs, &Error{File: c.file, Pos: p, Msg: fmt.Sprintf(format, args...)})
}

func (c *collector) err() error {
	if len(c.errs) == 0 {
		return nil
	}
	return c.errs
}
