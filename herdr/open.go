package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Placement is where plugin.pane.open puts a pane.
type Placement string

// The placements herdr knows. Size, workspace, target pane and direction
// each apply only to some of them; herdr answers invalid_params otherwise.
const (
	Overlay Placement = "overlay"
	Popup   Placement = "popup"
	Split   Placement = "split"
	Tab     Placement = "tab"
	Zoomed  Placement = "zoomed"
)

// Size is a popup dimension: Cells, or Percent of the terminal. The zero
// value lets herdr choose (half the screen).
type Size struct {
	Cells   int
	Percent int
}

// MarshalJSON writes what herdr reads: a number of cells, or "N%".
func (s Size) MarshalJSON() ([]byte, error) {
	if s.Percent > 0 {
		return json.Marshal(fmt.Sprintf("%d%%", s.Percent))
	}
	return json.Marshal(s.Cells)
}

func (s Size) isZero() bool { return s.Cells == 0 && s.Percent == 0 }

// OpenOptions name a plugin's [[panes]] entrypoint and how to show it.
type OpenOptions struct {
	PluginID   string // as in herdr-plugin.toml
	Entrypoint string // the [[panes]] id
	// Placement overrides the manifest's; "" keeps it.
	Placement Placement
	// Width and Height apply to a popup only. A size given here wins over
	// the manifest's.
	Width, Height Size
	// WorkspaceID, TargetPaneID and Direction ("right", "down") place a
	// split or a tab; overlay and popup always open over the active pane.
	WorkspaceID, TargetPaneID, Direction string
	Cwd                                  string
	Focus                                bool
	Env                                  map[string]string
	// BusyNotice, when set, is shown as a notification if herdr answers
	// ui_busy, so the user learns why nothing opened.
	BusyNotice string
}

// Opened is the pane plugin.pane.open created. For a popup it is empty:
// herdr 0.9.2 answers a popup with a bare "ok", because a popup has no
// public pane id. It is not in pane.list, cannot be read or closed by id, and
// closes when its process exits or the user closes it. The other placements
// answer with the pane.
type Opened struct {
	PluginID, Entrypoint string
	PaneID, WorkspaceID  string
	TabID                string
}

// ErrBusy means another popup is open; herdr shows one at a time. Open's
// error wraps it, and an *APIError with CodeUIBusy.
var ErrBusy = errors.New("another popup is open")

type openParams struct {
	PluginID     string            `json:"plugin_id"`
	Entrypoint   string            `json:"entrypoint"`
	Placement    Placement         `json:"placement,omitempty"`
	Width        *Size             `json:"width,omitempty"`
	Height       *Size             `json:"height,omitempty"`
	WorkspaceID  string            `json:"workspace_id,omitempty"`
	TargetPaneID string            `json:"target_pane_id,omitempty"`
	Direction    string            `json:"direction,omitempty"`
	Cwd          string            `json:"cwd,omitempty"`
	Focus        bool              `json:"focus"`
	Env          map[string]string `json:"env,omitempty"`
}

type openResult struct {
	PluginPane struct {
		PluginID   string `json:"plugin_id"`
		Entrypoint string `json:"entrypoint"`
		Pane       struct {
			PaneID      string `json:"pane_id"`
			WorkspaceID string `json:"workspace_id"`
			TabID       string `json:"tab_id"`
		} `json:"pane"`
	} `json:"plugin_pane"`
}

// MethodPaneOpen opens a plugin's pane.
const MethodPaneOpen = "plugin.pane.open"

// Open opens a plugin's pane through plugin.pane.open. This is the only way
// to choose a popup's size and environment at run time: a [[panes]] entry
// and a keybinding popup have fixed ones. For a popup the returned Opened is
// empty; see Opened.
func (c *Client) Open(ctx context.Context, o OpenOptions) (Opened, error) {
	p := openParams{
		PluginID: o.PluginID, Entrypoint: o.Entrypoint, Placement: o.Placement,
		WorkspaceID: o.WorkspaceID, TargetPaneID: o.TargetPaneID, Direction: o.Direction,
		Cwd: o.Cwd, Focus: o.Focus, Env: o.Env,
	}
	if !o.Width.isZero() {
		p.Width = &o.Width
	}
	if !o.Height.isZero() {
		p.Height = &o.Height
	}
	var res openResult
	if err := c.Call(ctx, MethodPaneOpen, p, &res); err != nil {
		if ErrorCode(err) == CodeUIBusy {
			if o.BusyNotice != "" {
				_, _ = c.Notify(ctx, Notice{Title: o.BusyNotice, Body: "Close the open popup first."})
			}
			return Opened{}, fmt.Errorf("%w: %w", ErrBusy, err)
		}
		return Opened{}, err
	}
	pp := res.PluginPane
	return Opened{
		PluginID: pp.PluginID, Entrypoint: pp.Entrypoint,
		PaneID: pp.Pane.PaneID, WorkspaceID: pp.Pane.WorkspaceID, TabID: pp.Pane.TabID,
	}, nil
}

// CodePopupNotOpen: popup.close found no popup to close.
const CodePopupNotOpen = "popup_not_open"

// ClosePopup closes the open popup, whichever it is, as the user's close
// would: herdr ends its process with SIGHUP, then SIGTERM, then SIGKILL. It
// is the only way to close a popup from outside, since a popup has no pane
// id. With no popup open the error carries CodePopupNotOpen.
func (c *Client) ClosePopup(ctx context.Context) error {
	return c.Call(ctx, "popup.close", nil, nil)
}
