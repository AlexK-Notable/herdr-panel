//go:build unix

// Package integration runs herdr-panel inside a real, isolated herdr: a
// herdr client in a pseudo-terminal, read through an x/vt screen, its
// server and plugin registry under a temporary directory. Every HERDR_*
// variable is removed and every XDG directory redirected, so nothing reaches
// a live session. The tests skip when herdr is not installed.
package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"

	"github.com/AlexK-Notable/herdr-panel/herdr"
)

const cols, rows = 120, 36

// world is one isolated herdr: its directories, its environment, a client
// in a pty and the screen that client draws.
type world struct {
	t      *testing.T
	root   string
	env    []string
	socket string
	scr    *vt.SafeEmulator
	ptmx   *os.File
	client *exec.Cmd
	mu     sync.Mutex
}

func newWorld(t *testing.T) *world {
	t.Helper()
	if _, err := exec.LookPath("herdr"); err != nil {
		t.Skip("herdr is not installed")
	}
	// A socket path must fit in sun_path (104 bytes on macOS); t.TempDir()
	// names are too long.
	root, err := os.MkdirTemp("", "hpi") //nolint:usetesting // t.TempDir() overruns sun_path
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, root: root}
	w.env = isolatedEnv(root)
	w.socket = filepath.Join(root, "config", "herdr", "herdr.sock")
	t.Cleanup(w.close)
	// The isolated config skips the first-run welcome, which is modal and
	// would cover the popup (herdr's config.rs: onboarding = false).
	if err := os.MkdirAll(filepath.Join(root, "config", "herdr"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "herdr", "config.toml"), []byte("onboarding = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return w
}

// isolatedEnv is this process's environment without HERDR_* and with every
// XDG directory under root.
func isolatedEnv(root string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "HERDR_") || strings.HasPrefix(k, "XDG_") || k == "TERM" || k == "COLORTERM" {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"XDG_CONFIG_HOME="+filepath.Join(root, "config"),
		"XDG_STATE_HOME="+filepath.Join(root, "state"),
		"XDG_DATA_HOME="+filepath.Join(root, "data"),
		"XDG_CACHE_HOME="+filepath.Join(root, "cache"),
		"XDG_RUNTIME_DIR="+filepath.Join(root, "run"),
		"TERM=xterm-256color", "COLORTERM=truecolor",
	)
}

