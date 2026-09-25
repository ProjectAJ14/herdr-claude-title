package main

import (
	"context"
	"testing"

	"github.com/ProjectAJ14/herdr-claude-title/internal/config"
	"github.com/ProjectAJ14/herdr-claude-title/internal/session"
)

func settings() config.Settings {
	return config.Settings{
		TabMaxColumns: 30, BranchMaxColumns: 12, ShowTabNumber: true, NamePanes: true,
		NameWorkspaces: true, UserRenamesFile: "/tmp/renames.json", WorkspaceMaxColumns: 20,
	}
}

func TestNamersFollowTheSettings(t *testing.T) {
	n := namers(settings())
	if n.Panes == nil || n.Workspaces == nil {
		t.Fatalf("namers = %+v", n)
	}

	tab := session.Tab{Position: 2, Speaker: &session.Pane{ID: "p", Dir: "/work/api"}}
	if got := n.Tabs.NameTab(tab).Label; got != "2 · api" {
		t.Errorf("numbered tab = %q", got)
	}

	s := settings()
	s.ShowTabNumber, s.NamePanes, s.UserRenamesFile = false, false, ""
	n = namers(s)

	if n.Panes != nil || n.Workspaces != nil {
		t.Error("panes off, and workspaces need a renames file")
	}

	if got := n.Tabs.NameTab(tab).Label; got != "api" {
		t.Errorf("unnumbered tab = %q", got)
	}
}

func TestClaudeGetsTheRoomTheNumberLeaves(t *testing.T) {
	s := settings()
	if got := claudeTitleColumns(s); got != 25 {
		t.Errorf("with a number: %d, want 25", got)
	}

	s.ShowTabNumber = false
	if got := claudeTitleColumns(s); got != 30 {
		t.Errorf("without: %d", got)
	}

	s.ShowTabNumber, s.TabMaxColumns = true, 8
	if got := claudeTitleColumns(s); got != 8 {
		t.Errorf("a narrow bar keeps its whole width: %d", got)
	}
}

func TestEngineReportingAndStopWithoutASummarizer(t *testing.T) {
	if engineInUse(true) != config.EngineClaude || engineInUse(false) != config.EngineRules {
		t.Error("engine names")
	}

	_, cancel := context.WithCancel(context.Background())
	stopSummarizer(cancel, nil) // must not block or panic

	s := settings()
	s.NamingEngine = config.EngineRules

	if startSummarizer(context.Background(), s, nil, "") != nil {
		t.Error("the rules engine starts no summarizer")
	}
}
