package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p, s string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil { // umask may have cut it
		t.Fatal(err)
	}
}

func TestWriteReplacesContentAndKeepsMode(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "c.env")
	write(t, p, "A=1\n", 0o640)
	_, rev, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(p, []byte("A=2\n"), rev, Options{}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	info, _ := os.Stat(p)
	if string(got) != "A=2\n" || info.Mode().Perm() != 0o640 {
		t.Errorf("content %q mode %v", got, info.Mode().Perm())
	}
}

func TestWriteThroughSymlinkKeepsTheLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "real.env")
	link := filepath.Join(dir, "link.env")
	write(t, target, "A=1\n", 0o600)
	if err := os.Symlink("real.env", link); err != nil {
		t.Fatal(err)
	}
	_, rev, _ := Read(link)
	if _, err := Write(link, []byte("A=2\n"), rev, Options{}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a regular file")
	}
	if got, _ := os.ReadFile(target); string(got) != "A=2\n" {
		t.Errorf("target = %q", got)
	}
}

func TestWriteRefusesAConcurrentEdit(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "c.env")
	write(t, p, "A=1\n", 0o600)
	_, rev, _ := Read(p)
	write(t, p, "A=1\nB=edited elsewhere\n", 0o600)
	_, err := Write(p, []byte("A=2\n"), rev, Options{})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "A=1\nB=edited elsewhere\n" {
		t.Errorf("the other edit was lost: %q", got)
	}
}

func TestWriteNewFileAndDirs(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "a", "b", "c.env")
	if _, err := Write(p, []byte("A=1\n"), Revision{}, Options{}); err == nil {
		t.Fatal("wrote into a missing directory without MakeDirs")
	}
	if _, err := Write(p, []byte("A=1\n"), Revision{}, Options{MakeDirs: true}); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	di, _ := os.Stat(filepath.Dir(p))
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Errorf("modes file %v dir %v", fi.Mode().Perm(), di.Mode().Perm())
	}
	// "must not exist" now fails: someone (we) created it.
	if _, err := Write(p, []byte("A=2\n"), Revision{}, Options{}); !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}
}

// A process killed between writing the temporary file and renaming it must
// leave the original intact, and the next run's Sweep must remove the orphan.
// Not parallel: it swaps the package's beforeRename hook.
func TestKillBeforeRenameLeavesOriginalAndSweepCleans(t *testing.T) { //nolint:paralleltest // swaps a package hook
	dir := t.TempDir()
	p := filepath.Join(dir, "c.env")
	write(t, p, "A=1\n", 0o600)
	_, rev, _ := Read(p)

	killed := errors.New("killed")
	beforeRename = func() error { return killed }
	_, err := Write(p, []byte("A=2\n"), rev, Options{})
	beforeRename = func() error { return nil }
	if !errors.Is(err, killed) {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "A=1\n" {
		t.Fatalf("original damaged: %q", got)
	}
	orphans, _ := filepath.Glob(filepath.Join(dir, TempPrefix+"*"))
	if len(orphans) != 1 {
		t.Fatalf("orphans = %v, want one", orphans)
	}
	// Young temporary files may belong to a write in progress.
	if removed, _ := Sweep(dir, 3600e9); len(removed) != 0 {
		t.Errorf("swept a young file: %v", removed)
	}
	write(t, filepath.Join(dir, "unrelated.tmp"), "x", 0o600)
	removed, err := Sweep(dir, 0)
	if err != nil || len(removed) != 1 || removed[0] != orphans[0] {
		t.Errorf("removed %v, %v; want %v", removed, err, orphans)
	}
	if _, err := os.Stat(filepath.Join(dir, "unrelated.tmp")); err != nil {
		t.Error("Sweep removed a file that is not ours")
	}
}

func TestUnchangedWriteLeavesFileAlone(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "c.env")
	write(t, p, "A=1\n", 0o600)
	before, _ := os.Stat(p)
	_, rev, _ := Read(p)
	if got, err := Write(p, []byte("A=1\n"), rev, Options{}); err != nil || got != rev {
		t.Fatalf("rev %v err %v", got, err)
	}
	after, _ := os.Stat(p)
	if !os.SameFile(before, after) {
		t.Error("an unchanged write replaced the file")
	}
}
