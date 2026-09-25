// Command herdr-claude-title is the Herdr plugin process. Started once by
// Herdr, it stays alive naming tabs, panes and workspace rows after the work
// in them. `herdr-claude-title restart` starts a fresh one in its place.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ProjectAJ14/herdr-claude-title/internal/config"
	"github.com/ProjectAJ14/herdr-claude-title/internal/herdr"
	"github.com/ProjectAJ14/herdr-claude-title/internal/ownership"
	"github.com/ProjectAJ14/herdr-claude-title/internal/poller"
	"github.com/ProjectAJ14/herdr-claude-title/internal/singleton"
	"github.com/ProjectAJ14/herdr-claude-title/internal/summarizer"
	"github.com/ProjectAJ14/herdr-claude-title/internal/title"
	"github.com/ProjectAJ14/herdr-claude-title/internal/transcript"
)

func main() {
	var err error

	switch {
	case len(os.Args) == 1:
		err = run()
	case len(os.Args) == 2 && os.Args[1] == "restart":
		err = restart()
	case len(os.Args) == 2 && os.Args[1] == "preview":
		err = preview()
	default:
		err = errors.New("usage: herdr-claude-title [restart | preview]")
	}

	if err != nil {
		slog.Error("claude title stopped", "error", err)
		os.Exit(1)
	}
}

func setup() (config.Settings, *slog.Logger, *herdr.SocketClient, error) {
	settings, warnings := config.Load()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: settings.LogLevel}))
	slog.SetDefault(log)

	for _, w := range warnings {
		log.Warn(w)
	}

	client, err := herdr.NewSocketClient()

	return settings, log, client, err
}

func run() error {
	settings, log, client, err := setup()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Take waits for the instance it displaces; ownership is loaded only
	// after, so the two never write the same file at once.
	claim, stayed, err := singleton.Take(
		ctx,
		singleton.ClaimPath(config.StateDir(), client.SocketPath()),
		poller.LeaveTimeout,
	)
	if err != nil {
		log.Warn("could not claim the session, so a restart will not end this instance", "error", err)
	}

	if stayed != 0 {
		log.Warn("the instance this one replaces is still running", "pid", stayed)
	}

	reader := poller.PaneReaderOptions{ReadBranches: settings.BranchMaxColumns > 0}
	if settings.ReadTranscripts {
		reader.Transcripts = transcript.NewReader(settings.ClaudeHomes...)
	}

	sum := startSummarizer(ctx, settings, log, config.ClaudeTitlesPath())
	if sum != nil {
		reader.Summarizer = sum
	}

	log.Info("starting claude title",
		"poll", settings.PollInterval, "tab_max_columns", settings.TabMaxColumns,
		"engine", engineInUse(reader.Summarizer != nil), "model", settings.ClaudeModel)

	poller.New(poller.Options{
		Interval:        settings.PollInterval,
		PreferAgentPane: settings.PreferAgentPane,
		Namers:          namers(settings),
		Owners:          ownership.Load(settings.UserRenamesFile),
		Reader:          reader,
		Instance:        claim,
		Log:             log,
	}).Run(ctx, client)

	stopSummarizer(stop, sum)

	return nil
}

// summarizerExitWait covers a claude child's kill plus its WaitDelay.
const summarizerExitWait = 6 * time.Second

// stopSummarizer cancels the worker and waits for it, so an in-flight
// claude child is killed rather than left spending quota after we exit.
func stopSummarizer(stop context.CancelFunc, sum *summarizer.Summarizer) {
	stop()

	if sum == nil {
		return
	}

	select {
	case <-sum.Stopped():
	case <-time.After(summarizerExitWait):
	}
}

// startSummarizer returns nil when the Claude engine is off or cannot run.
func startSummarizer(ctx context.Context, s config.Settings, log *slog.Logger, store string) *summarizer.Summarizer {
	if !s.UsesClaude() {
		return nil
	}

	binary, err := summarizer.FindClaude(s.ClaudeBinary)
	if err != nil {
		log.Warn(
			"claude not found, so only the rules name tabs; set "+config.Prefix+config.KeyClaudeBinary,
			"error",
			err,
		)
		return nil
	}

	sum := summarizer.New(
		summarizer.ClaudeCLI{Binary: binary, Model: s.ClaudeModel, Timeout: s.ClaudeTimeout},
		summarizer.Options{
			RetitleEveryPrompts: s.RetitleEveryPrompts,
			MaxColumns:          claudeTitleColumns(s),
			StorePath:           store,
		},
		log,
	)
	go sum.Run(ctx)

	return sum
}

// tabNumberColumns is what "10 · " takes in front of a tab title.
const tabNumberColumns = 5

// claudeTitleColumns is the room a Claude title gets: the tab's width less
// the `N · ` in front, so the words Claude chose are the words shown.
func claudeTitleColumns(s config.Settings) int {
	if s.ShowTabNumber && s.TabMaxColumns > 10 {
		return s.TabMaxColumns - tabNumberColumns
	}

	return s.TabMaxColumns
}

func namers(s config.Settings) poller.Namers {
	opts := title.Options{
		MaxColumns:          s.TabMaxColumns,
		BranchMaxColumns:    s.BranchMaxColumns,
		HideAgentName:       !s.ShowAgentName,
		WorkspaceRowIsParts: s.WritesWorkspaces(),
	}

	tabs := title.ForTabs(opts)
	n := poller.Namers{Tabs: tabs}

	if s.ShowTabNumber {
		n.Tabs = title.WithTabNumber(tabs, s.TabMaxColumns)
	}

	if s.NamePanes {
		n.Panes = tabs
	}

	if s.WritesWorkspaces() {
		opts.MaxColumns = s.WorkspaceMaxColumns
		n.Workspaces = title.ForWorkspaces(opts)
	}

	return n
}

func engineInUse(claude bool) string {
	if claude {
		return config.EngineClaude
	}

	return config.EngineRules
}

// restart starts a fresh instance and reports the outcome in the action's
// log and as a Herdr notification.
func restart() error {
	_, log, client, err := setup()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	pid, err := singleton.Restart(ctx, singleton.ClaimPath(config.StateDir(), client.SocketPath()), exe,
		poller.LeaveTimeout+poller.PollTimeout)

	heading, body := "Claude Title restarted", fmt.Sprintf("pid %d is naming the session", pid)
	if err != nil {
		heading, body = "Claude Title did not restart", err.Error()
	}

	if notice, notifyErr := herdr.Notify(ctx, client, heading, body); notifyErr != nil {
		log.Warn("notification failed", "error", notifyErr)
	} else if !notice.Shown {
		log.Debug("notification not shown", "reason", notice.Reason)
	}

	if err != nil {
		return err
	}

	log.Info(heading, "pid", pid)

	return nil
}
