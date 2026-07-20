package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The resolution order is the part of ADR-0004 most likely to break something
// that already worked, so these tests are written as much about what must NOT
// change as about the new behaviour.

const resolveConfigBody = `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]
`

// chdirToTemp moves into a fresh temporary directory for the duration of a
// test. Resolution depends on the working directory, and spec 07 section 5
// forbids a test operating on the developer's own repository.
func chdirToTemp(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	// t.TempDir can sit under a symlinked path; resolving it keeps later
	// comparisons honest, as canonicalise does in internal/security/paths.go.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return resolved
}

func writeLocalConfig(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "athenaeum.toml")
	if err := os.WriteFile(path, []byte(resolveConfigBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestExplicitPathWins covers rule 1: an explicit path opens unchanged.
func TestExplicitPathWins(t *testing.T) {
	dir := chdirToTemp(t)
	elsewhere := t.TempDir()
	explicit := writeLocalConfig(t, elsewhere)
	writeLocalConfig(t, dir)

	cfg, err := resolveWorkspace(&flags{configPath: explicit, explicitPath: true})
	if err != nil {
		t.Fatalf("resolveWorkspace: %v", err)
	}
	if cfg == nil {
		t.Fatal("an explicit path resolved to the picker")
	}
	if !strings.HasPrefix(cfg.SourcePath, mustEval(t, elsewhere)) {
		t.Errorf("resolved %q, want the explicitly named workspace under %q", cfg.SourcePath, elsewhere)
	}
}

// TestExplicitPathStillErrorsWhenMissing is the regression guard that matters
// most: a named workspace that cannot be opened must remain a hard failure, not
// silently fall through to the picker.
func TestExplicitPathStillErrorsWhenMissing(t *testing.T) {
	chdirToTemp(t)

	cfg, err := resolveWorkspace(&flags{configPath: "/nonexistent/athenaeum.toml", explicitPath: true})
	if err == nil {
		t.Fatalf("a missing explicit path was accepted, resolving to %+v", cfg)
	}
}

// TestLocalConfigIsUsedWhenNoPathGiven covers rule 2, the common flow of
// running athenaeum from inside a repository. An earlier draft of ADR-0004 had
// the picker take precedence here and was rejected for exactly this reason.
func TestLocalConfigIsUsedWhenNoPathGiven(t *testing.T) {
	dir := chdirToTemp(t)
	writeLocalConfig(t, dir)

	cfg, err := resolveWorkspace(&flags{})
	if err != nil {
		t.Fatalf("resolveWorkspace: %v", err)
	}
	if cfg == nil {
		t.Fatal("a local athenaeum.toml was ignored in favour of the picker")
	}
	if cfg.Name != "Fixture" {
		t.Errorf("name = %q, want Fixture", cfg.Name)
	}
}

// TestNoPathAndNoLocalConfigStartsThePicker covers rule 3, which is the only
// behaviour ADR-0004 changes: this was previously nothing but an error.
func TestNoPathAndNoLocalConfigStartsThePicker(t *testing.T) {
	chdirToTemp(t)

	cfg, err := resolveWorkspace(&flags{})
	if err != nil {
		t.Fatalf("resolveWorkspace: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected the picker, got workspace %q", cfg.Name)
	}
}

// TestPickForcesThePickerOverALocalConfig covers --pick, which is how one
// drills out from a shell already inside a workspace.
func TestPickForcesThePickerOverALocalConfig(t *testing.T) {
	dir := chdirToTemp(t)
	writeLocalConfig(t, dir)

	cfg, err := resolveWorkspace(&flags{pick: true})
	if err != nil {
		t.Fatalf("resolveWorkspace: %v", err)
	}
	if cfg != nil {
		t.Fatalf("--pick opened %q instead of the picker", cfg.Name)
	}
}

// TestPickWithAPathIsRefused proves a contradictory invocation is an error
// rather than a silent preference for one or the other.
func TestPickWithAPathIsRefused(t *testing.T) {
	dir := chdirToTemp(t)
	path := writeLocalConfig(t, dir)

	_, err := resolveWorkspace(&flags{pick: true, configPath: path, explicitPath: true})
	if err == nil {
		t.Fatal("--pick combined with a workspace path was accepted")
	}
	if !strings.Contains(err.Error(), "--pick") {
		t.Errorf("the error does not name the offending flag: %v", err)
	}
}

// TestPickIsParsedAsABoolFlag guards the argument permuter: an unregistered
// bool flag would swallow the following argument.
func TestPickIsParsedAsABoolFlag(t *testing.T) {
	positional, flagArgs, err := splitArgs([]string{"--pick", "--port", "7999"})
	if err != nil {
		t.Fatalf("splitArgs: %v", err)
	}
	if len(positional) != 0 {
		t.Fatalf("positional = %v, want none", positional)
	}
	want := []string{"--pick", "--port", "7999"}
	if strings.Join(flagArgs, " ") != strings.Join(want, " ") {
		t.Fatalf("flags = %v, want %v", flagArgs, want)
	}
}

// TestRegistryIsParsedAsAValueFlag guards the same permuter for --registry,
// which takes a path.
func TestRegistryIsParsedAsAValueFlag(t *testing.T) {
	positional, flagArgs, err := splitArgs([]string{"--registry", "/tmp/w.toml", "workspace/athenaeum.toml"})
	if err != nil {
		t.Fatalf("splitArgs: %v", err)
	}
	if len(positional) != 1 || positional[0] != "workspace/athenaeum.toml" {
		t.Fatalf("positional = %v, want the workspace path alone", positional)
	}
	if len(flagArgs) != 2 || flagArgs[1] != "/tmp/w.toml" {
		t.Fatalf("flags = %v, want --registry and its value", flagArgs)
	}
}

func mustEval(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}
