// Package ownership tells a label the user wrote from one this plugin wrote,
// so a tab, pane or workspace the user renamed is never renamed back. There is
// no rename event to watch: a user rename is a label that moved between two
// polls to something the plugin neither set nor wanted. See CLAUDE.md here.
package ownership

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
)

// Verdict is what one poll makes of a label.
type Verdict int

const (
	// Rename: nobody else owns the label, so the plugin may write its own.
	Rename Verdict = iota
	// UserOwned: the user put the label there; leave it alone.
	UserOwned
	// Undecided: cannot be told apart yet (a workspace with no directory to
	// derive Herdr's default from) and must not be written over.
	Undecided
)

// Sighting is what one poll saw of one labelled thing.
type Sighting struct {
	ID     string
	Label  string // the label it carries now
	Wanted string // what the plugin would name it now
	// Unnamed is the label Herdr gives it while nobody has named it: a tab's
	// position, a workspace's directory basename, "" for a pane.
	Unnamed string
}

// Registry holds one Ledger per kind — Herdr numbers each kind apart — and
// persists user-owned labels so a restart does not rename them away.
// Only the poll goroutine uses it, so it holds no lock.
type Registry struct {
	Tabs       *Ledger
	Panes      *Ledger
	Workspaces *Ledger

	path string
	// pastFirstPoll: on the first poll nothing can be judged by movement.
	pastFirstPoll bool
}

// Ledger is what is known about one kind of labelled thing.
type Ledger struct {
	registry *Registry
	// judgeOnFirstSight: a workspace's unnamed label is its directory, so any
	// other label is its owner's on first sight. Tabs and panes need a move.
	judgeOnFirstSight bool
	seen              map[string]seenLabel
	awaitingDirLabel  map[string]struct{} // workspaces seen with no directory yet
	written           map[string]string   // last label written, for restarts
	userOwned         map[string]string   // id → the label it was claimed with
}

type seenLabel struct {
	label string
	// unanswered holds labels of renames sent without an answer. Herdr may
	// apply one seconds later; it is still the plugin's own label.
	unanswered map[string]struct{}
}

// storedLocks is the on-disk form.
type storedLocks struct {
	UserNamedTabs          map[string]string `json:"user_named_tabs"`
	UserNamedPanes         map[string]string `json:"user_named_panes"`
	UserNamedWorkspaces    map[string]string `json:"user_named_workspaces"`
	WorkspaceLabelsWritten map[string]string `json:"workspace_labels_written"`
}

// Load reads persisted ownership from path ("" keeps it in memory only).
// An unreadable file is an empty start, never a reason to refuse to run.
func Load(path string) *Registry {
	r := &Registry{path: path}
	r.Tabs = r.newLedger(false)
	r.Panes = r.newLedger(false)
	r.Workspaces = r.newLedger(true)

	raw, err := os.ReadFile(path) //nolint:gosec // a configured path, never terminal-derived
	if err != nil {
		return r
	}

	var stored storedLocks
	if json.Unmarshal(raw, &stored) != nil {
		return r
	}

	maps.Copy(r.Tabs.userOwned, stored.UserNamedTabs)
	maps.Copy(r.Panes.userOwned, stored.UserNamedPanes)
	maps.Copy(r.Workspaces.userOwned, stored.UserNamedWorkspaces)
	maps.Copy(r.Workspaces.written, stored.WorkspaceLabelsWritten)

	return r
}

// LoadReadOnly reads what Load reads but never writes it back — for preview,
// which must respect the user's claims without touching their file.
func LoadReadOnly(path string) *Registry {
	r := Load(path)
	r.path = ""

	return r
}

func (r *Registry) newLedger(judgeOnFirstSight bool) *Ledger {
	return &Ledger{
		registry:          r,
		judgeOnFirstSight: judgeOnFirstSight,
		seen:              make(map[string]seenLabel),
		awaitingDirLabel:  make(map[string]struct{}),
		written:           make(map[string]string),
		userOwned:         make(map[string]string),
	}
}

// FirstPollDone marks the end of a complete poll. After it, a thing never
// seen before is a thing that did not exist before.
func (r *Registry) FirstPollDone() { r.pastFirstPoll = true }

// IsUserOwned reports whether the user has claimed this id.
func (l *Ledger) IsUserOwned(id string) bool {
	_, owned := l.userOwned[id]
	return owned
}

