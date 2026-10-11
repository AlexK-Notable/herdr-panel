package paneltest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AlexK-Notable/herdr-panel/paneltest"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

// A config as the viewer documents it: every setting commented out, and a
// commented-out [keys] table.
const viewerConfig = `# my config
#hide_dotfiles = false
#scroll_lines = 3
#tree_position = "left"

#[keys]
#refresh = "g"
`

// The file-viewer example edits a TOML config in place: each setting lands
// on its commented default, typed as the viewer reads it.
func TestFileViewerExample(t *testing.T) {
	t.Parallel()
	s, err := spec.Load("../examples/file-viewer.toml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(viewerConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	p := paneltest.New(t, s, paneltest.Config{Height: 40, Stores: map[string]string{"config": path}})
	p.Click("field:hide_dotfiles")
	p.Click("field:scroll_lines")
	p.Key("esc", "right", "right") // leave the number's editor, then step it
	p.Click("field:tree_position")
	p.Key("esc", "right")
	p.Key("ctrl+s")
	p.RequireFrame("✓ saved")
	want := `# my config
hide_dotfiles = true
scroll_lines = 5
tree_position = "right"

#[keys]
#refresh = "g"
`
	if got := p.StoreText("config"); got != want {
		t.Errorf("config:\n%s\nwant:\n%s\nframe:\n%s", got, want, p.Frame())
	}
}
