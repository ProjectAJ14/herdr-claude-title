package ownership

import (
	"os"
	"path/filepath"
	"testing"
)

func tab(label, wanted string, position string) Sighting {
	return Sighting{ID: "t1", Label: label, Wanted: wanted, Unnamed: position}
}

func TestFirstPollClaimsNothing(t *testing.T) {
	r := Load("")
	if v := r.Tabs.Judge(tab("my name", "1 · dashboard", "1")); v != Rename {
		t.Fatalf("first poll verdict = %v, want Rename", v)
	}
}

func TestAMovedLabelIsTheUsers(t *testing.T) {
	r := Load("")
	r.Tabs.Judge(tab("1", "1 · dashboard", "1"))
	r.Tabs.Renamed("t1", "1 · dashboard")
	r.FirstPollDone()

	if v := r.Tabs.Judge(tab("1 · dashboard", "1 · dashboard", "1")); v != Rename {
		t.Fatalf("our own label read as %v", v)
	}

	if v := r.Tabs.Judge(tab("prod fire", "1 · dashboard", "1")); v != UserOwned || !r.Tabs.IsUserOwned("t1") {
		t.Fatalf("user rename verdict = %v", v)
	}

	// Clearing the name hands the tab back.
	r.Tabs.Prune(map[string]string{"t1": ""})

	if r.Tabs.IsUserOwned("t1") || r.Tabs.Judge(tab("", "1 · dashboard", "1")) != Rename {
		t.Fatal("a cleared tab should be ours again")
	}
}

func TestANewTabArrivingNamedIsTheUsers(t *testing.T) {
	r := Load("")
	r.FirstPollDone()

	if v := r.Tabs.Judge(tab("scratch", "2 · api", "2")); v != UserOwned {
		t.Fatalf("verdict = %v, want UserOwned", v)
	}
}

func TestALateRenameIsStillOurs(t *testing.T) {
	r := Load("")
	r.Tabs.Judge(tab("1", "1 · api", "1"))
	r.FirstPollDone()
	r.Tabs.RenameUnanswered("t1", "1 · api")

	// Herdr applied it late, after the wanted name moved on.
	if v := r.Tabs.Judge(tab("1 · api", "2 · api", "2")); v != Rename {
		t.Fatalf("late rename verdict = %v, want Rename", v)
	}
}

func TestWorkspaceJudgedOnSightAndPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "renames.json")
	r := Load(path)

	if v := r.Workspaces.Judge(Sighting{ID: "w1", Label: "Payments", Unnamed: "dashboard"}); v != UserOwned {
		t.Fatalf("custom row verdict = %v", v)
	}

	if v := r.Workspaces.Judge(Sighting{ID: "w2", Label: "x", Unnamed: ""}); v != Undecided {
		t.Fatalf("no-directory row verdict = %v", v)
	}

	if v := r.Workspaces.Judge(Sighting{ID: "w3", Label: "api", Wanted: "api › fix", Unnamed: "api"}); v != Rename {
		t.Fatalf("default row verdict = %v", v)
	}

	r.Workspaces.Renamed("w3", "api › fix")

	reloaded := Load(path)
	if !reloaded.Workspaces.IsUserOwned("w1") {
		t.Error("claim lost across restart")
	}

	// A restarted plugin recognises its own row rather than claiming it.
	if v := reloaded.Workspaces.Judge(
		Sighting{ID: "w3", Label: "api › fix", Wanted: "api › other", Unnamed: "api"},
	); v != Rename {
		t.Errorf("own row after restart = %v, want Rename", v)
	}
}

func TestLoadReadOnlyNeverWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "renames.json")
	r := LoadReadOnly(path)
	r.FirstPollDone()
	r.Tabs.Judge(tab("mine", "1 · api", "1"))

	if _, err := os.Stat(path); err == nil {
		t.Fatal("a read-only registry wrote its file")
	}
}

func TestPanesHaveOneUnnamedSpellingAndPruneForgets(t *testing.T) {
	r := Load("")
	pane := func(label string) Sighting { return Sighting{ID: "p1", Label: label, Wanted: "api"} }

	r.Panes.Judge(pane(""))
	r.FirstPollDone()

	if r.Panes.Judge(pane("")) != Rename {
		t.Fatal("an unnamed pane is ours")
	}

	r.Panes.Prune(map[string]string{}) // the pane closed

	if r.Panes.Judge(pane("scratch")) != UserOwned {
		t.Fatal("a pane first seen after the first poll wearing a name is the user's")
	}
}

func TestACorruptFileIsAnEmptyStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "renames.json")
	os.WriteFile(path, []byte("{not json"), 0o600)

	if r := Load(path); r.Tabs.IsUserOwned("t1") {
		t.Fatal("corrupt file must load empty")
	}
}
