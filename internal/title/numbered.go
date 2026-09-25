package title

import (
	"strconv"

	"github.com/ProjectAJ14/herdr-claude-title/internal/session"
)

// tabNumberMark is deliberately not Separator: the position is not one more
// thing the title says about the tab.
const tabNumberMark = " · "

// numbered puts the tab's position in front of its label — the key that
// switches to it, which naming a tab otherwise takes away. A decorator, not
// a source: the position says nothing about what a tab holds.
type numbered struct {
	inner      TabNamer
	maxColumns int // the inner namer's bound; the number counts against it
}

// WithTabNumber wraps inner, which must have been built with maxColumns.
func WithTabNumber(inner TabNamer, maxColumns int) TabNamer {
	if maxColumns <= 0 {
		maxColumns = DefaultTabMaxColumns
	}

	return numbered{inner: inner, maxColumns: maxColumns}
}

// NameTab leads with the number, since truncation cuts the tail. When the
// bar is too narrow for both, the number goes and the name stays.
func (n numbered) NameTab(tab session.Tab) Decision {
	d := n.inner.NameTab(tab)

	prefix := strconv.Itoa(tab.Position) + tabNumberMark
	if room := n.maxColumns - columns(prefix); room > 0 {
		if name := fitColumns(d.Label, room); name != "" {
			d.Label = prefix + name
		}
	}

	return d
}
