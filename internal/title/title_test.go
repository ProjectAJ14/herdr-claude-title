package title

import (
	"testing"

	"github.com/ProjectAJ14/herdr-claude-title/internal/checkout"
	"github.com/ProjectAJ14/herdr-claude-title/internal/session"
)

func TestSanitize(t *testing.T) {
	cases := []struct {
		name, in string
		max      int
		want     string
	}{
		{"strips ANSI", "\x1b[31mred\x1b[0m", 0, "red"},
		{"drops bidi override", "abc\u202edef", 0, "abcdef"},
		{"collapses spaces", "a     b", 0, "a b"},
		{"normalises separators", "a›› b ›c", 0, "a › b › c"},
		{"trims dangling separator", "› a ›", 0, "a"},
		{"cuts by columns, not runes", "日本語テキスト", 6, "日本語"},
		{"keeps emoji clusters whole", "work 👨‍👩‍👧‍👦", 6, "work"},
		{"no separator left after a cut", "abcdefg › hij", 9, "abcdefg"},
		{"cuts at a word", "Explaner artifact : I really like", 26, "Explaner artifact : I"},
		{"keeps a word ending at the limit", "fix login page", 9, "fix login"},
		{"one long word cuts hard", "abcdefghij", 4, "abcd"},
	}

	for _, c := range cases {
		if got := Sanitize(c.in, c.max); got != c.want {
			t.Errorf("%s: Sanitize(%q, %d) = %q, want %q", c.name, c.in, c.max, got, c.want)
		}

		if again := Sanitize(Sanitize(c.in, c.max), c.max); again != Sanitize(c.in, c.max) {
			t.Errorf("%s: not idempotent: %q", c.name, again)
		}
	}
}

