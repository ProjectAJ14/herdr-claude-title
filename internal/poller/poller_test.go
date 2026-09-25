package poller

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ProjectAJ14/herdr-claude-title/internal/herdr"
	"github.com/ProjectAJ14/herdr-claude-title/internal/ownership"
	"github.com/ProjectAJ14/herdr-claude-title/internal/title"
	"github.com/ProjectAJ14/herdr-claude-title/internal/transcript"
)

// fakeHerdr answers the methods the poller calls from an in-memory session,
// applying renames the way Herdr does.
type fakeHerdr struct {
	mu        sync.Mutex
	snap      herdr.Snapshot
	identity  string
	processes map[string][]herdr.Process
	fail      map[string]error // method → error to answer with
	renames   []string
	procReads int
}

func (f *fakeHerdr) ServerIdentity() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.identity == "" {
		return "server-1"
	}

	return f.identity
}

func (f *fakeHerdr) Call(_ context.Context, method string, params, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.fail[method]; err != nil {
		return err
	}

	var p struct {
		TabID       string `json:"tab_id"`
		PaneID      string `json:"pane_id"`
		WorkspaceID string `json:"workspace_id"`
		Label       string `json:"label"`
	}

	raw, _ := json.Marshal(params)
	_ = json.Unmarshal(raw, &p)

	switch method {
	case "session.snapshot":
		out, _ := json.Marshal(map[string]any{"snapshot": f.snap})
		return json.Unmarshal(out, result)
	case "pane.process_info":
		f.procReads++
		out, _ := json.Marshal(
			map[string]any{"process_info": map[string]any{"foreground_processes": f.processes[p.PaneID]}},
		)

		return json.Unmarshal(out, result)
	case "tab.rename":
		f.renames = append(f.renames, "tab:"+p.Label)
		for i := range f.snap.Tabs {
			if f.snap.Tabs[i].ID == p.TabID {
				f.snap.Tabs[i].Label = p.Label
			}
		}
	case "pane.rename":
		f.renames = append(f.renames, "pane:"+p.Label)
		for i := range f.snap.Panes {
			if f.snap.Panes[i].ID == p.PaneID {
				f.snap.Panes[i].Label = p.Label
			}
		}
	case "workspace.rename":
		f.renames = append(f.renames, "workspace:"+p.Label)
		for i := range f.snap.Workspaces {
			if f.snap.Workspaces[i].ID == p.WorkspaceID {
				f.snap.Workspaces[i].Label = p.Label
			}
		}
	}

	return nil
}

func (f *fakeHerdr) setTabLabel(label string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snap.Tabs[0].Label = label
}

func (f *fakeHerdr) renamed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.renames...)
}

type soleInstance struct{}

func (soleInstance) Superseded() bool { return false }
func (soleInstance) MarkReady()       {}

type fixedSummary string

func (s fixedSummary) Title(string, transcript.Conversation) string { return string(s) }
func (fixedSummary) Retain([]string)                                {}

func newPoller(summary Summarizer) *Poller {
	tabs := title.ForTabs(title.Options{MaxColumns: 40})

	return New(Options{
		Interval: time.Millisecond,
		Namers:   Namers{Tabs: title.WithTabNumber(tabs, 40), Panes: tabs},
		Owners:   ownership.Load(""),
		Reader:   PaneReaderOptions{Summarizer: summary},
		Instance: soleInstance{},
		Log:      slog.New(slog.DiscardHandler),
	})
}

func oneTabSession() *fakeHerdr {
	return &fakeHerdr{snap: herdr.Snapshot{
		Workspaces: []herdr.Workspace{{ID: "w1", Label: "work"}},
		Tabs:       []herdr.Tab{{ID: "t1", WorkspaceID: "w1", Label: "1"}},
		Panes:      []herdr.Pane{{ID: "p1", TabID: "t1", Focused: true, CWD: "/work/api"}},
	}}
}

