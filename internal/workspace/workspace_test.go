package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"athenaeum/internal/config"
	"athenaeum/internal/security"
)

// fixture builds a temporary workspace and returns its opened form.
// Spec 07 section 5 requires temporary workspaces for every filesystem test.
func fixture(t *testing.T, configBody string, files map[string]string) *Workspace {
	t.Helper()
	dir := t.TempDir()

	for rel, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	configPath := filepath.Join(dir, config.DefaultFileName)
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	ws, err := Open(cfg)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	return ws
}

const standardConfig = `
schema_version = 1
name = "Fixture"
root = "."
include = ["README.md", "docs/**/*.md"]
exclude = ["**/node_modules/**", "docs/private/**"]

[[groups]]
id = "design"
title = "Design"
patterns = ["docs/design/**/*.md"]

[security]
writable = ["docs/**/*.md"]
`

var standardFiles = map[string]string{
	"README.md":                       "# Readme\n",
	"docs/design/rendering.md":        "# Rendering\n",
	"docs/operations/runbook.md":      "# Runbook\n",
	"docs/private/secret.md":          "# Secret\n",
	"docs/node_modules/pkg/readme.md": "# Vendored\n",
	"notes.txt":                       "not markdown\n",
	"CHANGELOG.md":                    "# Changes\n",
}

func ids(ws *Workspace) []string {
	var out []string
	for _, doc := range ws.Documents() {
		out = append(out, doc.ID)
	}
	return out
}

// TestIncludeExclude covers acceptance B1.
func TestIncludeExclude(t *testing.T) {
	ws := fixture(t, standardConfig, standardFiles)
	got := ids(ws)

	want := []string{"README.md", "docs/design/rendering.md", "docs/operations/runbook.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("documents = %v, want %v", got, want)
	}
}

func TestExcludedFilesAreNotLookupable(t *testing.T) {
	ws := fixture(t, standardConfig, standardFiles)

	excluded := []string{
		"docs/private/secret.md",          // excluded by pattern
		"docs/node_modules/pkg/readme.md", // excluded by pattern
		"CHANGELOG.md",                    // never included
		"notes.txt",                       // wrong extension
	}
	for _, id := range excluded {
		t.Run(id, func(t *testing.T) {
			if _, ok := ws.Lookup(id); ok {
				t.Fatalf("%s is visible but should be excluded", id)
			}
		})
	}
}

// TestExcludedFilesCannotBeOpenedThroughAPI is the second half of B1: an
// excluded file must not be readable through a crafted document ID.
func TestExcludedFilesCannotBeOpenedThroughAPI(t *testing.T) {
	ws := fixture(t, standardConfig, standardFiles)

	crafted := []string{
		"docs/private/secret.md",
		"CHANGELOG.md",
		"docs/../CHANGELOG.md",
		"./CHANGELOG.md",
		"docs/design/../private/secret.md",
	}
	for _, id := range crafted {
		t.Run(id, func(t *testing.T) {
			if _, err := ws.ResolveRead(id); err == nil {
				t.Fatalf("ResolveRead(%q) succeeded for an excluded file", id)
			}
		})
	}
}

// TestExclusionIsIndistinguishableFromAbsence stops the API being used to probe
// for the existence of excluded files.
func TestExclusionIsIndistinguishableFromAbsence(t *testing.T) {
	ws := fixture(t, standardConfig, standardFiles)

	_, existsButExcluded := ws.ResolveRead("docs/private/secret.md")
	_, doesNotExist := ws.ResolveRead("docs/private/imaginary.md")

	if existsButExcluded == nil || doesNotExist == nil {
		t.Fatal("both lookups should fail")
	}
	if security.CodeOf(existsButExcluded) != security.CodeOf(doesNotExist) {
		t.Fatalf("excluded file reports %q but absent file reports %q; the codes must match",
			security.CodeOf(existsButExcluded), security.CodeOf(doesNotExist))
	}
}

func TestGroupsAssigned(t *testing.T) {
	ws := fixture(t, standardConfig, standardFiles)

	doc, ok := ws.Lookup("docs/design/rendering.md")
	if !ok {
		t.Fatal("design document missing")
	}
	if !slices.Contains(doc.Groups, "design") {
		t.Errorf("groups = %v, want it to contain design", doc.Groups)
	}

	other, ok := ws.Lookup("docs/operations/runbook.md")
	if !ok {
		t.Fatal("operations document missing")
	}
	if slices.Contains(other.Groups, "design") {
		t.Errorf("runbook was placed in the design group: %v", other.Groups)
	}
}

