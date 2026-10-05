package summarizer

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ProjectAJ14/herdr-claude-title/internal/transcript"
)

const id = "s1"

type fakeRunner struct {
	mu    sync.Mutex
	calls []Request
	reply string
	err   error
}

func (f *fakeRunner) Summarize(_ context.Context, req Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, req)

	return f.reply, f.err
}

func newTest(t *testing.T, runner Runner, store string) *Summarizer {
	t.Helper()

	return New(runner, Options{RetitleEveryPrompts: 3, MaxColumns: 30, StorePath: store},
		slog.New(slog.DiscardHandler))
}

// drain runs whatever Title queued, synchronously.
func drain(s *Summarizer) {
	for {
		select {
		case j := <-s.jobs:
			s.handle(context.Background(), j)
		default:
			return
		}
	}
}

func conv(prompts int) transcript.Conversation {
	return transcript.Conversation{FirstPrompt: "fix login", PromptCount: prompts, CaughtUp: true}
}

func TestTitlesOnFirstPromptThenEveryN(t *testing.T) {
	runner := &fakeRunner{reply: `"Login test fix."`}
	s := newTest(t, runner, "")

	if got := s.Title(id, conv(0)); got != "" || len(s.jobs) != 0 {
		t.Fatal("no prompt yet: nothing to title")
	}

	s.Title(id, conv(1))
	s.Title(id, conv(1)) // in flight: not queued twice
	drain(s)

	if got := s.Title(id, conv(1)); got != "Login test fix" || len(runner.calls) != 1 {
		t.Fatalf("title = %q after %d calls", got, len(runner.calls))
	}

	s.Title(id, conv(3))
	drain(s)

	if len(runner.calls) != 1 {
		t.Fatal("two new prompts are below the cadence of three")
	}

	s.Title(id, conv(4))
	drain(s)

	if len(runner.calls) != 2 || runner.calls[1].CurrentTitle != "Login test fix" {
		t.Fatalf("calls = %+v, want a retitle carrying the current title", runner.calls)
	}
}

func TestFailureCoolsOffAndUnavailableStops(t *testing.T) {
	runner := &fakeRunner{err: errors.New("boom")}
	s := newTest(t, runner, "")
	now := time.Now()
	s.now = func() time.Time { return now }

	s.Title(id, conv(1))
	drain(s)
	s.Title(id, conv(1))
	drain(s)

	if len(runner.calls) != 1 {
		t.Fatal("a failed session must cool off before retrying")
	}

	now = now.Add(3 * time.Minute)
	runner.err = ErrUnavailable
	s.Title(id, conv(1))
	drain(s)
	s.Title("s2", conv(1))

	if len(runner.calls) != 2 || len(s.jobs) != 0 {
		t.Fatal("an unavailable claude must not be asked again")
	}
}

func TestTitlesSurviveARestartAndPrune(t *testing.T) {
	store := filepath.Join(t.TempDir(), "titles.json")
	s := newTest(t, &fakeRunner{reply: "Token refresh"}, store)
	s.Title(id, conv(2))
	drain(s)

	runner := &fakeRunner{reply: "unused"}
	again := newTest(t, runner, store)

	if got := again.Title(id, conv(2)); got != "Token refresh" || len(again.jobs) != 0 {
		t.Fatalf("after restart: %q, queued %d", got, len(again.jobs))
	}

	again.Retain(nil)

	if len(newTest(t, runner, store).sessions) != 0 {
		t.Error("a closed session's title should be pruned from disk")
	}
}

func TestParseAnswer(t *testing.T) {
	got, err := parseAnswer([]byte(`{"is_error":false,"result":"x","structured_output":{"title":"Retry logic"}}`))
	if err != nil || got != "Retry logic" {
		t.Errorf("got %q, %v", got, err)
	}

	if _, err := parseAnswer([]byte(`{"is_error":true,"result":"rate limited"}`)); err == nil {
		t.Error("is_error must be an error")
	}
}

