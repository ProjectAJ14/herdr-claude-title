package session

import (
	"testing"
	"time"

	"github.com/ProjectAJ14/herdr-claude-title/internal/herdr"
)

var t0 = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func TestPaneFromPrefersTheForegroundDirectory(t *testing.T) {
	p := PaneFrom(herdr.Pane{ID: "p", CWD: "/work/old", ForegroundCWD: "/work/new/"}, t0)
	if p.Dir != "/work/new" {
		t.Errorf("dir = %q, want the foreground dir, cleaned", p.Dir)
	}

	if p = PaneFrom(herdr.Pane{ID: "p", CWD: "/work/shell"}, t0); p.Dir != "/work/shell" {
		t.Errorf("dir = %q, want cwd when there is no foreground dir", p.Dir)
	}

	if p = PaneFrom(herdr.Pane{ID: "p"}, t0); p.Dir != "" {
		t.Errorf("dir = %q, want empty to stay empty, not \".\"", p.Dir)
	}
}

func TestApplyProcessesTakesTheLastProcessDirectory(t *testing.T) {
	p := &Pane{Dir: "/guess"}
	p.ApplyProcesses([]herdr.Process{
		{Name: "node", CWD: "/elsewhere/mcp"},
		{Name: "pwsh.EXE", Argv: []string{"pwsh"}, CWD: "/work/api"},
	})

	fg, ok := p.Foreground()
	if !ok || fg.Name != "pwsh" || p.Dir != "/work/api" || len(p.Processes) != 2 {
		t.Fatalf("pane = %+v", p)
	}

	p.ApplyProcesses(nil)

	if _, ok := p.Foreground(); ok || p.Dir != "/work/api" {
		t.Errorf("an empty read must clear processes and keep the dir: %+v", p)
	}
}

func TestAgentBusy(t *testing.T) {
	cases := map[string]bool{herdr.AgentWorking: true, herdr.AgentBlocked: true, "idle": false, "done": false}
	for status, want := range cases {
		if got := (&Pane{Agent: "claude", AgentStatus: status}).AgentBusy(); got != want {
			t.Errorf("status %s: busy = %v", status, got)
		}
	}

	if (&Pane{AgentStatus: herdr.AgentWorking}).AgentBusy() {
		t.Error("no agent, not busy")
	}

	var nilPane *Pane
	if nilPane.HasAgent() {
		t.Error("a nil pane has no agent")
	}
}

func TestSpeakerRules(t *testing.T) {
	older, newer := t0, t0.Add(time.Second)

	focused := &Pane{ID: "p3", Focused: true, LastChanged: older}
	busy := &Pane{ID: "p2", Agent: "claude", AgentStatus: herdr.AgentWorking, LastChanged: older}
	idleAgent := &Pane{ID: "p4", Agent: "claude", AgentStatus: "idle", LastChanged: older}
	recent := &Pane{ID: "p1", LastChanged: newer}

	pick := func(preferAgent bool, panes ...*Pane) string {
		return TabFrom(herdr.Tab{ID: "t"}, "", 1, panes, preferAgent).Speaker.ID
	}

	if got := pick(false, recent, busy, focused); got != "p3" {
		t.Errorf("focus first: got %s", got)
	}

	if got := pick(false, recent, busy); got != "p2" {
		t.Errorf("then a busy agent: got %s", got)
	}

	if got := pick(false, recent, idleAgent); got != "p1" {
		t.Errorf("then the most recently changed: got %s", got)
	}

	if got := pick(true, focused, idleAgent); got != "p4" {
		t.Errorf("prefer-agent outranks focus, any agent state: got %s", got)
	}

	tieA, tieB := &Pane{ID: "b", LastChanged: older}, &Pane{ID: "a", LastChanged: older}
	if got := pick(false, tieA, tieB); got != "a" {
		t.Errorf("a tie keeps the lowest id: got %s", got)
	}

	if tab := TabFrom(herdr.Tab{ID: "t"}, "", 1, nil, false); tab.Speaker != nil {
		t.Error("a paneless tab has no speaker")
	}

	tab := TabFrom(herdr.Tab{ID: "t", Label: "1"}, "ws", 2, []*Pane{tieA, tieB}, false)
	if tab.Panes[0].ID != "a" || tab.Position != 2 || tab.WorkspaceLabel != "ws" || tab.Label != "1" {
		t.Errorf("tab = %+v", tab)
	}
}

func TestWorkspaceDirLabel(t *testing.T) {
	cases := map[string]string{"/work/api": "api", "/": "", "relative/dir": "", "": ""}
	for dir, want := range cases {
		if got := WorkspaceFrom(herdr.Workspace{ID: "w"}, &Pane{Dir: dir}).DirLabel; got != want {
			t.Errorf("dir %q: label %q, want %q", dir, got, want)
		}
	}

	if ws := WorkspaceFrom(herdr.Workspace{ID: "w", Label: "x"}, nil); ws.DirLabel != "" || ws.Label != "x" {
		t.Errorf("no speaker: %+v", ws)
	}
}

func TestRevisionTracker(t *testing.T) {
	now := t0
	tr := NewRevisionTracker()
	tr.now = func() time.Time { return now }

	tr.Observe([]herdr.Pane{{ID: "p", Revision: 5}})

	if tr.LastChanged("p") != t0 {
		t.Fatal("a new pane changed now")
	}

	if _, ok := tr.CachedProcesses("p"); ok {
		t.Fatal("nothing read yet")
	}

	tr.RememberProcesses("p", []herdr.Process{{Name: "nvim"}})
	tr.RememberProcesses("gone", []herdr.Process{{Name: "x"}}) // must not resurrect

	now = now.Add(time.Second)
	tr.Observe([]herdr.Pane{{ID: "p", Revision: 5}})

	if got, ok := tr.CachedProcesses("p"); !ok || got[0].Name != "nvim" || tr.LastChanged("p") != t0 {
		t.Fatal("an unchanged revision keeps its read and its change time")
	}

	now = now.Add(2 * time.Second)
	if _, ok := tr.CachedProcesses("p"); ok {
		t.Fatal("a read older than the reuse limit must be made again")
	}

	tr.RememberProcesses("p", nil)
	tr.Observe([]herdr.Pane{{ID: "p", Revision: 3}}) // backwards: a recycled id

	if _, ok := tr.CachedProcesses("p"); ok || tr.LastChanged("p") != now {
		t.Fatal("any revision change drops the read and counts as a change")
	}

	tr.Observe(nil)

	if !tr.LastChanged("p").IsZero() || !tr.LastChanged("gone").IsZero() {
		t.Fatal("closed panes are forgotten")
	}
}
