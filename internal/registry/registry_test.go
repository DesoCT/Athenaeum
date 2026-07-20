package registry

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"athenaeum/internal/security"
)

// Every test here works in a temporary directory. Nothing reads the developer's
// real home directory or real registry (spec 07 section 5).

// writeWorkspace creates a minimal valid workspace and returns its directory.
func writeWorkspace(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create workspace dir: %v", err)
	}
	body := "schema_version = 1\nname = \"" + name + "\"\nroot = \".\"\ninclude = [\"**/*.md\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "athenaeum.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write athenaeum.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	return dir
}

func writeRegistry(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	return path
}

func TestLoadMissingFileIsEmptyNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "athenaeum", FileName)

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("a missing registry must not be an error, got %v", err)
	}
	if reg.Present {
		t.Error("Present must be false when no registry file exists")
	}
	if len(reg.Entries) != 0 {
		t.Errorf("expected no entries, got %d", len(reg.Entries))
	}
	if reg.SourcePath != path {
		t.Errorf("SourcePath = %q, want the path that was looked for", reg.SourcePath)
	}
}

func TestLoadResolvesEntries(t *testing.T) {
	home := t.TempDir()
	alpha := writeWorkspace(t, filepath.Join(home, "dev", "alpha"), "Alpha")
	writeWorkspace(t, filepath.Join(home, "notes"), "Field notes")

	path := writeRegistry(t, home, `
[[workspace]]
name = "Athenaeum"
path = "`+alpha+`"

[[workspace]]
name = "Field notes"
path = "notes"
`)

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(reg.Entries))
	}
	for _, entry := range reg.Entries {
		if !entry.Available {
			t.Errorf("%s should be available, got %s: %s", entry.Name, entry.Code, entry.Reason)
		}
		if entry.ConfigPath == "" {
			t.Errorf("%s has no ConfigPath", entry.Name)
		}
	}

	// A relative path resolves against the registry file's own directory.
	if want := security.Canonicalise(filepath.Join(home, "notes")); reg.Entries[1].Path != want {
		t.Errorf("relative path resolved to %q, want %q", reg.Entries[1].Path, want)
	}
}

func TestLoadCanonicalisesPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	home := t.TempDir()
	real := writeWorkspace(t, filepath.Join(home, "real"), "Real")
	link := filepath.Join(home, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	path := writeRegistry(t, home, "[[workspace]]\nname = \"Linked\"\npath = \""+link+"\"\n")

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry := reg.Entries[0]
	if !entry.Available {
		t.Fatalf("entry unavailable: %s %s", entry.Code, entry.Reason)
	}
	// The resolved path must be the symlink target, or later containment checks
	// against the canonical workspace root would spuriously report an escape.
	if entry.Path != security.Canonicalise(real) {
		t.Errorf("Path = %q, want the canonical target %q", entry.Path, security.Canonicalise(real))
	}
	if strings.Contains(entry.Path, "link") {
		t.Errorf("Path %q still contains the symlink name", entry.Path)
	}
}

func TestLoadExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	writeWorkspace(t, filepath.Join(home, "notes"), "Field notes")

	path := writeRegistry(t, home, "[[workspace]]\nname = \"Notes\"\npath = \"~/notes\"\n")

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry := reg.Entries[0]
	if !entry.Available {
		t.Fatalf("~ was not expanded: %s %s", entry.Code, entry.Reason)
	}
	if entry.Path != security.Canonicalise(filepath.Join(home, "notes")) {
		t.Errorf("Path = %q, want the expanded home path", entry.Path)
	}
}

func TestLoadRejectsOtherUsersHome(t *testing.T) {
	home := t.TempDir()
	path := writeRegistry(t, home, "[[workspace]]\nname = \"Theirs\"\npath = \"~someone/notes\"\n")

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reg.Entries[0].Available {
		t.Error("~otheruser must not resolve")
	}
	if len(reg.Diagnostics) == 0 {
		t.Error("expected a diagnostic naming the field")
	}
}

// One unavailable entry must never hide the others: that is the difference
// between a launcher and a fragile mount table (ADR-0004).
func TestUnavailableEntryDoesNotBreakTheOthers(t *testing.T) {
	home := t.TempDir()
	good := writeWorkspace(t, filepath.Join(home, "good"), "Good")

	// A directory with no athenaeum.toml.
	bare := filepath.Join(home, "bare")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A workspace whose configuration will not parse.
	broken := filepath.Join(home, "broken")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(broken, "athenaeum.toml"), []byte("this is not toml ["), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	path := writeRegistry(t, home, `
[[workspace]]
name = "Gone"
path = "`+filepath.Join(home, "absent")+`"

[[workspace]]
name = "Bare"
path = "`+bare+`"

[[workspace]]
name = "Broken"
path = "`+broken+`"

[[workspace]]
name = "Good"
path = "`+good+`"
`)

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(reg.Entries))
	}

	want := []struct {
		name string
		code string
	}{
		{"Gone", CodePathMissing},
		{"Bare", CodeConfigMissing},
		{"Broken", CodeConfigInvalid},
		{"Good", ""},
	}
	for i, expect := range want {
		entry := reg.Entries[i]
		if entry.Name != expect.name {
			t.Errorf("entry %d name = %q, want %q", i, entry.Name, expect.name)
		}
		if entry.Code != expect.code {
			t.Errorf("%s code = %q, want %q", expect.name, entry.Code, expect.code)
		}
		if expect.code == "" {
			continue
		}
		if entry.Available {
			t.Errorf("%s must not be available", expect.name)
		}
		// R1 and C8: name the problem and the remedy, never just fail.
		if entry.Reason == "" || entry.Remedy == "" {
			t.Errorf("%s must carry both a reason and a remedy, got %q / %q",
				expect.name, entry.Reason, entry.Remedy)
		}
	}

	if available := reg.Available(); len(available) != 1 || available[0].Name != "Good" {
		t.Errorf("Available() = %v, want just Good", available)
	}
}

