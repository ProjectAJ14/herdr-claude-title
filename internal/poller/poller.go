// Package poller is the plugin's loop: read the whole session, decide every
// label from what was just read, rename what differs, repeat. It polls
// rather than subscribes — see CLAUDE.md here for why and what it costs.
package poller

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ProjectAJ14/herdr-claude-title/internal/herdr"
	"github.com/ProjectAJ14/herdr-claude-title/internal/ownership"
	"github.com/ProjectAJ14/herdr-claude-title/internal/session"
	"github.com/ProjectAJ14/herdr-claude-title/internal/title"
)

// PollTimeout bounds one poll: the snapshot and every rename it decides.
const PollTimeout = 5 * time.Second

// LeaveTimeout is how long a displaced instance gets to notice and exit: a
// poll past its deadline plus up to five seconds of its own interval.
const LeaveTimeout = PollTimeout + 5*time.Second

// Instance is this process's claim on the session.
type Instance interface {
	Superseded() bool
	MarkReady()
}

// Namers decide labels. Panes or Workspaces nil leaves those labels alone.
type Namers struct {
	Tabs       title.TabNamer
	Panes      *title.Namer
	Workspaces *title.Namer
}

// Poller is one run of the loop.
type Poller struct {
	interval    time.Duration
	preferAgent bool
	namers      Namers
	log         *slog.Logger
	revisions   *session.RevisionTracker
	owners      *ownership.Registry
	reader      *paneReader
	instance    Instance
	failures    failureRun
	// server is the Herdr this instance answers to, learned on the first
	// readable poll. A different one means its own instance is starting.
	server string
}

// Options configure a Poller.
type Options struct {
	Interval        time.Duration
	PreferAgentPane bool
	Namers          Namers
	Owners          *ownership.Registry
	Reader          PaneReaderOptions
	Instance        Instance
	Log             *slog.Logger
}

func New(opts Options) *Poller {
	revisions := session.NewRevisionTracker()

	return &Poller{
		interval:    opts.Interval,
		preferAgent: opts.PreferAgentPane,
		namers:      opts.Namers,
		log:         opts.Log,
		revisions:   revisions,
		owners:      opts.Owners,
		reader:      newPaneReader(opts.Reader, revisions, opts.Log),
		instance:    opts.Instance,
	}
}

// Run polls until ctx ends or a successor takes over.
func (p *Poller) Run(ctx context.Context, client herdr.Client) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for p.Poll(ctx, client) { // the first poll runs at once, not after a tick
		select {
		case <-ctx.Done():
			p.log.Info("shutting down")
			return
		case <-ticker.C:
		}
	}
}

// Poll is one turn, reporting whether there should be another. No failure
// is fatal: Herdr's socket can lag the process it just launched.
func (p *Poller) Poll(ctx context.Context, client herdr.Client) bool {
	if reason := p.successor(client); reason != "" {
		p.log.Info(reason + ", leaving")
		return false
	}

	err := p.readAndRename(ctx, client)

	switch {
	case ctx.Err() != nil:
	case err != nil:
		if run := p.failures.failed(); run > 0 {
			p.log.Warn("poll failed", "error", err, "in_a_row", run)
		}
	default:
		p.instance.MarkReady()

		if missed := p.failures.recovered(); missed > 0 {
			p.log.Info("the session is answering again", "polls_missed", missed)
		}
	}

	return true
}

// successor says why this instance should leave, or "".
func (p *Poller) successor(client herdr.Client) string {
	if p.instance.Superseded() {
		return "a newer instance has claimed the session"
	}

	switch current := client.ServerIdentity(); {
	case current == "": // Herdr is down or late: not a successor
	case p.server == "":
		p.server = current
	case current != p.server:
		return "another Herdr server holds the socket and starts its own instance"
	}

	return ""
}