// TestWriteBoundary covers acceptance B3.
func TestWriteBoundary(t *testing.T) {
	ws := fixture(t, standardConfig, standardFiles)

	// Included and inside security.writable.
	if _, err := ws.ResolveWrite("docs/design/rendering.md"); err != nil {
		t.Fatalf("a writable document was rejected: %v", err)
	}

	// Included but outside security.writable: readable, not writable.
	if _, err := ws.ResolveRead("README.md"); err != nil {
		t.Fatalf("README.md should be readable: %v", err)
	}
	_, err := ws.ResolveWrite("README.md")
	if err == nil {
		t.Fatal("README.md is outside security.writable but was accepted for writing")
	}
	if code := security.CodeOf(err); code != security.CodePathNotWritable {
		t.Errorf("code = %q, want %q", code, security.CodePathNotWritable)
	}

	doc, _ := ws.Lookup("README.md")
	if doc.Writable {
		t.Error("README.md is reported as writable")
	}
}

// TestWriteBoundaryDefaultsToIncluded covers the spec 03 section 7 default.
func TestWriteBoundaryDefaultsToIncluded(t *testing.T) {
	ws := fixture(t, `
schema_version = 1
name = "Fixture"
include = ["docs/**/*.md"]
`, map[string]string{
		"docs/a.md": "# A\n",
		"other.md":  "# Other\n",
	})

	if _, err := ws.ResolveWrite("docs/a.md"); err != nil {
		t.Fatalf("with no writable list, an included document should be writable: %v", err)
	}
	if _, err := ws.ResolveWrite("other.md"); err == nil {
		t.Fatal("a non-included document was writable")
	}
}

func TestUnmatchedIncludeWarns(t *testing.T) {
	ws := fixture(t, `
schema_version = 1
name = "Fixture"
include = ["docs/**/*.md", "reports/**/*.md"]
`, map[string]string{"docs/a.md": "# A\n"})

	diags := ws.Diagnostics()
	var found bool
	for _, d := range diags {
		if strings.Contains(d.Message, "reports/**/*.md") && strings.Contains(d.Message, "matched no files") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no warning for the unmatched include pattern; diagnostics: %v", diags)
	}
	if diags.HasErrors() {
		t.Error("an unmatched include produced an error; spec 05 section 6 makes it a warning")
	}
}

func TestDocumentSizeFlags(t *testing.T) {
	ws := fixture(t, `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]

[documents]
max_editable_bytes = 100
large_file_warning_bytes = 10
`, map[string]string{
		"small.md":  "# S\n",
		"medium.md": "# M\n" + strings.Repeat("x", 50),
		"big.md":    "# B\n" + strings.Repeat("x", 200),
	})

	small, _ := ws.Lookup("small.md")
	if small.TooLarge || small.LargeWarning {
		t.Errorf("small.md flagged: too_large=%v warning=%v", small.TooLarge, small.LargeWarning)
	}

	medium, _ := ws.Lookup("medium.md")
	if medium.TooLarge {
		t.Error("medium.md marked too large")
	}
	if !medium.LargeWarning {
		t.Error("medium.md did not raise the large-file warning")
	}

	big, _ := ws.Lookup("big.md")
	if !big.TooLarge {
		t.Error("big.md was not marked too large")
	}
}

// TestSymlinkedFileSkippedByDefault keeps external content out of enumeration
// unless the workspace opts in.
func TestSymlinkedFileSkippedByDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	root := filepath.Join(dir, "ws")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("# Secret\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "real.md"), []byte("# Real\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "linked.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	configPath := filepath.Join(dir, config.DefaultFileName)
	if err := os.WriteFile(configPath, []byte(`
schema_version = 1
name = "Fixture"
root = "ws"
include = ["**/*.md"]
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	ws, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, ok := ws.Lookup("linked.md"); ok {
		t.Error("a symlink to a file outside the workspace was enumerated")
	}
	if _, ok := ws.Lookup("real.md"); !ok {
		t.Error("the real document was not enumerated")
	}
}

func TestRefreshPicksUpNewFiles(t *testing.T) {
	ws := fixture(t, `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]
`, map[string]string{"a.md": "# A\n"})

	if ws.Count() != 1 {
		t.Fatalf("initial count = %d, want 1", ws.Count())
	}

	newFile := filepath.Join(ws.Guard().Root(), "b.md")
	if err := os.WriteFile(newFile, []byte("# B\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := ws.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if ws.Count() != 2 {
		t.Fatalf("count after refresh = %d, want 2", ws.Count())
	}
}

func TestDocumentsAreOrderedDeterministically(t *testing.T) {
	ws := fixture(t, `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]
`, map[string]string{
		"z.md":   "# Z\n",
		"a.md":   "# A\n",
		"m/b.md": "# B\n",
		"m/a.md": "# A\n",
	})

	first := ids(ws)
	if err := ws.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !slices.Equal(first, ids(ws)) {
		t.Fatal("enumeration order is not stable across refreshes")
	}
	if !slices.IsSorted(first) {
		t.Fatalf("documents are not sorted: %v", first)
	}
}
