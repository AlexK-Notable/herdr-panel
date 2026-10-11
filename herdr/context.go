// Package herdr is herdr-panel's adapter to herdr: it reads the context a
// popup is opened with, talks to herdr's socket, opens panels and sends
// notifications. Nothing else in herdr-panel depends on herdr; a panel runs in
// any terminal.
package herdr

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Kind says how the current process was started.
type Kind string

// The kinds of context.
const (
	// PluginPopup is a pane a plugin opened ([[panes]] or plugin.pane.open):
	// it has the plugin's directories and HERDR_PLUGIN_CONTEXT_JSON.
	PluginPopup Kind = "plugin"
	// KeybindingPopup is a [[keys.command]] type = "popup": it has only the
	// HERDR_ACTIVE_* ids, and no plugin directories.
	KeybindingPopup Kind = "keybinding"
	// Outside means not inside herdr at all.
	Outside Kind = "none"
)

// Context is what herdr told the process about where it runs.
type Context struct {
	Kind Kind

	PluginID   string
	PluginRoot string
	ConfigDir  string
	StateDir   string // the plugin's, or herdr-panel's own when there is none
	SocketPath string
	BinPath    string

	WorkspaceID    string
	WorkspaceLabel string
	TabID          string
	TabLabel       string
	PaneID         string // the pane the user was in, not the popup
	Cwd            string
}

// pluginContext is HERDR_PLUGIN_CONTEXT_JSON, as herdr 0.9.2 writes it.
type pluginContext struct {
	WorkspaceID    string `json:"workspace_id"`
	WorkspaceLabel string `json:"workspace_label"`
	WorkspaceCwd   string `json:"workspace_cwd"`
	TabID          string `json:"tab_id"`
	TabLabel       string `json:"tab_label"`
	FocusedPaneID  string `json:"focused_pane_id"`
	FocusedPaneCwd string `json:"focused_pane_cwd"`
}

// FromEnv reads the context from the environment; getenv is os.Getenv in
// production.
func FromEnv(getenv func(string) string) Context {
	c := Context{
		Kind:       Outside,
		PluginID:   getenv("HERDR_PLUGIN_ID"),
		PluginRoot: getenv("HERDR_PLUGIN_ROOT"),
		ConfigDir:  getenv("HERDR_PLUGIN_CONFIG_DIR"),
		StateDir:   getenv("HERDR_PLUGIN_STATE_DIR"),
		SocketPath: getenv("HERDR_SOCKET_PATH"),
		BinPath:    getenv("HERDR_BIN_PATH"),
	}
	switch {
	case c.PluginID != "":
		c.Kind = PluginPopup
		var pc pluginContext
		if raw := getenv("HERDR_PLUGIN_CONTEXT_JSON"); raw != "" && json.Unmarshal([]byte(raw), &pc) == nil {
			c.WorkspaceID, c.WorkspaceLabel = pc.WorkspaceID, pc.WorkspaceLabel
			c.TabID, c.TabLabel = pc.TabID, pc.TabLabel
			c.PaneID, c.Cwd = pc.FocusedPaneID, pc.FocusedPaneCwd
			if c.Cwd == "" {
				c.Cwd = pc.WorkspaceCwd
			}
		}
	case getenv("HERDR_ACTIVE_PANE_ID") != "" || getenv("HERDR_ACTIVE_WORKSPACE_ID") != "":
		c.Kind = KeybindingPopup
		c.WorkspaceID = getenv("HERDR_ACTIVE_WORKSPACE_ID")
		c.TabID = getenv("HERDR_ACTIVE_TAB_ID")
		c.PaneID = getenv("HERDR_ACTIVE_PANE_ID")
		c.Cwd = getenv("HERDR_ACTIVE_PANE_CWD")
	}
	if c.StateDir == "" {
		c.StateDir = fallbackStateDir(getenv)
	}
	return c
}

// fallbackStateDir is herdr-panel's own state directory, for popups that
// have no plugin one: $XDG_STATE_HOME/herdr-panel, or ~/.local/state/herdr-panel.
func fallbackStateDir(getenv func(string) string) string {
	if d := getenv("XDG_STATE_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "herdr-panel")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", "herdr-panel")
	}
	return filepath.Join(os.TempDir(), "herdr-panel")
}

// Values are the {ctx.*} placeholder values. A name with no value is left
// out, so a spec that needs it fails clearly instead of using "".
func (c Context) Values() map[string]string {
	m := map[string]string{}
	set := func(k, v string) {
		if v != "" {
			m[k] = v
		}
	}
	set("workspace_id", c.WorkspaceID)
	set("tab_id", c.TabID)
	set("pane_id", c.PaneID)
	set("cwd", c.Cwd)
	set("plugin_id", c.PluginID)
	set("config_dir", c.ConfigDir)
	set("state_dir", c.StateDir)
	return m
}
