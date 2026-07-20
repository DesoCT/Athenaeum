package gitview

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRepo builds a temporary repository. Spec 07 section 5 forbids tests
// touching the developer's own repository.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "fixture@example.invalid"},
		{"config", "user.name", "Fixture"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// commitAll uses the git binary directly rather than the adapter, precisely
// because the adapter must never be able to do this (acceptance J3).
func commitAll(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "--quiet", "-m", "fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestStates(t *testing.T) {
	root := gitRepo(t)
	write(t, root, "docs/clean.md", "# Clean\n")
	write(t, root, "docs/changed.md", "# Changed\n")
	commitAll(t, root)

	write(t, root, "docs/changed.md", "# Changed\n\nEdited.\n")
	write(t, root, "docs/new.md", "# New\n")

	adapter := New(root, nil)
	if !adapter.Available() {
		t.Fatal("Git should be available for a real repository")
	}
	adapter.reload(context.Background())

	tests := map[string]string{
		"docs/clean.md":   StateClean,
		"docs/changed.md": StateModified,
		"docs/new.md":     StateUntracked,
	}
	for id, want := range tests {
		got, ok := adapter.State(id)
		if !ok {
			t.Fatalf("State(%q) reported Git unavailable", id)
		}
		if got != want {
			t.Errorf("State(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestPathsWithSpacesAndQuotes proves the -z porcelain parse is not fooled by
// filenames that would break a naive line split.
func TestPathsWithSpacesAndQuotes(t *testing.T) {
	root := gitRepo(t)
	write(t, root, "docs/a file with spaces.md", "# Spaces\n")
	write(t, root, `docs/quote"inside.md`, "# Quote\n")

	adapter := New(root, nil)
	adapter.reload(context.Background())

	for _, id := range []string{"docs/a file with spaces.md", `docs/quote"inside.md`} {
		got, ok := adapter.State(id)
		if !ok {
			t.Fatalf("State(%q) reported Git unavailable", id)
		}
		if got != StateUntracked {
			t.Errorf("State(%q) = %q, want %q", id, got, StateUntracked)
		}
	}
}

// TestWorkspaceInsideRepositorySubdirectory proves document IDs are made
// relative to the workspace root, not the repository root.
func TestWorkspaceInsideRepositorySubdirectory(t *testing.T) {
	root := gitRepo(t)
	write(t, root, "workspace/docs/note.md", "# Note\n")
	write(t, root, "elsewhere/other.md", "# Other\n")

	adapter := New(filepath.Join(root, "workspace"), nil)
	if !adapter.Available() {
		t.Fatal("Git should be available for a subdirectory of a repository")
	}
	adapter.reload(context.Background())

	if got, _ := adapter.State("docs/note.md"); got != StateUntracked {
		t.Errorf("State = %q, want %q", got, StateUntracked)
	}
	// A file outside the workspace root must not appear as a document at all.
	if _, present := adapter.States()["../elsewhere/other.md"]; present {
		t.Error("a file outside the workspace root was recorded as a document")
	}
	if _, present := adapter.States()["elsewhere/other.md"]; present {
		t.Error("a file outside the workspace root leaked in under a workspace ID")
	}
}

// TestUnavailableWithoutARepository covers acceptance J4: core functionality
// stays available and Git simply reports itself absent.
func TestUnavailableWithoutARepository(t *testing.T) {
	// A temporary directory that is not a repository. If the test machine has a
	// repository above the temp root this would find it, so the check is on the
	// behaviour of the pair rather than on availability alone.
	dir := t.TempDir()
	adapter := New(dir, nil)

	state, ok := adapter.State("docs/anything.md")
	if adapter.Available() {
		// The temp dir turned out to be inside a repository; the contract is
		// still that a state comes back.
		if !ok {
			t.Fatal("an available adapter must report a state")
		}
		return
	}
	if ok || state != "" {
		t.Fatalf("an unavailable adapter returned (%q, %v)", state, ok)
	}
}

// TestOnlyReadOnlySubcommandsAreReachable is acceptance J3 at the unit level:
// the allow-list is the mechanism preventing repository mutation, so it is
// asserted rather than assumed.
func TestOnlyReadOnlySubcommandsAreReachable(t *testing.T) {
	root := gitRepo(t)
	write(t, root, "docs/note.md", "# Note\n")
	commitAll(t, root)

	adapter := New(root, nil)

	mutating := []string{
		"add", "commit", "push", "pull", "fetch", "reset", "checkout",
		"switch", "rebase", "merge", "cherry-pick", "revert", "stash",
		"tag", "branch", "clean", "rm", "mv", "apply", "restore", "gc",
	}
	for _, subcommand := range mutating {
		if allowed[subcommand] {
			t.Errorf("%q is in the allow-list; D-019 forbids repository mutation", subcommand)
		}
		if _, err := adapter.run(context.Background(), subcommand); err == nil {
			t.Errorf("run(%q) succeeded; it must be refused", subcommand)
		}
	}

	// The repository must be untouched: still exactly one commit, no changes.
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("the working tree changed: %s", out)
	}
}

// TestAllowListIsExhaustive documents the complete set of reachable commands,
// so widening it is a deliberate, reviewed edit rather than a quiet one.
func TestAllowListIsExhaustive(t *testing.T) {
	want := map[string]bool{"rev-parse": true, "status": true}
	if len(allowed) != len(want) {
		t.Fatalf("the allow-list has %d entries, expected %d: %v", len(allowed), len(want), allowed)
	}
	for name := range want {
		if !allowed[name] {
			t.Errorf("%q is missing from the allow-list", name)
		}
	}
}
