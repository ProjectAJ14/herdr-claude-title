package herdr

import (
	"context"
	"encoding/json"
)

// DryRun reads through Inner but swallows every rename, reporting it to
// Renamed instead. It is what `preview` runs against a live session, so the
// user can see what would change without anything changing.
type DryRun struct {
	Inner   Client
	Renamed func(kind, id, label string)
}

func (d DryRun) ServerIdentity() string { return d.Inner.ServerIdentity() }

func (d DryRun) Call(ctx context.Context, method string, params, result any) error {
	kind, isRename := map[string]string{
		methodTabRename: "tab", methodPaneRename: "pane", methodWorkspaceRename: "workspace",
	}[method]
	if !isRename {
		return d.Inner.Call(ctx, method, params, result)
	}

	var target struct {
		TabID       string `json:"tab_id"`
		PaneID      string `json:"pane_id"`
		WorkspaceID string `json:"workspace_id"`
		Label       string `json:"label"`
	}

	raw, _ := json.Marshal(params)
	_ = json.Unmarshal(raw, &target)
	d.Renamed(kind, target.TabID+target.PaneID+target.WorkspaceID, target.Label)

	return nil
}
