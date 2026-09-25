package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/ProjectAJ14/herdr-claude-title/internal/herdr"
	"github.com/ProjectAJ14/herdr-claude-title/internal/ownership"
	"github.com/ProjectAJ14/herdr-claude-title/internal/poller"
	"github.com/ProjectAJ14/herdr-claude-title/internal/transcript"
)

// previewWait bounds how long preview waits for Claude to title sessions.
const previewWait = 3 * time.Minute

// preview prints what the plugin would name every tab, pane and row of the
// live session, Claude titles included, and renames nothing. It claims no
// session and writes no file (the user's renames are read, never written),
// so it is safe beside a running instance.
func preview() error {
	settings, log, client, err := setup()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	reader := poller.PaneReaderOptions{ReadBranches: settings.BranchMaxColumns > 0}
	if settings.ReadTranscripts {
		reader.Transcripts = transcript.NewReader(settings.ClaudeHomes...)
	}

	sum := startSummarizer(ctx, settings, log, "")
	if sum != nil {
		reader.Summarizer = sum
	}

	wanted := map[string]string{}
	dry := herdr.DryRun{Inner: client, Renamed: func(kind, id, label string) {
		wanted[fmt.Sprintf("%-9s %s", kind, id)] = label
	}}

	// A fresh poller per pass: renames never land in a dry run, so a second
	// pass of the same poller would read its own unapplied names as moved by
	// the user. A first pass never claims anything.
	pass := func() {
		poller.New(poller.Options{
			PreferAgentPane: settings.PreferAgentPane,
			Namers:          namers(settings),
			Owners:          ownership.LoadReadOnly(settings.UserRenamesFile),
			Reader:          reader,
			Instance:        unclaimed{},
			Log:             log,
		}).Poll(ctx, dry)
	}

	pass()

	// Keep passing while a long transcript is still being caught up (one
	// chunk per pass) or Claude is still answering.
	busy := func() bool {
		return sum != nil && (sum.Pending() > 0 || (reader.Transcripts != nil && reader.Transcripts.Behind()))
	}

	for deadline := time.Now().Add(previewWait); busy() && time.Now().Before(deadline); {
		fmt.Fprintf(os.Stderr, "waiting for Claude to title %d session(s)...\n", sum.Pending())
		if sum.Pending() > 0 {
			time.Sleep(2 * time.Second) // Claude is answering; catch-up needs no wait
		}

		pass() // reads finished titles and queues what became due
	}

	pass()

	stopSummarizer(stop, sum)

	keys := make([]string, 0, len(wanted))
	for k := range wanted {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		fmt.Printf("%-24s → %s\n", k, wanted[k])
	}

	if len(keys) == 0 {
		fmt.Println("every label already matches what the plugin would write")
	}

	return nil
}

type unclaimed struct{}

func (unclaimed) Superseded() bool { return false }
func (unclaimed) MarkReady()       {}
