package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// stateSchemaVersion guards the on-disk session format. A file written by a
// different version is discarded rather than migrated: losing a layout is a
// minor inconvenience, and nothing here is authoritative (spec 03 section 1).
const stateSchemaVersion = 1

// Bounds on restored state. A session file is written by the browser, so it is
// treated as untrusted input even though it never leaves this machine.
const (
	maxTabs         = 50
	maxRecent       = 30
	maxStateBytes   = 1 << 20
	maxDocumentIDLn = 1024
)

// View modes (spec 04 section 6).
const (
	ModeSplit   = "split"
	ModeSource  = "source"
	ModePreview = "preview"
)

var validModes = map[string]bool{ModeSplit: true, ModeSource: true, ModePreview: true}

// Tab is one open document and the state needed to reopen it where it was left.
type Tab struct {
	DocumentID string `json:"document_id"`
	// Mode is the source/preview/split selection for this tab.
	Mode string `json:"mode"`
	// PreviewScroll is a 0..1 fraction of the preview's scroll height, so a
	// restored position survives a window of a different size.
	PreviewScroll float64 `json:"preview_scroll"`
	// SourceLine is the 1-based line the editor was showing.
	SourceLine int `json:"source_line"`
}

// Layout is the pane arrangement (spec 04 section 2).
type Layout struct {
	Navigation bool `json:"navigation"`
	Context    bool `json:"context"`
	// Search reports whether the search panel was the visible navigation view.
	Search bool `json:"search"`
}

// State is everything R13 restores.
//
// Deliberately absent: search query history. R13 permits restoring "command
// history that contains no sensitive content", and a query is exactly the kind
// of content spec 03 section 12 keeps out of durable storage. Recording what a
// user searched their private notes for is not worth a convenience.
type State struct {
	SchemaVersion int    `json:"schema_version"`
	UpdatedAt     string `json:"updated_at"`

	Tabs           []Tab    `json:"tabs"`
	ActiveDocument string   `json:"active_document,omitempty"`
	Recent         []string `json:"recent,omitempty"`
	Layout         Layout   `json:"layout"`
}

// StateStore persists UI session state outside the workspace.
type StateStore struct {
	path string
	mu   sync.Mutex
}

// NewStateStore opens the session state store for a workspace.
func NewStateStore(dirs Dirs) (*StateStore, error) {
	if err := os.MkdirAll(dirs.State, 0o700); err != nil {
		return nil, fmt.Errorf("create the session state directory: %w", err)
	}
	return &StateStore{path: dirs.SessionFile()}, nil
}

// Default is the state a workspace opens with when nothing was saved.
func Default() State {
	return State{
		SchemaVersion: stateSchemaVersion,
		Tabs:          []Tab{},
		Layout:        Layout{Navigation: true, Context: true},
	}
}

// Load reads the saved session.
//
// A missing, unreadable, oversized, or malformed file yields the default state
// and no error: a corrupt layout must never stop a workspace opening.
func (s *StateStore) Load() State {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, err := os.Stat(s.path)
	if err != nil || info.Size() > maxStateBytes {
		return Default()
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return Default()
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return Default()
	}
	if state.SchemaVersion != stateSchemaVersion {
		return Default()
	}
	return sanitise(state)
}

// Save replaces the stored session state.
//
// The write is atomic so an interrupted save cannot leave a truncated file that
// would parse as a valid but wrong layout.
func (s *StateStore) Save(state State) error {
	state = sanitise(state)
	state.SchemaVersion = stateSchemaVersion
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode session state: %w", err)
	}
	if len(payload) > maxStateBytes {
		return fmt.Errorf("the session state is %d bytes, above the %d byte limit",
			len(payload), maxStateBytes)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	temp, err := os.CreateTemp(dir, ".session-*.tmp")
	if err != nil {
		return fmt.Errorf("create a temporary session file: %w", err)
	}
	name := temp.Name()

	if _, err := temp.Write(payload); err != nil {
		temp.Close()
		os.Remove(name)
		return fmt.Errorf("write session state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		os.Remove(name)
		return fmt.Errorf("flush session state: %w", err)
	}
	if err := temp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("close session state: %w", err)
	}
	_ = os.Chmod(name, 0o600)
	if err := os.Rename(name, s.path); err != nil {
		os.Remove(name)
		return fmt.Errorf("replace session state: %w", err)
	}
	return nil
}

// sanitise clamps and normalises state from disk or from the browser.
func sanitise(state State) State {
	state.Tabs = slices.DeleteFunc(state.Tabs, func(tab Tab) bool {
		return tab.DocumentID == "" || len(tab.DocumentID) > maxDocumentIDLn
	})
	if len(state.Tabs) > maxTabs {
		state.Tabs = state.Tabs[:maxTabs]
	}
	for i := range state.Tabs {
		if !validModes[state.Tabs[i].Mode] {
			state.Tabs[i].Mode = ModeSplit
		}
		if state.Tabs[i].PreviewScroll < 0 || state.Tabs[i].PreviewScroll > 1 {
			state.Tabs[i].PreviewScroll = 0
		}
		if state.Tabs[i].SourceLine < 0 {
			state.Tabs[i].SourceLine = 0
		}
	}
	if state.Tabs == nil {
		state.Tabs = []Tab{}
	}

	state.Recent = slices.DeleteFunc(state.Recent, func(id string) bool {
		return id == "" || len(id) > maxDocumentIDLn
	})
	if len(state.Recent) > maxRecent {
		state.Recent = state.Recent[:maxRecent]
	}

	if len(state.ActiveDocument) > maxDocumentIDLn {
		state.ActiveDocument = ""
	}
	return state
}

// Filter drops references to documents the workspace no longer includes.
//
// A tab pointing at an excluded or deleted document must not come back on
// restart, and the caller must not be able to learn that the file exists but is
// excluded (acceptance B1) — both cases simply disappear.
func (s State) Filter(includes func(string) bool) State {
	s.Tabs = slices.DeleteFunc(slices.Clone(s.Tabs), func(tab Tab) bool {
		return !includes(tab.DocumentID)
	})
	if s.Tabs == nil {
		s.Tabs = []Tab{}
	}
	s.Recent = slices.DeleteFunc(slices.Clone(s.Recent), func(id string) bool {
		return !includes(id)
	})
	if s.ActiveDocument != "" && !includes(s.ActiveDocument) {
		s.ActiveDocument = ""
	}
	return s
}
