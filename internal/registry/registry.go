// Package registry reads the workspace registry described by ADR-0004.
//
// The registry is a launcher, not a mount table. It lists named workspaces so
// the user can pick one; opening an entry is exactly equivalent to launching
// with that path. A session still has one root, one write boundary, and one
// index — nothing in this package loads two workspaces, and nothing in it may
// be widened until D-006 is amended.
//
// Athenaeum only ever reads this file. Writing it would create a sixth data
// authority outside every workspace, which spec 03 section 1 does not define
// and constitution C3 requires to be explicit. The user edits it by hand.
package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"athenaeum/internal/config"
	"athenaeum/internal/security"
)

// FileName is the registry file inside the Athenaeum user-config directory.
const FileName = "workspaces.toml"

// Availability codes. Every unavailable entry carries one so the API can return
// a stable code (requirement N6) and the picker can explain itself without
// parsing prose.
const (
	// CodePathMissing means the registered path does not exist.
	CodePathMissing = "REGISTRY_PATH_MISSING"
	// CodePathUnreadable means the path exists but could not be examined.
	CodePathUnreadable = "REGISTRY_PATH_UNREADABLE"
	// CodeConfigMissing means the directory holds no athenaeum.toml.
	CodeConfigMissing = "REGISTRY_CONFIG_MISSING"
	// CodeConfigInvalid means the configuration could not be loaded or failed
	// validation.
	CodeConfigInvalid = "REGISTRY_CONFIG_INVALID"
	// CodeNameUnknown means no entry carries the requested name.
	CodeNameUnknown = "REGISTRY_NAME_UNKNOWN"
	// CodeNameAmbiguous means several entries share the requested name.
	CodeNameAmbiguous = "REGISTRY_NAME_AMBIGUOUS"
)

// Entry is one registered workspace.
//
// An entry is self-describing: when it cannot be opened it says why and what to
// do about it, in the same shape as a configuration diagnostic (R1).
type Entry struct {
	// Name labels the workspace in the picker. It comes from the registry file,
	// falling back to the workspace's configured name and then to the directory
	// name, so an entry always has something to show.
	Name string
	// RawPath is the path exactly as written in the registry, kept for
	// diagnostics so a message can quote what the user typed.
	RawPath string
	// Path is the resolved, canonical absolute path of the entry: the directory
	// when it is a workspace, otherwise the best resolution available.
	Path string
	// ConfigPath is the canonical absolute path of the workspace configuration
	// file. Empty when the entry is unavailable.
	ConfigPath string
	// Available reports whether this entry can be opened right now.
	Available bool
	// Code is a stable reason code when Available is false.
	Code string
	// Reason states what is wrong, and Remedy states what to do about it.
	Reason string
	Remedy string
}

// Registry is the loaded workspace list.
type Registry struct {
	// SourcePath is the registry file that was read. It is reported even when
	// the file is absent, so the picker can name the file to create.
	SourcePath string
	// Present reports whether the registry file exists. A missing registry is
	// not an error: it means an empty list.
	Present bool
	// Entries are the registered workspaces in file order, available or not.
	Entries []Entry
	// Diagnostics are file-level problems: duplicates, unknown keys, entries
	// that could not be resolved at all. They never stop the other entries
	// loading.
	Diagnostics config.Diagnostics
}

// DefaultPath returns <user-config>/athenaeum/workspaces.toml.
//
// Spec 05 section 1 already sanctions reading from that directory, so the
// registry adds no new authority. Tests pass an explicit path instead: nothing
// in the test suite may read the developer's real configuration (spec 07
// section 5).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate the user config directory: %w", err)
	}
	return filepath.Join(dir, "athenaeum", FileName), nil
}

// registryFile mirrors the on-disk shape.
type registryFile struct {
	Workspace []registryEntry `toml:"workspace"`
}

type registryEntry struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}

