package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"athenaeum/internal/documents"
)

// maxSaveBytes bounds a save request body. Documents are capped separately by
// documents.max_editable_bytes; this is a transport guard against a request
// that would exhaust memory before that check runs (spec 02 section 3.12).
const maxSaveBytes = 64 << 20

// saveRequest is the body of PUT /api/v1/documents/{id}.
type saveRequest struct {
	Content string `json:"content"`
	// Version is the version the editor last observed. Spec 02 section 5
	// requires it on every write; omitting it forces an overwrite and is only
	// legitimate after the user has resolved a conflict.
	Version string `json:"version"`
	// Force skips the version check. The client sets it only from the conflict
	// resolution screen, after the user chose to keep their version.
	Force      bool   `json:"force"`
	LineEnding string `json:"line_ending"`
	KeepBOM    bool   `json:"keep_bom"`
}

// conflictResponse is returned with HTTP 409. It carries the disk version
// alongside the standard error object so the comparison view has both sides
// without a second request that could race again (R6).
type conflictResponse struct {
	Error    apiErrorBody    `json:"error"`
	Conflict conflictPayload `json:"conflict"`
}

type conflictPayload struct {
	CurrentVersion string `json:"current_version"`
	CurrentContent string `json:"current_content"`
}

func (s *Server) handleDocumentSave(w http.ResponseWriter, r *http.Request) {
	b := s.current()
	if b == nil || b.Documents == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "WORKSPACE_UNAVAILABLE",
			"No workspace is open in this process.")
		return
	}

	id := r.PathValue("id")

	var req saveRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSaveBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE",
				"The save request is larger than this server accepts.")
			return
		}
		s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"The save request could not be understood.")
		return
	}

	// A save without a version is only allowed when the client says so
	// explicitly, so an omitted field can never silently clobber a file.
	expected := req.Version
	if req.Force {
		expected = ""
	} else if expected == "" {
		s.writeErrorWithDetails(w, r, http.StatusBadRequest, "VERSION_REQUIRED",
			"A save must state the version it is replacing.", map[string]string{"document_id": id})
		return
	}

	result, err := b.Documents.Write(documents.WriteRequest{
		ID:              id,
		Content:         req.Content,
		ExpectedVersion: expected,
		LineEnding:      req.LineEnding,
		KeepBOM:         req.KeepBOM,
	})
	if err != nil {
		s.writeSaveError(w, r, id, err)
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// writeSaveError maps a write failure onto a stable API error.
func (s *Server) writeSaveError(w http.ResponseWriter, r *http.Request, id string, err error) {
	var writeErr *documents.WriteError
	if errors.As(err, &writeErr) {
		switch writeErr.Code {
		case documents.CodeConflict:
			s.writeJSON(w, http.StatusConflict, conflictResponse{
				Error: apiErrorBody{
					Code:    writeErr.Code,
					Message: writeErr.Message,
					Details: map[string]string{"document_id": id},
				},
				Conflict: conflictPayload{
					CurrentVersion: writeErr.CurrentVersion,
					CurrentContent: writeErr.CurrentContent,
				},
			})
			return
		case documents.CodeReadOnly:
			s.writeErrorWithDetails(w, r, http.StatusForbidden, writeErr.Code,
				writeErr.Message, map[string]string{"document_id": id})
			return
		case documents.CodeTooLarge, documents.CodeInvalidUTF8:
			s.writeErrorWithDetails(w, r, http.StatusBadRequest, writeErr.Code,
				writeErr.Message, map[string]string{"document_id": id})
			return
		default:
			s.log.Error("save document", "document_id", id, "code", writeErr.Code, "error", err)
			s.writeErrorWithDetails(w, r, http.StatusInternalServerError, writeErr.Code,
				writeErr.Message, map[string]string{"document_id": id})
			return
		}
	}
	// Path and boundary failures share the read mapping.
	s.writeDocumentError(w, r, id, err)
}
