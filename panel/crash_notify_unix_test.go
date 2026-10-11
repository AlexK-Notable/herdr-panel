//go:build unix

package panel_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/panel"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

type explodingView struct{}

func (explodingView) Render(panel.RenderInput) panel.RenderOutput { panic("render exploded") }
func (explodingView) Update(tea.Msg) tea.Cmd                      { return nil }

// fakeHerdr answers every request with a shown notification and hands each
// request line to got.
func fakeHerdr(t *testing.T) (string, <-chan string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "hp") //nolint:usetesting // t.TempDir() overruns sun_path
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan string, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadString('\n')
			got <- line
			_, _ = conn.Write([]byte(`{"id":"x","result":{"type":"notification_show","shown":true,"reason":"shown"}}` + "\n"))
			_ = conn.Close()
		}
	}()
	return path, got
}

// A crash inside herdr is also reported as a herdr notification naming the
// crash log: the popup, and its crash screen, are gone once closed.
func TestCrashNotifiesHerdr(t *testing.T) {
	sock, got := fakeHerdr(t)
	t.Setenv("HERDR_SOCKET_PATH", sock)
	s, err := spec.Parse([]byte("spec = 1\ntitle = \"T\"\n[[section]]\nkind = \"view\"\nview = \"x\"\n"), "x.toml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in, inW := io.Pipe()
	out := &firstFrame{marker: "render exploded", done: make(chan struct{})}
	errc := make(chan error, 1)
	go func() {
		errc <- panel.Run(context.Background(), s, panel.Options{
			Input: in, Output: out, Width: 80, Height: 24, CrashDir: t.TempDir(),
			Context: map[string]string{}, Views: map[string]panel.View{"x": explodingView{}},
		})
	}()
	select {
	case <-out.done:
	case <-time.After(5 * time.Second):
		t.Fatal("no crash screen")
	}
	_, _ = inW.Write([]byte("q"))
	select {
	case err := <-errc:
		if !errors.Is(err, panel.ErrCrashed) {
			t.Errorf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the crash screen did not close on a key")
	}
	select {
	case line := <-got:
		if !strings.Contains(line, `"notification.show"`) || !strings.Contains(line, "herdr-panel crashed") || !strings.Contains(line, "crash-") {
			t.Errorf("request = %s", line)
		}
	case <-time.After(time.Second):
		t.Error("herdr was not notified")
	}
}

type panicOnKey struct{}

func (panicOnKey) Render(panel.RenderInput) panel.RenderOutput {
	return panel.RenderOutput{Lines: []string{"press any key"}}
}

func (panicOnKey) Update(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		panic("view update exploded")
	}
	return nil
}

// A panic in Update, in a real Bubble Tea program, shows the panel's own
// crash screen (with the panic), not Bubble Tea's fallback.
func TestUpdatePanicShowsCrashScreenInARealProgram(t *testing.T) {
	t.Parallel()
	s, err := spec.Parse([]byte("spec = 1\ntitle = \"T\"\n[[section]]\nkind = \"view\"\nview = \"x\"\n"), "x.toml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in, inW := io.Pipe()
	ready := &firstFrame{marker: "press any key", done: make(chan struct{})}
	errc := make(chan error, 1)
	go func() {
		errc <- panel.Run(context.Background(), s, panel.Options{
			Input: in, Output: ready, Width: 80, Height: 24, CrashDir: t.TempDir(),
			Context: map[string]string{}, Views: map[string]panel.View{"x": panicOnKey{}},
		})
	}()
	select {
	case <-ready.done:
	case <-time.After(5 * time.Second):
		t.Fatal("no first frame")
	}
	_, _ = inW.Write([]byte("a"))
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(ready.text(), "view update exploded") {
		if time.Now().After(deadline) {
			t.Fatalf("the crash screen never showed the panic:\n%s", ready.text())
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = inW.Write([]byte("q"))
	if err := <-errc; !errors.Is(err, panel.ErrCrashed) {
		t.Errorf("err = %v", err)
	}
}

// A spec whose view nobody supplies is refused on screen, not by a silent
// exit: in a popup the message would leave with the process.
func TestMissingViewIsShownThenRefused(t *testing.T) {
	t.Parallel()
	s, err := spec.Parse([]byte("spec = 1\ntitle = \"T\"\n[[section]]\nkind = \"view\"\nview = \"graph\"\n"), "x.toml", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in, inW := io.Pipe()
	out := &firstFrame{marker: "view graph", done: make(chan struct{})}
	errc := make(chan error, 1)
	go func() {
		errc <- panel.Run(context.Background(), s, panel.Options{Input: in, Output: out, Width: 100, Height: 20, CrashDir: t.TempDir(), Context: map[string]string{}})
	}()
	select {
	case <-out.done:
	case err := <-errc:
		t.Fatalf("ended without a screen: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("no screen")
	}
	if !strings.Contains(out.text(), "This panel cannot start") {
		t.Errorf("screen:\n%s", out.text())
	}
	_, _ = inW.Write([]byte("q"))
	if err := <-errc; !errors.Is(err, panel.ErrNoView) {
		t.Errorf("err = %v", err)
	}
}
