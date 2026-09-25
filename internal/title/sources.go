package title

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ProjectAJ14/herdr-claude-title/internal/session"
)

// Ranks order the confidence ladder. A source never overrides a part a
// higher-ranked one supplied, but a lower one can complete the other half.
// The gaps leave room for the next source.
const (
	RankFallback      = 10
	RankDirectory     = 30
	RankBranch        = 40
	RankSSH           = 60
	RankProcess       = 70
	RankTranscript    = 75
	RankTerminalTitle = 80
	RankClaudeSummary = 85
	RankAgentTitle    = 90
)

// Source contributes title parts read from one pane (never nil).
type Source interface {
	Name() string
	Rank() int
	Contribute(pane *session.Pane) (Parts, bool)
}

// agentTitle is the agent's own statement of its work; rarely set in
// practice (Claude Code reports through the terminal title instead).
type agentTitle struct{}

func (agentTitle) Name() string { return "agent_title" }
func (agentTitle) Rank() int    { return RankAgentTitle }

func (agentTitle) Contribute(p *session.Pane) (Parts, bool) {
	if !p.HasAgent() {
		return Parts{}, false
	}

	return activityFor(p, p.AgentTitle, paneKind(p))
}

// claudeSummary is the title Claude wrote from the conversation itself. It
// outranks the terminal title, which for Claude Code is its own ai-title.
type claudeSummary struct{}

func (claudeSummary) Name() string { return "claude_summary" }
func (claudeSummary) Rank() int    { return RankClaudeSummary }

func (claudeSummary) Contribute(p *session.Pane) (Parts, bool) {
	if !p.HasAgent() {
		return Parts{}, false
	}

	return activityFor(p, p.ClaudeSummary, paneKind(p))
}

// terminalTitle is what a program went out of its way to title its window.
// bindKind=false reads it without the program (`auth.ts`, not `nvim › auth.ts`).
type terminalTitle struct{ bindKind bool }

func (terminalTitle) Name() string { return "terminal_title" }
func (terminalTitle) Rank() int    { return RankTerminalTitle }

func (s terminalTitle) Contribute(p *session.Pane) (Parts, bool) {
	value := p.TerminalTitle
	if value == "" {
		value = p.TerminalTitleRaw
	}

	// While ssh connects, the local shell titles the window `ssh host`.
	if echoesSSHCommand(p, value) {
		return Parts{}, false
	}

	kind := paneKind(p)
	if !s.bindKind && !p.HasAgent() {
		kind = ""
	}

	return activityFor(p, value, kind)
}

// transcript is what the agent's session says it is about, for an agent
// that has not titled its terminal (a slash-command session never does).
type transcript struct{}

func (transcript) Name() string { return "transcript" }
func (transcript) Rank() int    { return RankTranscript }

func (transcript) Contribute(p *session.Pane) (Parts, bool) {
	if !p.HasAgent() {
		return Parts{}, false
	}

	return activityFor(p, p.ConversationTopic, paneKind(p))
}

// process names a pane after its lone program: `dashboard › nvim`.
type process struct{}

func (process) Name() string { return "process" }
func (process) Rank() int    { return RankProcess }

func (process) Contribute(p *session.Pane) (Parts, bool) {
	kind := paneKind(p)
	if kind == "" {
		return Parts{}, false
	}

	return placeKind(p, kind, ""), true
}

// directory names the place after the pane's directory basename. Home, root
// and relative paths say nothing.
type directory struct{ home string }

func newDirectory() directory {
	home, _ := os.UserHomeDir()
	if home != "" {
		home = filepath.Clean(home)
	}

	return directory{home: home}
}

func (directory) Name() string { return "directory" }
func (directory) Rank() int    { return RankDirectory }

func (d directory) Contribute(p *session.Pane) (Parts, bool) {
	if p.Dir == "" {
		return Parts{}, false
	}

	clean := filepath.Clean(p.Dir)
	if !filepath.IsAbs(clean) || clean == filepath.VolumeName(clean)+string(filepath.Separator) || d.isHome(clean) {
		return Parts{}, false
	}

	return Parts{Place: filepath.Base(clean)}, true
}

func (d directory) isHome(dir string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(dir, d.home)
	}

	return dir == d.home
}

// activityFor turns an untrusted value into an activity bound to the pane's
// kind, refusing values that say nothing or only echo the agent's name.
func activityFor(p *session.Pane, value, kind string) (Parts, bool) {
	activity, ok := Meaningful(Sanitize(value, 0))
	if !ok || strings.EqualFold(activity, p.Agent) || strings.EqualFold(activity, p.AgentDisplayName) {
		return Parts{}, false
	}

	return placeKind(p, kind, activity), true
}

// placeKind puts a kind where it belongs: an agent's name is a part of its
// own (the user can hide it), any other kind qualifies the activity.
func placeKind(p *session.Pane, kind, activity string) Parts {
	if p.HasAgent() {
		return Parts{Agent: kind, Activity: stripKind(activity, kind)}
	}

	return Parts{Activity: qualify(activity, kind)}
}

// paneKind names what the pane runs, or "": an agent first (process lists
// never name one), else a lone non-shell program. A build tool is `esbuild`
// and five `node`s, and picking one would be a guess.
func paneKind(p *session.Pane) string {
	if p.HasAgent() {
		return Sanitize(p.Agent, 0)
	}

	var kind string

	for _, proc := range p.Processes {
		name := strings.TrimSpace(proc.Name)
		if _, shell := shellNames[strings.ToLower(name)]; name == "" || shell {
			continue
		}

		if kind != "" {
			return ""
		}

		kind = name
	}

	// ssh is marked on the host instead; a runtime names a language, not work.
	if lower := strings.ToLower(kind); kind == "" || lower == sshKind || isGeneric(lower) {
		return ""
	}

	return Sanitize(kind, 0)
}

// qualify binds a kind to its detail: `nvim › auth.ts`, or `nvim` alone.
func qualify(activity, kind string) string {
	if kind == "" {
		return activity
	}

	if detail := stripKind(activity, kind); detail != "" {
		return kind + Separator + detail
	}

	return kind
}

// stripKind drops a kind the detail already carries at either edge, so
// `auth.ts - Nvim` under kind nvim does not say nvim twice.
func stripKind(detail, kind string) string {
	trimmed := strings.TrimSpace(detail)
	if trimmed == "" || kind == "" {
		return trimmed
	}

	// Fold-compared by bytes: lower-casing can change a character's length.
	switch n := len(kind); {
	case strings.EqualFold(trimmed, kind):
		return ""
	case len(trimmed) > n && strings.EqualFold(trimmed[len(trimmed)-n:], kind):
		trimmed = trimmed[:len(trimmed)-n]
	case len(trimmed) > n && strings.EqualFold(trimmed[:n], kind):
		trimmed = trimmed[n:]
	}

	return strings.Trim(trimmed, " -–—|:"+separatorRune)
}
