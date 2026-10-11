// The socket transport (Client.Call, APIError, ErrUnanswered, the dialer) is
// adapted from herdr-auto-title's internal/herdr package:
// https://github.com/AlexK-Notable/herdr-auto-title
// Copyright (c) 2026 Alexander Satretdinov, MIT License.

package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
)

// Client speaks herdr's socket API: one JSON request per connection, one
// JSON answer, then herdr closes it. A Client is safe for concurrent use and
// holds no connection between calls, so there is nothing to reconnect.
type Client struct {
	path   string
	prefix string
	seq    atomic.Uint64
}

// NewClient returns a client for the socket at path; inside herdr that is
// Context.SocketPath (HERDR_SOCKET_PATH). It does no I/O.
func NewClient(path string) *Client {
	return &Client{path: path, prefix: "herdr-panel"}
}

// FromContext returns a client for the socket herdr named, or an error
// saying the process does not run inside herdr.
func FromContext(c Context) (*Client, error) {
	if c.SocketPath == "" {
		return nil, ErrNoSocket
	}
	return NewClient(c.SocketPath), nil
}

// ErrNoSocket means HERDR_SOCKET_PATH is not set: the process was not
// started by herdr, or by a popup keybinding that does not pass it.
var ErrNoSocket = errors.New("not inside herdr: HERDR_SOCKET_PATH is not set")

// APIError is an error herdr answered with.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return fmt.Sprintf("herdr: %s: %s", e.Code, e.Message) }

// Error codes a panel reacts to.
const (
	// CodeUIBusy: a popup is already open. herdr shows one popup at a time.
	CodeUIBusy = "ui_busy"
	// CodeInvalidParams: herdr rejected the request's parameters.
	CodeInvalidParams = "invalid_params"
	// CodePluginNotFound and CodePluginPaneNotFound: no such plugin, or no
	// [[panes]] entrypoint of that name.
	CodePluginNotFound     = "plugin_not_found"
	CodePluginPaneNotFound = "plugin_pane_not_found"
)

// ErrorCode returns the herdr error code err carries, or "".
func ErrorCode(err error) string {
	if e, ok := errors.AsType[*APIError](err); ok {
		return e.Code
	}
	return ""
}

// ErrUnanswered marks a call that failed after its request was sent. herdr
// carries out a request it has read even when the caller hangs up, so the
// call may still have taken effect.
var ErrUnanswered = errors.New("no answer from herdr")

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type frame struct {
	Result json.RawMessage `json:"result"`
	Error  *APIError       `json:"error"`
}

type emptyParams struct{}

// Call sends one request and decodes the result into result, which may be
// nil. herdr requires params on every method; nil sends {}.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	if params == nil {
		params = emptyParams{}
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.path)
	if err != nil {
		return fmt.Errorf("connect to herdr at %s: %w", c.path, err)
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	req := request{ID: fmt.Sprintf("%s-%d", c.prefix, c.seq.Add(1)), Method: method, Params: params}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return unanswered(ctx, fmt.Errorf("send %s: %w", method, err))
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return unanswered(ctx, fmt.Errorf("read the %s answer: %w", method, err))
	}
	var f frame
	if err := json.Unmarshal(line, &f); err != nil {
		return unanswered(ctx, fmt.Errorf("decode the %s answer: %w", method, err))
	}
	if f.Error != nil {
		return fmt.Errorf("%s: %w", method, f.Error)
	}
	if result != nil && len(f.Result) > 0 {
		if err := json.Unmarshal(f.Result, result); err != nil {
			return fmt.Errorf("decode the %s result: %w", method, err)
		}
	}
	return nil
}

func unanswered(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = ctxErr
	}
	return fmt.Errorf("%w: %w", ErrUnanswered, err)
}

// Server is what herdr answers a ping with.
type Server struct {
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

// Ping checks that herdr answers on the socket, and reports its version and
// protocol number.
func (c *Client) Ping(ctx context.Context) (Server, error) {
	var s Server
	err := c.Call(ctx, "ping", nil, &s)
	return s, err
}
