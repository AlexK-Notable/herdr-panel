// Package atomicfile reads and replaces small files safely: a write goes to
// a temporary file beside the target and is renamed over it, so a reader sees
// the old content or the new, never half of one. A write resolves symlinks
// first, so the link stays a link. It keeps the file's mode, and it refuses to
// replace a file that changed since it was read.
package atomicfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Revision identifies a file's content at the time it was read: its hash,
// or the fact that it did not exist.
type Revision struct {
	sum    [sha256.Size]byte
	exists bool
}

// Exists reports whether the file existed when this revision was taken.
func (r Revision) Exists() bool { return r.exists }

// String is a short form for logs.
func (r Revision) String() string {
	if !r.exists {
		return "absent"
	}
	return hex.EncodeToString(r.sum[:6])
}

// RevisionOf returns the revision of content that exists.
func RevisionOf(data []byte) Revision {
	return Revision{sum: sha256.Sum256(data), exists: true}
}

// ErrConflict is returned by Write when the file changed after it was read.
var ErrConflict = errors.New("the file changed on disk since it was read")

// TempPrefix starts the name of every temporary file this package creates.
// Sweep removes only files that carry it.
const TempPrefix = ".herdr-panel-"

const tempSuffix = ".tmp"

// Read returns a file's content and revision. A missing file is not an
// error: it returns no data and a revision whose Exists is false.
func Read(path string) ([]byte, Revision, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path comes from the user's spec
	if errors.Is(err, fs.ErrNotExist) {
		return nil, Revision{}, nil
	}
	if err != nil {
		return nil, Revision{}, err
	}
	return data, RevisionOf(data), nil
}

// Options control a write.
type Options struct {
	// NewMode is the mode of a file that does not exist yet; 0600 when zero.
	NewMode fs.FileMode
	// MakeDirs creates missing parent directories, with mode 0700.
	MakeDirs bool
}

// beforeRename runs between writing the temporary file and renaming it. Tests
// set it to simulate a process killed at that moment.
var beforeRename = func() error { return nil }

// Write replaces path's content with data, if the file still has revision
// expect (an absent revision means "must not exist"). It returns the new
// revision.
func Write(path string, data []byte, expect Revision, opts Options) (Revision, error) {
	target, err := resolve(path)
	if err != nil {
		return Revision{}, err
	}
	current, rev, err := Read(target)
	if err != nil {
		return Revision{}, err
	}
	if rev != expect {
		return Revision{}, fmt.Errorf("%s: %w", path, ErrConflict)
	}
	if rev.exists && bytes.Equal(current, data) {
		return rev, nil // nothing to change; leave the file and its mtime alone
	}

	mode := opts.NewMode
	if mode == 0 {
		mode = 0o600
	}
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(target)
	if opts.MakeDirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Revision{}, err
		}
	}

	tmp, err := os.CreateTemp(dir, TempPrefix+filepath.Base(target)+"-*"+tempSuffix)
	if err != nil {
		return Revision{}, err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return Revision{}, err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return Revision{}, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return Revision{}, err
	}
	if err := tmp.Close(); err != nil {
		return Revision{}, err
	}
	if err := beforeRename(); err != nil {
		keep = true // as a killed process would leave it, for Sweep to find
		return Revision{}, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return Revision{}, err
	}
	keep = true
	syncDir(dir)
	return RevisionOf(data), nil
}

// resolve follows symlinks so a write replaces the link's target, not the
// link. For a file that does not exist yet, its directory is resolved.
func resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	// A dangling symlink is written through to where it points.
	if target, lerr := os.Readlink(abs); lerr == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(abs), target)
		}
		return resolve(target)
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return abs, nil // MakeDirs creates it
		}
		return "", err
	}
	return filepath.Join(dir, filepath.Base(abs)), nil
}

// syncDir makes the rename durable. A failure here cannot be acted on: the
// rename has happened, and some file systems do not support it.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil { //nolint:gosec // G304: the target's own directory
		_ = d.Sync()
		_ = d.Close()
	}
}

// Sweep removes temporary files this package left in dir when a process was
// killed between writing and renaming, if they are older than minAge. A
// young one may belong to a write in progress in another panel.
func Sweep(dir string, minAge time.Duration) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var removed []string
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(name, TempPrefix) || !strings.HasSuffix(name, tempSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < minAge {
			continue
		}
		p := filepath.Join(dir, name)
		if err := os.Remove(p); err == nil {
			removed = append(removed, p)
		}
	}
	return removed, nil
}
