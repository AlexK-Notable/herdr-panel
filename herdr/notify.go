package herdr

import "context"

// Notice is a notification for the user.
type Notice struct {
	Title string
	Body  string
	// Sound is "" for none, "done" or "request".
	Sound string
}

// Shown says whether herdr showed a notice, and why not when it did not.
type Shown struct {
	Shown  bool
	Reason string // one of the Reason constants
}

// Why herdr did not show a notice.
const (
	ReasonShown              = "shown"
	ReasonDisabled           = "disabled"     // the user turned notifications off
	ReasonRateLimited        = "rate_limited" // another one came too soon before
	ReasonNoForegroundClient = "no_foreground_client"
	ReasonBusy               = "busy"
)

// MethodNotify shows a notification.
const MethodNotify = "notification.show"

type notifyParams struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Sound string `json:"sound,omitempty"`
}

// Notify asks herdr to show a notice. A notice herdr declined is not an
// error: Shown says so, with the reason.
func (c *Client) Notify(ctx context.Context, n Notice) (Shown, error) {
	var res Shown
	var raw struct {
		Shown  bool   `json:"shown"`
		Reason string `json:"reason"`
	}
	err := c.Call(ctx, MethodNotify, notifyParams(n), &raw)
	res.Shown, res.Reason = raw.Shown, raw.Reason
	return res, err
}
