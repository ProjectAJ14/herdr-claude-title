// Package title turns a pane's read state into a label for its tab, for the
// pane itself, or for the workspace row. A fixed ladder of sources decides;
// identical state always yields an identical label. The Claude summary is
// one rung of that ladder, written asynchronously by package summarizer.
package title

import (
	"cmp"
	"slices"
	"strings"

	"github.com/ProjectAJ14/herdr-claude-title/internal/session"
)

// Defaults, in terminal columns.
const (
	DefaultTabMaxColumns       = 30
	DefaultWorkspaceMaxColumns = 20 // what a default-width sidebar holds
)

// Fallback names a pane nothing else can name.
const Fallback = "Shell"

// NewTab names a tab with no activity yet (a bare shell, an agent not yet
// prompted): the workspace row already names the project. ssh is the exception.
const NewTab = "New"

// Parts are what a title says, read from the general to the particular:
// "<place> › <branch> › <agent> › <activity>".
type Parts struct {
	Place    string // where: a project directory or `ssh › host`
	Branch   string // qualifies the place
	Agent    string // its own part because the user may hide it
	Activity string // what is being done there
}

func (p Parts) ordered() []string { return []string{p.Place, p.Branch, p.Agent, p.Activity} }

// Decision is a label and which source answered for it.
type Decision struct {
	Label  string
	Rank   int
	Source string
}

// TabNamer names a tab. Two implementations: *Namer and its numbered wrapper.
type TabNamer interface {
	NameTab(tab session.Tab) Decision
}

// Options shape how a title is assembled.
type Options struct {
	MaxColumns       int
	BranchMaxColumns int // 0 leaves branches out
	HideAgentName    bool
	// WorkspaceRowIsParts: the plugin writes workspace rows, so a row is read
	// back as separator-split parts rather than as opaque user text.
	WorkspaceRowIsParts bool
}

// Namer walks a ladder of sources and assembles what they found.
type Namer struct {
	sources []Source
	opts    Options
}

var _ TabNamer = (*Namer)(nil)

// NewNamer orders sources by rank (stable for equal ranks).
func NewNamer(opts Options, sources ...Source) *Namer {
	if opts.MaxColumns <= 0 {
		opts.MaxColumns = DefaultTabMaxColumns
	}

	ordered := slices.Clone(sources)
	slices.SortStableFunc(ordered, func(a, b Source) int { return cmp.Compare(b.Rank(), a.Rank()) })

	return &Namer{sources: ordered, opts: opts}
}

// ForTabs is the shipped ladder for tabs and panes.
func ForTabs(opts Options) *Namer {
	return NewNamer(opts,
		agentTitle{}, claudeSummary{}, terminalTitle{bindKind: true}, transcript{},
		process{}, remoteHost{}, branch{maxColumns: opts.BranchMaxColumns}, newDirectory(),
	)
}

// ForWorkspaces is the ladder for the workspace row: no foreground process,
// because a row that followed the process would rewrite itself every prompt.
func ForWorkspaces(opts Options) *Namer {
	return NewNamer(opts,
		agentTitle{}, claudeSummary{}, terminalTitle{bindKind: false}, transcript{},
		remoteHost{}, branch{maxColumns: opts.BranchMaxColumns}, newDirectory(),
	)
}

// NameTab names a tab after its speaker, minus what the workspace row says.
func (n *Namer) NameTab(tab session.Tab) Decision {
	found := n.collect(tab.Speaker)
	if found.parts.Activity == "" && found.source != (remoteHost{}).Name() {
		return Decision{Label: fitColumns(NewTab, n.opts.MaxColumns), Rank: RankFallback, Source: "fallback"}
	}

	return n.assemble(found, tab.WorkspaceLabel)
}

// NamePanes names each pane (in tab.Panes order) by what tells it from the
// tab above it in the goto panel. The tab's parts, not its finished label,
// are the row above: a pane must not pick back up what the tab dropped.
func (n *Namer) NamePanes(tab session.Tab) []Decision {
	above := n.collect(tab.Speaker).parts

	decisions := make([]Decision, len(tab.Panes))
	for i, pane := range tab.Panes {
		decisions[i] = n.assemble(n.collect(pane), tab.WorkspaceLabel, above)
	}

	return decisions
}

// NameWorkspace names a one-tab workspace row after its tab. An over-long
// row drops whole parts from the front, since a sidebar column of rows that
// all start the same would otherwise all read the same. "" leaves it alone.
func (n *Namer) NameWorkspace(ws session.Workspace) Decision {
	found := n.collect(ws.Speaker)

	label := FormatKeepingEnd(withoutSelfRepetition(found.parts), n.opts.MaxColumns)
	if label == "" {
		return Decision{}
	}

	return Decision{Label: label, Rank: found.rank, Source: found.source}
}

