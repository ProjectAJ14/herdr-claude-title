// Package transcript reads what a Claude Code session is about from the JSONL
// transcript the agent appends to. Herdr says which session a pane holds; the
// transcript says what the user asked for in it.
package transcript

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// ClaudeAgent is Herdr's label for Claude Code, the only agent read here.
const ClaudeAgent = "claude"

const (
	// maxReadBytes bounds one poll's read. A long session first seen
	// mid-flight is caught up a chunk per poll rather than skipped, because
	// its early prompts are what a Claude title is written from.
	maxReadBytes = 2 << 20
	// searchRetry spaces out the glob across every project directory for a
	// session Herdr named before the agent wrote its first line.
	searchRetry     = 10 * time.Second
	firstPromptMax  = 200
	recentPromptMax = 400
	// RecentPromptLimit is how many of the latest prompts are kept for the
	// summarizer: enough to see where the conversation went, cheap to send.
	RecentPromptLimit = 6
)

// Conversation is what a session has said so far.
type Conversation struct {
	// AITitle is the title Claude Code generated itself; the last one wins.
	AITitle string
	// FirstPrompt is the first line the user typed; a slash command reads as
	// its arguments, else its name (`/wt fix login` → `fix login`, `/review` → `review`).
	FirstPrompt string
	// RecentPrompts are the latest prompts the user typed, oldest first.
	RecentPrompts []string
	// PromptCount counts the prompts the user typed that this reader has seen.
	PromptCount int
	// WorkingDir is where the agent last worked: an agent sent into a
	// worktree stays there while the pane's own directory does not move.
	WorkingDir string
	// CaughtUp: the whole file has been read, so the counts are final.
	CaughtUp bool
}

// Topic is what the conversation contributes to a deterministic title.
func (c Conversation) Topic() string {
	if c.AITitle != "" {
		return c.AITitle
	}

	return c.FirstPrompt
}

// Reader tails session transcripts, remembering how far each was read.
type Reader struct {
	mu       sync.Mutex
	homes    []string // Claude config homes, searched in order
	sessions map[string]*tail
	now      func() time.Time
}

type tail struct {
	path       string // "" while not found; searchedAt says when we last looked
	offset     int64
	midLine    bool // the last chunk ended inside a line longer than a chunk
	searchedAt time.Time
	conv       Conversation
}

// NewReader reads transcripts under homes. No homes answers nothing.
func NewReader(homes ...string) *Reader {
	return &Reader{homes: homes, sessions: make(map[string]*tail), now: time.Now}
}

// Read reports the conversation so far. paneDir is where the transcript is
// looked for first, since Claude Code files a session under its start dir.
func (r *Reader) Read(sessionID, paneDir string) Conversation {
	if len(r.homes) == 0 || !sessionIDPattern.MatchString(sessionID) {
		return Conversation{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	t, known := r.sessions[sessionID]
	if !known {
		t = &tail{}
		r.sessions[sessionID] = t
	}

	if t.path == "" && !r.find(t, sessionID, paneDir) {
		return t.conv.clone() // what was read before the file went missing still helps
	}

	t.readNew()

	return t.conv.clone()
}

// Behind reports a found transcript not yet read to its end.
func (r *Reader) Behind() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, t := range r.sessions {
		if t.path != "" && !t.conv.CaughtUp {
			return true
		}
	}

	return false
}

// Retain forgets every session but these, so a closed pane takes its state.
func (r *Reader) Retain(sessionIDs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	kept := make(map[string]*tail, len(sessionIDs))
	for _, id := range sessionIDs {
		if t, known := r.sessions[id]; known {
			kept[id] = t
		}
	}

	r.sessions = kept
}

// sessionIDPattern: the id arrives over the socket and becomes part of a
// path, so anything not shaped like a UUID is refused rather than cleaned.
var sessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

func (r *Reader) find(t *tail, sessionID, paneDir string) bool {
	now := r.now()
	if !t.searchedAt.IsZero() && now.Sub(t.searchedAt) < searchRetry {
		return false
	}

	t.searchedAt = now
	name := sessionID + ".jsonl"

	for _, home := range r.homes {
		if path, ok := findUnder(filepath.Join(home, "projects"), name, paneDir); ok {
			t.path = path
			return true
		}
	}

	return false
}

func findUnder(projects, name, paneDir string) (string, bool) {
	if paneDir != "" {
		candidate := filepath.Join(projects, projectSlug(paneDir), name)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, true
		}
	}

	matches, err := filepath.Glob(filepath.Join(projects, "*", name))
	if err != nil || len(matches) == 0 {
		return "", false
	}

	return matches[0], true
}