func TestNamesThenLeavesAUserRenameAlone(t *testing.T) {
	h := oneTabSession()
	p := newPoller(nil)
	ctx := context.Background()

	p.Poll(ctx, h)
	p.Poll(ctx, h) // settled: nothing new to say

	if len(h.renames) != 2 || h.renames[0] != "tab:1 · api" || h.renames[1] != "pane:api" {
		t.Fatalf("renames = %q", h.renames)
	}

	h.setTabLabel("prod fire")
	p.Poll(ctx, h)
	p.Poll(ctx, h)

	if len(h.renames) != 2 {
		t.Fatalf("a user-renamed tab was renamed back: %q", h.renames)
	}

	h.setTabLabel("") // clearing hands it back
	p.Poll(ctx, h)

	if last := h.renames[len(h.renames)-1]; last != "tab:1 · api" {
		t.Fatalf("a cleared tab was not taken back: %q", h.renames)
	}
}

func TestClaudeSummaryNamesAnAgentTab(t *testing.T) {
	const id = "7296ede5-8e78-4232-a6fb-8679ba11b8e9"

	home := t.TempDir()
	dir := filepath.Join(home, "projects", "-work-api")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, id+".jsonl"),
		[]byte(`{"type":"user","origin":{"kind":"human"},"message":{"content":"refresh tokens"}}`+"\n"), 0o600)

	h := oneTabSession()
	h.snap.Panes[0].Agent = "claude"
	h.snap.Panes[0].AgentSession = &herdr.AgentSession{Agent: "claude", Kind: "id", Value: id}

	p := newPoller(fixedSummary("Token refresh"))
	p.reader.opts.Transcripts = transcript.NewReader(home)
	p.Poll(context.Background(), h)

	if h.renames[0] != "tab:1 · api › claude › Token refresh" {
		t.Fatalf("renames = %q", h.renames)
	}
}

// instanceFlag is a claim a test can supersede.
type instanceFlag struct {
	mu                sync.Mutex
	superseded, ready bool
}

func (i *instanceFlag) Superseded() bool { i.mu.Lock(); defer i.mu.Unlock(); return i.superseded }
func (i *instanceFlag) MarkReady()       { i.mu.Lock(); defer i.mu.Unlock(); i.ready = true }

func twoPaneTab() *fakeHerdr {
	h := oneTabSession()
	h.snap.Panes = append(h.snap.Panes, herdr.Pane{ID: "p2", TabID: "t1", CWD: "/work/api"})
	h.processes = map[string][]herdr.Process{
		"p1": {{Name: "nvim", Argv: []string{"nvim"}, CWD: "/work/api"}},
		"p2": {{Name: "zsh", Argv: []string{"zsh"}, CWD: "/work/web"}},
	}

	return h
}

func TestProcessReadsNamePanesAndAreReused(t *testing.T) {
	h := twoPaneTab()
	p := newPoller(nil)

	p.Poll(context.Background(), h)

	got := h.renamed()
	want := []string{"tab:1 · api › nvim", "pane:nvim", "pane:web"}

	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("renames = %q, want %q", got, want)
	}

	if h.procReads != 2 {
		t.Fatalf("process reads = %d, want one per pane (the speaker is not read twice)", h.procReads)
	}

	p.Poll(context.Background(), h)

	if h.procReads != 2 || len(h.renamed()) != 3 {
		t.Fatalf("a still session must reuse reads and rename nothing: reads=%d renames=%q", h.procReads, h.renamed())
	}
}

func TestWithoutPaneNamingOnlyTheSpeakerIsRead(t *testing.T) {
	h := twoPaneTab()
	p := newPoller(nil)
	p.namers.Panes = nil

	p.Poll(context.Background(), h)

	if h.procReads != 1 || len(h.renamed()) != 1 {
		t.Fatalf("reads=%d renames=%q", h.procReads, h.renamed())
	}
}

func TestAUserOwnedTabIsNotReadWhenPanesAreNotNamed(t *testing.T) {
	h := oneTabSession()
	p := newPoller(nil)
	p.namers.Panes = nil
	ctx := context.Background()

	p.Poll(ctx, h)
	h.setTabLabel("mine")
	p.Poll(ctx, h) // claims it
	reads := h.procReads
	h.snap.Panes[0].Revision++ // would force a fresh read if the tab were named
	p.Poll(ctx, h)

	if h.procReads != reads {
		t.Fatal("a claimed tab must cost nothing but the snapshot")
	}
}

