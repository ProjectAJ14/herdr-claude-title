# internal/config — settings

A plugin inherits the **Herdr server's** environment, not the user's shell, so
exported variables do not reliably reach it: `config.env` is the real delivery.
The environment still wins over the file, so `HERDR_CLAUDE_TITLE_LOG_LEVEL=debug
./herdr-claude-title` overrides it for one run.

- Every setting is `HERDR_CLAUDE_TITLE_<KEY>`; the keys and their defaults live
  in `config.go` and **must match `config.env.example` and the README table** —
  change all three together.
- A bad value warns and keeps the default; a bad line in the file is skipped
  with a warning (the old godotenv approach lost the whole file). Nothing is
  expanded: `${HOME}` stays literal.
- Files are looked for in `$XDG_CONFIG_HOME`, `~/.config`, then the platform dir;
  the first that holds the file wins, and a **new** file goes in the last (machine
  state must not seed a dotfiles-synced `~/.config`). Each file resolves alone.
- Read once at start. Half the settings shape the namers built in `main`, so a
  reload would apply some and not others — restart instead.
- `CLAUDE_CONFIG_DIR` is Claude Code's variable, read here so nothing downstream
  touches the environment.
