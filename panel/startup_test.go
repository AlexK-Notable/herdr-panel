package panel_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlexK-Notable/herdr-panel/panel"
)

// firstFrame is an io.Writer that notes when the panel's first full frame
// (one showing a marker string) has been written.
type firstFrame struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	marker string
	done   chan struct{}
	once   sync.Once
}

func (f *firstFrame) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buf.Write(p)
	if strings.Contains(f.buf.String(), f.marker) {
		f.once.Do(func() { close(f.done) })
	}
	return len(p), nil
}

// autoTitleCopy writes the auto-title example into dir with its store
// moved into dir, so the real config is neither read nor swept.
func autoTitleCopy(tb testing.TB, dir string) string {
	tb.Helper()
	data, err := os.ReadFile("../examples/auto-title.toml")
	if err != nil {
		tb.Fatal(err)
	}
	src := strings.Replace(string(data), `path = "{env.XDG_CONFIG_HOME:-~/.config}/herdr-auto-title/config.env"`, `path = "config.env"`, 1)
	if src == string(data) {
		tb.Fatal("the example's store path changed; update autoTitleCopy")
	}
	p := filepath.Join(dir, "auto-title.toml")
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		tb.Fatal(err)
	}
	return p
}

// startup runs the spec at path in a real Bubble Tea program writing to a
// buffer, and returns how long the first full frame took: loading and
// validating the spec, building the panel, reading the store, drawing.
func startup(tb testing.TB, path, crash string) time.Duration {
	tb.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &firstFrame{marker: "Longest title", done: make(chan struct{})}
	in, inW := io.Pipe()
	defer func() { _ = inW.Close() }()
	start := time.Now()
	errc := make(chan error, 1)
	go func() {
		errc <- panel.RunFile(ctx, path, panel.Options{
			Input: in, Output: out, Width: 100, Height: 40, CrashDir: crash, Context: map[string]string{},
		})
	}()
	var took time.Duration
	select {
	case <-out.done:
		took = time.Since(start)
	case err := <-errc:
		tb.Fatalf("the panel ended before drawing: %v", err)
	case <-time.After(5 * time.Second):
		tb.Fatal("no frame within 5 s")
	}
	cancel()
	if err := <-errc; err != nil && !errors.Is(err, context.Canceled) {
		tb.Fatalf("run: %v", err)
	}
	return took
}

// The plan's budget: the first frame within 50 ms on this host. The median
// of several runs is compared, so one slow scheduling hiccup does not fail
// it. The race detector slows everything several times over; under it the
// budget is not checked.
func TestFirstFrameBudget(t *testing.T) { //nolint:paralleltest // timing
	dir := t.TempDir()
	path := autoTitleCopy(t, dir)
	var runs []time.Duration
	for range 7 {
		runs = append(runs, startup(t, path, dir))
	}
	slices.Sort(runs)
	median := runs[len(runs)/2]
	t.Logf("first frame: median %s, runs %v", median, runs)
	if !raceEnabled && median > 50*time.Millisecond {
		t.Errorf("first frame took %s (median of %d), budget 50 ms", median, len(runs))
	}
}

func BenchmarkFirstFrame(b *testing.B) {
	dir := b.TempDir()
	path := autoTitleCopy(b, dir)
	var total time.Duration
	for b.Loop() {
		total += startup(b, path, dir)
	}
	b.ReportMetric(float64(total.Microseconds())/float64(b.N)/1000, "ms/frame")
}
