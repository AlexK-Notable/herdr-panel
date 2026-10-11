package ui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/AlexK-Notable/herdr-panel/internal/observe"
	"github.com/AlexK-Notable/herdr-panel/internal/store"
	"github.com/AlexK-Notable/herdr-panel/internal/ui"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

type fixture struct {
	t    *testing.T
	d    *observe.Driver
	sb   *observe.Sandbox
	file string
}

// open builds a panel from spec text, with its store "c" in a temporary
// file holding initial, and commands answered by stubs.
func open(t *testing.T, src, initial string, stubs ...observe.Stub) *fixture {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "c.env")
	if initial != "" {
		if err := os.WriteFile(file, []byte(initial), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := spec.Parse([]byte(src), "t.toml", dir)
	if err != nil {
		t.Fatal(err)
	}
	sb := &observe.Sandbox{Stubs: &observe.Stubs{Commands: stubs}}
	d := observe.New(ui.Options{
		Spec: s, Runner: sb, CrashDir: dir,
		Stores: map[string]store.Store{"c": &store.EnvFile{Path: file, Create: true}},
	}, 80, 30)
	return &fixture{t: t, d: d, sb: sb, file: file}
}

func (f *fixture) click(id string) {
	f.t.Helper()
	if err := f.d.Click(id); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) key(names ...string) {
	f.t.Helper()
	for _, n := range names {
		if err := f.d.Key(n); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *fixture) frame() string { return f.d.Model().Frame() }

func (f *fixture) fileText() string {
	data, _ := os.ReadFile(f.file)
	return string(data)
}

func (f *fixture) expectFrame(want ...string) {
	f.t.Helper()
	fr := f.frame()
	for _, w := range want {
		if !strings.Contains(fr, w) {
			f.t.Fatalf("frame lacks %q:\n%s", w, fr)
		}
	}
}

const settings = `spec = 1
title = "Settings"
[stores.c]
kind = "envfile"
path = "c.env"
apply = ["restart-it"]
[[section]]
title = "General"
fields = [
  { key = "ON", type = "bool", label = "Enabled", default = true },
  { key = "MODE", type = "choice", label = "Mode", choices = ["fast", "slow"], default = "fast" },
  { key = "N", type = "int", label = "Count", min = 1, max = 9, default = 3 },
  { key = "WAIT", type = "duration_s", label = "Wait", min = 10, max = 3600 },
  { key = "NAME", type = "text", label = "Name", pattern = "[a-z]+" },
  { key = "TOKEN", type = "secret", label = "Token", test = ["check-token"] },
]
`

func TestGoldenFirstFrame(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "# my settings\nMODE=slow\nTOKEN=hunter2\n")
	golden.RequireEqual(t, f.frame())
}

func TestToggleSaveApply(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "# my settings\nMODE=slow\n")
	f.click("field:ON")
	f.expectFrame("[ ] off • changed", "● 1 unsaved")
	if f.fileText() != "# my settings\nMODE=slow\n" {
		t.Fatal("saved before ctrl+s")
	}
	f.key("ctrl+s")
	if got := f.fileText(); got != "# my settings\nMODE=slow\nON=false\n" {
		t.Fatalf("file = %q", got)
	}
	f.expectFrame("✓ saved and applied")
	if calls := f.sb.Calls(); len(calls) != 1 || calls[0].Argv[0] != "restart-it" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestLiveModeSavesEachEdit(t *testing.T) {
	t.Parallel()
	f := open(t, strings.Replace(settings, `title = "Settings"`, "title = \"Settings\"\nsave = \"live\"", 1), "")
	f.click("field:ON")
	if got := f.fileText(); got != "ON=false\n" {
		t.Fatalf("file = %q", got)
	}
	f.expectFrame("saves as you edit")
}

func TestApplyFailureIsVisible(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "", observe.Stub{Match: []string{"restart-it"}, Stderr: "daemon not running", Exit: 1})
	f.click("field:ON")
	f.key("ctrl+s")
	f.expectFrame("✗ saved, not applied: daemon not running")
	if f.fileText() != "ON=false\n" {
		t.Error("the save itself did not happen")
	}
}