// Load reads the registry at path.
//
// A missing file yields an empty registry and no error. Every other failure
// that concerns a single entry is recorded on that entry: one broken workspace
// must never hide the rest, which is the whole point of a launcher.
func Load(path string) (*Registry, error) {
	reg := &Registry{SourcePath: path}

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return reg, nil
		}
		return nil, fmt.Errorf("read the workspace registry %s: %w", path, err)
	}
	reg.Present = true

	var file registryFile
	md, err := toml.Decode(string(raw), &file)
	if err != nil {
		return nil, fmt.Errorf("parse the workspace registry %s: %w", path, err)
	}

	// Unknown keys are warnings rather than errors here. The workspace
	// configuration makes them errors because a typo in a security field is
	// dangerous; the registry grants no authority at all, and a stray key must
	// not stop the user reaching their workspaces.
	for _, key := range md.Undecoded() {
		reg.Diagnostics.Warn(key.String(),
			"unknown key in the workspace registry",
			"the registry understands [[workspace]] tables with name and path")
	}

	base := filepath.Dir(path)
	for i, item := range file.Workspace {
		reg.Entries = append(reg.Entries, resolveEntry(&reg.Diagnostics, i, base, item))
	}

	reg.checkDuplicates()
	return reg, nil
}

// resolveEntry turns one registry record into an Entry, deciding availability.
func resolveEntry(ds *config.Diagnostics, index int, base string, item registryEntry) Entry {
	field := fmt.Sprintf("workspace[%d]", index)
	entry := Entry{Name: strings.TrimSpace(item.Name), RawPath: item.Path}

	if strings.TrimSpace(item.Path) == "" {
		ds.Error(field+".path",
			"a registered workspace needs a path",
			"set path to a directory holding athenaeum.toml, or to the file itself")
		entry.Code = CodePathMissing
		entry.Reason = "no path is configured for this entry"
		entry.Remedy = "set path in " + field + " of the workspace registry"
		if entry.Name == "" {
			entry.Name = fmt.Sprintf("entry %d", index+1)
		}
		return entry
	}

	abs, err := resolvePath(base, item.Path)
	if err != nil {
		ds.Error(field+".path", err.Error(),
			"use an absolute path, a path starting with ~/, or a path relative to the registry file")
		entry.Path = item.Path
		entry.Code = CodePathUnreadable
		entry.Reason = err.Error()
		entry.Remedy = "correct path in " + field + " of the workspace registry"
		entry.Name = fallbackName(entry.Name, item.Path)
		return entry
	}

	// Canonicalise before anything compares paths. On macOS /var is a symlink to
	// /private/var, and this project has twice been bitten by comparing a
	// non-canonical path against a canonical root.
	entry.Path = security.Canonicalise(abs)
	entry.Name = fallbackName(entry.Name, entry.Path)

	configPath, code, reason, remedy := locateConfig(entry.Path)
	if code != "" {
		entry.Code, entry.Reason, entry.Remedy = code, reason, remedy
		return entry
	}

	// Loading proves the file is readable and structurally sound. It is the
	// cheapest honest answer to "can this be opened?", and it costs one small
	// file per entry — no workspace is enumerated here.
	cfg, err := config.Load(configPath)
	if err != nil {
		entry.Code = CodeConfigInvalid
		entry.Reason = "the workspace configuration could not be loaded"
		entry.Remedy = "run `athenaeum validate " + configPath + "` to see the detail"
		return entry
	}
	if diags := cfg.Validate(); diags.HasErrors() {
		count, _ := diags.Counts()
		entry.Code = CodeConfigInvalid
		entry.Reason = fmt.Sprintf("the workspace configuration has %d error(s)", count)
		entry.Remedy = "run `athenaeum validate " + configPath + "` to see the detail"
		return entry
	}

	entry.ConfigPath = cfg.SourcePath
	// The workspace's own root is authoritative for what the entry points at:
	// a configuration may set root to something other than its own directory.
	entry.Path = cfg.AbsRoot
	if item.Name == "" && cfg.Name != "" {
		entry.Name = cfg.Name
	}
	entry.Available = true
	return entry
}

