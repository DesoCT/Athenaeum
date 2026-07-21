package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"athenaeum/internal/annotations"
)

// maxAnnotationBytes bounds an annotation request body. Annotation bodies are
// short prose; this only guards against a request that would exhaust memory
// before validation runs (spec 02 section 3.12).
const maxAnnotationBytes = 1 << 20

// annotationResult is the success payload for a create or update.
type annotationResult struct {
	Annotation annotations.Annotation `json:"annotation"`
	Revision   int                    `json:"revision"`
}

// annotationConflictResponse is returned with HTTP 409, carrying the current
// sidecar state so the client can reconcile without a second request (spec 02
// section 5), mirroring the document conflict shape.
type annotationConflictResponse struct {
	Error    apiErrorBody              `json:"error"`
	Conflict annotationConflictPayload `json:"conflict"`
}

type annotationConflictPayload struct {
	Visibility      string                   `json:"visibility"`
	CurrentRevision int                      `json:"current_revision"`
	Current         []annotations.Annotation `json:"current"`
}

// annotationCreateRequest is the body of POST /api/v1/annotations.
type annotationCreateRequest struct {
	DocumentID       string             `json:"document_id"`
	Kind             string             `json:"kind"`
	Visibility       string             `json:"visibility"`
	Status           string             `json:"status"`
	Body             string             `json:"body"`
	ExpectedRevision int                `json:"expected_revision"`
	Anchor           annotations.Anchor `json:"anchor"`
}

// annotationUpdateRequest is the body of PATCH /api/v1/annotations/{id}.
// Visibility is fixed at creation in this version, so it names which sidecar to
// address rather than moving the annotation.
type annotationUpdateRequest struct {
	DocumentID       string  `json:"document_id"`
	Visibility       string  `json:"visibility"`
	ExpectedRevision int     `json:"expected_revision"`
	Body             *string `json:"body"`
	Status           *string `json:"status"`
}

// annotationDeleteRequest is the body of DELETE /api/v1/annotations/{id}.
type annotationDeleteRequest struct {
	DocumentID       string `json:"document_id"`
	Visibility       string `json:"visibility"`
	ExpectedRevision int    `json:"expected_revision"`
}

// annotationService resolves the open workspace's annotation service, or writes
// the standard unavailable response and returns nil.
func (s *Server) annotationService(w http.ResponseWriter, r *http.Request) *annotations.Service {
	b := s.current()
	if b == nil || b.Annotations == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "WORKSPACE_UNAVAILABLE",
			"No workspace with annotation storage is open in this process.")
		return nil
	}
	return b.Annotations
}

func (s *Server) handleAnnotationList(w http.ResponseWriter, r *http.Request) {
	svc := s.annotationService(w, r)
	if svc == nil {
		return
	}
	document := r.URL.Query().Get("document")
	if document == "" {
		s.writeError(w, r, http.StatusBadRequest, "DOCUMENT_REQUIRED",
			"List annotations for a specific document with ?document=<id>.")
		return
	}
	result, err := svc.List(document)
	if err != nil {
		s.writeAnnotationError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAnnotationCreate(w http.ResponseWriter, r *http.Request) {
	svc := s.annotationService(w, r)
	if svc == nil {
		return
	}
	var req annotationCreateRequest
	if !s.decodeAnnotation(w, r, &req) {
		return
	}
	ann, revision, err := svc.Create(annotations.CreateRequest{
		DocumentID:       req.DocumentID,
		Kind:             req.Kind,
		Visibility:       req.Visibility,
		Status:           req.Status,
		Body:             req.Body,
		Anchor:           req.Anchor,
		ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		s.writeAnnotationError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, annotationResult{Annotation: *ann, Revision: revision})
}

func (s *Server) handleAnnotationUpdate(w http.ResponseWriter, r *http.Request) {
	svc := s.annotationService(w, r)
	if svc == nil {
		return
	}
	var req annotationUpdateRequest
	if !s.decodeAnnotation(w, r, &req) {
		return
	}
	ann, revision, err := svc.Update(annotations.UpdateRequest{
		DocumentID:       req.DocumentID,
		Visibility:       req.Visibility,
		ID:               r.PathValue("id"),
		Body:             req.Body,
		Status:           req.Status,
		ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		s.writeAnnotationError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, annotationResult{Annotation: *ann, Revision: revision})
}

func (s *Server) handleAnnotationDelete(w http.ResponseWriter, r *http.Request) {
	svc := s.annotationService(w, r)
	if svc == nil {
		return
	}
	var req annotationDeleteRequest
	if !s.decodeAnnotation(w, r, &req) {
		return
	}
	revision, err := svc.Delete(annotations.DeleteRequest{
		DocumentID:       req.DocumentID,
		Visibility:       req.Visibility,
		ID:               r.PathValue("id"),
		ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		s.writeAnnotationError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]int{"revision": revision})
}

// decodeAnnotation reads a JSON body under the size cap, writing the standard
// error and returning false on failure.
func (s *Server) decodeAnnotation(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAnnotationBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE",
				"The annotation request is larger than this server accepts.")
			return false
		}
		s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"The annotation request could not be understood.")
		return false
	}
	return true
}

// writeAnnotationError maps a store failure onto a stable API error.
func (s *Server) writeAnnotationError(w http.ResponseWriter, r *http.Request, err error) {
	var validationErr *annotations.ValidationError
	if errors.As(err, &validationErr) {
		s.writeErrorWithDetails(w, r, http.StatusBadRequest, "INVALID_ANNOTATION",
			validationErr.Message, map[string]string{"field": validationErr.Field})
		return
	}
	var conflictErr *annotations.ConflictError
	if errors.As(err, &conflictErr) {
		s.writeJSON(w, http.StatusConflict, annotationConflictResponse{
			Error: apiErrorBody{
				Code:    "ANNOTATION_CONFLICT",
				Message: "The annotations changed since they were last read.",
			},
			Conflict: annotationConflictPayload{
				Visibility:      conflictErr.Visibility,
				CurrentRevision: conflictErr.CurrentRevision,
				Current:         conflictErr.Current,
			},
		})
		return
	}
	var notFoundErr *annotations.NotFoundError
	if errors.As(err, &notFoundErr) {
		s.writeError(w, r, http.StatusNotFound, "ANNOTATION_NOT_FOUND",
			"That annotation does not exist in the addressed sidecar.")
		return
	}
	var unavailableErr *annotations.UnavailableError
	if errors.As(err, &unavailableErr) {
		s.writeError(w, r, http.StatusServiceUnavailable, "ANNOTATION_STORAGE_UNAVAILABLE",
			"That annotation store is not available in this session.")
		return
	}
	var sourceErr *annotations.SourceError
	if errors.As(err, &sourceErr) {
		s.writeError(w, r, http.StatusBadRequest, "ANNOTATION_TARGET_UNREADABLE",
			"The document this annotation targets could not be read.")
		return
	}
	s.log.Error("annotation request failed", "error", err)
	s.writeError(w, r, http.StatusInternalServerError, "ANNOTATION_FAILED",
		"The annotation request could not be completed.")
}
