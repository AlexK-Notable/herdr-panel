// Package store reads and writes the files a panel's fields live in.
package store

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/AlexK-Notable/herdr-panel/internal/atomicfile"
)

// Snapshot is a store's values at one revision of its file.
type Snapshot struct {
	Values   map[string]string // only keys that are set
	Revision atomicfile.Revision
}

// Change is one edit: a new value, or Unset to remove the key so its default
// applies again.
type Change struct {
	Value string
	Unset bool
}

// Store is where a spec's fields keep their values.
type Store interface {
	// Load reads the current values.
	Load(ctx context.Context) (Snapshot, error)
	// Save applies changes to the file as it was at base. If the file changed
	// since, it returns an error wrapping ErrConflict and writes nothing; the
	// caller can Load again and re-apply the same changes.
	Save(ctx context.Context, base Snapshot, changes map[string]Change) (Snapshot, error)
}

// ErrConflict means the file changed on disk after it was loaded.
var ErrConflict = atomicfile.ErrConflict

// EnvFile is a KEY=value file.
type EnvFile struct {
	Path   string
	Create bool // create the file, and its directories, on first save
}

// Load implements Store.
func (e *EnvFile) Load(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	data, rev, err := atomicfile.Read(e.Path)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Values: parseEnv(data).values(), Revision: rev}, nil
}

// ErrUnterminated means a quoted value in the file never closes, so a line
// written after it would be read as part of it. The file must be fixed by hand.
var ErrUnterminated = errors.New("a quoted value is never closed; fix the file by hand before saving")

// ErrMissing means the file does not exist and the store may not create it.
var ErrMissing = errors.New("the file does not exist")

// Save implements Store.
func (e *EnvFile) Save(ctx context.Context, base Snapshot, changes map[string]Change) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	data, rev, err := atomicfile.Read(e.Path)
	if err != nil {
		return Snapshot{}, err
	}
	if rev != base.Revision {
		return Snapshot{}, fmt.Errorf("%s: %w", e.Path, ErrConflict)
	}
	if !rev.Exists() && !e.Create {
		return Snapshot{}, fmt.Errorf("%s: %w", e.Path, ErrMissing)
	}
	doc := parseEnv(data)
	if doc.unterminated > 0 {
		return Snapshot{}, fmt.Errorf("%s:%d: %w", e.Path, doc.unterminated, ErrUnterminated)
	}
	for _, key := range sortedKeys(changes) {
		if c := changes[key]; c.Unset {
			doc.unset(key)
		} else {
			doc.set(key, c.Value)
		}
	}
	out := doc.bytes()
	newRev, err := atomicfile.Write(e.Path, out, rev, atomicfile.Options{MakeDirs: e.Create})
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Values: parseEnv(out).values(), Revision: newRev}, nil
}

func sortedKeys(m map[string]Change) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