// projectSlug is Claude Code's project directory name: every character that
// is not an ASCII letter or digit becomes a dash.
func projectSlug(dir string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}

		return '-'
	}, dir)
}

// readNew reads up to maxReadBytes of whole lines appended since the last
// read. A shorter file is a different file, and starts over.
func (t *tail) readNew() {
	info, err := os.Stat(t.path)
	if err != nil {
		t.path, t.offset, t.midLine = "", 0, false // moved or deleted: search again later
		return
	}

	size := info.Size()
	if size < t.offset {
		t.offset, t.midLine, t.conv = 0, false, Conversation{}
	}

	defer func() { t.conv.CaughtUp = t.offset == size }()

	if size == t.offset {
		return
	}

	file, err := os.Open(t.path)
	if err != nil {
		return
	}
	defer file.Close()

	if _, err := file.Seek(t.offset, io.SeekStart); err != nil {
		return
	}

	content, err := io.ReadAll(io.LimitReader(file, maxReadBytes))
	if err != nil {
		return
	}

	end := bytes.LastIndexByte(content, '\n')
	if end < 0 {
		if len(content) == maxReadBytes {
			t.offset += int64(len(content)) // one giant line: skip it chunk by chunk
			t.midLine = true
		}

		return // otherwise the agent is mid-line; the rest arrives next poll
	}

	t.offset += int64(end) + 1
	lines := content[:end]

	if t.midLine {
		_, lines, _ = bytes.Cut(lines, []byte("\n")) // the tail of the skipped line
		t.midLine = false
	}

	t.conv.absorb(lines)
}

// line is one transcript entry; only what names the session is decoded.
type line struct {
	Type    string `json:"type"`
	CWD     string `json:"cwd"`
	AITitle string `json:"aiTitle"`
	Origin  *struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func (c *Conversation) absorb(lines []byte) {
	for raw := range bytes.SplitSeq(lines, []byte("\n")) {
		var l line
		if len(raw) == 0 || json.Unmarshal(raw, &l) != nil {
			continue
		}

		if l.CWD != "" {
			c.WorkingDir = l.CWD
		}

		switch {
		case l.Type == "ai-title" && l.AITitle != "":
			c.AITitle = l.AITitle
		case l.Type == "user" && l.Origin != nil && l.Origin.Kind == "human":
			// Only what the user typed: slash-command expansions and resume
			// caveats also arrive as user lines, without this origin.
			c.addPrompt(promptText(l.Message.Content))
		}
	}
}

func (c *Conversation) addPrompt(text string) {
	if text == "" {
		return
	}

	c.PromptCount++

	if c.FirstPrompt == "" {
		first, _, _ := strings.Cut(text, "\n")
		c.FirstPrompt = cutRunes(strings.TrimSpace(first), firstPromptMax)
	}

	c.RecentPrompts = append(c.RecentPrompts, cutRunes(strings.Join(strings.Fields(text), " "), recentPromptMax))
	if extra := len(c.RecentPrompts) - RecentPromptLimit; extra > 0 {
		c.RecentPrompts = slices.Delete(c.RecentPrompts, 0, extra)
	}
}

func (c Conversation) clone() Conversation {
	c.RecentPrompts = slices.Clone(c.RecentPrompts)
	return c
}

var (
	commandPattern = regexp.MustCompile(`<command-name>\s*/?([^<\s]+)\s*</command-name>`)
	argsPattern    = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
	// markupPattern: blocks Claude Code wraps around text the user did not type.
	markupPattern = regexp.MustCompile(`(?s)<[a-z][a-z-]*>.*?</[a-z][a-z-]*>`)
)

// promptText is what the user typed, with a slash command read as its first
// line of arguments: the name (`wt`) says how, not what, and is noise in a title.
func promptText(content json.RawMessage) string {
	text := contentText(content)

	if command := commandPattern.FindStringSubmatch(text); command != nil {
		if args := argsPattern.FindStringSubmatch(text); args != nil {
			if first, _, _ := strings.Cut(strings.TrimSpace(args[1]), "\n"); strings.TrimSpace(first) != "" {
				return strings.TrimSpace(first)
			}
		}

		return command[1]
	}

	return strings.TrimSpace(markupPattern.ReplaceAllString(text, " "))
}

// contentText reads message content: a string, or a list of blocks.
func contentText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}

	var parts []string

	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}

	return strings.Join(parts, " ")
}

func cutRunes(s string, limit int) string {
	if runes := []rune(s); len(runes) > limit {
		return string(runes[:limit])
	}

	return s
}
