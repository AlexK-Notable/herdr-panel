//go:build unix

package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestInstallBuildsFromTag checks the two [[build]] forms docs/herdr.md
// gives, the way a user meets them: `herdr plugin install` of a plugin
// from GitHub, into an isolated herdr, building herdr-panel at a tag.
// The plugins are integration/testdata/plugins/{go-build,sh-build}, taken
// from the main branch; both build herdr-panel at HERDR_PANEL_BUILD_TAG,
// which go-build's go.mod pins and sh-build's manifest names.
//
// It needs network access, and while the repository is private, GOPRIVATE in
// the environment: the isolated world moves XDG_CONFIG_HOME, so a GOPRIVATE
// set with `go env -w` is not seen. Go's build cache moves with
// XDG_CACHE_HOME too, so each install builds cold, as a first install does;
// run it with -timeout 30m.
func TestInstallBuildsFromTag(t *testing.T) {
	tag := os.Getenv("HERDR_PANEL_BUILD_TAG")
	if tag == "" {
		t.Skip("HERDR_PANEL_BUILD_TAG is not set")
	}
	for _, p := range []struct{ dir, id string }{
		{"go-build", "herdr-panel-test.go-build"},
		{"sh-build", "herdr-panel-test.sh-build"},
	} {
		t.Run(p.dir, func(t *testing.T) {
			w := newWorld(t)
			w.start()
			install := exec.Command("herdr", "plugin", "install", //nolint:noctx // bounded by the test's own timeout
				"AlexK-Notable/herdr-panel/integration/testdata/plugins/"+p.dir, "--ref", "main", "--yes")
			install.Env = append(w.env, ghConfig()...)
			if out, err := install.CombinedOutput(); err != nil {
				t.Fatalf("install: %v\n%s", err, out)
			}
			root := pluginRoot(t, w, p.id)
			if !strings.HasPrefix(root, w.root+"/") {
				t.Fatalf("installed at %s, outside the isolated root", root)
			}
			bin := filepath.Join(root, "bin", "herdr-panel")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, bin, "version").Output()
			if got, want := strings.TrimSpace(string(out)), "herdr-panel "+tag; err != nil || got != want {
				t.Fatalf("%s version = %q (%v), want %q", bin, got, err, want)
			}
			w.cli("plugin", "pane", "open", "--plugin", p.id, "--entrypoint", "settings", "--focus")
			w.waitText("Build check (" + p.dir + ")")
			w.waitText("Tag build")
		})
	}
}

// ghConfig points gh at the user's own configuration. The isolated world
// moves XDG_CONFIG_HOME, and with it the login that gh, git's credential
// helper for a private repository, looks for; nothing of herdr's is shared.
func ghConfig() []string {
	if dir := os.Getenv("GH_CONFIG_DIR"); dir != "" {
		return []string{"GH_CONFIG_DIR=" + dir}
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return []string{"GH_CONFIG_DIR=" + filepath.Join(dir, "gh")}
	}
	return nil
}

// pluginRoot is where herdr installed a plugin.
func pluginRoot(t *testing.T, w *world, id string) string {
	t.Helper()
	var list struct {
		Result struct {
			Plugins []struct {
				ID   string `json:"plugin_id"`
				Root string `json:"plugin_root"`
			} `json:"plugins"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(w.cli("plugin", "list", "--json")), &list); err != nil {
		t.Fatal(err)
	}
	for _, p := range list.Result.Plugins {
		if p.ID == id {
			return p.Root
		}
	}
	t.Fatalf("%s is not installed", id)
	return ""
}