// Judge records a sighting and says who owns the label.
func (l *Ledger) Judge(s Sighting) Verdict {
	prev, known := l.seen[s.ID]
	_, unanswered := prev.unanswered[s.Label]
	ours := unanswered || s.Label == s.Wanted

	// A watched workspace wearing a label that is ours is written down, so a
	// restart finds it recorded rather than reading it as the owner's.
	if l.judgeOnFirstSight && ours && known && s.Label != "" && l.written[s.ID] != s.Label {
		l.written[s.ID] = s.Label
		l.registry.save()
	}

	delete(prev.unanswered, s.Label)
	l.seen[s.ID] = seenLabel{label: s.Label, unanswered: prev.unanswered}

	if l.judgeOnFirstSight && !known {
		return l.judgeFirstSight(s)
	}

	return l.judgeByMovement(s, prev, known, ours)
}

// judgeByMovement is the tab and pane rule: a label that moved, not by us
// and not back to unnamed, is the user's.
func (l *Ledger) judgeByMovement(s Sighting, prev seenLabel, known, ours bool) Verdict {
	switch {
	case ours:
		return Rename
	case s.Label == "", s.Label == s.Unnamed:
		// Unnamed: a tab shows either its position or "" once a name is
		// cleared, which is also how a user hands one back.
		return Rename
	case known:
		if s.Label == prev.label {
			return Rename
		}
	case !l.registry.pastFirstPoll:
		return Rename // the first poll cannot tell anyone's label from nobody's
	}

	return l.claim(s)
}

// judgeFirstSight is the workspace rule: on first sight, the directory
// basename is Herdr's, our last written label is ours, anything else the
// owner's. A workspace created after the first poll is simply ours to name.
func (l *Ledger) judgeFirstSight(s Sighting) Verdict {
	if _, awaiting := l.awaitingDirLabel[s.ID]; l.registry.pastFirstPoll && !awaiting {
		return Rename
	}

	delete(l.awaitingDirLabel, s.ID)

	switch written, ok := l.written[s.ID]; {
	case ok && written == s.Label:
		return Rename
	case s.Unnamed == "":
		l.awaitingDirLabel[s.ID] = struct{}{}
		delete(l.seen, s.ID) // stays "first sight" until a directory shows
		return Undecided
	case s.Label == "", s.Label == s.Unnamed:
		return Rename
	}

	return l.claim(s)
}

func (l *Ledger) claim(s Sighting) Verdict {
	l.userOwned[s.ID] = s.Label
	l.registry.save()

	return UserOwned
}

// Renamed records a rename Herdr confirmed, so the next poll does not read
// the plugin's own work as the user's.
func (l *Ledger) Renamed(id, label string) {
	seen := l.seen[id]
	seen.label = label
	l.seen[id] = seen

	if l.judgeOnFirstSight {
		l.written[id] = label
		l.registry.save()
	}
}

// RenameUnanswered records a rename sent but not answered. Only a request
// that was actually sent belongs here; a refused dial never reached Herdr.
func (l *Ledger) RenameUnanswered(id, label string) {
	seen := l.seen[id]
	if seen.unanswered == nil {
		seen.unanswered = make(map[string]struct{})
	}

	seen.unanswered[label] = struct{}{}
	l.seen[id] = seen
}

// Prune forgets what the session no longer holds and releases a claim whose
// owner now carries a different label. Herdr reuses ids across sessions, so
// the label is what makes a reloaded claim safe — and clearing a name is how
// a user hands a thing back.
func (l *Ledger) Prune(liveLabels map[string]string) {
	changed := false

	for id, label := range l.userOwned {
		if current, alive := liveLabels[id]; !alive || current != label {
			delete(l.userOwned, id)

			changed = true
		}
	}

	for id := range l.written {
		if _, alive := liveLabels[id]; !alive {
			delete(l.written, id)

			changed = true
		}
	}

	maps.DeleteFunc(l.seen, func(id string, _ seenLabel) bool { return !has(liveLabels, id) })
	maps.DeleteFunc(l.awaitingDirLabel, func(id string, _ struct{}) bool { return !has(liveLabels, id) })

	if changed {
		l.registry.save()
	}
}

func has(m map[string]string, id string) bool {
	_, ok := m[id]
	return ok
}

// save writes through a temp file and a rename, so a crash cannot leave a
// half-written file. Failure is silent: persistence is a convenience.
func (r *Registry) save() {
	if r.path == "" || os.MkdirAll(filepath.Dir(r.path), 0o700) != nil {
		return
	}

	raw, err := json.MarshalIndent(storedLocks{
		UserNamedTabs:          r.Tabs.userOwned,
		UserNamedPanes:         r.Panes.userOwned,
		UserNamedWorkspaces:    r.Workspaces.userOwned,
		WorkspaceLabelsWritten: r.Workspaces.written,
	}, "", "  ")
	if err != nil {
		return
	}

	tmp := r.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) != nil {
		return
	}

	if os.Rename(tmp, r.path) != nil {
		_ = os.Remove(tmp)
	}
}
