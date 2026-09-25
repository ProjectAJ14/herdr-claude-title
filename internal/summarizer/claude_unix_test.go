//go:build !windows

package summarizer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeClaude writes a stand-in for the claude binary: it records its argv,
// stdin and environment beside itself, then runs body.
func fakeClaude(t *testing.T, body string) (binary, dir string) {
	t.Helper()

	dir = t.TempDir()
	binary = filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		`for a in "$@"; do printf '%s\n' "$a"; done > "` + dir + `/argv"` + "\n" +
		`cat > "` + dir + `/stdin"` + "\n" +
		`env > "` + dir + `/env"` + "\n" +
		`pwd > "` + dir + `/pwd"` + "\n" +
		body + "\n"

	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	return binary, dir
}

func read(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

var sample = Request{
	CurrentTitle:  "Login test",
	FirstPrompt:   "fix the flaky login test",
	RecentPrompts: []string{"it races the token refresh", "$(rm -rf ~) add a retry"},
	MaxColumns:    25,
}

func TestSummarizeSealsTheCallAndReadsStructuredOutput(t *testing.T) {
	binary, dir := fakeClaude(
		t,
		`echo '{"is_error":false,"result":"ignored","structured_output":{"title":"Token refresh retry"}}'`,
	)
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/herdr.sock")

	title, err := ClaudeCLI{
		Binary:  binary,
		Model:   "haiku",
		Timeout: 10 * time.Second,
	}.Summarize(
		context.Background(),
		sample,
	)
	if err != nil || title != "Token refresh retry" {
		t.Fatalf("title = %q, err = %v", title, err)
	}

	argv := strings.Split(strings.TrimSpace(read(t, filepath.Join(dir, "argv"))), "\n")
	joined := strings.Join(argv, " ")

	for _, want := range []string{"-p", "--safe-mode", "--no-session-persistence", "--model haiku", "--output-format json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv lacks %q: %q", want, argv)
		}
	}

	if !strings.Contains(joined, `"maxLength":25`) || !strings.Contains(joined, "at most 25 characters") {
		t.Errorf("the column budget must reach the schema and the prompt: %q", joined)
	}

	for i, a := range argv {
		if a == "--tools" && (i+1 >= len(argv) || argv[i+1] != "") {
			t.Errorf("--tools must be followed by an empty argument: %q", argv)
		}
	}

	if strings.Contains(joined, "flaky login") || strings.Contains(joined, "rm -rf") {
		t.Error("the user's prompts must never reach argv")
	}

	stdin := read(t, filepath.Join(dir, "stdin"))
	for _, want := range []string{"Current title: Login test", "fix the flaky login test", "1. it races", "2. $(rm -rf ~) add a retry"} {
		if !strings.Contains(stdin, want) {
			t.Errorf("stdin lacks %q:\n%s", want, stdin)
		}
	}

	if strings.Contains(read(t, filepath.Join(dir, "env")), "HERDR_") {
		t.Error("HERDR_* must be stripped from the child's environment")
	}

	if pwd := strings.TrimSpace(
		read(t, filepath.Join(dir, "pwd")),
	); !strings.HasPrefix(
		pwd,
		filepath.Clean(os.TempDir()),
	) &&
		!strings.HasPrefix(pwd, "/private"+filepath.Clean(os.TempDir())) {
		t.Errorf("the child runs in the temp dir, not %q", pwd)
	}
}

func TestSummarizeFailures(t *testing.T) {
	cases := []struct {
		name, body string
		timeout    time.Duration
	}{
		{"exit status with stderr", `echo 'rate limited' >&2; exit 3`, 10 * time.Second},
		{"answer reports an error", `echo '{"is_error":true,"result":"Credit balance too low"}'`, 10 * time.Second},
		{"answer is not json", `echo 'hello'`, 10 * time.Second},
		{"slower than the timeout", `sleep 5`, 300 * time.Millisecond},
	}

	for _, c := range cases {
		binary, _ := fakeClaude(t, c.body)

		_, err := ClaudeCLI{Binary: binary, Model: "haiku", Timeout: c.timeout}.Summarize(context.Background(), sample)
		if err == nil || errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: err = %v, want a retryable failure", c.name, err)
			continue
		}

		if c.name == "exit status with stderr" && !strings.Contains(err.Error(), "rate limited") {
			t.Errorf("stderr should be in the error: %v", err)
		}
	}

	_, err := ClaudeCLI{
		Binary:  filepath.Join(t.TempDir(), "gone"),
		Timeout: time.Second,
	}.Summarize(
		context.Background(),
		sample,
	)
	if err == nil {
		t.Error("a missing binary must fail")
	}
}

func TestPlainResultIsAFallback(t *testing.T) {
	if got, err := parseAnswer(
		[]byte(`{"is_error":false,"result":"Plain title"}`),
	); err != nil ||
		got != "Plain title" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestFindClaude(t *testing.T) {
	binary, dir := fakeClaude(t, "true")

	if got, err := FindClaude(binary); err != nil || got != binary {
		t.Errorf("configured path: %q, %v", got, err)
	}

	t.Setenv("PATH", dir)

	if got, err := FindClaude(""); err != nil || got != binary {
		t.Errorf("on PATH: %q, %v", got, err)
	}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	if _, err := FindClaude(""); err != nil && !errors.Is(err, ErrUnavailable) {
		t.Errorf("not found must be ErrUnavailable, got %v", err)
	}

	if _, err := FindClaude("/definitely/not/claude"); err == nil {
		t.Error("a configured path that does not exist must fail")
	}
}

func TestUserMessageWithoutATitleYet(t *testing.T) {
	msg := userMessage(Request{FirstPrompt: "only prompt"})
	if !strings.Contains(msg, "(none yet)") || strings.Contains(msg, "Latest requests") {
		t.Errorf("message:\n%s", msg)
	}
}
