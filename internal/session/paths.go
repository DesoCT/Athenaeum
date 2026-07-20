// Package session persists UI state and crash-recovery buffers outside the
// workspace (spec 02 section 3.11, spec 03 sections 1 and 2.3).
//
// Nothing this package writes is authoritative. Losing all of it costs the
// user their layout and any unsaved buffers, never a saved document.
package session

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// WorkspaceKey identifies a workspace's private state directories.
//
// Spec 03 section 2.2 requires a stable hash of the canonical root path, and
// requires that the raw path never appear in a filename — a workspace path can
// itself be sensitive.
type WorkspaceKey string

// NewWorkspaceKey derives the key for a canonical workspace root.
//
// uuid is the optional configured workspace identifier. When present it takes
// precedence, so moving or renaming a workspace keeps its personal state. That
// resolves the instability noted during the Phase 0 review: without it, the key
// is path-derived and a move orphans everything.
func NewWorkspaceKey(canonicalRoot, uuid string) WorkspaceKey {
	material := canonicalRoot
	if uuid != "" {
		material = "uuid:" + uuid
	}
	// On case-insensitive filesystems the same workspace can be addressed by
	// differently-cased paths; folding keeps one key per workspace.
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		material = strings.ToLower(material)
	}

	sum := sha256.Sum256([]byte(material))
	// Base32 without padding is filename-safe on every platform and
	// case-insensitive, unlike base64.
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
	return WorkspaceKey(strings.ToLower(encoded[:26]))
}

// Dirs are the per-workspace private directories.
type Dirs struct {
	// State holds session layout and recovery buffers.
	State string
	// Cache holds the disposable search index.
	Cache string
	// Data holds personal annotations and notes.
	Data string
}

// Recovery is the directory holding crash-recovery buffers.
func (d Dirs) Recovery() string { return filepath.Join(d.State, "recovery") }

// SessionFile is the persisted UI state file.
func (d Dirs) SessionFile() string { return filepath.Join(d.State, "session.json") }

// ResolveDirs returns the per-workspace directories, creating them if needed.
//
// Layout follows spec 03 section 2.3 and the operating system's conventions:
// os.UserConfigDir and os.UserCacheDir already do the right thing on macOS,
// Linux, and Windows.
func ResolveDirs(key WorkspaceKey) (Dirs, error) {
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return Dirs{}, fmt.Errorf("locate the user cache directory: %w", err)
	}
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return Dirs{}, fmt.Errorf("locate the user config directory: %w", err)
	}

	// XDG separates state from config on Linux; elsewhere the config root is
	// the conventional home for both.
	stateRoot := configRoot
	if runtime.GOOS == "linux" {
		if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
			stateRoot = xdg
		} else if home, err := os.UserHomeDir(); err == nil {
			stateRoot = filepath.Join(home, ".local", "state")
		}
	}

	dirs := Dirs{
		State: filepath.Join(stateRoot, "athenaeum", string(key)),
		Cache: filepath.Join(cacheRoot, "athenaeum", string(key)),
		Data:  filepath.Join(configRoot, "athenaeum", "workspaces", string(key)),
	}

	// 0o700: recovery buffers contain document text, so they are private.
	for _, dir := range []string{dirs.State, dirs.Cache, dirs.Data, dirs.Recovery()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Dirs{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return dirs, nil
}
