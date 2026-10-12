package ui_test

import (
	"strings"
	"testing"

	"github.com/AlexK-Notable/herdr-panel/internal/observe"
	"github.com/AlexK-Notable/herdr-panel/internal/ui"
)

// notes is a panel whose first section is an untitled text, like the
// environment overrides the Auto Title panel lists above its settings.
const notes = `spec = 1
title = "Notes panel"
[stores.c]
kind = "envfile"
path = "c.env"
[[section]]
kind = "text"
source = ["say"]
[[section]]
title = "General"
fields = [ { key = "ON", type = "bool", label = "Enabled", default = true } ]
`

// lines is the frame's lines with trailing blanks removed.
func lines(f *fixture) []string {
	out := strings.Split(f.frame(), "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	return out
}

func TestEmptyTextTakesNoRoom(t *testing.T) {
	t.Parallel()
	for _, out := range []string{"", "\n", "  \n\n"} {
		f := open(t, notes, "", observe.Stub{Match: []string{"say"}, Stdout: out})
		l := lines(f)
		// The title row, the rule, then the next section at once.
		if len(l) < 3 || !strings.Contains(l[2], "General") {
			t.Fatalf("text %q: line 3 = %q, want the General section:\n%s", out, l[2], f.frame())
		}
	}
}

func TestEmptyTextBetweenSectionsLeavesOneGap(t *testing.T) {
	t.Parallel()
	src := `spec = 1
title = "Middle"
[stores.c]
kind = "envfile"
path = "c.env"
[[section]]
title = "First"
fields = [ { key = "A", type = "bool", label = "Alpha", default = true } ]
[[section]]
kind = "text"
source = ["say"]
[[section]]
title = "Second"
fields = [ { key = "B", type = "bool", label = "Beta", default = true } ]
`
	f := open(t, src, "", observe.Stub{Match: []string{"say"}, Stdout: ""})
	l := lines(f)
	// Title row, rule, First, Alpha, one blank line, Second.
	if !strings.Contains(l[3], "Alpha") || l[4] != "" || !strings.Contains(l[5], "Second") {
		t.Fatalf("want one blank line between the sections:\n%s", f.frame())
	}
}

func TestTextWithContentKeepsItsPlace(t *testing.T) {
	t.Parallel()
	f := open(t, notes, "", observe.Stub{Match: []string{"say"}, Stdout: "MODEL is set by the environment"})
	l := lines(f)
	if !strings.Contains(l[2], "MODEL is set") || l[3] != "" || !strings.Contains(l[4], "General") {
		t.Fatalf("want the text, a blank line, then General:\n%s", f.frame())
	}
}

func TestTitledEmptyTextShowsOnlyItsTitle(t *testing.T) {
	t.Parallel()
	src := strings.Replace(notes, "[[section]]\nkind = \"text\"", "[[section]]\ntitle = \"Overrides\"\nkind = \"text\"", 1)
	f := open(t, src, "", observe.Stub{Match: []string{"say"}, Stdout: ""})
	l := lines(f)
	if !strings.Contains(l[2], "Overrides") || l[3] != "" || !strings.Contains(l[4], "General") {
		t.Fatalf("want Overrides, a blank line, then General:\n%s", f.frame())
	}
}

func TestFramedLeavesTitleToHerdr(t *testing.T) {
	t.Parallel()
	framed := func(o *ui.Options) { o.Framed = true }
	f := openWith(t, settings, "", framed)
	if strings.Contains(f.frame(), "Settings") {
		t.Fatalf("a framed panel drew its own title:\n%s", f.frame())
	}
	if l := lines(f); !strings.Contains(l[0], "General") {
		t.Fatalf("line 1 = %q, want the first section:\n%s", l[0], f.frame())
	}
	// The unsaved count, which shares the title's row, moves to the footer.
	f.click("field:ON")
	l := lines(f)
	if last := l[len(l)-1]; !strings.Contains(last, "[Close esc]") || !strings.Contains(last, "● 1 unsaved") {
		t.Fatalf("footer = %q, want the buttons and the unsaved count:\n%s", last, f.frame())
	}

	plain := open(t, settings, "")
	if l := lines(plain); !strings.Contains(l[0], "Settings") {
		t.Fatalf("an unframed panel lost its title:\n%s", plain.frame())
	}
	plain.click("field:ON")
	if l := lines(plain); strings.Contains(l[len(l)-1], "unsaved") || !strings.Contains(l[0], "● 1 unsaved") {
		t.Fatalf("an unframed panel keeps the count beside its title:\n%s", plain.frame())
	}
}
