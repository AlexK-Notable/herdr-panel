// The test server is adapted from herdr-auto-title's internal/herdr tests:
// https://github.com/AlexK-Notable/herdr-auto-title
// Copyright (c) 2026 Alexander Satretdinov, MIT License.

package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type incoming struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type server struct {
	path  string
	mu    sync.Mutex
	reqs  []incoming
	reply func(incoming) string // "" hangs up without answering
}

// newServer listens on a Unix socket. t.TempDir() is too long for a
// socket's sun_path (104 bytes on macOS), so the directory is made short.
func newServer(t *testing.T, reply func(incoming) string) *server {
	t.Helper()
	dir, err := os.MkdirTemp("", "hp") //nolint:usetesting // t.TempDir() overruns sun_path
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	s := &server{path: filepath.Join(dir, "h.sock"), reply: reply}
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *server) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	var req incoming
	if json.Unmarshal(line, &req) != nil {
		return
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()
	if out := s.reply(req); out != "" {
		_, _ = conn.Write([]byte(out + "\n"))
	}
}

func (s *server) last(t *testing.T) incoming {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		t.Fatal("no request reached the server")
	}
	return s.reqs[len(s.reqs)-1]
}

func ok(result string) func(incoming) string {
	return func(r incoming) string { return `{"id":"` + r.ID + `","result":` + result + `}` }
}

func TestPing(t *testing.T) {
	t.Parallel()
	s := newServer(t, ok(`{"type":"pong","version":"0.9.2","protocol":21}`))
	got, err := NewClient(s.path).Ping(context.Background())
	if err != nil || got.Version != "0.9.2" || got.Protocol != 21 {
		t.Fatalf("ping = %+v, %v", got, err)
	}
	if r := s.last(t); r.Method != "ping" || string(r.Params) != "{}" || !strings.HasPrefix(r.ID, "herdr-panel-") {
		t.Errorf("request = %+v", r)
	}
}

func TestOpenSendsWhatHerdrReads(t *testing.T) {
	t.Parallel()
	s := newServer(t, ok(`{"type":"plugin_pane_opened","plugin_pane":{"plugin_id":"p","entrypoint":"settings","pane":{"pane_id":"w1:p9","terminal_id":"t","workspace_id":"w1","tab_id":"w1:t1","focused":true}}}`))
	got, err := NewClient(s.path).Open(context.Background(), OpenOptions{
		PluginID: "p", Entrypoint: "settings", Placement: Popup,
		Width: Size{Percent: 70}, Height: Size{Cells: 20}, Focus: true,
		Env: map[string]string{"PANEL_VIA": "socket"},
	})
	if err != nil || got.PaneID != "w1:p9" || got.TabID != "w1:t1" || got.Entrypoint != "settings" {
		t.Fatalf("open = %+v, %v", got, err)
	}
	var p map[string]any
	if err := json.Unmarshal(s.last(t).Params, &p); err != nil {
		t.Fatal(err)
	}
	if p["width"] != "70%" || p["height"] != float64(20) || p["placement"] != "popup" || p["focus"] != true {
		t.Errorf("params = %v", p)
	}
	if _, has := p["workspace_id"]; has {
		t.Errorf("an empty workspace_id was sent; herdr rejects it for a popup: %v", p)
	}
}

// herdr answers a popup with a bare "ok" and no pane (verified against
// 0.9.2 in the integration test): Open must not fail on it.
func TestOpenPopupAnswersOK(t *testing.T) {
	t.Parallel()
	s := newServer(t, ok(`{"type":"ok"}`))
	got, err := NewClient(s.path).Open(context.Background(), OpenOptions{PluginID: "p", Entrypoint: "e", Placement: Popup})
	if err != nil || got != (Opened{}) {
		t.Errorf("open = %+v, %v", got, err)
	}
}

func TestClosePopup(t *testing.T) {
	t.Parallel()
	s := newServer(t, func(r incoming) string {
		return `{"id":"` + r.ID + `","error":{"code":"popup_not_open","message":"no popup is open"}}`
	})
	err := NewClient(s.path).ClosePopup(context.Background())
	if ErrorCode(err) != CodePopupNotOpen || s.last(t).Method != "popup.close" || string(s.last(t).Params) != "{}" {
		t.Errorf("err = %v, request %+v", err, s.last(t))
	}
}

func TestOpenBusyNotifies(t *testing.T) {
	t.Parallel()
	s := newServer(t, func(r incoming) string {
		if r.Method == MethodPaneOpen {
			return `{"id":"` + r.ID + `","error":{"code":"ui_busy","message":"a popup pane is already open"}}`
		}
		return `{"id":"` + r.ID + `","result":{"type":"notification_show","shown":true,"reason":"shown"}}`
	})
	_, err := NewClient(s.path).Open(context.Background(), OpenOptions{PluginID: "p", Entrypoint: "e", BusyNotice: "Settings could not open"})
	if !errors.Is(err, ErrBusy) || ErrorCode(err) != CodeUIBusy {
		t.Fatalf("err = %v", err)
	}
	if r := s.last(t); r.Method != MethodNotify || !strings.Contains(string(r.Params), "Settings could not open") {
		t.Errorf("no notification after ui_busy: %+v", r)
	}
}

func TestNotifyDeclinedIsNotAnError(t *testing.T) {
	t.Parallel()
	s := newServer(t, ok(`{"type":"notification_show","shown":false,"reason":"rate_limited"}`))
	got, err := NewClient(s.path).Notify(context.Background(), Notice{Title: "t"})
	if err != nil || got.Shown || got.Reason != ReasonRateLimited {
		t.Errorf("notify = %+v, %v", got, err)
	}
	var p map[string]any
	_ = json.Unmarshal(s.last(t).Params, &p)
	if _, has := p["sound"]; has {
		t.Errorf("an empty sound was sent: %v", p)
	}
}

func TestHangupIsUnanswered(t *testing.T) {
	t.Parallel()
	s := newServer(t, func(incoming) string { return "" })
	err := NewClient(s.path).Call(context.Background(), "ping", nil, nil)
	if !errors.Is(err, ErrUnanswered) {
		t.Errorf("err = %v", err)
	}
}

func TestCancelUnblocksACall(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	s := newServer(t, func(incoming) string { <-block; return "" })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := NewClient(s.path).Call(ctx, "ping", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrUnanswered) || time.Since(start) > time.Second {
		t.Errorf("err = %v after %s", err, time.Since(start))
	}
}

func TestNoSocket(t *testing.T) {
	t.Parallel()
	if _, err := FromContext(Context{}); !errors.Is(err, ErrNoSocket) {
		t.Errorf("err = %v", err)
	}
	err := NewClient("/nonexistent/h.sock").Call(context.Background(), "ping", nil, nil)
	if err == nil || errors.Is(err, ErrUnanswered) {
		t.Errorf("a failed dial must not be 'unanswered': %v", err)
	}
}
