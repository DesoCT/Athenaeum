package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stateFixture(t *testing.T) (*StateStore, Dirs) {
	t.Helper()
	dir := t.TempDir()
	dirs := Dirs{State: dir, Cache: dir, Data: dir}
	store, err := NewStateStore(dirs)
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	return store, dirs
}

// TestRoundTrip covers R13: tabs, active document, layout, scroll positions,
// mode, and recent documents all survive a restart.
func TestRoundTrip(t *testing.T) {
	store, _ := stateFixture(t)

	want := State{
		Tabs: []Tab{
			{DocumentID: "docs/a.md", Mode: ModeSource, PreviewScroll: 0.25, SourceLine: 42},
			{DocumentID: "docs/b.md", Mode: ModePreview, PreviewScroll: 0.5, SourceLine: 1},
		},
		ActiveDocument: "docs/b.md",
		Recent:         []string{"docs/b.md", "docs/a.md"},
		Layout:         Layout{Navigation: true, Context: false, Search: true},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := store.Load()
	if len(got.Tabs) != 2 {
		t.Fatalf("tabs = %d, want 2", len(got.Tabs))
	}
	if got.Tabs[0] != want.Tabs[0] || got.Tabs[1] != want.Tabs[1] {
		t.Errorf("tabs = %+v, want %+v", got.Tabs, want.Tabs)
	}
	if got.ActiveDocument != want.ActiveDocument {
		t.Errorf("active = %q", got.ActiveDocument)
	}
	if got.Layout != want.Layout {
		t.Errorf("layout = %+v, want %+v", got.Layout, want.Layout)
	}
	if strings.Join(got.Recent, ",") != strings.Join(want.Recent, ",") {
		t.Errorf("recent = %v", got.Recent)
	}
	if got.UpdatedAt == "" {
		t.Error("updated_at was not stamped")
	}
}

// TestStateLivesOutsideTheWorkspace guards spec 03 section 1: session state is
// never written into the repository.
func TestStateLivesOutsideTheWorkspace(t *testing.T) {
	store, dirs := stateFixture(t)
	if err := store.Save(Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirs.State, "session.json")); err != nil {
		t.Fatalf("session file was not written where expected: %v", err)
	}
}

// TestMissingFileYieldsDefaults proves a first launch is not an error.
func TestMissingFileYieldsDefaults(t *testing.T) {
	store, _ := stateFixture(t)
	got := store.Load()
	if len(got.Tabs) != 0 || got.ActiveDocument != "" {
		t.Fatalf("a missing session file must produce empty state, got %+v", got)
	}
	if !got.Layout.Navigation {
		t.Error("the default layout should show the navigation panel")
	}
}

// TestCorruptStateIsNotFatal proves a damaged layout costs the layout only.
func TestCorruptStateIsNotFatal(t *testing.T) {
	store, dirs := stateFixture(t)
	for _, body := range []string{"{not json", "", "null", `{"schema_version": 99}`} {
		if err := os.WriteFile(dirs.SessionFile(), []byte(body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		got := store.Load()
		if len(got.Tabs) != 0 {
			t.Errorf("corrupt state %q produced tabs %+v", body, got.Tabs)
		}
	}
}

// TestSanitiseClampsUntrustedInput proves the browser cannot store arbitrary
// values, even though it is on the same machine.
func TestSanitiseClampsUntrustedInput(t *testing.T) {
	store, _ := stateFixture(t)

	tabs := make([]Tab, 0, maxTabs+10)
	for i := range maxTabs + 10 {
		tabs = append(tabs, Tab{DocumentID: string(rune('a'+i%26)) + ".md", Mode: "nonsense"})
	}
	tabs = append(tabs, Tab{DocumentID: "", Mode: ModeSplit})
	tabs = append(tabs, Tab{DocumentID: strings.Repeat("x", maxDocumentIDLn+1), Mode: ModeSplit})

	if err := store.Save(State{
		Tabs:   tabs,
		Recent: []string{"ok.md", ""},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := store.Load()
	if len(got.Tabs) > maxTabs {
		t.Errorf("tabs = %d, above the %d limit", len(got.Tabs), maxTabs)
	}
	for _, tab := range got.Tabs {
		if !validModes[tab.Mode] {
			t.Errorf("an invalid mode survived: %q", tab.Mode)
		}
		if tab.DocumentID == "" {
			t.Error("an empty document ID survived")
		}
		if len(tab.DocumentID) > maxDocumentIDLn {
			t.Error("an oversized document ID survived")
		}
	}
	for _, id := range got.Recent {
		if id == "" {
			t.Error("an empty recent entry survived")
		}
	}
}

// TestScrollFractionIsClamped keeps a restored position inside the document.
func TestScrollFractionIsClamped(t *testing.T) {
	store, _ := stateFixture(t)
	if err := store.Save(State{Tabs: []Tab{
		{DocumentID: "a.md", Mode: ModeSplit, PreviewScroll: 12, SourceLine: -5},
	}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := store.Load()
	if got.Tabs[0].PreviewScroll != 0 {
		t.Errorf("scroll = %v, want it clamped to 0", got.Tabs[0].PreviewScroll)
	}
	if got.Tabs[0].SourceLine != 0 {
		t.Errorf("line = %d, want it clamped to 0", got.Tabs[0].SourceLine)
	}
}

// TestFilterDropsDocumentsTheWorkspaceNoLongerIncludes is acceptance B1 applied
// to session restoration: a tab must not resurrect an excluded document, and
// the caller must not learn that it exists.
func TestFilterDropsDocumentsTheWorkspaceNoLongerIncludes(t *testing.T) {
	state := State{
		Tabs: []Tab{
			{DocumentID: "docs/kept.md", Mode: ModeSplit},
			{DocumentID: "docs/private/secret.md", Mode: ModeSplit},
		},
		ActiveDocument: "docs/private/secret.md",
		Recent:         []string{"docs/kept.md", "docs/private/secret.md"},
	}

	filtered := state.Filter(func(id string) bool { return id == "docs/kept.md" })

	if len(filtered.Tabs) != 1 || filtered.Tabs[0].DocumentID != "docs/kept.md" {
		t.Fatalf("tabs = %+v", filtered.Tabs)
	}
	if filtered.ActiveDocument != "" {
		t.Errorf("active = %q, want it cleared", filtered.ActiveDocument)
	}
	if len(filtered.Recent) != 1 || filtered.Recent[0] != "docs/kept.md" {
		t.Errorf("recent = %v", filtered.Recent)
	}
}

// TestSaveIsAtomic proves an interrupted write cannot leave a half file: the
// temporary file is in the same directory and renamed over the target.
func TestSaveIsAtomic(t *testing.T) {
	store, dirs := stateFixture(t)
	if err := store.Save(State{Tabs: []Tab{{DocumentID: "a.md", Mode: ModeSplit}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dirs.State)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("a temporary file was left behind: %s", entry.Name())
		}
	}
}