func TestChoicePickerAndCustomValue(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "MODE=turbo\n")
	f.expectFrame("‹ custom: turbo ›")
	f.click("field:MODE")
	f.expectFrame("fast", "slow")
	f.click("option:1")
	f.expectFrame("‹ slow › • changed")
	f.key("right")
	f.expectFrame("‹ fast › • changed")
}

func TestIntEditRejectsOutOfRange(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "")
	f.click("field:N")
	f.key("ctrl+u")
	f.d.Type("42")
	f.key("enter")
	f.expectFrame("✗ 42 is above the maximum 9")
	f.key("backspace")
	f.key("enter")
	f.expectFrame("4 • changed")
}

func TestDurationAcceptsBothForms(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "")
	f.click("field:WAIT")
	f.d.Type("1m30s")
	f.key("enter")
	f.expectFrame("1m30s • changed")
	f.key("ctrl+s")
	if !strings.Contains(f.fileText(), "WAIT=90\n") {
		t.Errorf("file = %q", f.fileText())
	}
	f.key("left")
	f.expectFrame("1m20s")
}

func TestTextPatternAndPaste(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "")
	f.click("field:NAME")
	f.d.Paste("Bad Name")
	f.key("enter")
	f.expectFrame("does not match")
	f.key("ctrl+u")
	f.d.Paste("good")
	f.key("enter")
	f.expectFrame("good • changed")
}

func TestSecretIsNeverShown(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "TOKEN=hunter2\n")
	f.expectFrame("Token", "set")
	f.click("field:TOKEN")
	f.d.Type("s3cret")
	if strings.Contains(f.frame(), "s3cret") || !strings.Contains(f.frame(), "******") {
		t.Fatalf("secret visible while typing:\n%s", f.frame())
	}
	f.key("enter")
	f.click("test:TOKEN")
	f.key("ctrl+s")
	for _, c := range f.sb.Calls() {
		for _, e := range append(c.Argv, c.Env...) {
			if strings.Contains(e, "s3cret") || strings.Contains(e, "hunter2") {
				t.Errorf("a command received the secret: %q", e)
			}
		}
	}
	if strings.Contains(f.frame(), "hunter2") || strings.Contains(f.frame(), "s3cret") {
		t.Error("secret rendered")
	}
	if !strings.Contains(f.fileText(), "TOKEN=s3cret") {
		t.Errorf("file = %q", f.fileText())
	}
}

func TestResetToDefaultUnsetsKey(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "N=7\n")
	f.click("field:N")
	f.key("esc") // leave the editor the click opened
	f.key("delete")
	f.expectFrame("3 (default)", "● 1 unsaved")
	f.key("ctrl+s")
	if f.fileText() != "" {
		t.Errorf("file = %q", f.fileText())
	}
}

func TestConflictOverwriteKeepsTheirEdit(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "MODE=slow\n")
	f.click("field:ON")
	if err := os.WriteFile(f.file, []byte("MODE=slow\nOTHER=theirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.key("ctrl+s")
	f.expectFrame("Changed on disk", "Reload theirs", "Overwrite with mine")
	f.click("dialog:1")
	if got := f.fileText(); got != "MODE=slow\nOTHER=theirs\nON=false\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestConflictReloadDropsMine(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "MODE=slow\n")
	f.click("field:ON")
	if err := os.WriteFile(f.file, []byte("MODE=fast\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.key("ctrl+s")
	f.click("dialog:0")
	if f.d.Model().Dirty() != 0 || f.fileText() != "MODE=fast\n" {
		t.Fatalf("dirty %d file %q", f.d.Model().Dirty(), f.fileText())
	}
	f.expectFrame("‹ fast ›")
}

func TestCloseWithUnsavedAsks(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "")
	f.click("field:ON")
	f.key("esc")
	f.expectFrame("Unsaved changes", "1 unsaved change.")
	if f.d.Quit {
		t.Fatal("closed without asking")
	}
	f.click("dialog:0") // save and close
	if !f.d.Quit || f.fileText() != "ON=false\n" {
		t.Errorf("quit %v file %q", f.d.Quit, f.fileText())
	}
}

func TestDialogIsModal(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "")
	f.click("field:MODE")
	// A click on a field behind the picker is swallowed.
	for _, e := range f.d.Model().Elements() {
		if e.ID == "field:ON" {
			f.d.Send(clickAt(e.X+1, e.Y))
		}
	}
	if f.d.Model().Dirty() != 0 {
		t.Error("a click went through the dialog")
	}
}

func TestEveryElementCenterHitsItself(t *testing.T) {
	t.Parallel()
	f := open(t, listSpec, "", rowsStub(`[{"id":"a","name":"Alpha"},{"id":"b","name":"Beta"}]`))
	check := func(when string) {
		for _, e := range f.d.Model().Elements() {
			if got := f.d.Model().Hit(e.X+e.W/2, e.Y); got != e.ID && e.ID != "dialog" {
				t.Errorf("%s: center of %s hits %q", when, e.ID, got)
			}
		}
	}
	check("panel")
	f.click("rowaction:1:1") // opens a confirm dialog
	check("dialog")
}

func TestTooSmall(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "")
	f.d.Resize(20, 4)
	if got := strings.TrimSpace(f.frame()); !strings.HasPrefix(got, "make me bigger 20x4") {
		t.Errorf("frame = %q", got)
	}
}

