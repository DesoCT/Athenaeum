package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"athenaeum/internal/session"
)

// maxRecoveryRequestBytes bounds a recovery write request.
const maxRecoveryRequestBytes = 32 << 20

type recoveryPutRequest struct {
	DocumentID  string `json:"document_id"`
	Content     string `json:"content"`
	BaseVersion string `json:"base_version"`
}

type recoveryListResponse struct {
	Buffers []session.Buffer `json:"buffers"`
}

// handleRecoveryList returns pending buffers so the UI can offer them.
//
// Listing never consumes a buffer: acceptance E3 requires that restarting
// offers recovery and neither applies nor discards it.
func (s *Server) handleRecoveryList(w http.ResponseWriter, r *http.Request) {
	b := s.current()
	if b == nil || b.Recovery == nil {
		s.writeJSON(w, http.StatusOK, recoveryListResponse{})
		return
	}
	buffers, err := b.Recovery.List()
	if err != nil {
		s.log.Error("list recovery buffers", "error", err)
		s.writeError(w, r, http.StatusInternalServerError, "RECOVERY_UNAVAILABLE",
			"Recovery buffers could not be read.")
		return
	}
	if buffers == nil {
		buffers = []session.Buffer{}
	}
	s.writeJSON(w, http.StatusOK, recoveryListResponse{Buffers: buffers})
}

// handleRecoveryPut records an unsaved buffer.
func (s *Server) handleRecoveryPut(w http.ResponseWriter, r *http.Request) {
	b := s.current()
	if b == nil || b.Recovery == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "RECOVERY_UNAVAILABLE",
			"Recovery storage is not available in this process.")
		return
	}

	var req recoveryPutRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRecoveryRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE",
				"The recovery buffer is larger than this server accepts.")
			return
		}
		s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"The recovery request could not be understood.")
		return
	}

	// The buffer must belong to a document this workspace actually includes,
	// so recovery cannot be used to write arbitrary text into user state.
	if b.Workspace != nil {
		if _, ok := b.Workspace.Lookup(req.DocumentID); !ok {
			s.writeErrorWithDetails(w, r, http.StatusNotFound, "PATH_NOT_FOUND",
				"No such document in this workspace.",
				map[string]string{"document_id": req.DocumentID})
			return
		}
	}

	if err := b.Recovery.Put(session.Buffer{
		DocumentID:  req.DocumentID,
		Content:     req.Content,
		BaseVersion: req.BaseVersion,
	}); err != nil {
		s.log.Error("store recovery buffer", "document_id", req.DocumentID, "error", err)
		s.writeError(w, r, http.StatusBadRequest, "RECOVERY_REJECTED",
			"The recovery buffer could not be stored.")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRecoveryDelete discards a buffer. Only an explicit user action, or a
// successful save, reaches this route.
func (s *Server) handleRecoveryDelete(w http.ResponseWriter, r *http.Request) {
	b := s.current()
	if b == nil || b.Recovery == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	id := r.PathValue("id")
	if err := b.Recovery.Discard(id); err != nil {
		s.log.Error("discard recovery buffer", "document_id", id, "error", err)
		s.writeError(w, r, http.StatusInternalServerError, "RECOVERY_UNAVAILABLE",
			"The recovery buffer could not be discarded.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
