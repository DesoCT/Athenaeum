// Package config loads and resolves the workspace configuration described in
// spec 05.
//
// Phase 0 scope: enough loading and structural checking to launch a workspace
// (acceptance A1). The full validation surface required by R1 and acceptance
// B4 — glob correctness, overlap detection, writable-path containment, unknown
// field rejection — is Phase 1 work and is deliberately not implemented here.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// SchemaVersion is the only workspace schema version v0.1 understands.
const SchemaVersion = 1

// DefaultFileName is the conventional workspace configuration file name.
const DefaultFileName = "athenaeum.toml"

// Config is the effective, resolved workspace configuration.
type Config struct {
	SchemaVersion int      `toml:"schema_version"`
	Name          string   `toml:"name"`
	Root          string   `toml:"root"`
	Include       []string `toml:"include"`
	Exclude       []string `toml:"exclude"`

	Documents     Documents     `toml:"documents"`
	Assets        Assets        `toml:"assets"`
	Annotations   Annotations   `toml:"annotations"`
	Search        Search        `toml:"search"`
	Git           Git           `toml:"git"`
	Security      Security      `toml:"security"`
	Relationships Relationships `toml:"relationships"`
	Groups        []Group       `toml:"groups"`

	// SourcePath is the absolute path of the loaded configuration file.
	SourcePath string `toml:"-"`
	// AbsRoot is the canonicalised absolute workspace root.
	AbsRoot string `toml:"-"`

	// undecoded holds keys present in the file that no field claimed. Spec 05
	// section 6 makes these errors, reported by Validate.
	undecoded []string
}

// Documents holds renderer and editor limits (spec 05 section 2).
type Documents struct {
	RawHTML               bool     `toml:"raw_html"`
	WikiLinks             bool     `toml:"wiki_links"`
	Footnotes             bool     `toml:"footnotes"`
	Callouts              bool     `toml:"callouts"`
	Math                  bool     `toml:"math"`
	Mermaid               bool     `toml:"mermaid"`
	FrontMatter           []string `toml:"front_matter"`
	MaxEditableBytes      int64    `toml:"max_editable_bytes"`
	LargeFileWarningBytes int64    `toml:"large_file_warning_bytes"`
}

// Assets configures the managed asset directory (R11, D-016).
type Assets struct {
	Directory   string `toml:"directory"`
	AllowRemote bool   `toml:"allow_remote"`
	PasteNaming string `toml:"paste_naming"`
}

// Annotations configures sidecar placement and default visibility (D-009).
type Annotations struct {
	SharedDirectory   string `toml:"shared_directory"`
	DefaultVisibility string `toml:"default_visibility"`
}

// Search configures the disposable FTS projection (R7, D-014).
type Search struct {
	Enabled          bool `toml:"enabled"`
	IndexCodeBlocks  bool `toml:"index_code_blocks"`
	IndexFrontMatter bool `toml:"index_front_matter"`
}

// Git toggles the read-only Git context (R12, D-019).
type Git struct {
	Enabled bool `toml:"enabled"`
}

// Relationships configures which front-matter fields name explicit
// relationships between documents (R10, spec 05 section 2). Athenaeum never
// infers a relationship; these fields are the only front-matter source it reads.
type Relationships struct {
	FrontMatter RelationshipFrontMatter `toml:"front_matter"`
}

// RelationshipFrontMatter lists the front-matter keys treated as relationships.
// The key name is the relationship kind (e.g. "related", "implements").
type RelationshipFrontMatter struct {
	Fields []string `toml:"fields"`
}

// Security holds the write boundary and external-read authority (spec 03).
type Security struct {
	Writable           []string `toml:"writable"`
	AllowExternalReads bool     `toml:"allow_external_reads"`
}

// Group is a configured document grouping shown in the Map Room (R2).
type Group struct {
	ID       string   `toml:"id"`
	Title    string   `toml:"title"`
	Patterns []string `toml:"patterns"`
}

// Defaults returns the baseline configuration before any file is applied.
func Defaults() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		Root:          ".",
		Exclude:       []string{"**/node_modules/**", "**/vendor/**", ".git/**"},
		Documents: Documents{
			RawHTML:               false,
			FrontMatter:           []string{"yaml", "toml"},
			MaxEditableBytes:      10485760,
			LargeFileWarningBytes: 2097152,
		},
		Assets:      Assets{Directory: "assets", AllowRemote: true, PasteNaming: "date-hash"},
		Annotations: Annotations{SharedDirectory: ".athenaeum/shared", DefaultVisibility: "personal"},
		Search:      Search{Enabled: true},
		Git:         Git{Enabled: true},
	}
}

// Load reads and resolves a workspace configuration.
//
// path may be a configuration file or a directory containing one. An empty
// path resolves to DefaultFileName in the working directory.
func Load(path string) (*Config, error) {
	resolved, err := resolveConfigPath(path)
	if err != nil {
		return nil, err
	}

	cfg := Defaults()
	md, err := toml.DecodeFile(resolved, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", resolved, err)
	}

	cfg.SourcePath = resolved
	if err := cfg.finalise(md); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func resolveConfigPath(path string) (string, error) {
	if path == "" {
		path = DefaultFileName
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("read config %s: %w", abs, err)
	}
	if info.IsDir() {
		abs = filepath.Join(abs, DefaultFileName)
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("read config %s: %w", abs, err)
		}
	}
	return abs, nil
}

// finalise applies the checks that must pass before anything else can run, and
// canonicalises the workspace root. Semantic validation lives in Validate.
func (c *Config) finalise(md toml.MetaData) error {
	for _, key := range md.Undecoded() {
		c.undecoded = append(c.undecoded, key.String())
	}

	if !md.IsDefined("schema_version") {
		return errors.New("config: schema_version is required (spec 05 section 6)")
	}
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("config: schema_version %d is unsupported; this build understands %d",
			c.SchemaVersion, SchemaVersion)
	}
	if c.Name == "" {
		return errors.New("config: name is required and names the workspace in the Map Room")
	}

	// The root is relative to the directory holding the configuration file, so
	// a workspace stays portable when it is moved or cloned.
	base := filepath.Dir(c.SourcePath)
	root := c.Root
	if root == "" {
		root = "."
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(base, root)
	}
	// EvalSymlinks canonicalises the root so every later containment check
	// compares against a real path (spec 03 section 6 step 1).
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("config: workspace root %s is not readable: %w", root, err)
	}
	abs, err := filepath.Abs(canonical)
	if err != nil {
		return fmt.Errorf("config: canonicalise workspace root %s: %w", canonical, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("config: workspace root %s is not readable: %w", abs, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("config: workspace root %s is not a directory", abs)
	}
	c.AbsRoot = abs
	return nil
}

// ApplySafeMode disables the feature surface that --safe-mode must switch off
// (spec 05 section 4). Documents remain readable and editable.
func (c *Config) ApplySafeMode() {
	c.Git.Enabled = false
	c.Documents.RawHTML = false
	c.Documents.Mermaid = false
	c.Assets.AllowRemote = false
}
