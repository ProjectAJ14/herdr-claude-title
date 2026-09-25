package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileEnvironmentAndBadValues(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	os.MkdirAll(filepath.Join(xdg, dirName), 0o700)
	os.WriteFile(filepath.Join(xdg, dirName, ConfigFileName), []byte(strings.Join([]string{
		"# comment",
		"export " + Prefix + "TAB_MAX_COLUMNS=40",
		Prefix + `CLAUDE_MODEL="sonnet" `,
		Prefix + "POLL_INTERVAL_MS=250 # faster",
		Prefix + "SHOW_TAB_NUMBER=maybe",
		"not a setting line",
		Prefix + "LOG_LEVEL=info",
	}, "\n")), 0o600)
	t.Setenv(Prefix+"LOG_LEVEL", "debug") // the environment wins

	s, warnings := Load()

	if s.TabMaxColumns != 40 || s.ClaudeModel != "sonnet" || s.PollInterval != 250*time.Millisecond ||
		s.LogLevel != slog.LevelDebug || !s.ShowTabNumber || s.NamingEngine != EngineClaude {
		t.Errorf("settings = %+v", s)
	}

	if len(warnings) != 2 {
		t.Errorf("warnings = %q, want the bad line and the bad boolean", warnings)
	}
}

func TestWorkspacesNeedAFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(Prefix+KeyNameWorkspaces, "true")
	t.Setenv(Prefix+KeyUserRenamesFile, "")

	if s, warnings := Load(); s.WritesWorkspaces() || len(warnings) != 1 {
		t.Errorf("writes=%v warnings=%q", s.WritesWorkspaces(), warnings)
	}
}

func TestChoicesHomesAndDerivedSwitches(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	extra := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "/claude/home")
	t.Setenv(
		Prefix+KeyExtraClaudeHomes,
		extra+"/"+string(os.PathListSeparator)+" "+string(os.PathListSeparator)+"/not/yet",
	)
	t.Setenv(Prefix+KeyNamingEngine, "GPT")
	t.Setenv(Prefix+KeyReadTranscripts, "false")
	t.Setenv(Prefix+KeyLogLevel, "loud")
	t.Setenv(Prefix+KeyPollIntervalMS, "0")

	s, warnings := Load()

	if len(s.ClaudeHomes) != 3 || s.ClaudeHomes[0] != "/claude/home" || s.ClaudeHomes[1] != extra ||
		s.ClaudeHomes[2] != "/not/yet" {
		t.Errorf("homes = %q", s.ClaudeHomes)
	}

	if s.NamingEngine != EngineClaude || s.UsesClaude() || s.LogLevel != slog.LevelInfo ||
		s.PollInterval != 500*time.Millisecond {
		t.Errorf("settings = %+v", s)
	}

	// engine, log level, poll interval, the missing home, and claude-without-transcripts.
	if len(warnings) != 5 {
		t.Errorf("warnings = %q", warnings)
	}

	t.Setenv(Prefix+KeyNamingEngine, "rules")
	t.Setenv(Prefix+KeyReadTranscripts, "true")

	if s, _ := Load(); s.NamingEngine != EngineRules || s.UsesClaude() {
		t.Errorf("rules engine: %+v", s)
	}
}

func TestPaths(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	// A file nobody holds yet goes in the platform dir — which on Linux is
	// $XDG_CONFIG_HOME itself, and on macOS Application Support.
	platform, _ := os.UserConfigDir()
	if got := FilePath(ConfigFileName); got != filepath.Join(platform, dirName, ConfigFileName) {
		t.Errorf("a new file goes in the platform dir %q, got %q", platform, got)
	}

	os.MkdirAll(filepath.Join(xdg, dirName), 0o700)
	os.WriteFile(filepath.Join(xdg, dirName, ConfigFileName), nil, 0o600)

	if got := FilePath(ConfigFileName); got != filepath.Join(xdg, dirName, ConfigFileName) {
		t.Errorf("an existing file is read where it is: %q", got)
	}

	if StateDir() == "" || filepath.Base(ClaudeTitlesPath()) != claudeTitlesName {
		t.Errorf("state dir %q, titles %q", StateDir(), ClaudeTitlesPath())
	}

	t.Setenv("XDG_CONFIG_HOME", "relative/dir")

	for _, dir := range configDirs() {
		if !filepath.IsAbs(dir) {
			t.Errorf("a relative XDG_CONFIG_HOME must be ignored: %q", dir)
		}
	}
}

func TestUnreadableConfigFileWarns(t *testing.T) {
	dir := t.TempDir()
	if _, err := readEnvFile(dir); err == nil {
		t.Error("a directory where the file should be must warn")
	}

	if values, err := readEnvFile(""); err != nil || len(values) != 0 {
		t.Error("no path is no file")
	}

	if unquote(`'single'`) != "single" || unquote(`"a # b"`) != "a # b" || unquote(`"sonnet" # fast`) != "sonnet" ||
		unquote(`plain # note`) != "plain" || unquote(`"unclosed`) != `"unclosed` {
		t.Error("quotes keep what is inside them")
	}
}
