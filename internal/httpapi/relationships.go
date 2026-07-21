package httpapi

import "net/http"

// handleRelationships answers GET /api/v1/relationships/{id} with a document's
// outgoing links and backlinks (R10, acceptance H1). The id carries slashes, so
// the route uses a trailing wildcard like the document routes.
func (s *Server) handleRelationships(w http.ResponseWriter, r *http.Request) {
	b := s.current()
	if b == nil || b.Relationships == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "WORKSPACE_UNAVAILABLE",
			"No workspace with relationship data is open in this process.")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "DOCUMENT_REQUIRED",
			"Request relationships for a specific document.")
		return
	}
	result, err := b.Relationships.Get(id)
	if err != nil {
		s.log.Error("relationships failed", "document_id", id, "error", err)
		s.writeError(w, r, http.StatusInternalServerError, "RELATIONSHIPS_FAILED",
			"The relationships for this document could not be computed.")
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}