func TestEntryMayNameTheConfigFileItself(t *testing.T) {
	home := t.TempDir()
	ws := writeWorkspace(t, filepath.Join(home, "ws"), "Direct")

	path := writeRegistry(t, home,
		"[[workspace]]\nname = \"Direct\"\npath = \""+filepath.Join(ws, "athenaeum.toml")+"\"\n")

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry := reg.Entries[0]
	if !entry.Available {
		t.Fatalf("naming athenaeum.toml directly must work: %s %s", entry.Code, entry.Reason)
	}
	if filepath.Base(entry.ConfigPath) != "athenaeum.toml" {
		t.Errorf("ConfigPath = %q", entry.ConfigPath)
	}
}

func TestDuplicateNamesAndPathsAreDiagnosed(t *testing.T) {
	home := t.TempDir()
	ws := writeWorkspace(t, filepath.Join(home, "ws"), "Only")

	path := writeRegistry(t, home, `
[[workspace]]
name = "Same"
path = "`+ws+`"

[[workspace]]
name = "Same"
path = "`+ws+`"
`)

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var names, paths int
	for _, d := range reg.Diagnostics {
		if strings.HasSuffix(d.Field, ".name") {
			names++
		}
		if strings.HasSuffix(d.Field, ".path") {
			paths++
		}
	}
	if names != 1 {
		t.Errorf("expected 1 duplicate-name diagnostic, got %d", names)
	}
	if paths != 1 {
		t.Errorf("expected 1 duplicate-path diagnostic, got %d", paths)
	}

	// An ambiguous name is refused rather than guessed.
	if _, err := reg.Lookup("Same"); err == nil {
		t.Fatal("Lookup of an ambiguous name must fail")
	} else {
		var le *LookupError
		if !asLookupError(err, &le) || le.Code != CodeNameAmbiguous {
			t.Errorf("expected %s, got %v", CodeNameAmbiguous, err)
		}
	}
}

func TestLookup(t *testing.T) {
	home := t.TempDir()
	ws := writeWorkspace(t, filepath.Join(home, "ws"), "Only")
	path := writeRegistry(t, home, "[[workspace]]\nname = \"Only\"\npath = \""+ws+"\"\n")

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	entry, err := reg.Lookup("Only")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if entry.ConfigPath == "" {
		t.Error("looked-up entry has no ConfigPath")
	}

	if _, err := reg.Lookup("Absent"); err == nil {
		t.Fatal("Lookup of an unknown name must fail")
	} else {
		var le *LookupError
		if !asLookupError(err, &le) || le.Code != CodeNameUnknown {
			t.Errorf("expected %s, got %v", CodeNameUnknown, err)
		}
	}
}

func TestUnknownKeysAreWarningsNotFailures(t *testing.T) {
	home := t.TempDir()
	ws := writeWorkspace(t, filepath.Join(home, "ws"), "Only")
	path := writeRegistry(t, home,
		"colour = \"blue\"\n\n[[workspace]]\nname = \"Only\"\npath = \""+ws+"\"\nnickname = \"x\"\n")

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("an unknown key must not stop the registry loading: %v", err)
	}
	if len(reg.Entries) != 1 || !reg.Entries[0].Available {
		t.Fatal("the workspace should still load")
	}
	if reg.Diagnostics.HasErrors() {
		t.Error("unknown registry keys are warnings, not errors")
	}
	if len(reg.Diagnostics) < 2 {
		t.Errorf("expected a warning per unknown key, got %d", len(reg.Diagnostics))
	}
}

func TestEntryWithoutNameFallsBackToTheWorkspaceName(t *testing.T) {
	home := t.TempDir()
	ws := writeWorkspace(t, filepath.Join(home, "ws"), "Configured name")
	path := writeRegistry(t, home, "[[workspace]]\npath = \""+ws+"\"\n")

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := reg.Entries[0].Name; got != "Configured name" {
		t.Errorf("Name = %q, want the workspace's configured name", got)
	}
}

func TestEntryWithoutPathIsDiagnosed(t *testing.T) {
	home := t.TempDir()
	path := writeRegistry(t, home, "[[workspace]]\nname = \"Nowhere\"\n")

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reg.Entries[0].Available {
		t.Error("an entry without a path cannot be available")
	}
	if !reg.Diagnostics.HasErrors() {
		t.Error("a missing path is a configuration error")
	}
}

func TestDefaultPathSitsUnderTheUserConfigDirectory(t *testing.T) {
	// XDG_CONFIG_HOME is how os.UserConfigDir is redirected on Linux, and is
	// also what keeps the browser tests off the developer's real registry.
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME only redirects os.UserConfigDir on Linux")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if want := filepath.Join(dir, "athenaeum", FileName); path != want {
		t.Errorf("DefaultPath = %q, want %q", path, want)
	}
}

func asLookupError(err error, target **LookupError) bool {
	le, ok := err.(*LookupError)
	if ok {
		*target = le
	}
	return ok
}
