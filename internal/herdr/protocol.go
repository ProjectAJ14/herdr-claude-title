// Package herdr is a client for the Herdr local socket API: NDJSON over the
// socket HERDR_SOCKET_PATH names (a named pipe on Windows), one request per
// connection. Verified against Herdr 0.8.2+, protocol 20.
package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
)

// The methods this plugin calls, and no others.
const (
	methodSessionSnapshot = "session.snapshot"
	methodPaneProcessInfo = "pane.process_info"
	methodTabRename       = "tab.rename"
	methodPaneRename      = "pane.rename"
	methodWorkspaceRename = "workspace.rename"
	methodNotify          = "notification.show"
)

// Error codes the plugin reacts to: the target closed between the snapshot
// that listed it and the call that followed.
const (
	CodeTabNotFound       = "tab_not_found"
	CodePaneNotFound      = "pane_not_found"
	CodeWorkspaceNotFound = "workspace_not_found"
)

// Agent statuses that make a pane worth naming its tab after.
const (
	AgentWorking = "working"
	AgentBlocked = "blocked"
)

// ErrUnanswered marks a call that failed after its request was sent. Herdr
// carries out a request it has read even once the caller hangs up, so the
// call may still take effect seconds later.
var ErrUnanswered = errors.New("no answer from herdr")

// APIError is an error frame Herdr answered with.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("herdr %s: %s", e.Code, e.Message)
}

// ErrorCode is the Herdr error code err carries, or "" for any other error.
func ErrorCode(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}

	return ""
}

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type response struct {
	Result json.RawMessage `json:"result"`
	Error  *APIError       `json:"error"`
}

// Snapshot is the whole session. Fields exist only where code reads them.
type Snapshot struct {
	Workspaces []Workspace `json:"workspaces"`
	Tabs       []Tab       `json:"tabs"`
	Panes      []Pane      `json:"panes"`
}

// Workspace is the row Herdr shows above a group of tabs. An unrenamed one is
// labelled after the basename of the directory it was created in.
type Workspace struct {
	ID    string `json:"workspace_id"`
	Label string `json:"label"`
}

// Tab arrives in display order. An unnamed tab is labelled with its position
// (or "" once a name is cleared), never with Herdr's own tab number.
type Tab struct {
	ID          string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

// Pane is one pane of a tab. Every optional field decodes JSON null to "".
type Pane struct {
	ID       string `json:"pane_id"`
	TabID    string `json:"tab_id"`
	Focused  bool   `json:"focused"`
	Revision uint64 `json:"revision"`
	// Label is omitted until the pane is named; unnamed is only ever "".
	Label string `json:"label"`
	// CWD is the pane's shell's; ForegroundCWD the deepest descendant's.
	// Neither is exact: only a process read says where the pane is.
	CWD                   string `json:"cwd"`
	ForegroundCWD         string `json:"foreground_cwd"`
	TerminalTitle         string `json:"terminal_title"`
	TerminalTitleStripped string `json:"terminal_title_stripped"`
	// AgentTitle is the agent's own title, null in practice for Claude Code.
	AgentTitle   string        `json:"title"`
	Agent        string        `json:"agent"`
	DisplayAgent string        `json:"display_agent"`
	AgentStatus  string        `json:"agent_status"`
	AgentSession *AgentSession `json:"agent_session"`
}

// AgentSession is what an agent's Herdr integration hook reported about the
// conversation it holds. Nil until `herdr integration install <agent>` ran.
type AgentSession struct {
	Agent string `json:"agent"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// SessionID is the session id this reference holds for agent, if any.
func (s *AgentSession) SessionID(agent string) (string, bool) {
	if s == nil || s.Agent != agent || s.Kind != "id" {
		return "", false
	}

	return s.Value, true
}

// Process is one foreground process of a pane. Herdr lists descendants
// first, so the pane's own foreground process is the last one.
type Process struct {
	Name string   `json:"name"`
	Argv []string `json:"argv"`
	CWD  string   `json:"cwd"`
}

// NotifyResult says whether a notice was shown, and Herdr's reason if not.
type NotifyResult struct {
	Shown  bool   `json:"shown"`
	Reason string `json:"reason"`
}

type emptyParams struct{}

type paneTarget struct {
	PaneID string `json:"pane_id"`
}

type tabRename struct {
	TabID string `json:"tab_id"`
	Label string `json:"label"`
}

type paneRename struct {
	PaneID string `json:"pane_id"`
	Label  string `json:"label"`
}

type workspaceRename struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

type notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