func TestTheWorkerRunsQueuedCallsAndStopsWithItsContext(t *testing.T) {
	s := newTest(t, &fakeRunner{reply: "Worker title"}, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		s.Run(ctx)
		close(done)
	}()

	s.Title(id, conv(1))

	for deadline := time.Now().Add(5 * time.Second); s.Pending() > 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}

	if got := s.Title(id, conv(1)); got != "Worker title" {
		t.Fatalf("title = %q", got)
	}

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run must return once its context ends")
	}
}

func TestAnEmptyAnswerIsAFailureAndAClosedSessionIsDropped(t *testing.T) {
	runner := &fakeRunner{reply: "  \"\"  "}
	s := newTest(t, runner, "")

	s.Title(id, conv(1))
	drain(s)

	if got := s.Title(id, conv(1)); got != "" || s.sessions[id].retryAt.IsZero() {
		t.Fatal("an empty answer must cool off like a failure")
	}

	runner.reply = "Late answer"
	s.Title("s2", conv(1))
	s.Retain([]string{id}) // s2's pane closed while Claude was thinking

	if _, kept := s.sessions["s2"]; !kept {
		t.Fatal("an in-flight record must survive Retain, or a reappearing session is asked twice")
	}

	s.Title("s2", conv(1)) // it reappears before the answer

	if len(s.jobs) != 1 {
		t.Fatalf("queued %d jobs for s2, want the one already in flight", len(s.jobs))
	}

	drain(s)
	s.Retain([]string{id})

	if _, kept := s.sessions["s2"]; kept {
		t.Fatal("once its call returned, a closed session is dropped")
	}
}

func TestAFullQueueAsksAgainNextPoll(t *testing.T) {
	s := newTest(t, &fakeRunner{reply: "x"}, "")

	for i := range queueSize + 3 {
		s.Title(string(rune('a'+i)), conv(1))
	}

	if len(s.jobs) != queueSize || s.Pending() != queueSize {
		t.Fatalf("queued %d, pending %d", len(s.jobs), s.Pending())
	}

	drain(s)
	s.Title(string(rune('a'+queueSize)), conv(1))

	if len(s.jobs) != 1 {
		t.Fatal("a session skipped by a full queue must be queued on the next poll")
	}
}

func TestTidy(t *testing.T) {
	for in, want := range map[string]string{`"Retry logic."`: "Retry logic", " `OAuth!` ": "OAuth", "“Fix CI”": "Fix CI"} {
		if got := tidy(in); got != want {
			t.Errorf("tidy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAPartlyReadTranscriptKeepsTheCachedTitleButQueuesNothing(t *testing.T) {
	runner := &fakeRunner{reply: "Token refresh"}
	s := newTest(t, runner, "")

	s.Title(id, conv(1))
	drain(s)

	partial := conv(9)
	partial.CaughtUp = false

	if got := s.Title(id, partial); got != "Token refresh" || len(s.jobs) != 0 {
		t.Fatalf("mid-write poll: title %q, queued %d; the title must hold and nothing be asked", got, len(s.jobs))
	}

	if s.Title("fresh", partial); len(s.jobs) != 0 {
		t.Fatal("a session not yet caught up must not be titled from a fraction of its prompts")
	}
}

func TestStoppedClosesWhenRunReturns(t *testing.T) {
	s := newTest(t, &fakeRunner{}, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx)

	select {
	case <-s.Stopped():
	default:
		t.Fatal("Stopped must be closed after Run returns")
	}
}

func TestIsTitle(t *testing.T) {
	for title, want := range map[string]bool{
		"Retry logic": true,
		"":            false,
		"I appreciate the instruction, but I need more": false,
		"Fix\nthe bug": false,
	} {
		if got := isTitle(title); got != want {
			t.Errorf("isTitle(%q) = %v, want %v", title, got, want)
		}
	}
}
