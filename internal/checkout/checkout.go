// Package checkout reads what a git repository has checked out from the files
// under .git, never by running git: reading HEAD costs 0.02 ms against 12 ms
// for `git rev-parse`, on a poll whose whole snapshot costs 0.5 ms.
package checkout

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	shortHashLength = 7
	maxRefFileBytes = 4 << 10 // ref files hold one line; the path is pane-derived
	headBranchRef   = "ref: refs/heads/"
	originHeadRef   = "ref: refs/remotes/origin/"
)

// Checkout is what the repository holding a directory has checked out.
// The zero value means "not a repository", or nothing readable.
type Checkout struct {
	// Branch is the checked-out branch, or the one a rebase set aside.
	Branch string
	// DetachedHash is the short hash HEAD points at when no branch does.
	DetachedHash string
	// DefaultBranch is origin's HEAD, "" when the repository records none.
	DefaultBranch string
	// SharedGitDir holds the refs a repository shares with its worktrees, so
	// it is identical for a repository and every worktree of it.
	SharedGitDir string
}

// SameRepository reports that both checkouts share their refs. Compared
// cleaned, not symlink-resolved: a mismatch only disables a feature.
func (c Checkout) SameRepository(other Checkout) bool {
	return c.SharedGitDir == other.SharedGitDir
}

// IsZero reports that dir held no readable repository.
func (c Checkout) IsZero() bool { return c == Checkout{} }

// Read reports what the repository holding dir has checked out.
func Read(dir string) Checkout {
	gitDir, sharedDir, found := discover(dir)
	if !found {
		return Checkout{}
	}

	head, ok := firstLine(filepath.Join(gitDir, "HEAD"))
	if !ok {
		return Checkout{}
	}

	c := Checkout{DefaultBranch: defaultBranch(sharedDir), SharedGitDir: sharedDir}

	if branch, onBranch := strings.CutPrefix(head, headBranchRef); onBranch {
		c.Branch = branch
	} else if branch, rebasing := rebasingBranch(gitDir); rebasing {
		c.Branch = branch // a rebase detaches HEAD, but the user is still on this
	} else {
		c.DetachedHash = shortHash(head)
	}

	if c.Branch == "" && c.DetachedHash == "" {
		return Checkout{}
	}

	return c
}

// discover walks up from dir to the repository holding it. In a worktree or
// submodule .git is a file pointing elsewhere, so the two dirs differ.
func discover(dir string) (gitDir, sharedDir string, found bool) {
	if dir == "" || !filepath.IsAbs(dir) {
		return "", "", false
	}

	// A deleted directory would otherwise be answered by an ancestor's repo,
	// which is how a removed worktree used to show its parent's branch.
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", "", false
	}

	for dir = filepath.Clean(dir); ; {
		candidate := filepath.Join(dir, ".git")
		if info, err := os.Stat(candidate); err == nil {
			if info.IsDir() {
				return candidate, candidate, true
			}

			linked, ok := linkedGitDir(candidate)
			if !ok {
				return "", "", false
			}

			return linked, sharedDirOf(linked), true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}

		dir = parent
	}
}

// linkedGitDir follows a `gitdir:` file; a relative target is relative to it.
func linkedGitDir(gitFile string) (string, bool) {
	line, ok := firstLine(gitFile)
	if !ok {
		return "", false
	}

	target, cut := strings.CutPrefix(line, "gitdir:")
	if target = strings.TrimSpace(target); !cut || target == "" {
		return "", false
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(gitFile), target)
	}

	return filepath.Clean(target), true
}

func sharedDirOf(gitDir string) string {
	common, ok := firstLine(filepath.Join(gitDir, "commondir"))
	if !ok {
		return gitDir
	}

	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}

	return filepath.Clean(common)
}

// defaultBranch reads origin/HEAD, a symbolic ref pack-refs never packs.
func defaultBranch(sharedDir string) string {
	line, ok := firstLine(filepath.Join(sharedDir, "refs", "remotes", "origin", "HEAD"))
	if !ok || !strings.HasPrefix(line, originHeadRef) {
		return ""
	}

	return strings.TrimPrefix(line, originHeadRef)
}

// rebasingBranch is the branch a rebase in progress set aside, for either
// backend (merge is the default; apply is --apply and git am).
func rebasingBranch(gitDir string) (string, bool) {
	for _, backend := range []string{"rebase-merge", "rebase-apply"} {
		line, ok := firstLine(filepath.Join(gitDir, backend, "head-name"))
		if branch, cut := strings.CutPrefix(line, "refs/heads/"); ok && cut && branch != "" {
			return branch, true
		}
	}

	return "", false
}

func firstLine(path string) (string, bool) {
	file, err := os.Open(path) //nolint:gosec // a .git file this package located itself
	if err != nil {
		return "", false
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxRefFileBytes))
	if err != nil {
		return "", false
	}

	line, _, _ := strings.Cut(string(content), "\n")
	line = strings.TrimSpace(line)

	return line, line != ""
}

// shortHash abbreviates a whole sha1 or sha256 hash; anything else is refused
// so a HEAD holding junk cannot become a tab label.
func shortHash(head string) string {
	if len(head) != 40 && len(head) != 64 {
		return ""
	}

	for _, r := range head {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return ""
		}
	}

	return head[:shortHashLength]
}
