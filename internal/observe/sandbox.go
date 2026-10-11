package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AlexK-Notable/herdr-panel/internal/run"
)

// Stub is a canned answer for commands whose argv starts with Match. The
// first element matches by base name when it has no slash: "tool" matches
// "/path/to/tool".
type Stub struct {
	Match  []string `json:"match"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr"`
	Exit   int      `json:"exit"`
}

// Stubs is the --stub file: answers by match, and one for everything else.
type Stubs struct {
	Commands []Stub `json:"commands"`
	Default  *Stub  `json:"default"`
}

// LoadStubs reads a --stub JSON file.
func LoadStubs(path string) (*Stubs, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the user's own --stub file
	if err != nil {
		return nil, err
	}
	var s Stubs
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// Call is a command the sandbox was asked to run.
type Call struct {
	Argv []string `json:"argv"`
	Env  []string `json:"env,omitempty"`
}

// Sandbox runs nothing: it records each command and answers from stubs, or
// with success and no output.
type Sandbox struct {
	Stubs *Stubs
	mu    sync.Mutex
	calls []Call
}

// Run implements run.Runner.
func (s *Sandbox) Run(_ context.Context, req run.Request) run.Result {
	s.mu.Lock()
	s.calls = append(s.calls, Call{Argv: append([]string(nil), req.Argv...), Env: panelEnv(req.Env)})
	s.mu.Unlock()
	st := s.match(req.Argv)
	res := run.Result{Stdout: []byte(st.Stdout), Stderr: []byte(st.Stderr), ExitCode: st.Exit}
	if st.Exit != 0 {
		res.Err = fmt.Errorf("exit status %d", st.Exit)
	}
	run.Interpret(&res)
	return res
}

// Calls returns the commands asked for so far.
func (s *Sandbox) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

func (s *Sandbox) match(argv []string) Stub {
	if s.Stubs != nil {
		for _, st := range s.Stubs.Commands {
			if matches(st.Match, argv) {
				return st
			}
		}
		if s.Stubs.Default != nil {
			return *s.Stubs.Default
		}
	}
	return Stub{}
}

func matches(pattern, argv []string) bool {
	if len(pattern) == 0 || len(pattern) > len(argv) {
		return false
	}
	for i, p := range pattern {
		a := argv[i]
		if i == 0 && !strings.ContainsRune(p, '/') {
			a = filepath.Base(a)
		}
		if p != a {
			return false
		}
	}
	return true
}

// panelEnv keeps only the variables the panel adds, for a readable record.
func panelEnv(env []string) []string {
	var out []string
	for _, e := range env {
		if strings.HasPrefix(e, "PANEL_") {
			out = append(out, e)
		}
	}
	return out
}
