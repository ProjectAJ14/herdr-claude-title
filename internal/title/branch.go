package title

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ProjectAJ14/herdr-claude-title/internal/checkout"
	"github.com/ProjectAJ14/herdr-claude-title/internal/session"
)

// DefaultBranchMaxColumns holds a tracker key, or a word and part of the next.
const DefaultBranchMaxColumns = 12

// trackerKey: two to six letters and two or more digits (MC-13675), which
// keeps hyphenated words and `utf-8` out.
var trackerKey = regexp.MustCompile(`(?i)\b[a-z]{2,6}-\d{2,6}\b`)

const branchWordBreaks = "-_./ "

// conventionalTrunks stand in for a default in a repository that records
// none (no remote). Compared exactly, as git refs are.
var conventionalTrunks = []string{"main", "master", "trunk"}

// branch qualifies the place with the branch checked out: the pane's own,
// or its agent's when the agent works in the same repository (a worktree
// never moves the pane's directory).
type branch struct{ maxColumns int }

func (branch) Name() string { return "branch" }
func (branch) Rank() int    { return RankBranch }

func (b branch) Contribute(p *session.Pane) (Parts, bool) {
	// The branch is read where ssh was launched and says nothing remote.
	if _, remote := sshArgs(p); remote || b.maxColumns <= 0 {
		return Parts{}, false
	}

	label := b.label(p.Repo)

	// Labelled before choosing, so an agent on the trunk brings nothing and a
	// detached one brings its hash, with no special case for either.
	if agentBelongsToPane(p) {
		if agent := b.label(p.AgentRepo); agent != "" {
			label = agent
		}
	}

	if label == "" {
		return Parts{}, false
	}

	return Parts{Branch: label}, true
}

// label is what a checkout says in a title: nothing on the trunk, the short
// hash when detached (where commits get lost), else the shortened branch.
func (b branch) label(c checkout.Checkout) string {
	if c.Branch == "" {
		return Sanitize(c.DetachedHash, 0)
	}

	if c.Branch == c.DefaultBranch || (c.DefaultBranch == "" && slices.Contains(conventionalTrunks, c.Branch)) {
		return ""
	}

	return shortenBranch(Sanitize(c.Branch, 0), b.maxColumns)
}

// agentBelongsToPane: the same repository, or — for a pane holding none —
// an agent directory inside the pane's. Containment is tested with Rel, not
// a prefix: `code-review` begins with `code` and is not inside it.
func agentBelongsToPane(p *session.Pane) bool {
	if p.AgentRepo.SameRepository(p.Repo) {
		return true
	}

	if !p.Repo.IsZero() {
		return false // a nested clone's branch must not take over
	}

	rel, err := filepath.Rel(p.Dir, p.AgentDir)

	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// shortenBranch leaves a fitting name whole (feat/oauth keeps its namespace);
// an over-long one yields its tracker key, else its last segment cut at a
// word break.
func shortenBranch(name string, maxColumns int) string {
	name = strings.Trim(name, branchWordBreaks)
	if name == "" || maxColumns <= 0 {
		return ""
	}

	if _, over := splitAtColumns(name, maxColumns); over == "" {
		return name
	}

	if key := trackerKey.FindString(name); key != "" {
		return strings.ToUpper(key) // atomic: the one value allowed past the limit
	}

	if cut := strings.LastIndex(name, "/"); cut >= 0 && cut+1 < len(name) {
		name = name[cut+1:]
	}

	head, rest := splitAtColumns(name, maxColumns)
	if rest == "" {
		return strings.Trim(name, branchWordBreaks)
	}

	// If the next character is already a break, head ends on a whole word.
	if next, _ := utf8.DecodeRuneInString(rest); !strings.ContainsRune(branchWordBreaks, next) {
		if cut := strings.LastIndexAny(head, branchWordBreaks); cut > 0 {
			head = head[:cut]
		}
	}

	return strings.Trim(head, branchWordBreaks)
}
