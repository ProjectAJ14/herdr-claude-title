// Package summarizer asks Claude — through the user's own `claude -p` and
// subscription — for a title that says what a Claude Code session is about.
// It never blocks the poll loop: a poll reads the cached title, and a due
// session is queued for a single background worker. See CLAUDE.md here.
package summarizer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ProjectAJ14/herdr-claude-title/internal/transcript"
)

// Request is what one title is written from. The prompts are the user's
// own words and are untrusted: the runner must give Claude no tools.
type Request struct {
	CurrentTitle  string
	FirstPrompt   string
	RecentPrompts []string
	MaxColumns    int
}

// Runner writes one title. ClaudeCLI is the real one; tests use a fake.
type Runner interface {
	Summarize(ctx context.Context, req Request) (string, error)
}

// ErrUnavailable marks a runner that can never succeed on this machine (no
// claude binary). The summarizer stops asking and says so once.
var ErrUnavailable = errors.New("claude is unavailable")

// Options tune when a session is (re)titled.
type Options struct {
	// RetitleEveryPrompts: after the first title, ask again once this many
	// new prompts arrived, so the title follows the conversation.
	RetitleEveryPrompts int
	MaxColumns          int
	// StorePath keeps titles across restarts ("" = memory only).
	StorePath string
	// RetryAfterFailure spaces out attempts for a session whose last failed.
	RetryAfterFailure time.Duration
}

// queueSize bounds pending work; a full queue just means "ask next poll".
const queueSize = 16

// Summarizer caches one title per Claude session.
type Summarizer struct {
	runner Runner
	opts   Options
	log    *slog.Logger
	now    func() time.Time
	jobs   chan job

	stopped chan struct{} // closed when Run returns

	mu          sync.Mutex
	sessions    map[string]*record
	unavailable bool
}

type record struct {
	Title          string `json:"title"`
	TitledAtPrompt int    `json:"titled_at_prompt"`
	inFlight       bool
	retryAt        time.Time
}

type job struct {
	sessionID   string
	promptCount int
	req         Request
}

func New(runner Runner, opts Options, log *slog.Logger) *Summarizer {
	if opts.RetitleEveryPrompts <= 0 {
		opts.RetitleEveryPrompts = 5
	}

	if opts.RetryAfterFailure <= 0 {
		opts.RetryAfterFailure = 2 * time.Minute
	}

	return &Summarizer{
		runner:   runner,
		opts:     opts,
		log:      log,
		now:      time.Now,
		jobs:     make(chan job, queueSize),
		stopped:  make(chan struct{}),
		sessions: load(opts.StorePath),
	}
}

// Title returns the cached title for a session ("" until one is written)
// and queues a new one when it is due. It never waits on Claude.
func (s *Summarizer) Title(sessionID string, conv transcript.Conversation) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, known := s.sessions[sessionID]
	if !known {
		rec = &record{}
		s.sessions[sessionID] = rec
	}

	if s.due(rec, conv) {
		j := job{sessionID: sessionID, promptCount: conv.PromptCount, req: Request{
			CurrentTitle:  rec.Title,
			FirstPrompt:   conv.FirstPrompt,
			RecentPrompts: conv.RecentPrompts,
			MaxColumns:    s.opts.MaxColumns,
		}}

		select {
		case s.jobs <- j:
			rec.inFlight = true
		default: // queue full: the next poll asks again
		}
	}

	return rec.Title
}

// due: the first prompt arrived and there is no title yet, or enough new
// prompts arrived since the last one — never while a call is in flight, a
// recent failure is cooling off, or the transcript is only partly read (a
// title from a fraction of the prompts would be redone moments later).
func (s *Summarizer) due(rec *record, conv transcript.Conversation) bool {
	switch {
	case s.unavailable, rec.inFlight, !conv.CaughtUp, conv.PromptCount == 0, s.now().Before(rec.retryAt):
		return false
	case rec.Title == "":
		return true
	default:
		return conv.PromptCount-rec.TitledAtPrompt >= s.opts.RetitleEveryPrompts
	}
}

