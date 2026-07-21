package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"athenaeum/internal/notes"
)

// maxNoteBytes bounds a note request body. Notes are prose; this only guards
// against a request that would exhaust memory before validation (spec 02
// section 3.12).
const maxNoteBytes = 4 << 20

// noteConflictResponse is returned with HTTP 409, carrying the current note so
// the client can reconcile without a racing second request.
type noteConflictResponse struct {
	Error    apiErrorBody `json:"error"`
	Conflict *notes.Note  `json:"conflict"`
}

type noteCreateRequest struct {
	Title      string       `json:"title"`
	Visibility string       `json:"visibility"`
	Body       string       `json:"body"`
	Links      []notes.Link `json:"links"`
}

type noteUpdateRequest struct {
	Visibility      string        `json:"visibility"`
	ExpectedVersion string        `json:"expected_version"`
	Title           *string       `json:"title"`
	Body            *string       `json:"body"`
	Links           *[]notes.Link `json:"links"`
}

func (s *Server) noteService(w http.ResponseWriter, r *http.Request) *notes.Service {
	b := s.current()
	if b == nil || b.Notes == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "WORKSPACE_UNAVAILABLE",
			"No workspace with note storage is open in this process.")
		return nil
	}
	return b.Notes
}

func (s *Server) handleNoteList(w http.ResponseWriter, r *http.Request) {
	svc := s.noteService(w, r)
	if svc == nil {
		return
	}
	list, err := svc.List()
	if err != nil {
		s.writeNoteError(w, r, err)
		return
	}
	if list == nil {
		list = []notes.Summary{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"notes": list})
}

func (s *Server) handleNoteRead(w http.ResponseWriter, r *http.Request) {
	svc := s.noteService(w, r)
	if svc == nil {
		return
	}
	note, err := svc.Read(r.URL.Query().Get("visibility"), r.PathValue("id"))
	if err != nil {
		s.writeNoteError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, note)
}

func (s *Server) handleNoteCreate(w http.ResponseWriter, r *http.Request) {
	svc := s.noteService(w, r)
	if svc == nil {
		return
	}
	var req noteCreateRequest
	if !s.decodeNote(w, r, &req) {
		return
	}
	note, err := svc.Create(notes.CreateRequest{
		Title:      req.Title,
		Visibility: req.Visibility,
		Body:       req.Body,
		Links:      req.Links,
	})
	if err != nil {
		s.writeNoteError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, note)
}

func (s *Server) handleNoteUpdate(w http.ResponseWriter, r *http.Request) {
	svc := s.noteService(w, r)
	if svc == nil {
		return
	}
	var req noteUpdateRequest
	if !s.decodeNote(w, r, &req) {
		return
	}
	note, err := svc.Update(notes.UpdateRequest{
		ID:              r.PathValue("id"),
		Visibility:      req.Visibility,
		Title:           req.Title,
		Body:            req.Body,
		Links:           req.Links,
		ExpectedVersion: req.ExpectedVersion,
	})
	if err != nil {
		s.writeNoteError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, note)
}

func (s *Server) handleNoteDelete(w http.ResponseWriter, r *http.Request) {
	svc := s.noteService(w, r)
	if svc == nil {
		return
	}
	if err := svc.Delete(r.URL.Query().Get("visibility"), r.PathValue("id")); err != nil {
		s.writeNoteError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) decodeNote(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxNoteBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE",
				"The note request is larger than this server accepts.")
			return false
		}
		s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"The note request could not be understood.")
		return false
	}
	return true
}

func (s *Server) writeNoteError(w http.ResponseWriter, r *http.Request, err error) {
	var validationErr *notes.ValidationError
	if errors.As(err, &validationErr) {
		s.writeErrorWithDetails(w, r, http.StatusBadRequest, "INVALID_NOTE",
			validationErr.Message, map[string]string{"field": validationErr.Field})
		return
	}
	var conflictErr *notes.ConflictError
	if errors.As(err, &conflictErr) {
		s.writeJSON(w, http.StatusConflict, noteConflictResponse{
			Error: apiErrorBody{
				Code:    "NOTE_CONFLICT",
				Message: "The note changed on disk since it was last read.",
			},
			Conflict: conflictErr.Current,
		})
		return
	}
	var notFoundErr *notes.NotFoundError
	if errors.As(err, &notFoundErr) {
		s.writeError(w, r, http.StatusNotFound, "NOTE_NOT_FOUND", "That note does not exist.")
		return
	}
	var unavailableErr *notes.UnavailableError
	if errors.As(err, &unavailableErr) {
		s.writeError(w, r, http.StatusServiceUnavailable, "NOTE_STORAGE_UNAVAILABLE",
			"That note store is not available in this session.")
		return
	}
	s.log.Error("note request failed", "error", err)
	s.writeError(w, r, http.StatusInternalServerError, "NOTE_FAILED",
		"The note request could not be completed.")
}
