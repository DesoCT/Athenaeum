package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"athenaeum/internal/registry"
)

// maxOpenRequestBytes bounds the switch request body. It carries one name.
const maxOpenRequestBytes = 4 << 10

// Workspaces lists the registry and changes which workspace is open.
//
// The application implements it, because it owns service construction. Keeping
// it an interface is what stops this package from learning how to build a
// workspace, and stops it from being able to hold more than one.
//
// Note the shape: Open replaces, Leave clears. There is deliberately no
// operation that adds a workspace to a set, because there is no set. ADR-0004's
// test is that no feature may make two roots visible at the same moment, and an
// interface that cannot express two roots cannot fail it.
//
// The registry is never written. Athenaeum only reads it; the user edits the
// file by hand (ADR-0004, constitution C3 and C8).
type Workspaces interface {
	// List re-reads the registry from disk. It is re-read on every call rather
	// than cached because the file is hand-edited, and a user who adds an entry
	// expects to see it without restarting (C8).
	List() (*registry.Registry, error)
	// Open unloads whatever is open and opens the named entry. A failure leaves
	// the process at the picker rather than half-loaded.
	Open(name string) error
	// Leave unloads the current workspace and returns to the picker.
	Leave() error
}

// workspaceEntryPayload is one registry entry as the picker sees it.
type workspaceEntryPayload struct {
	Name string `json:"name"`
	// Path is the resolved root. As with the workspace summary, this is a place
	// an absolute path is deliberately shown, because the user needs to tell two
	// similarly named workspaces apart. It is never logged (spec 03 section 12).
	Path      string `json:"path"`
	Available bool   `json:"available"`
	// Code, Reason and Remedy explain an unavailable entry rather than hiding
	// it, so a mistyped path is visible and fixable (R1, requirement N6).
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
	Remedy string `json:"remedy,omitempty"`
	// Active marks the entry the process currently has open, matched on the
	// canonical root so a workspace registered twice cannot show as two
	// different active entries.
	Active bool `json:"active"`
}

// workspaceListResponse is the picker's whole view.
type workspaceListResponse struct {
	// RegistryPath is reported even when the file is absent, so the picker can
	// name the file to create.
	RegistryPath string `json:"registry_path"`
	Present      bool   `json:"present"`
	// Active describes the open workspace, or is nil at the picker.
	Active      *activeWorkspace        `json:"active"`
	Entries     []workspaceEntryPayload `json:"entries"`
	Diagnostics []diagnosticPayload     `json:"diagnostics,omitempty"`
}

type activeWorkspace struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// openWorkspaceResponse confirms what a switch left open.
//
// It is deliberately its own type rather than a reused list response: the list
// shape would have reported an empty registry path and a null entry list
// alongside the answer, which reads as "the registry is gone" to anyone
// inspecting the response.
type openWorkspaceResponse struct {
	Active *activeWorkspace `json:"active"`
}

func (s *Server) handleWorkspaceList(w http.ResponseWriter, r *http.Request) {
	if s.opts.Workspaces == nil {
		s.writeError(w, r, http.StatusNotFound, "REGISTRY_UNAVAILABLE",
			"This process was started without a workspace registry.")
		return
	}

	reg, err := s.opts.Workspaces.List()
	if err != nil {
		// The path is in the error, so the detail is logged at debug only.
		s.log.Warn("read the workspace registry", "error_code", "REGISTRY_UNREADABLE")
		s.log.Debug("workspace registry failure detail", "error", err)
		s.writeError(w, r, http.StatusInternalServerError, "REGISTRY_UNREADABLE",
			"The workspace registry could not be read. Check that it is valid TOML.")
		return
	}

	var active *activeWorkspace
	activeRoot := ""
	if b := s.current(); b.open() {
		active = &activeWorkspace{Name: b.Name, Path: b.Root}
		activeRoot = b.Root
	}

	entries := make([]workspaceEntryPayload, 0, len(reg.Entries))
	for _, entry := range reg.Entries {
		entries = append(entries, workspaceEntryPayload{
			Name:      entry.Name,
			Path:      entry.Path,
			Available: entry.Available,
			Code:      entry.Code,
			Reason:    entry.Reason,
			Remedy:    entry.Remedy,
			Active:    entry.Available && activeRoot != "" && entry.Path == activeRoot,
		})
	}

	s.writeJSON(w, http.StatusOK, workspaceListResponse{
		RegistryPath: reg.SourcePath,
		Present:      reg.Present,
		Active:       active,
		Entries:      entries,
		Diagnostics:  toDiagnosticPayloads(reg.Diagnostics),
	})
}

