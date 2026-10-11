//go:build unix

package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexK-Notable/herdr-panel/herdr"
)

// TestPluginManager drives herdr-plugin-manager, the third consumer, in an
// isolated herdr: following a list's selection, row actions that change
// herdr's state, and after = "close_then_run" opening another popup once the
// manager's has closed. It runs only when HERDR_PANEL_PLUGINMANAGER_DIR names
// a checkout with `make build` done.
func TestPluginManager(t *testing.T) {
	dir := os.Getenv("HERDR_PANEL_PLUGINMANAGER_DIR")
	if dir == "" {
		t.Skip("HERDR_PANEL_PLUGINMANAGER_DIR is not set")
	}
	for _, f := range []string{"herdr-plugin-manager", "bin/herdr-panel", "panel.toml"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s: run `make build` in the checkout first", err)
		}
	}
	target, err := filepath.Abs("testdata/plugins/popup-target")
	if err != nil {
		t.Fatal(err)
	}

	w := newWorld(t)
	cfg := filepath.Join(w.root, "config", "herdr", "config.toml")
	binding := "onboarding = false\n\n[[keys.command]]\nkey = \"prefix+t\"\ntype = \"plugin_action\"\ncommand = \"herdr-panel-test.popup-target.say\"\n"
	if err := os.WriteFile(cfg, []byte(binding), 0o600); err != nil {
		t.Fatal(err)
	}
	w.start()
	w.cli("plugin", "link", target)
	w.cli("plugin", "link", dir)
	w.cli("plugin", "action", "invoke", "open", "--plugin", "komi.plugin-manager")

	w.waitText("Installed")
	clickText(w, "Popup target")
	w.waitText("Keys for Popup target")
	w.waitText("prefix+t")     // the binding in this world's config.toml
	w.waitText("Target popup") // the pane, with no key

	// Disable and enable: herdr's own state changes.
	clickText(w, "Popup target")
	w.send("d")
	w.waitText("Disable Popup target?")
	w.send("y")
	w.waitFor("the target disabled", func() bool { return !enabled(t, w, "herdr-panel-test.popup-target") })
	w.send("e")
	w.waitFor("the target enabled", func() bool { return enabled(t, w, "herdr-panel-test.popup-target") })

	// The manager refuses to disable itself.
	clickText(w, "Plugin Manager")
	w.send("d")
	w.waitText("Disable Plugin Manager?")
	w.send("y")
	w.waitText("will not disable itself")
	if !enabled(t, w, "komi.plugin-manager") {
		t.Fatal("the manager disabled itself")
	}

	// Run on the pane: the manager closes, then the target's popup opens,
	// which herdr would refuse while the manager's was still open.
	clickText(w, "Popup target")
	w.waitText("Keys for Popup target")
	clickText(w, "Target popup")
	w.send("r")
	w.waitText("TARGET POPUP OPEN")
	if s := w.screen(); strings.Contains(s, "Keys for") {
		t.Errorf("the manager is still on screen:\n%s", s)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := herdr.NewClient(w.socket).ClosePopup(ctx); err != nil {
		t.Fatal(err)
	}
}

func clickText(w *world, s string) {
	w.t.Helper()
	w.waitText(s)
	x, y, ok := w.find(s)
	if !ok {
		w.t.Fatalf("%q is not on screen:\n%s", s, w.screen())
	}
	w.click(x, y)
}

func enabled(t *testing.T, w *world, id string) bool {
	t.Helper()
	var list struct {
		Result struct {
			Plugins []struct {
				ID      string `json:"plugin_id"`
				Enabled bool   `json:"enabled"`
			} `json:"plugins"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(w.cli("plugin", "list", "--json")), &list); err != nil {
		t.Fatal(err)
	}
	for _, p := range list.Result.Plugins {
		if p.ID == id {
			return p.Enabled
		}
	}
	t.Fatalf("%s is not installed", id)
	return false
}
