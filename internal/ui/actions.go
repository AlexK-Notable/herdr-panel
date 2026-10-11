package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/AlexK-Notable/herdr-panel/internal/run"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

type actionDoneMsg struct {
	id    string
	label string
	after spec.After
	res   run.Result
}

// startAction asks the action's confirm question when it has one, then runs.
func (m *Model) startAction(id string, a *spec.Action, row map[string]string) tea.Cmd {
	if m.busy[id] {
		m.setStatus(statusInfo, "%s is still running", a.Label)
		return nil
	}
	if !a.Confirm.IsZero() {
		q, err := a.Confirm.Expand(m.values(row))
		if err != nil {
			m.setStatus(statusErr, "%s: %v", a.Label, err)
			return nil
		}
		m.dialog = &dialog{
			kind: dialogConfirm, title: a.Label, text: q,
			buttons: []string{"Yes, " + lowerFirst(a.Label), "Cancel"},
			onChoose: func(i int) tea.Cmd {
				if i == 0 {
					return m.execAction(id, a, row)
				}
				return nil
			},
		}
		return nil
	}
	return m.execAction(id, a, row)
}

func (m *Model) execAction(id string, a *spec.Action, row map[string]string) tea.Cmd {
	argv, err := spec.ExpandArgv(a.Run.Argv, m.values(row), m.spec.Dir)
	if err != nil {
		m.setStatus(statusErr, "%s: %v", a.Label, err)
		return nil
	}
	m.busy[id] = true
	m.setStatus(statusInfo, "%s…", a.Label)
	req := run.Request{Argv: argv, Env: m.commandEnv(row), Dir: m.spec.Dir, Timeout: a.Run.Timeout}
	r, ctx := m.runner, m.ctx
	label, after := a.Label, a.After
	return m.safe(func() tea.Msg {
		return actionDoneMsg{id: id, label: label, after: after, res: r.Run(ctx, req)}
	})
}

func (m *Model) onActionDone(msg actionDoneMsg) tea.Cmd {
	delete(m.busy, msg.id)
	if !msg.res.OK {
		m.setStatus(statusErr, "✗ %s: %s", msg.label, msg.res.Message)
		return m.refreshAll() // a failed action may still have changed something
	}
	text := "✓ " + msg.label
	if msg.res.Message != "" {
		text += ": " + msg.res.Message
	}
	m.setStatus(statusOK, "%s", text)
	switch msg.after {
	case spec.AfterClose:
		return m.Quit()
	case spec.AfterNothing:
		return nil
	case spec.AfterRefresh:
	}
	return m.refreshAll()
}

// testSecret runs a secret field's test command. The command reads the
// secret itself; the panel never passes it.
func (m *Model) testSecret(f *spec.Field) tea.Cmd {
	if f.Test == nil {
		return nil
	}
	a := &spec.Action{Label: "Test " + f.Label, Run: f.Test, After: spec.AfterNothing}
	return m.startAction("test:"+f.ID, a, nil)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	return string(r)
}