func (n *Namer) assemble(found found, workspaceLabel string, rowsAbove ...Parts) Decision {
	parts := withoutSelfRepetition(found.parts)

	if n.opts.WorkspaceRowIsParts {
		parts = withoutRowSegments(parts, rowSegments(workspaceLabel))
	} else {
		parts = withoutRowAbove(parts, Parts{Place: workspaceLabel}) // opaque user text
	}

	for _, row := range rowsAbove {
		parts = withoutRowAbove(parts, row)
	}

	if label := Format(parts, n.opts.MaxColumns); label != "" {
		return Decision{Label: label, Rank: found.rank, Source: found.source}
	}

	return Decision{Label: fitColumns(Fallback, n.opts.MaxColumns), Rank: RankFallback, Source: "fallback"}
}

type found struct {
	parts  Parts
	source string
	rank   int
}

// collect fills each part from the highest source that supplies it. The
// activity's source answers for the title; others only while nothing has.
func (n *Namer) collect(pane *session.Pane) found {
	var f found
	if pane == nil {
		return f
	}

	for _, s := range n.sources {
		parts, ok := s.Contribute(pane)
		if !ok {
			continue
		}

		if f.parts.Activity == "" && parts.Activity != "" {
			f.parts.Activity, f.source, f.rank = parts.Activity, s.Name(), s.Rank()
		}

		fill(&f.parts.Agent, parts.Agent, &f, s)
		fill(&f.parts.Branch, parts.Branch, &f, s)
		fill(&f.parts.Place, parts.Place, &f, s)

		// Done once both halves are answered; the branch is optional.
		if f.parts.Place != "" && (f.parts.Activity != "" || f.parts.Agent != "") {
			break
		}
	}

	// Before any repetition check, so a title left with only its directory
	// keeps it rather than losing it to a name it will not show.
	if n.opts.HideAgentName {
		f.parts.Agent = ""
	}

	return f
}

func fill(slot *string, value string, f *found, s Source) {
	if *slot != "" || value == "" {
		return
	}

	*slot = value
	if f.source == "" {
		f.source, f.rank = s.Name(), s.Rank()
	}
}

// Format joins parts and sanitises the whole, cutting from the end.
func Format(parts Parts, maxColumns int) string {
	var kept []string

	for _, part := range parts.ordered() {
		if part != "" {
			kept = append(kept, part)
		}
	}

	return Sanitize(strings.Join(kept, Separator), maxColumns)
}

// FormatKeepingEnd drops whole parts from the front until the rest fits,
// measured on the sanitised result (Sanitize collapses whitespace).
func FormatKeepingEnd(parts Parts, maxColumns int) string {
	var kept []string

	for _, part := range parts.ordered() {
		if part != "" {
			kept = append(kept, part)
		}
	}

	for len(kept) > 1 && columns(Sanitize(strings.Join(kept, Separator), 0)) > maxColumns {
		kept = kept[1:]
	}

	return Sanitize(strings.Join(kept, Separator), maxColumns)
}

// withoutSelfRepetition drops a part that only repeats another: a shell
// titling its window after its directory, a prompt showing the branch, a
// worktree named after its branch (the directory leads, the branch goes).
func withoutSelfRepetition(p Parts) Parts {
	if strings.EqualFold(p.Activity, p.Place) {
		p.Activity = ""
	}

	if p.Branch != "" && strings.EqualFold(p.Activity, p.Branch) {
		p.Activity = ""
	}

	if p.Branch != "" && strings.EqualFold(p.Branch, p.Place) {
		p.Branch = ""
	}

	return p
}

// withoutRowAbove drops place, branch and agent a row shown above already
// carries. The activity always stays — it is what a row is for — and a
// title reduced to nothing keeps what it had.
func withoutRowAbove(p, above Parts) Parts {
	kept := p

	if strings.EqualFold(kept.Place, above.Place) {
		kept.Place = ""
	}

	if strings.EqualFold(kept.Branch, above.Branch) {
		kept.Branch = ""
	}

	if strings.EqualFold(kept.Agent, above.Agent) {
		kept.Agent = ""
	}

	if kept == (Parts{}) {
		return p
	}

	return kept
}

// rowSegments reads a workspace row back as the parts it was written from.
// Positions are lost (a long row loses parts from its front), so a tab
// drops a part the row carries anywhere.
func rowSegments(label string) []string {
	if label = Sanitize(label, 0); label == "" {
		return nil
	}

	return strings.Split(label, Separator)
}

func withoutRowSegments(p Parts, row []string) Parts {
	if len(row) == 0 {
		return p
	}

	kept := p
	kept.Place = unlessInRow(kept.Place, row)
	kept.Branch = unlessInRow(kept.Branch, row)
	kept.Agent = unlessInRow(kept.Agent, row)

	if kept == (Parts{}) {
		return p
	}

	return kept
}

// unlessInRow drops value when the row carries it as a run of segments: a
// part containing the separator itself reads back as several.
func unlessInRow(value string, row []string) string {
	if value == "" {
		return ""
	}

	want := strings.Split(Sanitize(value, 0), Separator)
	for start := 0; start+len(want) <= len(row); start++ {
		if slices.EqualFunc(row[start:start+len(want)], want, strings.EqualFold) {
			return ""
		}
	}

	return value
}
