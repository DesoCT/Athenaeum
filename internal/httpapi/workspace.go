package httpapi

import (
	"net/http"

	"athenaeum/internal/config"
	"athenaeum/internal/security"
	"athenaeum/internal/workspace"
)

// workspaceResponse describes the opened workspace for the Map Room (R2).
type workspaceResponse struct {
	Name string `json:"name"`
	// Root is shown as a summary. It is the one place an absolute path is
	// deliberately exposed, because the user needs to know which directory is
	// open; it is never written to logs (spec 03 section 12).
	Root          string              `json:"root"`
	DocumentCount int                 `json:"document_count"`
	Groups        []groupResponse     `json:"groups"`
	Diagnostics   []diagnosticPayload `json:"diagnostics,omitempty"`
	Capabilities  capabilities        `json:"capabilities"`
}

type groupResponse struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Count int    `json:"count"`
}

// capabilities tells the frontend which renderer features the workspace
// enables, so it does not offer what the configuration has switched off.
type capabilities struct {
	RawHTML   bool `json:"raw_html"`
	WikiLinks bool `json:"wiki_links"`
	Footnotes bool `json:"footnotes"`
	Callouts  bool `json:"callouts"`
	Math      bool `json:"math"`
	Mermaid   bool `json:"mermaid"`
	Git       bool `json:"git"`
	Search    bool `json:"search"`
}

type diagnosticPayload struct {
	Severity string `json:"severity"`
	Field    string `json:"field"`
	Message  string `json:"message"`
	Remedy   string `json:"remedy,omitempty"`
}

func toDiagnosticPayloads(ds config.Diagnostics) []diagnosticPayload {
	if len(ds) == 0 {
		return nil
	}
	out := make([]diagnosticPayload, 0, len(ds))
	for _, d := range ds {
		out = append(out, diagnosticPayload{
			Severity: string(d.Severity),
			Field:    d.Field,
			Message:  d.Message,
			Remedy:   d.Remedy,
		})
	}
	return out
}

func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	ws := s.opts.Workspace
	if ws == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "WORKSPACE_UNAVAILABLE",
			"No workspace is open in this process.")
		return
	}
	cfg := ws.Config()

	groupCounts := map[string]int{}
	for _, doc := range ws.Documents() {
		for _, id := range doc.Groups {
			groupCounts[id]++
		}
	}
	groups := make([]groupResponse, 0, len(cfg.Groups))
	for _, g := range cfg.Groups {
		title := g.Title
		if title == "" {
			title = g.ID
		}
		groups = append(groups, groupResponse{ID: g.ID, Title: title, Count: groupCounts[g.ID]})
	}

	s.writeJSON(w, http.StatusOK, workspaceResponse{
		Name:          cfg.Name,
		Root:          cfg.AbsRoot,
		DocumentCount: ws.Count(),
		Groups:        groups,
		Diagnostics:   toDiagnosticPayloads(ws.Diagnostics()),
		Capabilities: capabilities{
			RawHTML:   cfg.Documents.RawHTML,
			WikiLinks: cfg.Documents.WikiLinks,
			Footnotes: cfg.Documents.Footnotes,
			Callouts:  cfg.Documents.Callouts,
			Math:      cfg.Documents.Math,
			Mermaid:   cfg.Documents.Mermaid,
			Git:       cfg.Git.Enabled,
			Search:    cfg.Search.Enabled,
		},
	})
}

type documentListResponse struct {
	Documents []*workspace.Document `json:"documents"`
}

func (s *Server) handleDocumentList(w http.ResponseWriter, r *http.Request) {
	if s.opts.Workspace == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "WORKSPACE_UNAVAILABLE",
			"No workspace is open in this process.")
		return
	}
	docs := s.opts.Workspace.Documents()
	// Enumeration knows only file names; titles come from front matter or the
	// first heading (R2, spec 04 section 4.2).
	if s.opts.Documents != nil {
		s.opts.Documents.EnrichTitles(docs)
	}
	s.writeJSON(w, http.StatusOK, documentListResponse{Documents: docs})
}

func (s *Server) handleDocumentRead(w http.ResponseWriter, r *http.Request) {
	if s.opts.Documents == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "WORKSPACE_UNAVAILABLE",
			"No workspace is open in this process.")
		return
	}

	id := r.PathValue("id")
	doc, err := s.opts.Documents.Read(id)
	if err != nil {
		s.writeDocumentError(w, r, id, err)
		return
	}
	s.writeJSON(w, http.StatusOK, doc)
}

// writeDocumentError maps a path or read failure onto a stable API error.
//
// Every "cannot see this document" case returns 404 with the same code, so the
// API cannot be used to distinguish an excluded file from a missing one
// (acceptance B1).
func (s *Server) writeDocumentError(w http.ResponseWriter, r *http.Request, id string, err error) {
	code := security.CodeOf(err)
	switch code {
	case security.CodePathNotFound:
		s.writeErrorWithDetails(w, r, http.StatusNotFound, code,
			"No such document in this workspace.", map[string]string{"document_id": id})
	case security.CodePathAbsolute, security.CodePathTraversal, security.CodePathEscapesRoot:
		s.writeErrorWithDetails(w, r, http.StatusBadRequest, code,
			"That document identifier is not valid for this workspace.", map[string]string{"document_id": id})
	case security.CodePathNotRegular:
		s.writeErrorWithDetails(w, r, http.StatusBadRequest, code,
			"That path does not name a regular file.", map[string]string{"document_id": id})
	case security.CodePathNotWritable:
		s.writeErrorWithDetails(w, r, http.StatusForbidden, code,
			"That document is outside the configured write boundary.", map[string]string{"document_id": id})
	case security.CodePathEmpty:
		s.writeErrorWithDetails(w, r, http.StatusBadRequest, code,
			"A document identifier is required.", nil)
	default:
		s.log.Error("read document", "document_id", id, "error", err)
		s.writeErrorWithDetails(w, r, http.StatusInternalServerError, "DOCUMENT_READ_FAILED",
			"The document could not be read.", map[string]string{"document_id": id})
	}
}
