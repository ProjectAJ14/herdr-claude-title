package summarizer

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rivo/uniseg"
)

// TestLiveClaude spends one real Haiku call; run with `make live-claude`.
func TestLiveClaude(t *testing.T) {
	if os.Getenv("LIVE_CLAUDE") == "" {
		t.Skip("set LIVE_CLAUDE=1 to call the real claude")
	}

	binary, err := FindClaude("")
	if err != nil {
		t.Fatal(err)
	}

	title, err := ClaudeCLI{
		Binary:  binary,
		Model:   "haiku",
		Timeout: time.Minute,
	}.Summarize(
		context.Background(),
		Request{
			FirstPrompt: "the login test in auth.spec.ts fails on CI about one run in five",
			RecentPrompts: []string{
				"it is a race with the token refresh",
				"add a retry with backoff to refreshToken()",
			},
			MaxColumns: 30,
		},
	)
	if err != nil || title == "" || uniseg.StringWidth(title) > 30 {
		t.Fatalf("title = %q (%d columns), err = %v", title, uniseg.StringWidth(title), err)
	}

	t.Logf("claude titled it %q", title)
}