func TestRenameFailures(t *testing.T) {
	ctx := context.Background()

	gone := oneTabSession()
	gone.fail = map[string]error{"tab.rename": &herdr.APIError{Code: herdr.CodeTabNotFound}}
	p := newPoller(nil)
	p.Poll(ctx, gone)

	if p.owners.Tabs.IsUserOwned("t1") {
		t.Fatal("a tab closed mid-poll is not the user's")
	}

	// A rename Herdr read but did not answer may land late: when it does, the
	// label must read as ours, not as the user's.
	late := oneTabSession()
	late.fail = map[string]error{"tab.rename": herdr.ErrUnanswered}
	p = newPoller(nil)
	p.Poll(ctx, late)
	late.fail = nil
	late.setTabLabel("1 · api")
	late.snap.Tabs = append(late.snap.Tabs, herdr.Tab{ID: "t0", WorkspaceID: "w1", Label: "1"})
	late.snap.Tabs[0], late.snap.Tabs[1] = late.snap.Tabs[1], late.snap.Tabs[0] // t1 slid to position 2
	p.Poll(ctx, late)

	if p.owners.Tabs.IsUserOwned("t1") {
		t.Fatal("a late-landing rename must not be claimed as the user's")
	}

	if got := late.renamed(); !slices.Contains(got, "tab:2 · api") {
		t.Errorf("the tab should be renamed on to its new position: %q", got)
	}
}

func TestFailedPollsAreSurvivedAndReadyIsMarkedOnSuccess(t *testing.T) {
	h := oneTabSession()
	h.fail = map[string]error{"session.snapshot": errors.New("herdr is down")}
	inst := &instanceFlag{}
	p := newPoller(nil)
	p.instance = inst

	for range 3 {
		if !p.Poll(context.Background(), h) {
			t.Fatal("no failure is fatal")
		}
	}

	if inst.ready || p.failures.run != 3 {
		t.Fatalf("ready=%v run=%d", inst.ready, p.failures.run)
	}

	h.fail = nil
	p.Poll(context.Background(), h)

	if !inst.ready || p.failures.run != 0 {
		t.Fatalf("after recovery ready=%v run=%d", inst.ready, p.failures.run)
	}
}

func TestFailureLogDoubles(t *testing.T) {
	var f failureRun

	var logged []int

	for range 9 {
		if run := f.failed(); run > 0 {
			logged = append(logged, run)
		}
	}

	if len(logged) != 4 || logged[0] != 1 || logged[1] != 2 || logged[2] != 4 || logged[3] != 8 {
		t.Fatalf("logged at %v, want 1 2 4 8", logged)
	}

	if f.recovered() != 9 || f.recovered() != 0 {
		t.Fatal("recovered reports the run once, then resets")
	}
}

func TestASuccessorEndsTheRun(t *testing.T) {
	h := oneTabSession()
	inst := &instanceFlag{}
	p := newPoller(nil)
	p.instance = inst

	if !p.Poll(context.Background(), h) {
		t.Fatal("first poll continues")
	}

	h.mu.Lock()
	h.identity = "server-2"
	h.mu.Unlock()

	if p.Poll(context.Background(), h) {
		t.Fatal("another server on the socket must end the run")
	}

	p = newPoller(nil)
	p.instance = &instanceFlag{superseded: true}

	if p.Poll(context.Background(), oneTabSession()) {
		t.Fatal("a newer claim must end the run")
	}
}

func TestRunStopsOnCancelAndOnSuccessor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		newPoller(nil).Run(ctx, oneTabSession())
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run must return when its context ends")
	}

	p := newPoller(nil)
	p.instance = &instanceFlag{superseded: true}
	p.Run(context.Background(), oneTabSession()) // returns at once
}

func workspacePoller(t *testing.T) *Poller {
	t.Helper()

	p := newPoller(nil)
	p.namers.Workspaces = title.ForWorkspaces(title.Options{MaxColumns: 20, WorkspaceRowIsParts: true})
	p.owners = ownership.Load(filepath.Join(t.TempDir(), "renames.json"))

	return p
}

