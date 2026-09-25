// Package session turns a Herdr snapshot into the tabs, panes and workspaces
// the title package names. Each poll builds these and throws them away.
package session

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ProjectAJ14/herdr-claude-title/internal/checkout"
	"github.com/ProjectAJ14/herdr-claude-title/internal/herdr"
)

// Pane is one pane as a poll read it. The snapshot fills the first block;
// the poller's reads fill the rest for the panes worth reading.
type Pane struct {
	ID    string
	Label string // "" while unnamed
	// Dir is the foreground process's own directory once read, the
	// snapshot's guess until then.
	Dir              string
	TerminalTitle    string // Herdr's cleaned title
	TerminalTitleRaw string // fallback, still carrying escapes
	Agent            string // the agent Herdr recognised, "" if none
	AgentDisplayName string
	AgentTitle       string
	AgentStatus      string
	AgentSession     *herdr.AgentSession
	Focused          bool
	LastChanged      time.Time // when a poll last saw the revision move

	Processes []Process
	// Repo is the checkout holding Dir; AgentRepo the one the agent works in.
	Repo      checkout.Checkout
	AgentRepo checkout.Checkout
	AgentDir  string
	// ConversationTopic is what the agent's transcript says (AI title or
	// first prompt); ClaudeSummary is the Claude-written title, when ready.
	ConversationTopic string
	ClaudeSummary     string
}

// Process is one command running in a pane. Args includes the program name.
type Process struct {
	Name string
	Args []string
}

// PaneFrom builds a pane from its snapshot entry.
func PaneFrom(info herdr.Pane, lastChanged time.Time) *Pane {
	dir := info.ForegroundCWD
	if dir == "" {
		dir = info.CWD // a subshell leaves cwd behind, so it comes second
	}

	return &Pane{
		ID:               info.ID,
		Label:            info.Label,
		Dir:              cleanDir(dir),
		TerminalTitle:    info.TerminalTitleStripped,
		TerminalTitleRaw: info.TerminalTitle,
		Agent:            info.Agent,
		AgentDisplayName: info.DisplayAgent,
		AgentTitle:       info.AgentTitle,
		AgentStatus:      info.AgentStatus,
		AgentSession:     info.AgentSession,
		Focused:          info.Focused,
		LastChanged:      lastChanged,
	}
}

// ApplyProcesses records a process read: the last process is the pane's own
// foreground one, and its directory is the pane's.
func (p *Pane) ApplyProcesses(processes []herdr.Process) {
	p.Processes = nil
	for _, proc := range processes {
		p.Processes = append(p.Processes, Process{Name: withoutExe(proc.Name), Args: proc.Argv})
	}

	if last := len(processes) - 1; last >= 0 && processes[last].CWD != "" {
		p.Dir = cleanDir(processes[last].CWD)
	}
}

// Foreground is the process the pane runs rather than one it started.
func (p *Pane) Foreground() (Process, bool) {
	if len(p.Processes) == 0 {
		return Process{}, false
	}

	return p.Processes[len(p.Processes)-1], true
}

func (p *Pane) HasAgent() bool { return p != nil && p.Agent != "" }

// AgentBusy reports an agent working or waiting on the user.
func (p *Pane) AgentBusy() bool {
	return p.HasAgent() && (p.AgentStatus == herdr.AgentWorking || p.AgentStatus == herdr.AgentBlocked)
}

// cleanDir drops the trailing separator Windows adds; "" stays "".
func cleanDir(dir string) string {
	if dir == "" {
		return ""
	}

	return filepath.Clean(dir)
}

// withoutExe: `pwsh.exe` is the shell every other platform calls pwsh.
func withoutExe(name string) string {
	const exe = ".exe"
	if len(name) > len(exe) && strings.EqualFold(name[len(name)-len(exe):], exe) {
		return name[:len(name)-len(exe)]
	}

	return name
}

// Tab is one tab with its panes.
type Tab struct {
	ID             string
	Label          string
	WorkspaceLabel string
	// Position counts from one within the workspace: the key that switches
	// to the tab, and the label Herdr gives it while unnamed.
	Position int
	Panes    []*Pane // ordered by ID so identical sessions name identically
	// Speaker is the pane the tab is named after, nil only for a paneless
	// tab. Picked once so what is read and what is named cannot differ.
	Speaker *Pane
}

// TabFrom assembles a tab and picks its speaker. preferAgent makes a pane
// holding an agent, in any state, outrank focus.
func TabFrom(info herdr.Tab, workspaceLabel string, position int, panes []*Pane, preferAgent bool) Tab {
	ordered := slices.Clone(panes)
	slices.SortFunc(ordered, func(a, b *Pane) int { return strings.Compare(a.ID, b.ID) })

	rules := speakerRules
	if preferAgent {
		rules = agentFirstRules
	}

	return Tab{
		ID:             info.ID,
		Label:          info.Label,
		WorkspaceLabel: workspaceLabel,
		Position:       position,
		Panes:          ordered,
		Speaker:        pickSpeaker(ordered, rules),
	}
}

type paneRule func(*Pane) bool

// speakerRules: the focused pane, then a busy agent, then any pane — each
// taking the most recently changed pane it accepts.
var (
	speakerRules = []paneRule{
		func(p *Pane) bool { return p.Focused },
		(*Pane).AgentBusy,
		func(*Pane) bool { return true },
	}
	agentFirstRules = append([]paneRule{(*Pane).HasAgent}, speakerRules...)
)

func pickSpeaker(panes []*Pane, rules []paneRule) *Pane {
	for _, accepts := range rules {
		var best *Pane

		for _, p := range panes {
			// Strictly after, so a tie keeps the lowest ID.
			if accepts(p) && (best == nil || p.LastChanged.After(best.LastChanged)) {
				best = p
			}
		}

		if best != nil {
			return best
		}
	}

	return nil
}

// Workspace is the row above a workspace's tabs.
type Workspace struct {
	ID    string
	Label string
	// DirLabel is what Herdr calls an unrenamed workspace: the basename of
	// the directory it holds. "" when no pane says where that is.
	DirLabel string
	// Speaker is the speaker of the workspace's first tab.
	Speaker *Pane
}

func WorkspaceFrom(info herdr.Workspace, speaker *Pane) Workspace {
	ws := Workspace{ID: info.ID, Label: info.Label, Speaker: speaker}
	if speaker != nil {
		ws.DirLabel = dirBase(speaker.Dir)
	}

	return ws
}

// dirBase is the basename Herdr derives a workspace label from; a relative
// path or a root yields "" (no default to compare against).
func dirBase(dir string) string {
	clean := cleanDir(dir)
	if clean == "" || !filepath.IsAbs(clean) || clean == filepath.VolumeName(clean)+string(filepath.Separator) {
		return ""
	}

	return filepath.Base(clean)
}
