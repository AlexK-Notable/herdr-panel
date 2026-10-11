package spec

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
)

// MaxSize bounds a spec file; a panel description is never this large.
const MaxSize = 1 << 20

// Load reads, decodes and validates the spec at path. Its errors carry the
// path and the line and column of each problem.
func Load(path string) (*Spec, error) {
	f, err := os.Open(path) //nolint:gosec // G304: reading the spec the user named is the point
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only: closing reports nothing useful
	data, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSize {
		return nil, Errors{{File: path, Msg: fmt.Sprintf("larger than %d bytes", MaxSize)}}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return Parse(data, path, filepath.Dir(abs))
}

// Parse decodes and validates a spec held in memory. file names it in errors;
// dir is where relative paths and programs resolve.
func Parse(data []byte, file, dir string) (*Spec, error) {
	c := &collector{file: file}
	root := parseTree(data, c)
	if root == nil {
		return nil, c.err()
	}
	s := decode(root, c)
	s.File, s.Dir = file, dir
	validate(s, c)
	if len(c.errs) > 0 {
		sortErrors(c.errs)
		return nil, c.errs
	}
	return s, nil
}

func sortErrors(es Errors) {
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i].Pos, es[j].Pos
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
}

// IsSpecError reports whether err describes a problem in the spec itself, as
// opposed to failing to read it.
func IsSpecError(err error) bool {
	var es Errors
	return errors.As(err, &es)
}

const modulePath = "github.com/AlexK-Notable/herdr-panel"

// HostVersion is the version of this module in the running binary: "v0.3.1",
// or "devel" for a build from a working tree.
func HostVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	mods := append([]*debug.Module{&info.Main}, info.Deps...)
	for _, m := range mods {
		if m.Path == modulePath && m.Version != "" && m.Version != "(devel)" {
			return m.Version
		}
	}
	return "devel"
}

type semver [3]int

func parseSemver(s string) (semver, error) {
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return semver{}, fmt.Errorf("%q is not a version like 0.3 or 0.3.1", s)
	}
	var v semver
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, fmt.Errorf("%q is not a version like 0.3 or 0.3.1", s)
		}
		v[i] = n
	}
	return v, nil
}

func compareSemver(a, b semver) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
