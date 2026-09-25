package poller

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/ProjectAJ14/herdr-claude-title/internal/checkout"
	"github.com/ProjectAJ14/herdr-claude-title/internal/herdr"
	"github.com/ProjectAJ14/herdr-claude-title/internal/session"
	"github.com/ProjectAJ14/herdr-claude-title/internal/transcript"
)

// Summarizer supplies Claude-written titles without blocking a poll.
type Summarizer interface {
	Title(sessionID string, conv transcript.Conversation) string
	Retain(sessionIDs []string)
}

// PaneReaderOptions turn reads off rather than shape them.
type PaneReaderOptions struct {
	ReadBranches bool
	Transcripts  *transcript.Reader // nil: transcripts are off
	Summarizer   Summarizer         // nil: the Claude engine is off
}

// paneReader fills in what a snapshot cannot say about a pane. It outlives
// a poll because what it remembers does (process reads, transcript offsets).
type paneReader struct {
	opts      PaneReaderOptions
	revisions *session.RevisionTracker
	log       *slog.Logger
}

func newPaneReader(opts PaneReaderOptions, revisions *session.RevisionTracker, log *slog.Logger) *paneReader {
	return &paneReader{opts: opts, revisions: revisions, log: log}
}

// startPoll opens one poll's reads and forgets sessions no pane holds.
func (r *paneReader) startPoll(panes []herdr.Pane) *pollReads {
	var sessionIDs []string

	for _, p := range panes {
		if p.AgentSession != nil {
			sessionIDs = append(sessionIDs, p.AgentSession.Value)
		}
	}

	if r.opts.Transcripts != nil {
		r.opts.Transcripts.Retain(sessionIDs)
	}

	if r.opts.Summarizer != nil {
		r.opts.Summarizer.Retain(sessionIDs)
	}

	return &pollReads{reader: r, filled: map[string]bool{}, checkouts: map[string]checkout.Checkout{}}
}

// pollReads memoises within one poll only, so nothing it holds goes stale:
// a pane is filled once, and a directory's checkout is read once however
// many tabs share it.
type pollReads struct {
	reader    *paneReader
	filled    map[string]bool
	checkouts map[string]checkout.Checkout
}

func (r *pollReads) fill(ctx context.Context, client herdr.Client, pane *session.Pane) {
	if pane == nil || r.filled[pane.ID] {
		return
	}

	r.filled[pane.ID] = true
	pane.ApplyProcesses(r.processes(ctx, client, pane.ID))

	// The transcript first: it says where the agent works, and that is where
	// the agent's branch is read.
	conv := r.conversation(ctx, pane)
	pane.ConversationTopic = conv.Topic()
	pane.AgentDir = conv.WorkingDir
	pane.Repo = r.checkout(ctx, pane.Dir)
	pane.AgentRepo = r.checkout(ctx, conv.WorkingDir)

	// Asked on every fill, caught up or not: the cached title must hold while
	// a poll lands mid-write, or the tab flickers to the rules and back.
	if s := r.reader.opts.Summarizer; s != nil && conv.PromptCount > 0 {
		id, _ := pane.AgentSession.SessionID(transcript.ClaudeAgent)
		pane.ClaudeSummary = s.Title(id, conv)
	}
}

// processes reuses the last read while the pane's revision holds and the
// read is recent; neither test is exact alone, which is why there are two.
func (r *pollReads) processes(ctx context.Context, client herdr.Client, paneID string) []herdr.Process {
	if cached, ok := r.reader.revisions.CachedProcesses(paneID); ok {
		return cached
	}

	processes, err := herdr.ForegroundProcesses(ctx, client, paneID)
	if err != nil {
		if herdr.ErrorCode(err) != herdr.CodePaneNotFound && ctx.Err() == nil {
			r.reader.log.Debug("could not read what a pane runs", "pane", paneID, "error", err)
		}

		return nil // a failed read is not remembered as an answer
	}

	r.reader.revisions.RememberProcesses(paneID, processes)

	return processes
}

// checkout skips the read when branches are off or the poll is spent:
// file reads take no context, so the only bound is not starting them.
func (r *pollReads) checkout(ctx context.Context, dir string) checkout.Checkout {
	if !r.reader.opts.ReadBranches || ctx.Err() != nil {
		return checkout.Checkout{}
	}

	c, known := r.checkouts[dir]
	if !known {
		c = checkout.Read(dir)
		r.checkouts[dir] = c
	}

	return c
}

func (r *pollReads) conversation(ctx context.Context, pane *session.Pane) transcript.Conversation {
	reader := r.reader.opts.Transcripts
	if reader == nil || ctx.Err() != nil {
		return transcript.Conversation{}
	}

	id, ok := pane.AgentSession.SessionID(transcript.ClaudeAgent)
	if !ok {
		return transcript.Conversation{}
	}

	return reader.Read(id, pane.Dir)
}

func itoa(n int) string { return strconv.Itoa(n) }