// cli runs the herdr CLI against this world only.
func (w *world) cli(args ...string) string {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "herdr", args...)
	cmd.Env = w.env
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.t.Fatalf("herdr %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// start runs a herdr client in a pty and keeps the screen current.
func (w *world) start() {
	w.t.Helper()
	_ = os.MkdirAll(filepath.Join(w.root, "run"), 0o700)
	w.scr = vt.NewSafeEmulator(cols, rows)
	w.client = exec.Command("herdr") //nolint:noctx // lives as long as the test; killed in close
	w.client.Env = w.env
	ptmx, err := pty.StartWithSize(w.client, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		w.t.Fatal(err)
	}
	w.ptmx = ptmx
	go func() { _, _ = io.Copy(w.scr, ptmx) }() // what herdr draws
	go func() { _, _ = io.Copy(ptmx, w.scr) }() // the terminal's replies to herdr's queries
	w.waitFor("the herdr server's socket", func() bool {
		_, err := os.Stat(w.socket)
		return err == nil
	})
}

// screen is the client's screen as plain text, one line per row.
func (w *world) screen() string {
	var b strings.Builder
	for y := range rows {
		for x := 0; x < cols; x++ {
			c := w.scr.CellAt(x, y)
			switch {
			case c == nil:
				b.WriteByte(' ')
			case c.Width == 0:
				// the second cell of a wide character
			case c.Content == "":
				b.WriteByte(' ')
			default:
				b.WriteString(c.Content)
				x += max(c.Width, 1) - 1
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// find is the 0-based cell of needle's first character on screen.
func (w *world) find(needle string) (x, y int, ok bool) {
	for i, line := range strings.Split(w.screen(), "\n") {
		if j := strings.Index(line, needle); j >= 0 {
			return len([]rune(line[:j])), i, true
		}
	}
	return 0, 0, false
}

func (w *world) waitFor(what string, cond func() bool) {
	w.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			w.t.Fatalf("timed out waiting for %s; screen:\n%s", what, w.screen())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (w *world) waitText(s string) {
	w.t.Helper()
	w.waitFor(fmt.Sprintf("%q on screen", s), func() bool { return strings.Contains(w.screen(), s) })
}

// click sends a left click at a 0-based cell, as an SGR mouse report.
func (w *world) click(x, y int) {
	w.t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := fmt.Fprintf(w.ptmx, "\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+1, y+1); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) send(s string) {
	w.t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := io.WriteString(w.ptmx, s); err != nil {
		w.t.Fatal(err)
	}
}

// close stops this world's server, but only after herdr confirms the server
// is the one under root: a stray variable must never stop a live session.
func (w *world) close() {
	if w.client != nil && w.client.Process != nil {
		_ = w.client.Process.Kill()
		_ = w.client.Wait()
	}
	if w.ptmx != nil {
		_ = w.ptmx.Close()
	}
	if _, err := os.Stat(w.socket); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		status := exec.CommandContext(ctx, "herdr", "status")
		status.Env = w.env
		out, _ := status.CombinedOutput()
		if strings.Contains(string(out), w.root+"/") {
			stop := exec.CommandContext(ctx, "herdr", "server", "stop")
			stop.Env = w.env
			_ = stop.Run()
		} else {
			w.t.Errorf("not stopping a herdr server whose status does not name %s:\n%s", w.root, out)
		}
	}
	_ = os.RemoveAll(w.root)
}

// buildPanel builds herdr-panel from this checkout into dir, as a user
// would: in the main module.
func buildPanel(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "herdr-panel")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/herdr-panel")
	cmd.Dir = ".." // the main module, with its own go.sum
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// TestPopupRoundTrip is the milestone's end-to-end check: a real popup
// opens, a click toggles a field, ctrl+s saves it to disk, herdr's own
// pane read agrees with the screen, and closing the popup leaves no
// temporary files behind.
func TestPopupRoundTrip(t *testing.T) {
	w := newWorld(t)
	bin := buildPanel(t, w.root)

	plugin := filepath.Join(w.root, "plugin")
	store := filepath.Join(w.root, "store")
	for _, d := range []string{plugin, store} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	storeFile := filepath.Join(store, "c.env")
	files := map[string]string{
		filepath.Join(plugin, "herdr-plugin.toml"): fmt.Sprintf(`id = "test.panel"
name = "Panel test"
version = "0.0.1"
min_herdr_version = "0.7.0"
[[panes]]
id = "settings"
title = "Settings"
placement = "popup"
width = 70
height = 16
command = [%q, "panel.toml"]
`, bin),
		filepath.Join(plugin, "panel.toml"): fmt.Sprintf(`spec = 1
title = "Round trip"
[stores.c]
kind = "envfile"
path = %q
[[section]]
fields = [ { key = "ON", type = "bool", label = "Enabled", default = true } ]
`, storeFile),
		storeFile: "# kept\nON=true\n",
	}
	for p, content := range files {
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	w.start()
	w.cli("plugin", "link", plugin)

	client := herdr.NewClient(w.socket)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, err := client.Ping(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("isolated herdr %s, protocol %d", srv.Version, srv.Protocol)

	// A. The popup, as a user meets it.
	if _, err := client.Open(ctx, herdr.OpenOptions{PluginID: "test.panel", Entrypoint: "settings", Focus: true}); err != nil {
		t.Fatal(err)
	}
	// 1. It draws the panel.
	w.waitText("[x] on")
	x, y, _ := w.find("[x] on")
	// herdr shows one popup at a time.
	if _, err := client.Open(ctx, herdr.OpenOptions{PluginID: "test.panel", Entrypoint: "settings"}); !errors.Is(err, herdr.ErrBusy) {
		t.Errorf("a second popup: err = %v, want ErrBusy", err)
	}
	// 2. A click toggles the field.
	w.click(x+1, y)
	w.waitText("[ ] off • changed")
	// 3. ctrl+s writes it to disk; the comment and the order stay.
	w.send("\x13")
	w.waitText("✓ saved")
	w.waitFor("the store file to change", func() bool {
		data, _ := os.ReadFile(storeFile)
		return string(data) == "# kept\nON=false\n"
	})
	// 4. herdr closes the popup (SIGHUP, SIGTERM, SIGKILL); an unsaved edit
	// is discarded without a question, and nothing is left behind.
	w.click(x+1, y)
	w.waitText("[x] on • changed")
	if err := client.ClosePopup(ctx); err != nil {
		t.Fatal(err)
	}
	w.waitFor("the popup to close", func() bool { return !strings.Contains(w.screen(), "Round trip") })
	time.Sleep(600 * time.Millisecond) // past herdr's signal cadence
	if data, _ := os.ReadFile(storeFile); string(data) != "# kept\nON=false\n" {
		t.Errorf("closing saved the unsaved edit: %q", data)
	}
	noOrphans(t, store)

	// B. The same panel as an overlay, which has a pane id: herdr's own
	// reading of it must agree with the screen x/vt drew.
	opened, err := client.Open(ctx, herdr.OpenOptions{PluginID: "test.panel", Entrypoint: "settings", Placement: herdr.Overlay, Focus: true})
	if err != nil {
		t.Fatal(err)
	}
	if opened.PaneID == "" {
		t.Fatal("an overlay came back without a pane id")
	}
	w.waitText("[ ] off")
	// A frame can be caught mid-repaint (the overlay is resized as it
	// opens), so the two readings are compared until they settle; if they
	// never agree, the last difference is the failure.
	var lastDiff string
	agree := func() bool {
		read := w.cli("pane", "read", opened.PaneID, "--source", "visible")
		screen := w.screen()
		lines := 0
		for _, line := range strings.Split(read, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			lines++
			if !strings.Contains(screen, line) {
				lastDiff = fmt.Sprintf("pane read shows %q, which the screen does not:\n%s", line, screen)
				return false
			}
		}
		if lines < 3 || !strings.Contains(read, "Round trip") {
			lastDiff = fmt.Sprintf("pane read is incomplete (%d lines):\n%s", lines, read)
			return false
		}
		return true
	}
	deadline := time.Now().Add(5 * time.Second)
	for !agree() {
		if time.Now().After(deadline) {
			t.Fatalf("herdr's pane read never agreed with the screen: %s", lastDiff)
		}
		time.Sleep(100 * time.Millisecond)
	}
	w.cli("plugin", "pane", "close", opened.PaneID)
	w.waitFor("the overlay to close", func() bool { return !strings.Contains(w.screen(), "Round trip") })
	time.Sleep(600 * time.Millisecond)
	noOrphans(t, store)
}

// noOrphans fails if a panel left a temporary file beside its store.
func noOrphans(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".herdr-panel-") {
			t.Errorf("orphaned temporary file %s", e.Name())
		}
	}
}
