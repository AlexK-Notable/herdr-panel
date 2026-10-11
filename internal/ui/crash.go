package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/internal/store"
)

type storeSnapshot = store.Snapshot

// crash is a panic the panel caught. Bubble Tea's own recovery would print
// the stack and exit, and the popup would close with the message in it, so
// the panel catches panics itself, keeps the value and the stack, writes them
// to a log and shows them until a key is pressed.
type crash struct {
	value string
	stack string
	log   string // where it was written, or "" if that failed
}

type crashMsg struct{ c *crash }

func (m *Model) recordCrash(r any) *crash {
	c := &crash{value: fmt.Sprint(r), stack: string(debug.Stack())}
	p := m.crashLogPath()
	body := fmt.Sprintf("herdr-panel crashed\n\npanic: %s\n\n%s", c.value, c.stack)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err == nil {
		if err := os.WriteFile(p, []byte(body), 0o600); err == nil {
			c.log = p
		}
	}
	return c
}

// safe wraps a command so a panic inside it becomes a crash screen.
func (m *Model) safe(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = crashMsg{c: m.recordCrash(r)}
			}
		}()
		return cmd()
	}
}

func tempDir() string { return os.TempDir() }

func itoa(n int) string { return strconv.Itoa(n) }
