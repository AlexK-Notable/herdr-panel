// Slice S: a real Bubble Tea v2 + Lip Gloss compositor panel, run as a herdr
// popup, that records what the library's design depends on:
//
//   - which keyboard protocol herdr speaks to a popup (a KeyboardEnhancementsMsg
//     arrives only if herdr answers Bubble Tea's kitty query);
//   - every key, mouse and paste event as Bubble Tea decoded it;
//   - the time to the first View and the first WindowSizeMsg;
//   - which signals arrive when herdr closes the popup, and which children
//     survive (one in the panel's session, one started with Setsid);
//   - what a panic looks like with Bubble Tea's own recovery, and with a hold
//     screen after Run returns (SLICE_HOLD=1).
//
// Records are appended as JSON lines to SLICE_LOG (default
// spike/out/slice.jsonl, an absolute path because the installed checkout lives
// under herdr's plugin directory).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/sys/unix"
)

var (
	start   = time.Now()
	logPath = envOr("SLICE_LOG", "/home/user/repos/herdr-panel/spike/out/slice.jsonl")
	logMu   sync.Mutex
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func record(kind string, fields map[string]any) {
	logMu.Lock()
	defer logMu.Unlock()
	if fields == nil {
		fields = map[string]any{}
	}
	fields["kind"] = kind
	fields["pid"] = os.Getpid()
	fields["ms"] = time.Since(start).Milliseconds()
	fields["t"] = float64(time.Now().UnixMilli()) / 1000
	fields["via"] = os.Getenv("SLICE_VIA")
	line, _ := json.Marshal(fields)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// spawnChildren starts two sleepers: one stays in the panel's session (herdr's
// close should kill it), one gets its own session (it should survive).
func spawnChildren() map[string]any {
	out := map[string]any{}
	plain := exec.Command("sleep", "120")
	if err := plain.Start(); err == nil {
		out["plain_pid"] = plain.Process.Pid
		go func() { _ = plain.Wait() }()
	} else {
		out["plain_err"] = err.Error()
	}
	detached := exec.Command("sleep", "120")
	detached.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := detached.Start(); err == nil {
		out["setsid_pid"] = detached.Process.Pid
		go func() { _ = detached.Wait() }()
	} else {
		out["setsid_err"] = err.Error()
	}
	return out
}

type cmdPanic struct{}

type model struct {
	on        bool
	w, h      int
	comp      *lipgloss.Compositor
	viewCount int
	note      string
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "space", "t":
			m.on = !m.on
		case "p":
			panic("slice: deliberate panic in Update")
		case "P":
			return m, func() tea.Msg { panic("slice: deliberate panic in a Cmd") }
		}
		m.note = "key " + msg.String()
	case tea.MouseClickMsg:
		id := ""
		if m.comp != nil {
			if hit := m.comp.Hit(msg.X, msg.Y); !hit.Empty() {
				id = hit.ID()
			}
		}
		record("hit", map[string]any{"x": msg.X, "y": msg.Y, "id": id})
		m.note = fmt.Sprintf("click %d,%d -> %q", msg.X, msg.Y, id)
		switch id {
		case "toggle":
			m.on = !m.on
		case "panic":
			panic("slice: deliberate panic from a click")
		case "quit":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *model) View() tea.View {
	m.viewCount++
	if m.viewCount == 1 {
		record("first_view", nil)
	}
	check := "[ ]"
	if m.on {
		check = "[x]"
	}
	w, h := max(m.w, 1), max(m.h, 1)
	base := lipgloss.NewLayer(lipgloss.NewStyle().Width(w).Height(h).Render(
		fmt.Sprintf("SLICE %dx%d", m.w, m.h))).ID("base")
	toggle := lipgloss.NewLayer(check + " toggle (t)").ID("toggle").X(2).Y(2).Z(10)
	pan := lipgloss.NewLayer("[ panic (p) ]").ID("panic").X(2).Y(4).Z(10)
	quit := lipgloss.NewLayer("[ quit (q) ]").ID("quit").X(2).Y(6).Z(10)
	note := lipgloss.NewLayer(m.note).ID("note").X(2).Y(8).Z(10)
	m.comp = lipgloss.NewCompositor(base.AddLayers(toggle, pan, quit, note))
	v := tea.NewView(m.comp.Render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// describe turns a message into a loggable record, or "" to skip it.
func describe(msg tea.Msg) (string, map[string]any) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return "key", map[string]any{"str": msg.String(), "code": int(msg.Code),
			"text": msg.Text, "mod": int(msg.Mod), "repeat": msg.IsRepeat}
	case tea.KeyReleaseMsg:
		return "key_release", map[string]any{"str": msg.String()}
	case tea.MouseClickMsg:
		return "mouse_click", map[string]any{"x": msg.X, "y": msg.Y, "button": int(msg.Button)}
	case tea.MouseReleaseMsg:
		return "mouse_release", map[string]any{"x": msg.X, "y": msg.Y}
	case tea.MouseWheelMsg:
		return "mouse_wheel", map[string]any{"x": msg.X, "y": msg.Y, "button": int(msg.Button)}
	case tea.PasteMsg:
		return "paste", map[string]any{"len": len(msg.Content), "content": msg.Content}
	case tea.WindowSizeMsg:
		return "size", map[string]any{"w": msg.Width, "h": msg.Height}
	case tea.KeyboardEnhancementsMsg:
		return "keyboard_enhancements", map[string]any{"flags": int(msg.Flags),
			"disambiguation": msg.SupportsKeyDisambiguation(), "event_types": msg.SupportsEventTypes()}
	case tea.BackgroundColorMsg:
		return "background", map[string]any{"color": msg.String(), "dark": msg.IsDark()}
	case tea.MouseMotionMsg, cmdPanic:
		return "", nil
	}
	return "msg", map[string]any{"type": fmt.Sprintf("%T", msg)}
}

func hold(reason string) {
	// The popup closes the moment this process exits, taking any message with
	// it. Holding until a key (or a timeout) is what makes a crash readable.
	fmt.Print("\x1b[?1049h\x1b[2J\x1b[H")
	fmt.Printf("herdr-panel slice crashed:\r\n  %s\r\n\r\npress any key to close\r\n", reason)
	if raw, err := makeRaw(); err == nil {
		defer raw()
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 16)
		_, _ = os.Stdin.Read(buf)
		close(done)
	}()
	select {
	case <-done:
		record("hold_end", map[string]any{"how": "key"})
	case <-time.After(20 * time.Second):
		record("hold_end", map[string]any{"how": "timeout"})
	}
}

func main() {
	children := spawnChildren()
	sid, _ := unix.Getsid(0)
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HERDR_") || strings.HasPrefix(kv, "SLICE_") || strings.HasPrefix(kv, "TERM") {
			k, v, _ := strings.Cut(kv, "=")
			if k == "HERDR_PLUGIN_CONTEXT_JSON" {
				v = fmt.Sprintf("<%d bytes>", len(v))
			}
			env[k] = v
		}
	}
	record("start", map[string]any{"sid": sid, "pgid": syscall.Getpgrp(), "children": children, "env": env})

	// Bubble Tea handles SIGINT and SIGTERM only; herdr's close starts with
	// SIGHUP. This handler records each signal and leaves a marker file.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	var p *tea.Program
	go func() {
		for s := range sigs {
			record("signal", map[string]any{"sig": s.String()})
			_ = os.WriteFile(logPath+".marker", []byte(s.String()+"\n"), 0o644)
			if p != nil {
				p.Quit()
			}
		}
	}()

	m := &model{}
	p = tea.NewProgram(m, tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
		if kind, fields := describe(msg); kind != "" {
			record(kind, fields)
		}
		return msg
	}))
	_, err := p.Run()
	record("end", map[string]any{"err": fmt.Sprint(err), "panic": errors.Is(err, tea.ErrProgramPanic)})
	if errors.Is(err, tea.ErrProgramPanic) && os.Getenv("SLICE_HOLD") == "1" {
		hold(err.Error())
	}
}
