package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const session = "7296ede5-8e78-4232-a6fb-8679ba11b8e9"

func human(text string) string {
	return fmt.Sprintf(`{"type":"user","origin":{"kind":"human"},"message":{"content":%q}}`, text)
}

func TestReadsPromptsTitlesAndDirIncrementally(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "projects", projectSlug("/work/api"))
	os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, session+".jsonl")

	lines := []string{
		`{"type":"user","message":{"content":"<command-name>/review</command-name>"}}`, // expansion, not typed
		human("<command-name>/code-review</command-name><command-args>spec.md\nmore</command-args>"),
		`{"type":"assistant","cwd":"/work/api/.claude/worktrees/x"}`,
		human("fix the flaky login test"),
		`{"type":"ai-title","aiTitle":"Login test fix"}`,
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	r := NewReader(home)

	c := r.Read(session, "/work/api")
	if c.FirstPrompt != "code-review spec.md" || c.PromptCount != 2 || c.AITitle != "Login test fix" ||
		c.WorkingDir != "/work/api/.claude/worktrees/x" || c.Topic() != "Login test fix" {
		t.Fatalf("got %+v", c)
	}

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	for i := range RecentPromptLimit + 2 {
		fmt.Fprintln(f, human(fmt.Sprintf("prompt %d", i)))
	}

	f.WriteString(human("half a line")) // no newline yet: must not be read
	f.Close()

	c = r.Read(session, "/work/api")
	if c.PromptCount != 2+RecentPromptLimit+2 || len(c.RecentPrompts) != RecentPromptLimit ||
		c.RecentPrompts[RecentPromptLimit-1] != fmt.Sprintf("prompt %d", RecentPromptLimit+1) {
		t.Fatalf("after append: %+v", c)
	}
}

func TestRefusesIDsThatAreNotUUIDs(t *testing.T) {
	if c := NewReader(t.TempDir()).Read("../../etc/passwd", ""); c.PromptCount != 0 {
		t.Fatal("a non-UUID id must be refused")
	}
}

func TestALongTranscriptIsCaughtUpNotSkipped(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "projects", "p")
	os.MkdirAll(dir, 0o700)

	var b strings.Builder
	b.WriteString(human("the first prompt") + "\n")
	b.WriteString(`{"type":"assistant","big":"` + strings.Repeat("x", maxReadBytes+10) + `"}` + "\n") // one giant line

	for b.Len() < 3*maxReadBytes {
		b.WriteString(`{"type":"assistant","message":{"content":"` + strings.Repeat("y", 1000) + `"}}` + "\n")
	}

	b.WriteString(human("the last prompt") + "\n")
	os.WriteFile(filepath.Join(dir, session+".jsonl"), []byte(b.String()), 0o600)

	r := NewReader(home)

	if r.Read(session, ""); !r.Behind() {
		t.Fatal("after one chunk of a long file the reader is behind")
	}

	var c Conversation
	for range 10 {
		if c = r.Read(session, ""); c.CaughtUp {
			break
		}
	}

	if !c.CaughtUp || c.PromptCount != 2 || c.FirstPrompt != "the first prompt" || r.Behind() {
		t.Fatalf("got %+v, behind %v", c, r.Behind())
	}
}

func TestBlocksRetainAndMissingFiles(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "projects", "somewhere-else")
	os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, session+".jsonl")
	os.WriteFile(path, []byte(
		`{"type":"user","origin":{"kind":"human"},"message":{"content":[{"type":"image"},{"type":"text","text":"review"},{"type":"text","text":"the diff"}]}}`+"\n"+
			`{"type":"user","origin":{"kind":"human"},"message":{"content":"<system-reminder>noise</system-reminder>"}}`+"\n",
	), 0o600)

	r := NewReader(home)

	// Found by the glob, although the pane's directory slug differs.
	c := r.Read(session, "/work/api")
	if c.FirstPrompt != "review the diff" || c.PromptCount != 1 || c.Topic() != "review the diff" {
		t.Fatalf("got %+v", c)
	}

	os.Remove(path)

	if c = r.Read(session, ""); c.FirstPrompt != "review the diff" {
		t.Fatal("what was read before the file went missing is kept")
	}

	r.Retain(nil)

	if c = r.Read(session, ""); c.PromptCount != 0 {
		t.Fatal("a retained-away session starts over")
	}

	if c := NewReader().Read(session, ""); c.PromptCount != 0 {
		t.Fatal("no homes, no answers")
	}
}

func TestARewrittenShorterFileStartsOver(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "projects", projectSlug("/w"))
	os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, session+".jsonl")

	os.WriteFile(path, []byte(human("first")+"\n"+human("second")+"\n"), 0o600)
	r := NewReader(home)
	r.Read(session, "/w")

	os.WriteFile(path, []byte(human("new")+"\n"), 0o600)

	if c := r.Read(session, "/w"); c.FirstPrompt != "new" || c.PromptCount != 1 {
		t.Fatalf("got %+v", c)
	}
}

func TestCutRunes(t *testing.T) {
	if got := cutRunes("日本語テキスト", 3); got != "日本語" {
		t.Errorf("got %q", got)
	}
}
