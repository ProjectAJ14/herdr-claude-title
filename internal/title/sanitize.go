package title

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Separator joins every part of a title; where a part came from is not
// something a separator can convey, so there is only one.
const Separator = " " + separatorRune + " "

const (
	separatorRune   = "›"
	zeroWidthJoiner = '‍' // kept: emoji clusters are built from it
	edgeTrim        = " " + separatorRune
)

var (
	ansiPattern = regexp.MustCompile(
		"\x1b\\[[0-9;?<>=]*[ -/]*[@-~]" + // CSI
			"|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)" + // OSC
			"|\x1b[@-Z\\\\-_]", // single-character escapes
	)
	spaceRunPattern     = regexp.MustCompile(`\s+`)
	separatorRunPattern = regexp.MustCompile(separatorRune + `(?:\s*` + separatorRune + `)+`)
	separatorGapPattern = regexp.MustCompile(`\s*` + separatorRune + `\s*`)
)

// Sanitize makes an untrusted value safe as a label: escapes and invisible
// characters go, whitespace and separators are normalised, and the result is
// cut to maxColumns (0 = no cut). It is idempotent; "" means unusable.
func Sanitize(s string, maxColumns int) string {
	// Each regexp pass is guarded by a plain check for the one character it
	// needs; unguarded, their backtracking was half the cost of naming a tab.
	if strings.ContainsRune(s, '\x1b') {
		s = ansiPattern.ReplaceAllString(s, "")
	}

	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r):
			return -1
		case unicode.Is(unicode.Cf, r) && r != zeroWidthJoiner:
			return -1 // bidi overrides and zero-width spaces forge what is seen
		}

		return r
	}, s)

	if strings.Contains(s, "  ") {
		s = spaceRunPattern.ReplaceAllString(s, " ")
	}

	if strings.Contains(s, separatorRune) {
		s = separatorRunPattern.ReplaceAllString(s, separatorRune)
		s = separatorGapPattern.ReplaceAllString(s, Separator)
	}

	return fitColumns(strings.TrimSpace(strings.Trim(s, edgeTrim)), maxColumns)
}

// fitColumns cuts to maxColumns terminal columns and leaves no dangling
// separator: a title cut mid-structure must not hint at a lost part.
func fitColumns(s string, maxColumns int) string {
	if maxColumns <= 0 {
		return s
	}

	head, rest := splitAtColumns(s, maxColumns)
	if rest == "" {
		return s
	}

	// End on a whole word ("I r" reads as noise); one long word still cuts hard.
	if next, _ := utf8.DecodeRuneInString(rest); !unicode.IsSpace(next) {
		if cut := strings.LastIndexFunc(head, unicode.IsSpace); cut > 0 {
			head = head[:cut]
		}
	}

	return strings.TrimSpace(strings.TrimRight(head, edgeTrim))
}

// splitAtColumns returns the longest prefix fitting maxColumns, cut between
// grapheme clusters: CJK and emoji take two columns, a family emoji is four
// code points, and cutting by rune or byte breaks both.
func splitAtColumns(s string, maxColumns int) (head, rest string) {
	rest = s
	state := -1

	for width := 0; rest != ""; {
		_, next, clusterWidth, nextState := uniseg.FirstGraphemeClusterInString(rest, state)
		if width+clusterWidth > maxColumns {
			break
		}

		width, rest, state = width+clusterWidth, next, nextState
	}

	return s[:len(s)-len(rest)], rest
}

// columns is a value's width in the tab bar.
func columns(s string) int { return uniseg.StringWidth(s) }
