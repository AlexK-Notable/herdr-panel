package panel

import (
	"testing"

	"github.com/AlexK-Notable/herdr-panel/herdr"
)

// TestFramed covers the environments herdr 0.9.2 gives each kind of pane,
// as measured by integration's TestFramedTitle.
func TestFramed(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"plugin popup", map[string]string{"HERDR_PLUGIN_ID": "p"}, true},
		{"plugin split", map[string]string{"HERDR_PLUGIN_ID": "p", "HERDR_PANE_ID": "w1:p2"}, false},
		{"keybinding popup", map[string]string{"HERDR_ACTIVE_PANE_ID": "w1:p1"}, false},
		{"outside herdr", map[string]string{}, false},
	} {
		getenv := func(k string) string { return c.env[k] }
		if got := framed(herdr.FromEnv(getenv), getenv); got != c.want {
			t.Errorf("%s: framed = %v, want %v", c.name, got, c.want)
		}
	}
}
