//go:build unix

package integration

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AlexK-Notable/herdr-panel/herdr"
)

// TestAutoTitleSettingsPanel opens a consumer's panel the way its user
// would: the herdr-auto-title fork, linked into an isolated herdr, opened
// through its own action. It runs only when HERDR_PANEL_AUTOTITLE_DIR names
// a checkout with `make panel` done (the plugin binary and bin/herdr-panel
// built); its workspace and agent naming are off, so no model is called.
func TestAutoTitleSettingsPanel(t *testing.T) {
	dir := os.Getenv("HERDR_PANEL_AUTOTITLE_DIR")
	if dir == "" {
		t.Skip("HERDR_PANEL_AUTOTITLE_DIR is not set")
	}
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc to find the plugin's processes")
	}
	for _, f := range []string{"herdr-auto-title", "bin/herdr-panel", "panel.toml"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s: run `make panel` in the checkout first", err)
		}
	}
	// The plugin looks in XDG_CONFIG_HOME, then ~/.config: with HOME left
	// alone it would find, and edit, the real user's files.
	realFiles := userFiles(t)
	t.Cleanup(func() {
		if after := userFiles(t); after != realFiles {
			t.Errorf("the real user's auto-title files changed")
		}
	})

	w := newWorld(t)
	home := filepath.Join(w.root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	for i, kv := range w.env {
		if strings.HasPrefix(kv, "HOME=") {
			w.env[i] = "HOME=" + home
		}
	}
	w.env = append(w.env, "HERDR_AUTO_TITLE_NAME_WORKSPACES=false", "HERDR_AUTO_TITLE_AGENTS=false")
	t.Cleanup(func() { killPlugin(t, dir, w.root) }) // runs before the server stops

	w.start()
	w.cli("plugin", "link", dir)
	w.cli("plugin", "action", "invoke", "open-settings", "--plugin", "herdr.auto-title")
	w.waitText("Auto Title")
	w.waitText("Debug logging")
	// The two settings this test sets in herdr's environment win over the
	// file, and the panel says so.
	w.waitText("HERDR_AUTO_TITLE_AGENTS,")
	w.waitText("HERDR_AUTO_TITLE_NAME_WORKSPACES")

	x, y, ok := w.find("Debug logging")
	if !ok {
		t.Fatalf("no Debug logging field:\n%s", w.screen())
	}
	w.click(x, y)
	w.send("\x13") // ctrl+s
	config := filepath.Join(w.root, "config", "herdr-auto-title", "config.env")
	w.waitFor("the setting in "+config, func() bool {
		data, _ := os.ReadFile(config)
		return strings.Contains(string(data), "HERDR_AUTO_TITLE_DEBUG=true")
	})
	// Applying restarts the plugin; the old instance takes up to 15 s to go.
	deadline := time.Now().Add(40 * time.Second)
	for !strings.Contains(w.screen(), "saved and applied") {
		if time.Now().After(deadline) || strings.Contains(w.screen(), "not applied") {
			t.Fatalf("not applied:\n%s", w.screen())
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The workspace list, below the settings, from the plugin's own
	// `panel workspaces`: the isolated session's one workspace.
	w.send("\x1b[6~\x1b[6~") // PgDn twice
	w.waitText("Named by")
	w.waitFor("a workspace row", func() bool {
		s := w.screen()
		return strings.Contains(s, "auto-title") || strings.Contains(s, " you ")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := herdr.NewClient(w.socket).ClosePopup(ctx); err != nil {
		t.Fatal(err)
	}
	w.waitFor("the panel to exit", func() bool { return len(processes(dir+"/bin/herdr-panel", w.root)) == 0 })
	// The restarted instance has a session of its own: closing the popup
	// does not end it.
	time.Sleep(500 * time.Millisecond)
	if len(processes(dir+"/herdr-auto-title", w.root)) == 0 {
		t.Error("no auto-title instance survived the popup")
	}
}

// userFiles is a digest of the real user's auto-title configuration.
func userFiles(t *testing.T) [32]byte {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".config", "herdr-auto-title")
	return digest(filepath.Join(dir, "config.env"), filepath.Join(dir, "manual-names.json"), filepath.Join(dir, "agent-names.json"))
}

// digest is a digest of files' contents; a missing file digests as empty.
func digest(paths ...string) [32]byte {
	h := sha256.New()
	for _, p := range paths {
		data, _ := os.ReadFile(p)
		h.Write([]byte(p + "\x00" + strconv.Itoa(len(data)) + "\x00"))
		h.Write(data)
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

// processes are the pids running program inside the world under root: its
// XDG_CONFIG_HOME names root, so a live session's are never matched.
func processes(program, root string) []int {
	entries, _ := os.ReadDir("/proc")
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		argv0, _, _ := strings.Cut(string(cmdline), "\x00")
		// herdr starts a plugin's command as written, "./herdr-auto-title".
		relative := strings.HasPrefix(argv0, "./") && strings.HasSuffix(program, argv0[1:])
		if argv0 != program && !relative {
			continue
		}
		environ, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err != nil || !strings.Contains(string(environ), "XDG_CONFIG_HOME="+filepath.Join(root, "config")+"\x00") {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// killPlugin ends the plugin's processes in the world under root, and
// waits for them: a restarted instance has a session of its own and would
// otherwise outlive the test.
func killPlugin(t *testing.T, dir, root string) {
	t.Helper()
	progs := []string{dir + "/herdr-auto-title", dir + "/bin/herdr-panel"}
	left := func(sig syscall.Signal) int {
		n := 0
		for _, prog := range progs {
			for _, pid := range processes(prog, root) {
				_ = syscall.Kill(pid, sig)
				n++
			}
		}
		return n
	}
	left(syscall.SIGTERM)
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if left(0) == 0 {
			return
		}
	}
	if n := left(syscall.SIGKILL); n > 0 {
		t.Errorf("%d plugin processes ignored SIGTERM for 15 s", n)
	}
}

// TestFileViewerSettingsPanel checks the second consumer, a plugin written
// by someone else: herdr-file-viewer, with a settings panel added, linked
// into an isolated herdr and opened through its own action. It runs only
// when HERDR_PANEL_FILEVIEWER_DIR names that checkout with bin/herdr-panel
// built (scripts/build-panel.sh); the viewer itself need not be built.
func TestFileViewerSettingsPanel(t *testing.T) {
	dir := os.Getenv("HERDR_PANEL_FILEVIEWER_DIR")
	if dir == "" {
		t.Skip("HERDR_PANEL_FILEVIEWER_DIR is not set")
	}
	for _, f := range []string{"bin/herdr-panel", "panel.toml"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s: run scripts/build-panel.sh in the checkout first", err)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	real := []string{
		filepath.Join(home, ".config", "herdr", "plugins", "config", "herdr-file-viewer", "config.toml"),
		filepath.Join(home, ".config", "herdr-file-viewer", "config.toml"),
	}
	before := digest(real...)
	t.Cleanup(func() {
		if digest(real...) != before {
			t.Errorf("the real user's file-viewer config changed")
		}
	})

	w := newWorld(t)
	for i, kv := range w.env {
		if strings.HasPrefix(kv, "HOME=") {
			w.env[i] = "HOME=" + filepath.Join(w.root, "home")
		}
	}
	w.start()
	w.cli("plugin", "link", dir)
	config := filepath.Join(strings.TrimSpace(w.cli("plugin", "config-dir", "herdr-file-viewer")), "config.toml")
	if !strings.HasPrefix(config, w.root+"/") {
		t.Fatalf("the plugin's config dir %s is outside the isolated root", config)
	}
	// The viewer's documented starting point: every setting commented out.
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	seed := "# settings\n#hide_dotfiles = false\n\n#[keys]\n#refresh = \"g\"\n"
	if err := os.WriteFile(config, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	w.cli("plugin", "action", "invoke", "open-settings", "--plugin", "herdr-file-viewer")
	w.waitText("File viewer")
	w.waitText("Hide dotfiles")
	x, y, ok := w.find("Hide dotfiles")
	if !ok {
		t.Fatalf("no Hide dotfiles field:\n%s", w.screen())
	}
	w.click(x, y)
	w.send("\x13") // ctrl+s
	want := "# settings\nhide_dotfiles = true\n\n#[keys]\n#refresh = \"g\"\n"
	w.waitFor("the setting in "+config, func() bool {
		data, _ := os.ReadFile(config)
		return string(data) == want
	})
	w.waitText("✓ saved")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := herdr.NewClient(w.socket).ClosePopup(ctx); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		w.waitFor("the panel to exit", func() bool { return len(processes(dir+"/bin/herdr-panel", w.root)) == 0 })
	}
	noOrphans(t, filepath.Dir(config))
}