func TestAOneTabWorkspaceRowIsNamedAfterItsTab(t *testing.T) {
	h := oneTabSession()
	h.snap.Workspaces[0].Label = "api" // Herdr's default: the directory basename
	h.processes = map[string][]herdr.Process{"p1": {{Name: "nvim", Argv: []string{"nvim"}, CWD: "/work/api"}}}
	h.snap.Panes[0].TerminalTitle = "auth.ts - Nvim"

	p := workspacePoller(t)
	p.Poll(context.Background(), h)

	if got := h.renamed(); got[len(got)-1] != "workspace:api › auth.ts - Nvim" {
		t.Fatalf("renames = %q", got)
	}
}

func TestRowsTheUserNamedOrThatHoldSeveralTabsAreLeftAlone(t *testing.T) {
	h := oneTabSession()
	h.snap.Workspaces[0].Label = "Payments" // not the basename: the owner's

	p := workspacePoller(t)
	p.Poll(context.Background(), h)

	for _, r := range h.renamed() {
		if strings.HasPrefix(r, "workspace:") {
			t.Fatalf("a row the user named was renamed: %q", h.renamed())
		}
	}

	two := oneTabSession()
	two.snap.Workspaces[0].Label = "api"
	two.snap.Tabs = append(two.snap.Tabs, herdr.Tab{ID: "t2", WorkspaceID: "w1", Label: "2"})
	p = workspacePoller(t)
	p.Poll(context.Background(), two)

	for _, r := range two.renamed() {
		if strings.HasPrefix(r, "workspace:") {
			t.Fatalf("a two-tab row was renamed: %q", two.renamed())
		}
	}
}

func TestBranchesAreReadWhenOn(t *testing.T) {
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git"), 0o700)
	os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/feat/oauth\n"), 0o600)

	h := oneTabSession()
	h.snap.Workspaces[0].Label = "elsewhere"
	h.processes = map[string][]herdr.Process{"p1": {{Name: "zsh", CWD: repo}}}

	tabs := title.ForTabs(title.Options{MaxColumns: 60, BranchMaxColumns: 12})
	p := newPoller(nil)
	p.namers = Namers{Tabs: tabs}
	p.reader.opts.ReadBranches = true
	p.Poll(context.Background(), h)

	if got := h.renamed(); len(got) != 1 || got[0] != "tab:"+filepath.Base(repo)+" › feat/oauth" {
		t.Fatalf("renames = %q", got)
	}
}

// cache that is ignored mid-write would.
type recordingSummary struct{ calls int }

func (s *recordingSummary) Title(string, transcript.Conversation) string {
	s.calls++
	return "Token refresh"
}
func (*recordingSummary) Retain([]string) {}

func TestTheClaudeTitleHoldsWhileATranscriptIsMidWrite(t *testing.T) {
	const id = "7296ede5-8e78-4232-a6fb-8679ba11b8e9"

	home := t.TempDir()
	dir := filepath.Join(home, "projects", "-work-api")
	os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, id+".jsonl")
	os.WriteFile(
		path,
		[]byte(`{"type":"user","origin":{"kind":"human"},"message":{"content":"refresh tokens"}}`+"\n"),
		0o600,
	)

	h := oneTabSession()
	h.snap.Panes[0].Agent = "claude"
	h.snap.Panes[0].AgentSession = &herdr.AgentSession{Agent: "claude", Kind: "id", Value: id}

	sum := &recordingSummary{}
	p := newPoller(sum)
	p.reader.opts.Transcripts = transcript.NewReader(home)
	p.Poll(context.Background(), h)

	// The agent is halfway through appending a line.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"assistant","message":`)
	f.Close()
	h.snap.Panes[0].Revision++
	p.Poll(context.Background(), h)

	if got := h.renamed(); len(got) != 2 || got[0] != "tab:1 · api › claude › Token refresh" || sum.calls != 2 {
		t.Fatalf("renames = %q, summary asked %d times; the title must not flicker", got, sum.calls)
	}
}
