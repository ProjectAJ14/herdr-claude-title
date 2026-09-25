package session

import (
	"time"

	"github.com/ProjectAJ14/herdr-claude-title/internal/herdr"
)

// processReuseLimit caps how long a process read is reused. A revision alone
// cannot bound it: measured, it moved for only four of nine process changes.
const processReuseLimit = 2 * time.Second

// RevisionTracker carries what a snapshot cannot say across polls: when each
// pane last changed, and what it was running when last asked. Only the poll
// goroutine touches it, so it holds no lock.
type RevisionTracker struct {
	panes map[string]paneRecord
	now   func() time.Time
}

type paneRecord struct {
	revision  uint64
	changedAt time.Time
	processes []herdr.Process
	readAt    time.Time // zero: no read since the revision moved
}

func NewRevisionTracker() *RevisionTracker {
	return &RevisionTracker{panes: make(map[string]paneRecord), now: time.Now}
}

// Observe records one poll's revisions. Any difference counts as a change —
// a revision going backwards is a new pane wearing a recycled id — and
// panes the session no longer holds are forgotten.
func (t *RevisionTracker) Observe(panes []herdr.Pane) {
	now := t.now()
	next := make(map[string]paneRecord, len(panes))

	for _, pane := range panes {
		prev, known := t.panes[pane.ID]
		if !known || prev.revision != pane.Revision {
			prev = paneRecord{revision: pane.Revision, changedAt: now}
		}

		next[pane.ID] = prev
	}

	t.panes = next
}

// LastChanged is when the pane's revision last moved; zero if never seen.
func (t *RevisionTracker) LastChanged(paneID string) time.Time {
	return t.panes[paneID].changedAt
}

// CachedProcesses returns the last process read while it is still trusted.
func (t *RevisionTracker) CachedProcesses(paneID string) ([]herdr.Process, bool) {
	rec := t.panes[paneID]
	if rec.readAt.IsZero() || t.now().Sub(rec.readAt) >= processReuseLimit {
		return nil, false
	}

	return rec.processes, true
}

// RememberProcesses stores a read; a pane that has since gone stays gone.
func (t *RevisionTracker) RememberProcesses(paneID string, processes []herdr.Process) {
	rec, known := t.panes[paneID]
	if !known {
		return
	}

	rec.processes, rec.readAt = processes, t.now()
	t.panes[paneID] = rec
}
