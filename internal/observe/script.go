package observe

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/AlexK-Notable/herdr-panel/internal/ui"
)

// Version is the version of the JSON observe prints. A field is only ever
// added to it; a change that would break a reader is a new version.
const Version = 1

// Snapshot is the panel at one moment.
type Snapshot struct {
	Observe  int          `json:"observe"`
	Label    string       `json:"label,omitempty"`
	Size     Size         `json:"size"`
	Frame    string       `json:"frame"`
	Elements []ui.Element `json:"elements"`
	Focused  string       `json:"focused"`
	Status   string       `json:"status"`
	Dirty    int          `json:"dirty"`
	Quit     bool         `json:"quit"`
}

// Size is a terminal size.
type Size struct {
	W int `json:"w"`
	H int `json:"h"`
}

// Snapshot takes the panel's current state.
func (d *Driver) Snapshot(label string) Snapshot {
	m := d.m
	w, h := m.Size()
	return Snapshot{
		Observe: Version, Label: label, Size: Size{w, h},
		Frame: m.Frame(), Elements: m.Elements(), Focused: m.Focused(),
		Status: m.Status(), Dirty: m.Dirty(), Quit: d.Quit,
	}
}

// RunScript runs one action per line:
//
//	click <id>        click an element
//	key <name>        press a key: enter, ctrl+s, shift+tab, a
//	type <text>       type text, a key per character
//	paste <text>      paste text
//	resize <W>x<H>    change the terminal size
//	snapshot [label]  record the panel now
//
// Blank lines and lines starting with # are skipped. It stops at the first
// action that cannot be done, naming the line.
func (d *Driver) RunScript(r io.Reader) ([]Snapshot, error) {
	var snaps []Snapshot
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		lineText := strings.TrimSpace(sc.Text())
		if lineText == "" || strings.HasPrefix(lineText, "#") {
			continue
		}
		verb, arg, _ := strings.Cut(lineText, " ")
		arg = strings.TrimSpace(arg)
		var err error
		switch verb {
		case "click":
			err = d.Click(arg)
		case "key":
			err = d.Key(arg)
		case "type":
			d.Type(arg)
		case "paste":
			d.Paste(arg)
		case "resize":
			var w, h int
			w, h, err = parseSize(arg)
			if err == nil {
				d.Resize(w, h)
			}
		case "snapshot":
			snaps = append(snaps, d.Snapshot(arg))
		default:
			err = fmt.Errorf("unknown action %q (click, key, type, paste, resize, snapshot)", verb)
		}
		if err != nil {
			return snaps, fmt.Errorf("script line %d: %w", n, err)
		}
		if d.Quit {
			break
		}
	}
	return snaps, sc.Err()
}

// ParseSize reads "80x24".
func parseSize(s string) (int, int, error) {
	a, b, ok := strings.Cut(strings.ToLower(s), "x")
	w, err1 := strconv.Atoi(a)
	h, err2 := strconv.Atoi(b)
	if !ok || err1 != nil || err2 != nil || w < 1 || h < 1 || w > 1000 || h > 1000 {
		return 0, 0, fmt.Errorf("size %q is not WIDTHxHEIGHT, such as 80x24", s)
	}
	return w, h, nil
}

// ParseSize reads "80x24".
func ParseSize(s string) (int, int, error) { return parseSize(s) }