// Run is the single worker. One at a time keeps the subscription quota
// predictable: a burst of new sessions queues rather than fans out.
func (s *Summarizer) Run(ctx context.Context) {
	defer close(s.stopped)

	for {
		select {
		case <-ctx.Done():
			return
		case j := <-s.jobs:
			s.handle(ctx, j)
		}
	}
}

func (s *Summarizer) handle(ctx context.Context, j job) {
	started := s.now()
	title, err := s.runner.Summarize(ctx, j.req)
	title = tidy(title)

	s.mu.Lock()
	defer s.mu.Unlock()

	rec, known := s.sessions[j.sessionID]
	if !known {
		return
	}

	rec.inFlight = false

	switch {
	case errors.Is(err, ErrUnavailable):
		s.unavailable = true
		s.log.Warn("claude titles are off for this run", "error", err)
	case err == nil && !isTitle(title):
		err = fmt.Errorf("claude answered %q, which is not a title", title)
		fallthrough
	case err != nil:
		rec.retryAt = s.now().Add(s.opts.RetryAfterFailure)
		if ctx.Err() == nil {
			s.log.Warn(
				"claude title failed",
				"session",
				j.sessionID,
				"error",
				err,
				"retry_in",
				s.opts.RetryAfterFailure,
			)
		}
	default:
		rec.Title, rec.TitledAtPrompt = title, j.promptCount
		s.saveLocked()
		s.log.Info("claude titled a session", "session", j.sessionID, "title", title,
			"prompts", j.promptCount, "took", s.now().Sub(started).Round(time.Millisecond))
	}
}

// Stopped is closed once Run has returned, and with it any claude child —
// the process must not exit while one could outlive it.
func (s *Summarizer) Stopped() <-chan struct{} { return s.stopped }

// Pending counts sessions queued or being titled right now.
func (s *Summarizer) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0

	for _, rec := range s.sessions {
		if rec.inFlight {
			n++
		}
	}

	return n
}

// Retain forgets sessions no pane holds any more.
func (s *Summarizer) Retain(sessionIDs []string) {
	live := make(map[string]struct{}, len(sessionIDs))
	for _, id := range sessionIDs {
		live[id] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false

	// An in-flight record stays until its call returns: dropped now, a session
	// that reappears would get a fresh record and a duplicate call.
	maps.DeleteFunc(s.sessions, func(id string, rec *record) bool {
		_, live := live[id]
		drop := !live && !rec.inFlight
		changed = changed || (drop && rec.Title != "")

		return drop
	})

	if changed {
		s.saveLocked()
	}
}

// tidy trims what models add around a title despite being told not to.
func tidy(title string) string {
	title = strings.TrimSpace(title)
	title = strings.Trim(title, "\"'`“”‘’")

	return strings.TrimSpace(strings.TrimRight(title, ".!"))
}

// isTitle rejects what a sentence slipped through as: a title is a few words
// on one line, and the prompt asks for 2 to 5.
func isTitle(title string) bool {
	words := len(strings.Fields(title))
	return words > 0 && words <= maxTitleWords && !strings.ContainsAny(title, "\n\r")
}

const maxTitleWords = 6

func load(path string) map[string]*record {
	sessions := make(map[string]*record)
	if path == "" {
		return sessions
	}

	raw, err := os.ReadFile(path) //nolint:gosec // the plugin's own state file
	if err == nil {
		_ = json.Unmarshal(raw, &sessions) // unreadable: start empty
	}

	maps.DeleteFunc(sessions, func(_ string, rec *record) bool { return !isTitle(rec.Title) })

	return sessions
}

// saveLocked writes titled sessions through a temp file; the caller holds mu.
func (s *Summarizer) saveLocked() {
	if s.opts.StorePath == "" || os.MkdirAll(filepath.Dir(s.opts.StorePath), 0o700) != nil {
		return
	}

	titled := make(map[string]*record, len(s.sessions))
	for id, rec := range s.sessions {
		if rec.Title != "" {
			titled[id] = rec
		}
	}

	raw, err := json.MarshalIndent(titled, "", "  ")
	if err != nil {
		return
	}

	tmp := s.opts.StorePath + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil && os.Rename(tmp, s.opts.StorePath) != nil {
		_ = os.Remove(tmp)
	}
}