func TestLoadErrorScreen(t *testing.T) {
	t.Parallel()
	_, err := spec.Parse([]byte("spec = 1\ntitle = 3\n"), "bad.toml", "/tmp")
	d := observe.New(ui.Options{LoadErr: err}, 80, 20)
	fr := d.Model().Frame()
	if !strings.Contains(fr, "bad.toml:2:9: title must be a string") || !strings.Contains(fr, "press q to close") {
		t.Fatalf("frame:\n%s", fr)
	}
	if err := d.Key("q"); err != nil || !d.Quit {
		t.Error("q did not close the error screen")
	}
}

func TestKeyNamesRoundTrip(t *testing.T) {
	t.Parallel()
	for _, k := range []string{"a", "A", "?", "/", "enter", "esc", "space", "tab", "shift+tab", "ctrl+s", "ctrl+r", "alt+x", "pgup", "f5", "delete", "backspace", "up", "+", "ctrl++", "insert"} {
		if _, err := observe.KeyMsg(k); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
}

// Save and close runs the store's apply before closing: closing first would
// cancel it before it started (the sandbox, like the real runner, refuses a
// cancelled context).
func TestSaveAndCloseAppliesFirst(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "")
	f.click("field:ON")
	f.key("esc")
	f.click("dialog:0") // Save and close
	if f.fileText() != "ON=false\n" {
		t.Errorf("file = %q", f.fileText())
	}
	calls := f.sb.Calls()
	if len(calls) != 1 || calls[0].Argv[0] != "restart-it" || calls[0].Cancelled {
		t.Fatalf("apply did not run before the close: %+v", calls)
	}
	if !f.d.Quit {
		t.Error("the panel did not close after saving and applying")
	}
}

// A failed apply keeps the panel open on "saved, not applied".
func TestSaveAndCloseStaysOpenWhenApplyFails(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "", observe.Stub{Match: []string{"restart-it"}, Stderr: "daemon not running", Exit: 1})
	f.click("field:ON")
	f.key("esc")
	f.click("dialog:0")
	if f.d.Quit {
		t.Fatal("closed over a failed apply")
	}
	f.expectFrame("✗ saved, not applied: daemon not running")
}

// A click elsewhere while typing keeps the value, as tab does: clicking
// Save straight after typing saves what was typed.
func TestClickSaveWhileTypingKeepsTheValue(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "")
	f.click("field:NAME")
	f.d.Type("hello")
	f.click("save")
	if f.fileText() != "NAME=hello\n" {
		t.Errorf("file = %q; status %q", f.fileText(), f.d.Model().Status())
	}
}

// An invalid value keeps the editor open and the click does nothing.
func TestClickAwayFromInvalidValueKeepsEditing(t *testing.T) {
	t.Parallel()
	f := open(t, settings, "")
	f.click("field:NAME")
	f.d.Type("Bad Name")
	f.click("save")
	f.expectFrame("does not match")
	if f.fileText() != "" || f.d.Model().Focused() != "field:NAME" {
		t.Errorf("file %q, focus %q", f.fileText(), f.d.Model().Focused())
	}
}
