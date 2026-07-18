package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeWorkspace creates a temporary workspace containing athenaeum.toml.
// Spec 07 section 5 requires every write test to use a temporary workspace.
func writeWorkspace(t *testing.T, toml string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	return path
}

const minimalConfig = `
schema_version = 1
name = "Fixture"
root = "."
include = ["**/*.md"]
`

func TestLoadMinimalConfig(t *testing.T) {
	path := writeWorkspace(t, minimalConfig)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Name != "Fixture" {
		t.Errorf("Name = %q, want Fixture", cfg.Name)
	}
	if cfg.SourcePath != path {
		t.Errorf("SourcePath = %q, want %q", cfg.SourcePath, path)
	}
	if !filepath.IsAbs(cfg.AbsRoot) {
		t.Errorf("AbsRoot = %q, want an absolute path", cfg.AbsRoot)
	}
	if len(cfg.Include) != 1 || cfg.Include[0] != "**/*.md" {
		t.Errorf("Include = %v", cfg.Include)
	}
}

// TestLoadAcceptsDirectory lets `athenaeum open .` work.
func TestLoadAcceptsDirectory(t *testing.T) {
	path := writeWorkspace(t, minimalConfig)

	cfg, err := Load(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Load(dir): %v", err)
	}
	if cfg.Name != "Fixture" {
		t.Errorf("Name = %q, want Fixture", cfg.Name)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(writeWorkspace(t, minimalConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Documents.RawHTML {
		t.Error("raw_html defaulted to true; spec 05 requires it off by default")
	}
	if cfg.Assets.Directory != "assets" {
		t.Errorf("asset directory = %q, want assets (D-016)", cfg.Assets.Directory)
	}
	if cfg.Annotations.SharedDirectory != ".athenaeum/shared" {
		t.Errorf("shared directory = %q, want .athenaeum/shared (D-010)", cfg.Annotations.SharedDirectory)
	}
	if cfg.Annotations.DefaultVisibility != "personal" {
		t.Errorf("default visibility = %q, want personal", cfg.Annotations.DefaultVisibility)
	}
	if cfg.Documents.MaxEditableBytes != 10485760 {
		t.Errorf("max_editable_bytes = %d, want 10485760 (N3)", cfg.Documents.MaxEditableBytes)
	}
}

func TestLoadRejectsMissingSchemaVersion(t *testing.T) {
	path := writeWorkspace(t, "name = \"Fixture\"\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("a config without schema_version was accepted")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Errorf("error does not name the missing field: %v", err)
	}
}

func TestLoadRejectsUnsupportedSchemaVersion(t *testing.T) {
	path := writeWorkspace(t, "schema_version = 99\nname = \"Fixture\"\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("an unsupported schema_version was accepted")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("error does not report the offending version: %v", err)
	}
}

func TestLoadRejectsMissingName(t *testing.T) {
	path := writeWorkspace(t, "schema_version = 1\n")

	if _, err := Load(path); err == nil {
		t.Fatal("a config without a name was accepted")
	}
}

func TestLoadRejectsMalformedTOML(t *testing.T) {
	path := writeWorkspace(t, "schema_version = = 1\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("malformed TOML was accepted")
	}
	// Acceptance B4 requires the message to identify the file.
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error does not name the config file: %v", err)
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err == nil {
		t.Fatal("a missing config file was accepted")
	}
}

func TestLoadRejectsUnreadableRoot(t *testing.T) {
	path := writeWorkspace(t, "schema_version = 1\nname = \"Fixture\"\nroot = \"does-not-exist\"\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("a config naming an absent root was accepted")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Errorf("error does not mention the root: %v", err)
	}
}

// TestRootIsRelativeToConfigFile keeps a workspace portable when it is moved
// or cloned to a different absolute location.
func TestRootIsRelativeToConfigFile(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, DefaultFileName)
	if err := os.WriteFile(path, []byte("schema_version = 1\nname = \"Fixture\"\nroot = \"workspace\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantSuffix := filepath.Join("workspace")
	if !strings.HasSuffix(cfg.AbsRoot, wantSuffix) {
		t.Errorf("AbsRoot = %q, want it to end with %q", cfg.AbsRoot, wantSuffix)
	}
}

// TestApplySafeMode covers the --safe-mode contract in spec 05 section 4.
func TestApplySafeMode(t *testing.T) {
	cfg, err := Load(writeWorkspace(t, `
schema_version = 1
name = "Fixture"

[documents]
raw_html = true
mermaid = true

[assets]
allow_remote = true

[git]
enabled = true
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cfg.ApplySafeMode()

	if cfg.Git.Enabled {
		t.Error("safe mode left Git enabled")
	}
	if cfg.Documents.RawHTML {
		t.Error("safe mode left raw HTML enabled")
	}
	if cfg.Documents.Mermaid {
		t.Error("safe mode left Mermaid enabled")
	}
	if cfg.Assets.AllowRemote {
		t.Error("safe mode left remote assets enabled")
	}
}
