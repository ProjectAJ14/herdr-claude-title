package config

import (
	"os"
	"path/filepath"
)

const (
	dirName             = "herdr-claude-title"
	ConfigFileName      = "config.env"
	userRenamesFileName = "user-renames.json"
	claudeTitlesName    = "claude-titles.json"
)

// FilePath is where the plugin's file called name lives: the first config
// directory already holding it, else the last one. A new file is machine
// state, so it never starts a ~/.config that a dotfiles repo would sync.
func FilePath(name string) string {
	dirs := configDirs()
	if len(dirs) == 0 {
		return ""
	}

	for _, dir := range dirs {
		path := filepath.Join(dir, dirName, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return filepath.Join(dirs[len(dirs)-1], dirName, name)
}

// StateDir holds what the plugin writes for itself only (instance claims,
// Claude titles). Always the platform directory, so every instance agrees.
func StateDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}

	return filepath.Join(dir, dirName)
}

// ClaudeTitlesPath is where Claude-written titles survive a restart.
func ClaudeTitlesPath() string {
	if dir := StateDir(); dir != "" {
		return filepath.Join(dir, claudeTitlesName)
	}

	return ""
}

// configDirs, in lookup order: $XDG_CONFIG_HOME (absolute only), ~/.config
// (also on Windows, where cross-platform tools keep theirs), then the
// platform's own (Application Support, %APPDATA%).
func configDirs() []string {
	var dirs []string

	if xdg := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		dirs = append(dirs, xdg)
	}

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config"))
	}

	if dir, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, dir)
	}

	return dirs
}