func (p *Poller) readAndRename(ctx context.Context, client herdr.Client) error {
	ctx, cancel := context.WithTimeout(ctx, PollTimeout)
	defer cancel()

	snap, err := herdr.ReadSnapshot(ctx, client)
	if err != nil {
		return err
	}

	p.revisions.Observe(snap.Panes)
	// From the snapshot, because this decides which tabs are skipped unread.
	p.owners.Tabs.Prune(tabLabels(snap.Tabs))
	p.owners.Panes.Prune(paneLabels(snap.Panes))
	p.owners.Workspaces.Prune(workspaceLabels(snap.Workspaces))

	tabs := p.assembleTabs(snap)
	reads := p.reader.startPoll(snap.Panes)

	for _, tab := range tabs {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		p.nameTab(ctx, client, reads, tab)

		if p.namers.Panes != nil {
			p.namePanes(ctx, client, reads, tab)
		}
	}

	// Last, so a poll cut short gives up a row rather than a tab.
	if p.namers.Workspaces != nil {
		p.nameWorkspaces(ctx, client, reads, snap, tabs)

		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	// Only after a complete poll: one cut short would leave the tabs it
	// missed looking brand new and already named by the user.
	p.owners.FirstPollDone()

	return nil
}

// assembleTabs builds tabs from the snapshot alone; reading is spent later,
// on the panes that earn it.
func (p *Poller) assembleTabs(snap herdr.Snapshot) []session.Tab {
	wsLabels := workspaceLabels(snap.Workspaces)

	panesByTab := make(map[string][]*session.Pane, len(snap.Tabs))
	for _, pane := range snap.Panes {
		panesByTab[pane.TabID] = append(
			panesByTab[pane.TabID],
			session.PaneFrom(pane, p.revisions.LastChanged(pane.ID)),
		)
	}

	// Tabs arrive in display order, so a running count is each one's position.
	positions := make(map[string]int, len(snap.Workspaces))
	tabs := make([]session.Tab, 0, len(snap.Tabs))

	for _, info := range snap.Tabs {
		positions[info.WorkspaceID]++
		tabs = append(tabs, session.TabFrom(info, wsLabels[info.WorkspaceID], positions[info.WorkspaceID],
			panesByTab[info.ID], p.preferAgent))
	}

	return tabs
}

func (p *Poller) nameTab(ctx context.Context, client herdr.Client, reads *pollReads, tab session.Tab) {
	if p.owners.Tabs.IsUserOwned(tab.ID) {
		return // a claimed tab costs nothing to skip
	}

	reads.fill(ctx, client, tab.Speaker)

	d := p.namers.Tabs.NameTab(tab)
	p.apply(ctx, client, tabTarget, p.owners.Tabs,
		ownership.Sighting{ID: tab.ID, Label: tab.Label, Wanted: d.Label, Unnamed: itoa(tab.Position)}, d)
}

// namePanes labels every pane for Herdr's goto panel, against the tab's own
// speaker — read even when the tab is user-owned.
func (p *Poller) namePanes(ctx context.Context, client herdr.Client, reads *pollReads, tab session.Tab) {
	reads.fill(ctx, client, tab.Speaker)

	for _, pane := range tab.Panes {
		if !p.owners.Panes.IsUserOwned(pane.ID) {
			reads.fill(ctx, client, pane)
		}
	}

	for i, d := range p.namers.Panes.NamePanes(tab) {
		pane := tab.Panes[i]
		if ctx.Err() != nil || p.owners.Panes.IsUserOwned(pane.ID) {
			continue
		}

		p.apply(ctx, client, paneTarget, p.owners.Panes,
			ownership.Sighting{ID: pane.ID, Label: pane.Label, Wanted: d.Label}, d)
	}
}

// nameWorkspaces judges every row and names those holding exactly one tab:
// with more there is nothing one row could say for all of them.
func (p *Poller) nameWorkspaces(ctx context.Context, client herdr.Client, reads *pollReads,
	snap herdr.Snapshot, tabs []session.Tab,
) {
	tabCount := make(map[string]int)
	firstTab := make(map[string]string)

	for _, t := range snap.Tabs {
		if tabCount[t.WorkspaceID]++; tabCount[t.WorkspaceID] == 1 {
			firstTab[t.WorkspaceID] = t.ID
		}
	}

	speakers := make(map[string]*session.Pane, len(tabs))
	for _, t := range tabs {
		speakers[t.ID] = t.Speaker
	}

	for _, info := range snap.Workspaces {
		if ctx.Err() != nil {
			return
		}

		speaker := speakers[firstTab[info.ID]]
		reads.fill(ctx, client, speaker) // before judging: the snapshot's dir is a guess

		ws := session.WorkspaceFrom(info, speaker)
		sighting := ownership.Sighting{ID: ws.ID, Label: ws.Label, Unnamed: ws.DirLabel}

		// Judged even when not named: a two-tab workspace today may hold one
		// tomorrow, and a reloaded claim needs a sighting to see the next move.
		if tabCount[ws.ID] != 1 || p.owners.Workspaces.IsUserOwned(ws.ID) {
			p.owners.Workspaces.Judge(sighting)
			continue
		}

		d := p.namers.Workspaces.NameWorkspace(ws)
		sighting.Wanted = d.Label
		p.apply(ctx, client, workspaceTarget, p.owners.Workspaces, sighting, d)
	}
}

// target is what differs between the three kinds of labelled thing.
type target struct {
	noun     string
	rename   func(context.Context, herdr.Client, string, string) error
	goneCode string // the thing closed between snapshot and rename
}

var (
	tabTarget       = target{"tab", herdr.RenameTab, herdr.CodeTabNotFound}
	paneTarget      = target{"pane", herdr.RenamePane, herdr.CodePaneNotFound}
	workspaceTarget = target{"workspace", herdr.RenameWorkspace, herdr.CodeWorkspaceNotFound}
)

// apply writes the decided label unless the user owns the current one.
// Nothing here cuts the poll short: the next poll decides again anyway.
func (p *Poller) apply(ctx context.Context, client herdr.Client, t target, ledger *ownership.Ledger,
	s ownership.Sighting, d title.Decision,
) {
	switch ledger.Judge(s) {
	case ownership.UserOwned:
		p.log.Info("leaving a "+t.noun+" the user renamed", "id", s.ID, "label", s.Label)
		return
	case ownership.Undecided:
		p.log.Debug("leaving a "+t.noun+" not yet judged", "id", s.ID, "label", s.Label)
		return
	case ownership.Rename:
	}

	if d.Label == "" || d.Label == s.Label {
		return // deduplication is what keeps a settled session silent
	}

	if err := t.rename(ctx, client, s.ID, d.Label); err != nil {
		switch {
		case herdr.ErrorCode(err) == t.goneCode:
			p.log.Debug(t.noun+" closed before it could be renamed", "id", s.ID)
			return
		case errors.Is(err, herdr.ErrUnanswered):
			ledger.RenameUnanswered(s.ID, d.Label) // Herdr may still apply it
		}

		p.log.Warn(t.noun+" rename failed", "id", s.ID, "label", d.Label, "error", err)

		return
	}

	ledger.Renamed(s.ID, d.Label) // before logging, so no later poll misreads it
	p.log.Info(t.noun+" renamed", "id", s.ID, "old", s.Label, "new", d.Label, "source", d.Source, "rank", d.Rank)
}

func tabLabels(tabs []herdr.Tab) map[string]string {
	m := make(map[string]string, len(tabs))
	for _, t := range tabs {
		m[t.ID] = t.Label
	}

	return m
}

func paneLabels(panes []herdr.Pane) map[string]string {
	m := make(map[string]string, len(panes))
	for _, p := range panes {
		m[p.ID] = p.Label
	}

	return m
}

func workspaceLabels(workspaces []herdr.Workspace) map[string]string {
	m := make(map[string]string, len(workspaces))
	for _, w := range workspaces {
		m[w.ID] = w.Label
	}

	return m
}

// failureRun logs a run of failed polls as it doubles: an hour of Herdr
// down is a dozen lines, not seven thousand.
type failureRun struct{ run, next int }

func (f *failureRun) failed() int {
	if f.run++; f.run < f.next {
		return 0
	}

	f.next = f.run * 2

	return f.run
}

func (f *failureRun) recovered() int {
	run := f.run
	*f = failureRun{}

	return run
}
