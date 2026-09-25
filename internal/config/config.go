// Package config reads the plugin's settings. A plugin inherits the Herdr
// server's environment, never the user's shell, so settings live in a
// config.env file; a variable set in the environment overrides the file.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ProjectAJ14/herdr-claude-title/internal/title"
)

// Prefix namespaces every setting.
const Prefix = "HERDR_CLAUDE_TITLE_"

// Setting names, without Prefix. Each is documented in config.env.example.
const (
	KeyLogLevel            = "LOG_LEVEL"
	KeyPollIntervalMS      = "POLL_INTERVAL_MS"
	KeyTabMaxColumns       = "TAB_MAX_COLUMNS"
	KeyBranchMaxColumns    = "BRANCH_MAX_COLUMNS"
	KeyShowTabNumber       = "SHOW_TAB_NUMBER"
	KeyShowAgentName       = "SHOW_AGENT_NAME"
	KeyPreferAgentPane     = "PREFER_AGENT_PANE"
	KeyNamePanes           = "NAME_PANES"
	KeyNameWorkspaces      = "NAME_WORKSPACES"
	KeyWorkspaceMaxColumns = "WORKSPACE_MAX_COLUMNS"
	KeyReadTranscripts     = "READ_TRANSCRIPTS"
	KeyExtraClaudeHomes    = "EXTRA_CLAUDE_HOMES"
	KeyUserRenamesFile     = "USER_RENAMES_FILE"
	KeyNamingEngine        = "NAMING_ENGINE"
	KeyClaudeModel         = "CLAUDE_MODEL"
	KeyClaudeBinary        = "CLAUDE_BINARY"
	KeyRetitleEvery        = "CLAUDE_RETITLE_EVERY_PROMPTS"
	KeyClaudeTimeoutSec    = "CLAUDE_TIMEOUT_SECONDS"
)

// claudeConfigDirEnv is Claude Code's own variable, not ours.
const claudeConfigDirEnv = "CLAUDE_CONFIG_DIR"

// Naming engines.
const (
	EngineClaude = "claude" // Claude summaries on top of the rules
	EngineRules  = "rules"  // deterministic rules only; nothing leaves the machine
)

// Settings is the plugin's whole configuration.
type Settings struct {
	LogLevel     slog.Level
	PollInterval time.Duration // also the fastest a label can change

	TabMaxColumns       int // columns, not characters: CJK and emoji take two
	BranchMaxColumns    int // 0 hides branches
	ShowTabNumber       bool
	ShowAgentName       bool
	PreferAgentPane     bool // name a tab after its agent pane even when unfocused
	NamePanes           bool // costs a process read per pane rather than per tab
	NameWorkspaces      bool // name one-tab workspace rows; needs UserRenamesFile
	WorkspaceMaxColumns int

	ReadTranscripts bool
	// ClaudeHomes are Claude config homes to read transcripts from, in order.
	ClaudeHomes []string
	// UserRenamesFile persists what the user renamed by hand ("" = memory).
	UserRenamesFile string

	NamingEngine        string
	ClaudeModel         string
	ClaudeBinary        string // "" finds it on PATH and in install locations
	RetitleEveryPrompts int
	ClaudeTimeout       time.Duration
}

// WritesWorkspaces says rows are named: asked for, and with a file, since
// memory alone cannot tell a row we wrote from the user's after a restart.
func (s Settings) WritesWorkspaces() bool { return s.NameWorkspaces && s.UserRenamesFile != "" }

// UsesClaude says the Claude engine is on and has transcripts to read.
func (s Settings) UsesClaude() bool { return s.NamingEngine == EngineClaude && s.ReadTranscripts }

// Load reads config.env and the environment. Bad values warn and keep the
// default: a typo must never stop the plugin.
func Load() (Settings, []string) {
	var warnings []string

	file, err := readEnvFile(FilePath(ConfigFileName))
	if err != nil {
		warnings = append(warnings, err.Error())
	}

	src := source{file: file, warnings: &warnings}

	s := Settings{
		LogLevel:            src.level(KeyLogLevel, slog.LevelInfo),
		PollInterval:        time.Duration(src.number(KeyPollIntervalMS, 500, 1)) * time.Millisecond,
		TabMaxColumns:       src.number(KeyTabMaxColumns, title.DefaultTabMaxColumns, 1),
		BranchMaxColumns:    src.number(KeyBranchMaxColumns, title.DefaultBranchMaxColumns, 0),
		ShowTabNumber:       src.boolean(KeyShowTabNumber, true),
		ShowAgentName:       src.boolean(KeyShowAgentName, false),
		PreferAgentPane:     src.boolean(KeyPreferAgentPane, false),
		NamePanes:           src.boolean(KeyNamePanes, true),
		NameWorkspaces:      src.boolean(KeyNameWorkspaces, false),
		WorkspaceMaxColumns: src.number(KeyWorkspaceMaxColumns, title.DefaultWorkspaceMaxColumns, 1),
		ReadTranscripts:     src.boolean(KeyReadTranscripts, true),
		UserRenamesFile:     FilePath(userRenamesFileName),
		NamingEngine:        src.choice(KeyNamingEngine, EngineClaude, EngineClaude, EngineRules),
		ClaudeModel:         src.text(KeyClaudeModel, "haiku"),
		ClaudeBinary:        src.text(KeyClaudeBinary, ""),
		RetitleEveryPrompts: src.number(KeyRetitleEvery, 5, 1),
		ClaudeTimeout:       time.Duration(src.number(KeyClaudeTimeoutSec, 60, 1)) * time.Second,
	}

	if path, set := src.lookup(Prefix + KeyUserRenamesFile); set {
		s.UserRenamesFile = path // any string is the path meant; "" = memory only
	}

	s.ClaudeHomes = claudeHomes(src, &warnings)

	if s.NameWorkspaces && !s.WritesWorkspaces() {
		warnings = append(warnings, Prefix+KeyNameWorkspaces+"=true needs "+Prefix+KeyUserRenamesFile+
			" to name a file, so workspace rows are left alone")
	}

	if s.NamingEngine == EngineClaude && !s.ReadTranscripts {
		warnings = append(warnings, Prefix+KeyNamingEngine+"=claude needs transcripts, which "+
			Prefix+KeyReadTranscripts+"=false turns off, so only the rules name tabs")
	}

	return s, warnings
}