func TestMeaningful(t *testing.T) {
	keep := map[string]string{
		"auth.ts (~/work/src) - Nvim": "auth.ts - Nvim",
		"Fix bug in src/auth.ts":      "Fix bug in src/auth.ts",
	}
	for in, want := range keep {
		if got, ok := Meaningful(in); !ok || got != want {
			t.Errorf("Meaningful(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}

	for _, in := range []string{"~", "zsh", "Claude Code", "alex@mac:~/work", "pwsh in dashboard", "/usr/bin"} {
		if got, ok := Meaningful(in); ok {
			t.Errorf("Meaningful(%q) = %q, want rejected", in, got)
		}
	}
}

func TestShortenBranch(t *testing.T) {
	cases := map[string]string{
		"feat/oauth":                      "feat/oauth",
		"bugfix-asa-cpanel-uapi-mc-13675": "MC-13675",
		"feature/rework-the-poll-loop":    "rework-the",
	}
	for in, want := range cases {
		if got := shortenBranch(in, 12); got != want {
			t.Errorf("shortenBranch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSSHDestination(t *testing.T) {
	cases := map[string][]string{
		"prod-01": {"ssh", "-p", "2222", "deploy@prod-01", "tail", "-f", "log"},
		"db":      {"ssh", "ssh://root@db:22"},
		"::1":     {"ssh", "[::1]:22"},
	}
	for want, argv := range cases {
		if got := sshDestination(argv); got != want {
			t.Errorf("sshDestination(%v) = %q, want %q", argv, got, want)
		}
	}

	if !sshIsTunnel([]string{"ssh", "-N", "-L", "80:x:80", "host"}) {
		t.Error("-N is a tunnel")
	}
}

func agentPane() *session.Pane {
	return &session.Pane{
		ID: "p1", Agent: "claude", Dir: "/work/dashboard",
		TerminalTitle:     "✳ Herdr plugin enhancement",
		ConversationTopic: "refer https://github.com/x",
	}
}

func TestClaudeSummaryOutranksTerminalTitle(t *testing.T) {
	pane := agentPane()
	pane.ClaudeSummary = "OAuth scope refresh"

	d := ForTabs(Options{MaxColumns: 60}).NameTab(session.Tab{Speaker: pane})
	if d.Label != "dashboard › claude › OAuth scope refresh" || d.Source != "claude_summary" {
		t.Errorf("got %+v", d)
	}
}

func TestWithoutSummaryTheTerminalTitleSpeaks(t *testing.T) {
	d := ForTabs(Options{MaxColumns: 60, HideAgentName: true}).NameTab(session.Tab{Speaker: agentPane()})
	if d.Label != "dashboard › ✳ Herdr plugin enhancement" {
		t.Errorf("got %q", d.Label)
	}
}

func TestWorkspaceLabelIsNotRepeated(t *testing.T) {
	pane := &session.Pane{ID: "p", Dir: "/work/dashboard", Processes: []session.Process{{Name: "nvim"}},
		TerminalTitle: "auth.ts - Nvim"}

	d := ForTabs(Options{MaxColumns: 60}).NameTab(session.Tab{Speaker: pane, WorkspaceLabel: "dashboard"})
	if d.Label != "nvim › auth.ts" {
		t.Errorf("got %q", d.Label)
	}
}

func TestBranchFromTheAgentsWorktree(t *testing.T) {
	pane := agentPane()
	pane.Repo = checkout.Checkout{Branch: "main", DefaultBranch: "main", SharedGitDir: "/work/dashboard/.git"}
	pane.AgentRepo = checkout.Checkout{
		Branch:        "feat/oauth",
		DefaultBranch: "main",
		SharedGitDir:  "/work/dashboard/.git",
	}

	d := ForTabs(Options{MaxColumns: 80, BranchMaxColumns: 12, HideAgentName: true}).NameTab(session.Tab{Speaker: pane})
	if d.Label != "dashboard › feat/oauth › ✳ Herdr plugin enhancement" {
		t.Errorf("got %q", d.Label)
	}
}

func TestFallbackAndNumber(t *testing.T) {
	namer := WithTabNumber(ForTabs(Options{MaxColumns: 30}), 30)
	if d := namer.NameTab(session.Tab{Position: 3, Speaker: &session.Pane{ID: "p"}}); d.Label != "3 · Shell" {
		t.Errorf("got %q", d.Label)
	}
}

func TestPaneDropsWhatItsTabSays(t *testing.T) {
	speaker := agentPane()
	speaker.ClaudeSummary = "Poll loop rework"
	other := &session.Pane{ID: "p2", Agent: "claude", Dir: "/work/dashboard", ClaudeSummary: "Token refresh"}

	got := ForTabs(
		Options{MaxColumns: 60},
	).NamePanes(session.Tab{Speaker: speaker, Panes: []*session.Pane{speaker, other}})
	if got[0].Label != "Poll loop rework" || got[1].Label != "Token refresh" {
		t.Errorf("got %q, %q", got[0].Label, got[1].Label)
	}
}

func TestWorkspaceRowKeepsItsEnd(t *testing.T) {
	got := FormatKeepingEnd(Parts{Place: "herdr-claude-title", Activity: "Poll loop"}, 20)
	if got != "Poll loop" {
		t.Errorf("got %q", got)
	}
}

func sshPane(title string, argv ...string) *session.Pane {
	return &session.Pane{
		ID: "p", Dir: "/work/api", TerminalTitle: title,
		Repo:      checkout.Checkout{Branch: "feat/x", SharedGitDir: "/work/api/.git"},
		Processes: []session.Process{{Name: "zsh"}, {Name: "ssh", Args: argv}},
	}
}

func TestSSHPanesNameTheHost(t *testing.T) {
	namer := ForTabs(Options{MaxColumns: 60, BranchMaxColumns: 12})

	cases := []struct {
		pane *session.Pane
		want string
	}{
		{sshPane("ssh deploy@productio", "ssh", "deploy@prod-01"), "ssh › prod-01"}, // local echo dropped, no branch
		{sshPane("Restart workers", "ssh", "-p", "2222", "prod-01"), "ssh › prod-01 › Restart workers"},
		{sshPane("", "ssh", "-p"), "ssh"},                                      // unreadable host
		{sshPane("", "ssh", "-N", "-L", "80:x:80", "bastion"), "api › feat/x"}, // a tunnel is not remote work
	}

	for _, c := range cases {
		if got := namer.NameTab(session.Tab{Speaker: c.pane}).Label; got != c.want {
			t.Errorf("argv %v: got %q, want %q", c.pane.Processes[1].Args, got, c.want)
		}
	}
}

func TestWorkspaceNamer(t *testing.T) {
	namer := ForWorkspaces(Options{MaxColumns: 20, BranchMaxColumns: 12})
	pane := &session.Pane{ID: "p", Dir: "/work/api", TerminalTitle: "auth.ts",
		Processes: []session.Process{{Name: "nvim"}}}

	if d := namer.NameWorkspace(session.Workspace{Speaker: pane}); d.Label != "api › auth.ts" {
		t.Errorf("row = %q (no process kind on a row)", d.Label)
	}

	if d := namer.NameWorkspace(session.Workspace{}); d.Label != "" {
		t.Errorf("a row with no pane is left alone, got %q", d.Label)
	}
}

func TestATabDropsWhatAWrittenRowCarries(t *testing.T) {
	pane := &session.Pane{ID: "p", Dir: "/work/api", TerminalTitle: "Fix login",
		Repo: checkout.Checkout{Branch: "feat/x", SharedGitDir: "/g"}}
	namer := ForTabs(Options{MaxColumns: 60, BranchMaxColumns: 12, WorkspaceRowIsParts: true})

	// The row was written by the plugin and cut from its front.
	if got := namer.NameTab(session.Tab{Speaker: pane, WorkspaceLabel: "feat/x  ›  api"}).Label; got != "Fix login" {
		t.Errorf("got %q, want the parts the row carries dropped", got)
	}

	// A place that itself contains the separator is matched as a run.
	pane.Dir = "/work/a › b"
	pane.Repo = checkout.Checkout{}

	if got := namer.NameTab(session.Tab{Speaker: pane, WorkspaceLabel: "x › a › b"}).Label; got != "Fix login" {
		t.Errorf("got %q", got)
	}
}

func TestAgentBranchNeedsTheSameRepoOrContainment(t *testing.T) {
	b := branch{maxColumns: 12}
	agent := checkout.Checkout{Branch: "feat/wt", SharedGitDir: "/other/.git"}

	contained := &session.Pane{Dir: "/work", AgentDir: "/work/code-review", AgentRepo: agent}
	if parts, _ := b.Contribute(contained); parts.Branch != "feat/wt" {
		t.Errorf("a pane with no repo takes a contained agent's branch, got %q", parts.Branch)
	}

	sibling := &session.Pane{Dir: "/work/code", AgentDir: "/work/code-review", AgentRepo: agent}
	if _, ok := b.Contribute(sibling); ok {
		t.Error("code-review is not inside code")
	}

	nested := &session.Pane{Dir: "/work", AgentDir: "/work/vendor/x", AgentRepo: agent,
		Repo: checkout.Checkout{Branch: "main", DefaultBranch: "main", SharedGitDir: "/work/.git"}}
	if _, ok := b.Contribute(nested); ok {
		t.Error("a pane with its own repo must not take a nested clone's branch")
	}

	detached := &session.Pane{Repo: checkout.Checkout{DetachedHash: "abc1234", SharedGitDir: "/g"}}
	if parts, _ := b.Contribute(detached); parts.Branch != "abc1234" {
		t.Errorf("a detached HEAD says its hash, got %q", parts.Branch)
	}

	noRemoteTrunk := &session.Pane{Repo: checkout.Checkout{Branch: "master", SharedGitDir: "/g"}}
	if _, ok := b.Contribute(noRemoteTrunk); ok {
		t.Error("a conventional trunk says nothing when no default is recorded")
	}

	if _, ok := (branch{}).Contribute(detached); ok {
		t.Error("0 columns turns branches off")
	}
}

func TestDirectoryRefusesPlacesThatSayNothing(t *testing.T) {
	d := directory{home: "/home/me"}
	for _, dir := range []string{"", "/", "/home/me", "relative"} {
		if parts, ok := d.Contribute(&session.Pane{Dir: dir}); ok {
			t.Errorf("dir %q gave %+v", dir, parts)
		}
	}
}

func TestSourcesAnswerOnlyForWhatTheyKnow(t *testing.T) {
	plain := &session.Pane{ID: "p", AgentTitle: "x", ClaudeSummary: "x", ConversationTopic: "x"}
	for _, s := range []Source{agentTitle{}, claudeSummary{}, transcript{}} {
		if _, ok := s.Contribute(plain); ok {
			t.Errorf("%s answered for a pane with no agent", s.Name())
		}
	}

	agent := &session.Pane{ID: "p", Agent: "claude", AgentDisplayName: "Claude Code", AgentTitle: "Claude Code"}
	if _, ok := (agentTitle{}).Contribute(agent); ok {
		t.Error("an agent echoing its own name is not an activity")
	}

	agent.AgentTitle = "Reviewing auth - claude"
	if parts, _ := (agentTitle{}).Contribute(agent); parts.Activity != "Reviewing auth" || parts.Agent != "claude" {
		t.Errorf("parts = %+v, want the name stripped from the edge", parts)
	}

	busy := &session.Pane{ID: "p", Processes: []session.Process{{Name: "node"}, {Name: "esbuild"}}}
	if _, ok := (process{}).Contribute(busy); ok {
		t.Error("several programs name nothing")
	}

	names := map[string]bool{}
	for _, s := range []Source{agentTitle{}, claudeSummary{}, terminalTitle{}, transcript{}, process{},
		remoteHost{}, branch{}, directory{}} {
		names[s.Name()] = true
	}

	if len(names) != 8 {
		t.Error("every source needs a distinct name for the rename log")
	}
}

func TestANarrowBarDropsTheNumberBeforeTheName(t *testing.T) {
	if d := WithTabNumber(
		ForTabs(Options{MaxColumns: 4}),
		4,
	).NameTab(session.Tab{Position: 12, Speaker: &session.Pane{ID: "p"}}); d.Label != "Shel" {
		t.Errorf("got %q", d.Label)
	}

	if d := WithTabNumber(
		ForTabs(Options{}),
		0,
	).NameTab(session.Tab{Position: 1, Speaker: &session.Pane{ID: "p"}}); d.Label != "1 · Shell" {
		t.Errorf("zero columns takes the default, got %q", d.Label)
	}
}

func TestTheFallbackFitsTheWidthToo(t *testing.T) {
	if d := ForTabs(Options{MaxColumns: 3}).NameTab(session.Tab{Speaker: &session.Pane{ID: "p"}}); d.Label != "She" {
		t.Errorf("got %q", d.Label)
	}
}
