package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Validate applies the semantic rules in spec 05 section 6 and requirement R1.
//
// Parse-level failures are reported by Load. Validate covers everything that
// needs the resolved configuration: pattern syntax, containment, duplicate
// identifiers, and combinations that are individually legal but jointly unsafe.
func (c *Config) Validate() Diagnostics {
	var ds Diagnostics

	c.checkUnknownFields(&ds)
	c.checkPatterns(&ds)
	c.checkWritable(&ds)
	c.checkDirectories(&ds)
	c.checkGroups(&ds)
	c.checkDocuments(&ds)
	c.checkVisibility(&ds)

	return ds
}

// checkUnknownFields turns every undecoded key into an error. Spec 05 section 6
// makes unknown top-level fields errors in v0.1; a typo in a security field
// must never be silently ignored.
func (c *Config) checkUnknownFields(ds *Diagnostics) {
	for _, key := range c.undecoded {
		ds.errorf(key,
			"unknown configuration field",
			"remove the field, or check it against docs/spec/05-CONFIGURATION-SCHEMA.md for the correct spelling")
	}
}

func (c *Config) checkPatterns(ds *Diagnostics) {
	for i, pattern := range c.Include {
		field := fmt.Sprintf("include[%d]", i)
		validatePattern(ds, field, pattern)
		if strings.HasPrefix(pattern, "/") || filepath.IsAbs(pattern) {
			ds.errorf(field,
				fmt.Sprintf("include pattern %q is absolute", pattern),
				"use a path relative to the workspace root")
		}
		if escapesRoot(pattern) {
			ds.errorf(field,
				fmt.Sprintf("include pattern %q escapes the workspace root", pattern),
				"remove the leading .. segments; a workspace has one root in v0.1")
		}
	}
	for i, pattern := range c.Exclude {
		validatePattern(ds, fmt.Sprintf("exclude[%d]", i), pattern)
	}

	if len(c.Include) == 0 {
		ds.warnf("include",
			"no include patterns are configured, so the workspace contains no documents",
			`add at least one pattern, for example include = ["**/*.md"]`)
	}

	// Duplicate include entries are a sign of a merge accident and make the
	// "matches nothing" warning ambiguous.
	seen := map[string]int{}
	for i, pattern := range c.Include {
		if first, ok := seen[pattern]; ok {
			ds.warnf(fmt.Sprintf("include[%d]", i),
				fmt.Sprintf("include pattern %q duplicates include[%d]", pattern, first),
				"remove the duplicate entry")
			continue
		}
		seen[pattern] = i
	}
}

// checkWritable enforces the rule that the write boundary cannot leave the
// workspace root (spec 05 section 6, spec 03 section 7).
func (c *Config) checkWritable(ds *Diagnostics) {
	for i, pattern := range c.Security.Writable {
		field := fmt.Sprintf("security.writable[%d]", i)
		validatePattern(ds, field, pattern)

		if strings.HasPrefix(pattern, "/") || filepath.IsAbs(pattern) {
			ds.errorf(field,
				fmt.Sprintf("writable pattern %q is absolute", pattern),
				"use a path relative to the workspace root; Athenaeum never grants write authority outside the root")
			continue
		}
		if escapesRoot(pattern) {
			ds.errorf(field,
				fmt.Sprintf("writable pattern %q escapes the workspace root", pattern),
				"remove the .. segments; writable paths must resolve inside the root")
		}
	}

	// A writable entry that no include pattern can ever match grants authority
	// over a file the workspace cannot open. That is not dangerous, but it is
	// almost always a mistake worth surfacing.
	for i, pattern := range c.Security.Writable {
		if isSidecarPattern(pattern) || isAssetPattern(pattern, c.Assets.Directory) {
			continue
		}
		if !slices.ContainsFunc(c.Include, func(inc string) bool {
			return patternsOverlap(inc, pattern)
		}) {
			ds.warnf(fmt.Sprintf("security.writable[%d]", i),
				fmt.Sprintf("writable pattern %q does not correspond to any include pattern", pattern),
				"add a matching include pattern, or remove the writable entry")
		}
	}
}