// claudeHomes: CLAUDE_CONFIG_DIR (or ~/.claude), then EXTRA_CLAUDE_HOMES.
// An extra home that does not exist yet is warned about and still searched.
func claudeHomes(src source, warnings *[]string) []string {
	var homes []string

	if dir, _ := src.lookup(claudeConfigDirEnv); dir != "" {
		homes = append(homes, dir)
	} else if home, err := os.UserHomeDir(); err == nil {
		homes = append(homes, filepath.Join(home, ".claude"))
	}

	extra, _ := src.lookup(Prefix + KeyExtraClaudeHomes)
	for _, dir := range filepath.SplitList(extra) {
		if dir = strings.TrimSpace(dir); dir == "" {
			continue
		}

		dir = filepath.Clean(dir)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			*warnings = append(
				*warnings,
				fmt.Sprintf("%s%s: %q is not a directory yet", Prefix, KeyExtraClaudeHomes, dir),
			)
		}

		homes = append(homes, dir)
	}

	return homes
}

// source looks a key up in the environment first, then in config.env.
type source struct {
	file     map[string]string
	warnings *[]string
}

func (s source) lookup(key string) (string, bool) {
	if v, ok := os.LookupEnv(key); ok {
		return v, true
	}

	v, ok := s.file[key]

	return v, ok
}

// raw is a setting's value, "" meaning "not set, keep the default".
func (s source) raw(key string) string {
	v, _ := s.lookup(Prefix + key)
	return strings.TrimSpace(v)
}

func (s source) warn(key, raw, why string, fallback any) {
	*s.warnings = append(*s.warnings, fmt.Sprintf("%s%s=%q %s, using %v", Prefix, key, raw, why, fallback))
}

func (s source) text(key, fallback string) string {
	if v := s.raw(key); v != "" {
		return v
	}

	return fallback
}

func (s source) boolean(key string, fallback bool) bool {
	raw := s.raw(key)
	if raw == "" {
		return fallback
	}

	v, err := strconv.ParseBool(raw)
	if err != nil {
		s.warn(key, raw, "is not true or false", fallback)
		return fallback
	}

	return v
}

func (s source) number(key string, fallback, lowest int) int {
	raw := s.raw(key)
	if raw == "" {
		return fallback
	}

	v, err := strconv.Atoi(raw)
	if err != nil || v < lowest {
		s.warn(key, raw, fmt.Sprintf("must be a whole number of at least %d", lowest), fallback)
		return fallback
	}

	return v
}

func (s source) choice(key, fallback string, allowed ...string) string {
	raw := strings.ToLower(s.raw(key))
	if raw == "" {
		return fallback
	}

	for _, a := range allowed {
		if raw == a {
			return a
		}
	}

	s.warn(key, raw, "must be one of "+strings.Join(allowed, ", "), fallback)

	return fallback
}

func (s source) level(key string, fallback slog.Level) slog.Level {
	raw := s.raw(key)
	if raw == "" {
		return fallback
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		s.warn(key, raw, "must be debug, info, warn or error", fallback)
		return fallback
	}

	return level
}

// readEnvFile parses KEY=value lines: `#` comments, blank lines, an
// optional `export `, and matching quotes around a value. Nothing is
// expanded. A malformed line is skipped with a warning, not the whole file.
func readEnvFile(path string) (map[string]string, error) {
	values := map[string]string{}
	if path == "" {
		return values, nil
	}

	raw, err := os.ReadFile(path) //nolint:gosec // the plugin's own config file
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}

	if err != nil {
		return values, fmt.Errorf("%s could not be read: %w", path, err)
	}

	var bad []string

	for n, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if key = strings.TrimSpace(key); !ok || key == "" || strings.ContainsAny(key, " \t") {
			bad = append(bad, strconv.Itoa(n+1))
			continue
		}

		values[key] = unquote(strings.TrimSpace(value))
	}

	if len(bad) > 0 {
		return values, fmt.Errorf("%s: lines %s are not KEY=value and were skipped", path, strings.Join(bad, ", "))
	}

	return values, nil
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
			return v[1 : end+1] // anything after the closing quote is a comment
		}
	}

	value, _, _ := strings.Cut(v, " #") // a trailing comment on an unquoted value

	return strings.TrimSpace(value)
}
