package herdr

import (
	"context"
	"testing"
)

type recorder struct{ methods []string }

func (r *recorder) ServerIdentity() string { return "srv" }

func (r *recorder) Call(_ context.Context, method string, _, _ any) error {
	r.methods = append(r.methods, method)
	return nil
}

func TestDryRunSwallowsRenamesAndPassesReadsThrough(t *testing.T) {
	inner := &recorder{}

	var renamed []string

	dry := DryRun{
		Inner:   inner,
		Renamed: func(kind, id, label string) { renamed = append(renamed, kind+" "+id+" "+label) },
	}
	ctx := context.Background()

	_, _ = ReadSnapshot(ctx, dry)
	_ = RenameTab(ctx, dry, "t1", "1 · api")
	_ = RenamePane(ctx, dry, "p1", "api")
	_ = RenameWorkspace(ctx, dry, "w1", "api › fix")

	if len(inner.methods) != 1 || inner.methods[0] != methodSessionSnapshot {
		t.Errorf("inner saw %q, want the snapshot only", inner.methods)
	}

	want := []string{"tab t1 1 · api", "pane p1 api", "workspace w1 api › fix"}
	if len(renamed) != 3 || renamed[0] != want[0] || renamed[1] != want[1] || renamed[2] != want[2] {
		t.Errorf("renamed = %q", renamed)
	}

	if dry.ServerIdentity() != "srv" {
		t.Error("identity passes through")
	}
}

func TestSessionID(t *testing.T) {
	var none *AgentSession
	if _, ok := none.SessionID("claude"); ok {
		t.Error("nil reference has no id")
	}

	if _, ok := (&AgentSession{Agent: "codex", Kind: "id", Value: "x"}).SessionID("claude"); ok {
		t.Error("another agent's session is not claude's")
	}

	if _, ok := (&AgentSession{Agent: "claude", Kind: "path", Value: "x"}).SessionID("claude"); ok {
		t.Error("only id references are read")
	}

	if id, ok := (&AgentSession{Agent: "claude", Kind: "id", Value: "x"}).SessionID("claude"); !ok || id != "x" {
		t.Error("an id reference is read")
	}
}

func TestNewSocketClientNeedsTheEnvironment(t *testing.T) {
	t.Setenv(socketPathEnv, "")

	if _, err := NewSocketClient(); err == nil {
		t.Error("no HERDR_SOCKET_PATH must fail")
	}

	t.Setenv(socketPathEnv, "/tmp/h.sock")

	if c, err := NewSocketClient(); err != nil || c.SocketPath() != "/tmp/h.sock" {
		t.Errorf("client = %+v, err = %v", c, err)
	}
}
