// Package workspace enumerates the documents a configuration includes and
// enforces the root and write boundaries (spec 02 section 3.2).
package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"

	"athenaeum/internal/config"
	"athenaeum/internal/security"
)

// Document is one included file.
type Document struct {
	// ID is the stable, slash-normalised path relative to the canonical root.
	ID string `json:"id"`
	// Title is the best available human label: front matter title, first
	// heading, or the file name. Filled in by the documents service; the
	// enumerator leaves it as the file name.
	Title string `json:"title"`
	// Size is the file size in bytes.
	Size int64 `json:"size"`
	// ModTime is the filesystem modification time, in RFC 3339 UTC.
	ModTime string `json:"mod_time"`
	// Groups lists the configured group IDs this document belongs to.
	Groups []string `json:"groups,omitempty"`
	// Writable reports whether the write boundary permits saving this file.
	Writable bool `json:"writable"`
	// TooLarge reports a file above documents.max_editable_bytes.
	TooLarge bool `json:"too_large"`
	// LargeWarning reports a file above documents.large_file_warning_bytes.
	LargeWarning bool `json:"large_warning"`
}

// Workspace is the enumerated, validated view of a configured root.
type Workspace struct {
	cfg   *config.Config
	guard *security.PathGuard

	mu        sync.RWMutex
	documents map[string]*Document
	order     []string
	diags     config.Diagnostics
}

// Open enumerates a workspace from an already-loaded configuration.
func Open(cfg *config.Config) (*Workspace, error) {
	guard, err := security.NewPathGuard(cfg.AbsRoot, cfg.Security.AllowExternalReads)
	if err != nil {
		return nil, err
	}
	ws := &Workspace{
		cfg:       cfg,
		guard:     guard,
		documents: make(map[string]*Document),
	}
	if err := ws.Refresh(); err != nil {
		return nil, err
	}
	return ws, nil
}

// Config returns the configuration this workspace was opened with.
func (w *Workspace) Config() *config.Config { return w.cfg }

// Guard returns the path guard bound to this workspace root.
func (w *Workspace) Guard() *security.PathGuard { return w.guard }

// Diagnostics returns enumeration warnings, such as include patterns that
// matched nothing or files that could not be read.
func (w *Workspace) Diagnostics() config.Diagnostics {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return slices.Clone(w.diags)
}

// Refresh re-enumerates the workspace from disk.
func (w *Workspace) Refresh() error {
	documents, order, diags, err := w.enumerate()
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.documents, w.order, w.diags = documents, order, diags
	w.mu.Unlock()
	return nil
}

