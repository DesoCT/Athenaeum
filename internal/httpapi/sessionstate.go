package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"athenaeum/internal/session"
)

// maxSessionRequestBytes bounds a session state write.
const maxSessionRequestBytes = 1 << 20

// handleSessionGet returns the restorable UI state (R13).
//
// Tabs and recent documents naming files the workspace no longer includes are
// filtered out here rather than in the browser, so a stale session cannot be
// used to learn that an excluded file exists (acceptance B1).
func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	if s.opts.SessionState == nil {
		s.writeJSON(w, http.StatusOK, session.Default())
		return
	}
	state := s.opts.SessionState.Load()
	s.writeJSON(w, http.StatusOK, s.filterSession(state))
}

// handleSessionPut records the UI state.
//
// Session state is disposable: a failure to persist it is reported but costs
// only the layout, never a document (spec 03 section 1).
func (s *Server) handleSessionPut(w http.ResponseWriter, r *http.Request) {
	if s.opts.SessionState == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "SESSION_STATE_UNAVAILABLE",
			"Session state storage is not available in this process.")
		return
	}

	var state session.State
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSessionRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE",
				"The session state is larger than this server accepts.")
			return
		}
		s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"The session state could not be understood.")
		return
	}

	// Only documents this workspace includes may be recorded, so session state
	// can never become a place to stash arbitrary paths.
	state = s.filterSession(state)

	if err := s.opts.SessionState.Save(state); err != nil {
		s.log.Warn("store session state", "error_code", "SESSION_STATE_REJECTED")
		s.writeError(w, r, http.StatusBadRequest, "SESSION_STATE_REJECTED",
			"The session state could not be stored.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// filterSession drops references to documents outside the workspace.
func (s *Server) filterSession(state session.State) session.State {
	if s.opts.Workspace == nil {
		return state
	}
	return state.Filter(func(id string) bool {
		_, ok := s.opts.Workspace.Lookup(id)
		return ok
	})
}