// checkDirectories requires the asset and shared sidecar directories to resolve
// inside the workspace (spec 05 section 6).
func (c *Config) checkDirectories(ds *Diagnostics) {
	c.checkContainedDirectory(ds, "assets.directory", c.Assets.Directory,
		"managed assets are workspace content and must live inside the root")
	c.checkContainedDirectory(ds, "annotations.shared_directory", c.Annotations.SharedDirectory,
		"shared sidecars are intended to be committed with the workspace")
}

func (c *Config) checkContainedDirectory(ds *Diagnostics, field, dir, why string) {
	if dir == "" {
		ds.errorf(field, "the directory is empty", "set a path relative to the workspace root")
		return
	}
	if filepath.IsAbs(dir) || strings.HasPrefix(dir, "/") {
		ds.errorf(field,
			fmt.Sprintf("%q is absolute", dir),
			"use a path relative to the workspace root; "+why)
		return
	}
	if escapesRoot(dir) {
		ds.errorf(field,
			fmt.Sprintf("%q escapes the workspace root", dir),
			"remove the .. segments; "+why)
		return
	}

	// If the directory already exists, confirm it really resolves inside the
	// root — a symlinked assets/ pointing elsewhere would otherwise pass.
	if c.AbsRoot == "" {
		return
	}
	candidate := filepath.Join(c.AbsRoot, filepath.FromSlash(dir))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		// Not existing yet is fine; it is created on first use.
		return
	}
	rel, err := filepath.Rel(c.AbsRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		ds.errorf(field,
			fmt.Sprintf("%q resolves outside the workspace root (via a symlink)", dir),
			"point the directory at a real location inside the root; "+why)
	}
}

func (c *Config) checkGroups(ds *Diagnostics) {
	seen := map[string]int{}
	for i, group := range c.Groups {
		field := fmt.Sprintf("groups[%d]", i)

		if group.ID == "" {
			ds.errorf(field+".id", "a group needs an id", "add a short slug, for example id = \"design\"")
		} else if first, ok := seen[group.ID]; ok {
			// Spec 05 section 6: duplicate group IDs are errors.
			ds.errorf(field+".id",
				fmt.Sprintf("duplicate group id %q, first defined at groups[%d]", group.ID, first),
				"give each group a unique id")
		} else {
			seen[group.ID] = i
		}

		if group.Title == "" {
			ds.warnf(field+".title",
				fmt.Sprintf("group %q has no title, so the Map Room will show its id", group.ID),
				"add a human-readable title")
		}
		if len(group.Patterns) == 0 {
			ds.warnf(field+".patterns",
				fmt.Sprintf("group %q has no patterns and will always be empty", group.ID),
				"add at least one pattern, or remove the group")
		}
		for j, pattern := range group.Patterns {
			validatePattern(ds, fmt.Sprintf("%s.patterns[%d]", field, j), pattern)
		}
	}
}

func (c *Config) checkDocuments(ds *Diagnostics) {
	if c.Documents.MaxEditableBytes <= 0 {
		ds.errorf("documents.max_editable_bytes",
			"the limit must be greater than zero",
			"remove the field to accept the default of 10485760 (10 MB)")
	}
	if c.Documents.LargeFileWarningBytes < 0 {
		ds.errorf("documents.large_file_warning_bytes",
			"the threshold must not be negative",
			"remove the field to accept the default of 2097152 (2 MB)")
	}
	if c.Documents.LargeFileWarningBytes > c.Documents.MaxEditableBytes && c.Documents.MaxEditableBytes > 0 {
		ds.warnf("documents.large_file_warning_bytes",
			"the warning threshold is above the editable limit, so it can never trigger",
			"set large_file_warning_bytes below max_editable_bytes")
	}

	for i, format := range c.Documents.FrontMatter {
		if format != "yaml" && format != "toml" {
			ds.errorf(fmt.Sprintf("documents.front_matter[%d]", i),
				fmt.Sprintf("unsupported front matter format %q", format),
				`use "yaml", "toml", or both`)
		}
	}

	// Spec 05 section 6: raw HTML combined with remote mode is high severity.
	// Remote mode is a launch flag, so this is surfaced again at startup; here
	// it warns that the workspace is configured in a way that makes remote
	// serving unsafe.
	if c.Documents.RawHTML {
		ds.warnf("documents.raw_html",
			"raw HTML is enabled, so document authors can inject arbitrary markup into the preview",
			"leave raw_html = false unless every document author is trusted; serving this workspace with --remote is rejected")
	}
}