// locateConfig finds the workspace configuration for a resolved path.
//
// ADR-0004 allows a registered path to name either a directory holding
// athenaeum.toml or the configuration file itself.
func locateConfig(path string) (configPath, code, reason, remedy string) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", CodePathMissing,
				"this path does not exist",
				"create the directory, or correct the path in the workspace registry"
		}
		return "", CodePathUnreadable,
			"this path could not be examined",
			"check the permissions on the path, or correct it in the workspace registry"
	}

	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return "", CodePathUnreadable,
				"this path is not a regular file or a directory",
				"point the entry at a workspace directory or at its athenaeum.toml"
		}
		return path, "", "", ""
	}

	candidate := filepath.Join(path, config.DefaultFileName)
	if _, err := os.Stat(candidate); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", CodeConfigMissing,
				"this directory holds no " + config.DefaultFileName,
				"create " + config.DefaultFileName + " in the directory, or point the entry at one elsewhere"
		}
		return "", CodePathUnreadable,
			config.DefaultFileName + " could not be read",
			"check the permissions on the file"
	}
	return candidate, "", "", ""
}

// resolvePath expands ~ and makes a relative path absolute against the registry
// file's own directory, so a registry stays portable when it is moved.
func resolvePath(base, raw string) (string, error) {
	path := strings.TrimSpace(raw)

	switch {
	case path == "~":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%q needs a home directory, which could not be located", raw)
		}
		path = home
	case strings.HasPrefix(path, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%q needs a home directory, which could not be located", raw)
		}
		path = filepath.Join(home, path[2:])
	case strings.HasPrefix(path, "~"):
		// ~otheruser needs the password database, which Athenaeum will not read.
		return "", fmt.Errorf("%q expands another user's home directory, which is not supported", raw)
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path), nil
}

// fallbackName gives an unnamed entry something to show.
func fallbackName(name, path string) string {
	if name != "" {
		return name
	}
	if base := filepath.Base(path); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return path
}

// checkDuplicates reports repeated names and repeated paths.
//
// Neither is fatal — the file is the user's — but a duplicate name makes the
// entry unselectable by name, and a duplicate path is almost always a mistake.
func (r *Registry) checkDuplicates() {
	seenName := map[string]int{}
	seenPath := map[string]int{}

	for i, entry := range r.Entries {
		field := fmt.Sprintf("workspace[%d]", i)

		if first, ok := seenName[entry.Name]; ok {
			r.Diagnostics.Warn(field+".name",
				fmt.Sprintf("the name %q is already used by workspace[%d]", entry.Name, first),
				"give each registered workspace a distinct name; an ambiguous name cannot be opened by name")
		} else {
			seenName[entry.Name] = i
		}

		if entry.Path == "" {
			continue
		}
		if first, ok := seenPath[entry.Path]; ok {
			r.Diagnostics.Warn(field+".path",
				fmt.Sprintf("this path is already registered by workspace[%d]", first),
				"remove the duplicate entry; both open the same workspace")
		} else {
			seenPath[entry.Path] = i
		}
	}
}

// LookupError reports a failed lookup with a stable code.
type LookupError struct {
	Code   string
	Name   string
	Reason string
}

func (e *LookupError) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Reason, e.Name) }

// Lookup finds the single entry with the given name.
//
// An ambiguous name is refused rather than guessed. Silently opening the first
// of two identically named workspaces would be exactly the hidden behaviour C8
// prohibits.
func (r *Registry) Lookup(name string) (Entry, error) {
	var (
		found Entry
		count int
	)
	for _, entry := range r.Entries {
		if entry.Name == name {
			found = entry
			count++
		}
	}

	switch count {
	case 0:
		return Entry{}, &LookupError{Code: CodeNameUnknown, Name: name,
			Reason: "no workspace with that name is registered"}
	case 1:
		return found, nil
	default:
		return Entry{}, &LookupError{Code: CodeNameAmbiguous, Name: name,
			Reason: fmt.Sprintf("%d registered workspaces share that name", count)}
	}
}

// Available returns the entries that can be opened.
func (r *Registry) Available() []Entry {
	out := make([]Entry, 0, len(r.Entries))
	for _, entry := range r.Entries {
		if entry.Available {
			out = append(out, entry)
		}
	}
	return out
}
