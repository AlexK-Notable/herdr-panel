package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AlexK-Notable/herdr-panel/spec"
)

// formatDuration shows whole seconds the way people read them: 45s, 2m,
// 1m30s, 1h, 1h5m.
func formatDuration(secs int64) string {
	if secs < 0 {
		return strconv.FormatInt(secs, 10) + "s"
	}
	d := time.Duration(secs) * time.Second
	h, m, s := int64(d.Hours()), int64(d.Minutes())%60, secs%60
	var b strings.Builder
	if h > 0 {
		fmt.Fprintf(&b, "%dh", h)
	}
	if m > 0 {
		fmt.Fprintf(&b, "%dm", m)
	}
	if s > 0 || b.Len() == 0 {
		fmt.Fprintf(&b, "%ds", s)
	}
	return b.String()
}

// parseDuration reads "90", "90s", "1m30s" or "2h" as whole seconds.
func parseDuration(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration (try 90, 90s or 1m30s)", s)
	}
	if d%time.Second != 0 {
		return 0, fmt.Errorf("%q is not a whole number of seconds", s)
	}
	return int64(d / time.Second), nil
}

// normalize checks an edited value and returns it as the store will hold it.
func normalize(f *spec.Field, input string) (string, error) {
	v := strings.TrimSpace(input)
	if f.Type == spec.Secret || f.Type == spec.Text {
		v = input // spaces may be meaningful
	}
	if v == "" {
		if f.Required {
			return "", fmt.Errorf("%s is required", f.Label)
		}
		return "", nil
	}
	switch f.Type {
	case spec.Int:
		if err := spec.CheckInt(f, v); err != nil {
			return "", err
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		return strconv.FormatInt(n, 10), nil
	case spec.Duration:
		n, err := parseDuration(v)
		if err != nil {
			return "", err
		}
		s := strconv.FormatInt(n, 10)
		if err := spec.CheckInt(f, s); err != nil {
			return "", fmt.Errorf("%s: %w", formatDuration(n), err)
		}
		return s, nil
	case spec.Text:
		if err := spec.CheckText(f, v); err != nil {
			return "", err
		}
	case spec.Secret:
		if f.MaxLen > 0 && len([]rune(v)) > f.MaxLen {
			return "", fmt.Errorf("longer than %d characters", f.MaxLen)
		}
	case spec.Bool, spec.Choice:
	}
	return v, nil
}

// display is how a stored value reads in the panel.
func display(f *spec.Field, v string) string {
	switch f.Type {
	case spec.Bool:
		if v == f.TrueValue {
			return "[x] on"
		}
		if v == f.FalseValue || v == "" {
			return "[ ] off"
		}
		return "[?] " + v
	case spec.Choice:
		for _, c := range f.Choices {
			if c.Value == v {
				return "‹ " + c.Label + " ›"
			}
		}
		if v == "" {
			return "‹ none ›"
		}
		return "‹ custom: " + v + " ›"
	case spec.Duration:
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return formatDuration(n)
		}
		return v
	case spec.Secret:
		if v == "" {
			return "unset"
		}
		return "set"
	case spec.Int, spec.Text:
	}
	return v
}
