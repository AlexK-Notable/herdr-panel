package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/AlexK-Notable/herdr-panel/herdr"
)

// TestFramedTitle checks the panel's title against herdr's frame. herdr
// titles a plugin's popup with the manifest's pane title, so the panel
// leaves its own out; a split has no frame title, so there the panel shows
// it. The panel tells the two apart by HERDR_PANE_ID, which herdr 0.9.2
// gives a split and not a popup.
func TestFramedTitle(t *testing.T) {
	w := newWorld(t)
	bin := buildPanel(t, w.root)
	plugin := filepath.Join(w.root, "plugin")
	if err := os.MkdirAll(plugin, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(plugin, "herdr-plugin.toml"): fmt.Sprintf(`id = "test.framed"
name = "Framed"
version = "0.0.1"
min_herdr_version = "0.7.0"
[[panes]]
id = "p"
title = "FrameTitle"
placement = "popup"
width = 60
height = 12
command = [%q, "panel.toml"]
`, bin),
		filepath.Join(plugin, "panel.toml"): `spec = 1
title = "PanelOwnTitle"
[[section]]
kind = "text"
text = "BODY_TEXT"
`,
	}
	for p, content := range files {
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w.start()
	w.cli("plugin", "link", plugin)

	// A popup: herdr's frame names it, the panel does not.
	w.cli("plugin", "pane", "open", "--plugin", "test.framed", "--entrypoint", "p", "--focus")
	w.waitText("BODY_TEXT")
	if !strings.Contains(w.screen(), "FrameTitle") {
		t.Fatalf("herdr's frame title is missing:\n%s", w.screen())
	}
	if strings.Contains(w.screen(), "PanelOwnTitle") {
		t.Fatalf("the popup shows its title twice:\n%s", w.screen())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := herdr.NewClient(w.socket).ClosePopup(ctx); err != nil {
		t.Fatal(err)
	}
	w.waitFor("the popup to close", func() bool { return !strings.Contains(w.screen(), "BODY_TEXT") })

	// A split: no frame title, so the panel shows its own.
	out := w.cli("plugin", "pane", "open", "--plugin", "test.framed", "--entrypoint", "p", "--placement", "split", "--focus")
	w.waitText("BODY_TEXT")
	w.waitText("PanelOwnTitle")
	if m := regexp.MustCompile(`"pane_id":"([^"]+)"`).FindStringSubmatch(out); m != nil {
		w.cli("plugin", "pane", "close", m[1])
	}
}
