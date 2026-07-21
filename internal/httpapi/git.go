package httpapi

import (
	"net/http"
	"sort"

	"athenaeum/internal/gitview"
)

// gitFile is one document's Git state in the status response.
type gitFile struct {
	DocumentID string `json:"document_id"`
	State      string `json:"state"`
}

// handleGitStatus reports repository availability and per-file states (R12,
// acceptance J1). An unavailable repository is not an error: the panel uses the
// available flag to explain the limitation (J4).
func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	b := s.current()
	if b == nil || b.Git == nil || !b.Git.Available() {
		s.writeJSON(w, http.StatusOK, map[string]any{"available": false, "files": []gitFile{}})
		return
	}
	states := b.Git.States()
	files := make([]gitFile, 0, len(states))
	for id, state := range states {
		files = append(files, gitFile{DocumentID: id, State: state})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].DocumentID < files[j].DocumentID })
	s.writeJSON(w, http.StatusOK, map[string]any{"available": true, "files": files})
}

// gitDocument resolves the requested document, confirming it belongs to the
// workspace before any Git command runs, and reports Git availability. It
// returns ok=false after having written the response.
func (s *Server) gitDocument(w http.ResponseWriter, r *http.Request) (*Bound, string, bool) {
	b := s.current()
	if b == nil || b.Git == nil || !b.Git.Available() {
		s.writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return nil, "", false
	}
	id := r.PathValue("id")
	if b.Workspace == nil {
		s.noWorkspace(w, r)
		return nil, "", false
	}
	if _, ok := b.Workspace.Lookup(id); !ok {
		s.writeErrorWithDetails(w, r, http.StatusNotFound, "DOCUMENT_NOT_FOUND",
			"That document is not part of this workspace.", map[string]string{"document_id": id})
		return nil, "", false
	}
	return b, id, true
}

func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	b, id, ok := s.gitDocument(w, r)
	if !ok {
		return
	}
	diff, err := b.Git.Diff(id)
	if err != nil {
		s.writeGitError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"available": true, "diff": diff})
}

func (s *Server) handleGitHistory(w http.ResponseWriter, r *http.Request) {
	b, id, ok := s.gitDocument(w, r)
	if !ok {
		return
	}
	commits, err := b.Git.History(id)
	if err != nil {
		s.writeGitError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"available": true, "commits": commits})
}

func (s *Server) handleGitBlame(w http.ResponseWriter, r *http.Request) {
	b, id, ok := s.gitDocument(w, r)
	if !ok {
		return
	}
	lines, err := b.Git.Blame(id)
	if err != nil {
		s.writeGitError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"available": true, "lines": lines})
}

// writeGitError turns an adapter failure into a response. The only error the
// read operations return is unavailability, which is reported as a flag, not a
// fault (J4).
func (s *Server) writeGitError(w http.ResponseWriter, r *http.Request, err error) {
	if err == gitview.ErrUnavailable {
		s.writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	s.log.Error("git operation failed", "error", err)
	s.writeError(w, r, http.StatusInternalServerError, "GIT_FAILED",
		"The Git operation could not be completed.")
}
