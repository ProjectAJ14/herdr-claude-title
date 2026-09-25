package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
)

const socketPathEnv = "HERDR_SOCKET_PATH"

// Client is the seam between the plugin and Herdr; tests swap in a fake.
type Client interface {
	// Call sends one request and decodes the result into result (may be nil).
	Call(ctx context.Context, method string, params, result any) error
	// ServerIdentity names the server bound to the socket, "" while none is.
	// It changes when another server binds the path.
	ServerIdentity() string
}

// SocketClient speaks to the socket. Herdr closes a connection once it has
// answered, so every call dials its own and nothing ever reconnects.
type SocketClient struct {
	path string
	seq  atomic.Uint64
}

// NewSocketClient builds a client for HERDR_SOCKET_PATH. It does no I/O.
func NewSocketClient() (*SocketClient, error) {
	path := os.Getenv(socketPathEnv)
	if path == "" {
		return nil, fmt.Errorf("%s is not set: the plugin must be started by Herdr", socketPathEnv)
	}

	return &SocketClient{path: path}, nil
}

// SocketPath is the socket dialled, which is what names a Herdr session.
func (c *SocketClient) SocketPath() string { return c.path }

// ServerIdentity reads the socket's identity without making a request.
func (c *SocketClient) ServerIdentity() string { return serverIdentity(c.path) }

func (c *SocketClient) Call(ctx context.Context, method string, params, result any) error {
	if params == nil {
		params = emptyParams{} // Herdr requires params on every request
	}

	conn, err := dial(ctx, c.path)
	if err != nil {
		return fmt.Errorf("connect to herdr socket %s: %w", c.path, err)
	}
	defer conn.Close()

	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	req := request{ID: fmt.Sprintf("claude-title-%d", c.seq.Add(1)), Method: method, Params: params}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return unanswered(ctx, fmt.Errorf("send %s: %w", method, err))
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return unanswered(ctx, fmt.Errorf("read %s: %w", method, err))
	}

	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return unanswered(ctx, fmt.Errorf("decode %s: %w", method, err))
	}

	if resp.Error != nil {
		return fmt.Errorf("%s: %w", method, resp.Error)
	}

	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}

	return nil
}

// unanswered marks a failure after the request left, reporting the
// cancellation instead when that is what closed the connection.
func unanswered(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = ctxErr
	}

	return fmt.Errorf("%w: %w", ErrUnanswered, err)
}

// ReadSnapshot fetches the whole session in one request. There is no event
// subscription beside it on purpose: see internal/poller/CLAUDE.md.
func ReadSnapshot(ctx context.Context, c Client) (Snapshot, error) {
	var res struct {
		Snapshot Snapshot `json:"snapshot"`
	}

	err := c.Call(ctx, methodSessionSnapshot, nil, &res)

	return res.Snapshot, err
}

// ForegroundProcesses reads what runs in a pane; the snapshot has no process.
func ForegroundProcesses(ctx context.Context, c Client, paneID string) ([]Process, error) {
	var res struct {
		ProcessInfo struct {
			Foreground []Process `json:"foreground_processes"`
		} `json:"process_info"`
	}

	err := c.Call(ctx, methodPaneProcessInfo, paneTarget{PaneID: paneID}, &res)

	return res.ProcessInfo.Foreground, err
}

func RenameTab(ctx context.Context, c Client, tabID, label string) error {
	return c.Call(ctx, methodTabRename, tabRename{TabID: tabID, Label: label}, nil)
}

// RenamePane labels a pane for the goto panel. An empty label clears it.
func RenamePane(ctx context.Context, c Client, paneID, label string) error {
	return c.Call(ctx, methodPaneRename, paneRename{PaneID: paneID, Label: label}, nil)
}

func RenameWorkspace(ctx context.Context, c Client, workspaceID, label string) error {
	return c.Call(ctx, methodWorkspaceRename, workspaceRename{WorkspaceID: workspaceID, Label: label}, nil)
}

// Notify shows a toast. Not being shown is an answer, not an error.
func Notify(ctx context.Context, c Client, title, body string) (NotifyResult, error) {
	var res NotifyResult
	err := c.Call(ctx, methodNotify, notification{Title: title, Body: body}, &res)

	return res, err
}