// enumerate walks the root once, applying excludes during the walk so that
// large ignored trees such as node_modules are never descended into.
func (w *Workspace) enumerate() (map[string]*Document, []string, config.Diagnostics, error) {
	var diags config.Diagnostics
	documents := make(map[string]*Document)
	matchedInclude := make([]bool, len(w.cfg.Include))
	root := w.guard.Root()

	err := filepath.WalkDir(root, func(absPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory is a warning, not a fatal error: the rest
			// of the workspace remains usable (requirement R1).
			id, idErr := w.guard.DocumentID(absPath)
			if idErr != nil {
				id = absPath
			}
			diags.Warn("include", fmt.Sprintf("%s could not be read: %v", id, err),
				"check the file permissions, or exclude the path")
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if absPath == root {
			return nil
		}

		id, err := w.guard.DocumentID(absPath)
		if err != nil {
			return nil
		}

		if entry.IsDir() {
			// Always skip the Git directory and the sidecar directory; neither
			// is workspace content.
			base := entry.Name()
			if base == ".git" {
				return filepath.SkipDir
			}
			if w.excludedDir(id) {
				return filepath.SkipDir
			}
			return nil
		}

		// Symlinked files are enumerated only when external reads are enabled,
		// and they are never writable (spec 03 section 6).
		if entry.Type()&os.ModeSymlink != 0 && !w.cfg.Security.AllowExternalReads {
			return nil
		}
		if !entry.Type().IsRegular() && entry.Type()&os.ModeSymlink == 0 {
			return nil
		}

		if w.excluded(id) {
			return nil
		}
		index, included := w.includedBy(id)
		if !included {
			return nil
		}
		matchedInclude[index] = true

		info, err := entry.Info()
		if err != nil {
			diags.Warn("include", fmt.Sprintf("%s could not be inspected: %v", id, err),
				"check the file permissions")
			return nil
		}

		documents[id] = &Document{
			ID:           id,
			Title:        strings.TrimSuffix(path.Base(id), path.Ext(id)),
			Size:         info.Size(),
			ModTime:      info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
			Groups:       w.groupsFor(id),
			Writable:     w.Writable(id),
			TooLarge:     info.Size() > w.cfg.Documents.MaxEditableBytes,
			LargeWarning: info.Size() > w.cfg.Documents.LargeFileWarningBytes,
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("enumerate workspace %s: %w", root, err)
	}

	// Spec 05 section 6: include patterns that match no files are warnings.
	for i, matched := range matchedInclude {
		if matched {
			continue
		}
		diags.Warn(fmt.Sprintf("include[%d]", i),
			fmt.Sprintf("include pattern %q matched no files", w.cfg.Include[i]),
			"check the pattern, or remove it if the files no longer exist")
	}

	order := make([]string, 0, len(documents))
	for id := range documents {
		order = append(order, id)
	}
	sort.Strings(order)

	return documents, order, diags, nil
}

// includedBy returns the index of the first include pattern matching an ID.
func (w *Workspace) includedBy(id string) (int, bool) {
	for i, pattern := range w.cfg.Include {
		if matchPattern(pattern, id) {
			return i, true
		}
	}
	return -1, false
}

func (w *Workspace) excluded(id string) bool {
	for _, pattern := range w.cfg.Exclude {
		if matchPattern(pattern, id) {
			return true
		}
	}
	return false
}

// excludedDir reports whether a directory can be skipped wholesale. A pattern
// such as "**/node_modules/**" should stop the walk at the directory itself,
// which the file-level match would not do.
func (w *Workspace) excludedDir(id string) bool {
	for _, pattern := range w.cfg.Exclude {
		if matchPattern(pattern, id) {
			return true
		}
		// "**/node_modules/**" implies the directory "**/node_modules".
		if trimmed := strings.TrimSuffix(pattern, "/**"); trimmed != pattern {
			if matchPattern(trimmed, id) {
				return true
			}
		}
	}
	return false
}

func (w *Workspace) groupsFor(id string) []string {
	var groups []string
	for _, group := range w.cfg.Groups {
		for _, pattern := range group.Patterns {
			if matchPattern(pattern, id) {
				groups = append(groups, group.ID)
				break
			}
		}
	}
	return groups
}

// Writable reports whether the configured write boundary permits saving an ID.
//
// Spec 03 section 7: when security.writable is absent the boundary defaults to
// the included documents. When it is present it is authoritative.
func (w *Workspace) Writable(id string) bool {
	if len(w.cfg.Security.Writable) == 0 {
		_, included := w.includedBy(id)
		return included
	}
	for _, pattern := range w.cfg.Security.Writable {
		if matchPattern(pattern, id) {
			return true
		}
	}
	return false
}

// matchPattern applies one glob to a slash-normalised document ID.
func matchPattern(pattern, id string) bool {
	ok, err := doublestar.Match(pattern, id)
	if err != nil {
		// Malformed patterns are reported by config validation; treating them
		// as non-matching here keeps enumeration total.
		return false
	}
	return ok
}

// Documents returns every included document in stable ID order.
func (w *Workspace) Documents() []*Document {
	w.mu.RLock()
	defer w.mu.RUnlock()

	out := make([]*Document, 0, len(w.order))
	for _, id := range w.order {
		out = append(out, w.documents[id])
	}
	return out
}

// Lookup returns one document by ID.
//
// A document that exists on disk but is not included must be indistinguishable
// from one that does not exist, so a crafted API path cannot probe for excluded
// files (acceptance B1).
func (w *Workspace) Lookup(id string) (*Document, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	doc, ok := w.documents[id]
	return doc, ok
}

// Count returns the number of included documents.
func (w *Workspace) Count() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.order)
}

// ResolveRead validates an ID and returns the absolute path to read, rejecting
// anything the configuration does not include.
func (w *Workspace) ResolveRead(id string) (string, error) {
	if _, ok := w.Lookup(id); !ok {
		return "", security.NotIncluded(id)
	}
	return w.guard.ResolveRead(id)
}

// ResolveWrite validates an ID for writing, enforcing the write boundary.
func (w *Workspace) ResolveWrite(id string) (string, error) {
	if _, ok := w.Lookup(id); !ok {
		return "", security.NotIncluded(id)
	}
	if !w.Writable(id) {
		return "", security.NotWritable(id)
	}
	return w.guard.ResolveWrite(id)
}
