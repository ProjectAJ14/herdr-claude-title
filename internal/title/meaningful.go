package title

import (
	"regexp"
	"strings"
)

// shellNames run in a pane without being what the pane is for.
var shellNames = set("bash", "zsh", "fish", "sh", "dash", "ksh", "tcsh", "csh", "login", "pwsh", "powershell", "cmd")

// genericNames name a program or a place, not what is being done there.
var genericNames = set(
	"shell",
	"terminal",
	"node",
	"windows powershell",
	"claude",
	"claude code",
	"agent",
	"coding agent",
)

func set(values ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(values))
	for _, v := range values {
		m[v] = struct{}{}
	}

	return m
}

func isGeneric(lowered string) bool {
	_, generic := genericNames[lowered]
	_, shell := shellNames[lowered]

	return generic || shell
}

var (
	uriPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
	// promptPattern: a shell's prompt as a title (`alex@mac:~/work`) says who
	// and where, never what.
	promptPattern = regexp.MustCompile(`^[^\s@]+@[^\s@:]+:\S*$`)
	// idleTitlePattern: Herdr's title for an idle Windows pane, `pwsh in dir`.
	idleTitlePattern = regexp.MustCompile(`^(\S+) in .+$`)
)

const punctuation = `()[]{}<>"'` + ",;:-–—|"

// Meaningful strips locations from a value and reports whether anything
// that says what the user is doing survived: `auth.ts (~/work/src) - Nvim`
// keeps `auth.ts - Nvim`; `~`, `zsh` and `alex@mac:~` keep nothing.
func Meaningful(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if m := idleTitlePattern.FindStringSubmatch(trimmed); m != nil && isGeneric(strings.ToLower(m[1])) {
		return "", false
	}

	cleaned := withoutLocations(trimmed)
	if cleaned == "" || isGeneric(strings.ToLower(cleaned)) || promptPattern.MatchString(cleaned) {
		return "", false
	}

	return cleaned, true
}

// withoutLocations drops absolute, home-anchored and URI words — the place
// half of a title already says where — and keeps relative paths, which
// describe work (`Fix bug in src/auth.ts`).
func withoutLocations(value string) string {
	var kept []string

	for word := range strings.FieldsSeq(value) {
		if isLocation(strings.Trim(word, punctuation)) {
			continue
		}

		isPunct := strings.Trim(word, punctuation) == ""
		if isPunct && (len(kept) == 0 || strings.Trim(kept[len(kept)-1], punctuation) == "") {
			continue // punctuation only survives between two real words
		}

		kept = append(kept, word)
	}

	for len(kept) > 0 && strings.Trim(kept[len(kept)-1], punctuation) == "" {
		kept = kept[:len(kept)-1]
	}

	return strings.Join(kept, " ")
}

func isLocation(word string) bool {
	switch {
	case word == "~", strings.HasPrefix(word, "~/"), strings.HasPrefix(word, `~\`):
		return true
	case strings.HasPrefix(word, "/"), strings.HasPrefix(word, `\\`):
		return true
	case len(word) >= 3 && word[1] == ':' && (word[2] == '\\' || word[2] == '/') &&
		word[0]|0x20 >= 'a' && word[0]|0x20 <= 'z':
		return true // a Windows drive path
	}

	return uriPattern.MatchString(word)
}
