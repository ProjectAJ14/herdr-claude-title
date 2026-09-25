package checkout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte(content+"\n"), 0o600)
}

func TestReadBranchDetachedAndWorktree(t *testing.T) {
	repo := t.TempDir()
	git := filepath.Join(repo, ".git")
	write(t, filepath.Join(git, "HEAD"), "ref: refs/heads/feat/oauth")
	write(t, filepath.Join(git, "refs/remotes/origin/HEAD"), "ref: refs/remotes/origin/main")

	sub := filepath.Join(repo, "src", "deep")
	os.MkdirAll(sub, 0o700)

	got := Read(sub)
	if got.Branch != "feat/oauth" || got.DefaultBranch != "main" || got.SharedGitDir != git {
		t.Fatalf("branch checkout = %+v", got)
	}

	// A worktree: .git is a file pointing into the main repo's worktrees dir.
	wtGit := filepath.Join(git, "worktrees", "wt")
	write(t, filepath.Join(wtGit, "HEAD"), strings.Repeat("a", 40))
	write(t, filepath.Join(wtGit, "commondir"), "../..")

	wt := t.TempDir()
	write(t, filepath.Join(wt, ".git"), "gitdir: "+wtGit)

	got = Read(wt)
	if got.DetachedHash != "aaaaaaa" || !got.SameRepository(Read(repo)) {
		t.Fatalf("worktree checkout = %+v", got)
	}

	// A rebase keeps the branch it set aside.
	write(t, filepath.Join(wtGit, "rebase-merge", "head-name"), "refs/heads/fix/token")

	if got = Read(wt); got.Branch != "fix/token" {
		t.Fatalf("rebasing checkout = %+v", got)
	}

	if !Read(filepath.Join(repo, "gone")).IsZero() {
		t.Error("a deleted directory must not borrow its parent's repository")
	}
}

func TestJunkIsNotACheckout(t *testing.T) {
	junkHead := t.TempDir()
	write(t, filepath.Join(junkHead, ".git", "HEAD"), "not-a-hash")

	badLink := t.TempDir()
	write(t, filepath.Join(badLink, ".git"), "nonsense")

	for _, dir := range []string{"", "relative", junkHead, badLink} {
		if c := Read(dir); !c.IsZero() {
			t.Errorf("Read(%q) = %+v, want zero", dir, c)
		}
	}

	relative := t.TempDir()
	target := filepath.Join(relative, "real-git")
	write(t, filepath.Join(target, "HEAD"), "ref: refs/heads/dev")
	write(t, filepath.Join(relative, ".git"), "gitdir: real-git")

	if c := Read(relative); c.Branch != "dev" || c.SharedGitDir != target {
		t.Errorf("a relative gitdir resolves against its file: %+v", c)
	}
}