func (c *Config) checkVisibility(ds *Diagnostics) {
	switch c.Annotations.DefaultVisibility {
	case "personal", "shared":
	case "":
		ds.errorf("annotations.default_visibility",
			"the default visibility is empty",
			`use "personal" or "shared"`)
	default:
		ds.errorf("annotations.default_visibility",
			fmt.Sprintf("unknown visibility %q", c.Annotations.DefaultVisibility),
			`use "personal" or "shared"`)
	}
}

// validatePattern reports a malformed glob.
func validatePattern(ds *Diagnostics, field, pattern string) {
	if pattern == "" {
		ds.errorf(field, "the pattern is empty", "remove the entry or give it a value")
		return
	}
	if !doublestar.ValidatePattern(pattern) {
		ds.errorf(field,
			fmt.Sprintf("%q is not a valid glob pattern", pattern),
			"check for an unclosed [ or { group; ** matches across directories and * does not")
	}
}

// escapesRoot reports a pattern or path with a leading parent segment.
func escapesRoot(pattern string) bool {
	clean := filepath.ToSlash(filepath.Clean(pattern))
	return clean == ".." || strings.HasPrefix(clean, "../")
}

func isSidecarPattern(pattern string) bool {
	return strings.HasPrefix(pattern, ".athenaeum/")
}

func isAssetPattern(pattern, assetDir string) bool {
	if assetDir == "" {
		return false
	}
	return strings.HasPrefix(pattern, strings.TrimSuffix(assetDir, "/")+"/")
}

// patternsOverlap is a deliberately conservative check used only to raise a
// warning. It reports true whenever the two patterns could plausibly describe
// the same files, so a false positive silences a warning rather than inventing
// an error.
func patternsOverlap(include, writable string) bool {
	if include == writable {
		return true
	}
	// A literal writable path matched by the include pattern.
	if ok, err := doublestar.Match(include, writable); err == nil && ok {
		return true
	}
	// A literal include path matched by the writable pattern.
	if ok, err := doublestar.Match(writable, include); err == nil && ok {
		return true
	}
	// Shared directory prefix, e.g. "docs/**/*.md" and "docs/**".
	return sharedPrefix(include) != "" && sharedPrefix(include) == sharedPrefix(writable)
}

// sharedPrefix returns the leading literal directory segments of a pattern.
func sharedPrefix(pattern string) string {
	var parts []string
	for _, segment := range strings.Split(filepath.ToSlash(pattern), "/") {
		if strings.ContainsAny(segment, "*?[{") {
			break
		}
		parts = append(parts, segment)
	}
	return strings.Join(parts, "/")
}

// ValidateRuntime applies checks that depend on launch options rather than the
// file alone. Spec 05 section 6 rejects raw HTML in remote mode.
func (c *Config) ValidateRuntime(remote bool) Diagnostics {
	var ds Diagnostics
	if remote && c.Documents.RawHTML {
		ds.errorf("documents.raw_html",
			"raw HTML is enabled and the workspace is being served remotely",
			"set raw_html = false, or serve this workspace on loopback only")
	}
	return ds
}

// ReadableRoot reports whether the workspace root is readable, which Load has
// already established. It exists so `validate` can state the fact explicitly.
func (c *Config) ReadableRoot() error {
	info, err := os.Stat(c.AbsRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", c.AbsRoot)
	}
	return nil
}
