package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"athenaeum/internal/search"
)

// handleSearch answers a workspace search (R7, spec 02 section 5).
//
// Nothing here logs the query, the snippets, or any document text: spec 03
// section 12 permits the operation, its duration, and counts, and nothing else.
// A search is a record of what the user was looking for in their own notes.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if s.opts.Search == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "SEARCH_DISABLED",
			"Search is not enabled for this workspace.")
		return
	}

	query := r.URL.Query()
	limit := 0
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST",
				"The limit must be a positive whole number.")
			return
		}
		limit = parsed
	}

	started := time.Now()
	response, err := s.opts.Search.Search(search.Request{
		Query: query.Get("q"),
		Filters: search.Filters{
			Path:  query.Get("path"),
			Group: query.Get("group"),
			Git:   query.Get("git"),
		},
		Limit: limit,
	})
	if err != nil {
		s.writeSearchError(w, r, err)
		return
	}

	// Counts and duration only — never the query or what it found.
	s.log.Debug("search", "results", len(response.Results),
		"duration_ms", time.Since(started).Milliseconds())

	s.writeJSON(w, http.StatusOK, response)
}

// writeSearchError maps a search failure onto a stable code.
//
// A malformed query is a 400 that explains itself, never a 500: FTS5 match
// syntax is user-controllable, and a user typing a stray quote has not caused a
// server fault.
func (s *Server) writeSearchError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, search.ErrNoSearchableTerms):
		s.writeError(w, r, http.StatusBadRequest, "SEARCH_QUERY_INVALID",
			"That query contains no words to search for. Enter at least one letter or digit.")
	case errors.Is(err, search.ErrGitFilterUnavailable):
		s.writeError(w, r, http.StatusConflict, "SEARCH_GIT_UNAVAILABLE",
			"Git state is not available for this workspace, so it cannot be used as a filter.")
	case errors.Is(err, search.ErrUnknownFilter):
		s.writeError(w, r, http.StatusBadRequest, "SEARCH_FILTER_INVALID",
			"That filter value is not one this workspace recognises.")
	case errors.Is(err, search.ErrUnavailable):
		s.writeError(w, r, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE",
			"The search index is not available. Documents remain readable and editable.")
	default:
		s.log.Error("search failed", "error_code", "SEARCH_FAILED")
		s.writeError(w, r, http.StatusInternalServerError, "SEARCH_FAILED",
			"The search could not be completed.")
	}
}

// handleSearchStatus reports index progress for the status bar (spec 04 section 8).
func (s *Server) handleSearchStatus(w http.ResponseWriter, r *http.Request) {
	if s.opts.Search == nil {
		s.writeJSON(w, http.StatusOK, search.Status{State: search.StateDisabled})
		return
	}
	s.writeJSON(w, http.StatusOK, s.opts.Search.Status())
}

// handleSearchRebuild re-examines every document (spec 04 section 4.3).
//
// It returns immediately: rebuilding runs in the background so the UI stays
// responsive (requirement N2).
func (s *Server) handleSearchRebuild(w http.ResponseWriter, r *http.Request) {
	if s.opts.Search == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "SEARCH_DISABLED",
			"Search is not enabled for this workspace.")
		return
	}
	s.opts.Search.Rebuild()
	s.writeJSON(w, http.StatusAccepted, s.opts.Search.Status())
}
