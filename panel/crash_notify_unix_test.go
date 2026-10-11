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