type openWorkspaceRequest struct {
	Name string `json:"name"`
}

// handleWorkspaceOpen switches the process to a registered workspace.
//
// Opening from the registry is exactly equivalent to launching with that path:
// the previous workspace is fully unloaded first, and the new one is built from
// scratch. Nothing is shared, and at no point are two roots loaded (ADR-0004).
func (s *Server) handleWorkspaceOpen(w http.ResponseWriter, r *http.Request) {
	if s.opts.Workspaces == nil {
		s.writeError(w, r, http.StatusNotFound, "REGISTRY_UNAVAILABLE",
			"This process was started without a workspace registry.")
		return
	}

	var req openWorkspaceRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxOpenRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"The request must be a JSON object with a name field.")
		return
	}
	if req.Name == "" {
		s.writeErrorWithDetails(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"Which workspace should be opened?",
			map[string]string{"field": "name", "remedy": "send the name of a registered workspace"})
		return
	}

	if err := s.opts.Workspaces.Open(req.Name); err != nil {
		s.writeOpenError(w, r, err)
		return
	}

	// The response describes what is now open, so the client never has to guess
	// whether the switch took effect.
	var active *activeWorkspace
	if b := s.current(); b.open() {
		active = &activeWorkspace{Name: b.Name, Path: b.Root}
	}
	s.writeJSON(w, http.StatusOK, openWorkspaceResponse{Active: active})
}

// handleWorkspaceLeave returns the process to the picker (ADR-0004: "leaving a
// workspace returns to the picker; nothing from a previous workspace remains
// loaded").
func (s *Server) handleWorkspaceLeave(w http.ResponseWriter, r *http.Request) {
	if s.opts.Workspaces == nil {
		s.writeError(w, r, http.StatusNotFound, "REGISTRY_UNAVAILABLE",
			"This process was started without a workspace registry.")
		return
	}

	if err := s.opts.Workspaces.Leave(); err != nil {
		s.log.Warn("leave the workspace", "error_code", "WORKSPACE_LEAVE_FAILED")
		s.log.Debug("workspace leave failure detail", "error", err)
		s.writeError(w, r, http.StatusInternalServerError, "WORKSPACE_LEAVE_FAILED",
			"The workspace could not be closed cleanly.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeOpenError maps a switch failure onto a stable code.
//
// The registry's own codes are passed through unchanged, so the picker can
// explain a bad entry using the same reason and remedy it already displays.
func (s *Server) writeOpenError(w http.ResponseWriter, r *http.Request, err error) {
	var lookup *registry.LookupError
	if errors.As(err, &lookup) {
		status := http.StatusNotFound
		if lookup.Code == registry.CodeNameAmbiguous {
			// The request is understood but cannot be satisfied: two entries
			// answer to that name, and guessing between them is exactly the
			// hidden behaviour C8 forbids.
			status = http.StatusConflict
		}
		s.writeErrorWithDetails(w, r, status, lookup.Code,
			"That workspace could not be opened from the registry.",
			map[string]string{
				"reason": lookup.Reason,
				"remedy": "check the names in the workspace registry",
			})
		return
	}

	var unavailable *EntryUnavailableError
	if errors.As(err, &unavailable) {
		s.writeErrorWithDetails(w, r, http.StatusConflict, unavailable.Code,
			"That workspace is registered but cannot be opened.",
			map[string]string{"reason": unavailable.Reason, "remedy": unavailable.Remedy})
		return
	}

	s.log.Warn("open a registered workspace", "error_code", "WORKSPACE_OPEN_FAILED")
	s.log.Debug("workspace open failure detail", "error", err)
	s.writeError(w, r, http.StatusInternalServerError, "WORKSPACE_OPEN_FAILED",
		"That workspace could not be opened. Run `athenaeum validate` against it for detail.")
}

// EntryUnavailableError reports a registered entry that cannot be opened.
//
// It carries the registry's own code, reason, and remedy so the failure the
// picker shows in the list and the failure it shows on a click are the same
// words (R1).
type EntryUnavailableError struct {
	Name   string
	Code   string
	Reason string
	Remedy string
}

func (e *EntryUnavailableError) Error() string {
	return e.Code + ": " + e.Reason + " (" + e.Name + ")"
}
