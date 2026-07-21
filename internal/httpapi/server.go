// Package httpapi serves the versioned JSON API and the embedded frontend
// (spec 02 section 3.12).
package httpapi

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"

	"athenaeum/internal/annotations"
	"athenaeum/internal/assets"
	"athenaeum/internal/documents"
	"athenaeum/internal/notes"
	"athenaeum/internal/relationships"
	"athenaeum/internal/search"
	"athenaeum/internal/security"
	"athenaeum/internal/session"
	"athenaeum/internal/watcher"
	"athenaeum/internal/workspace"
)

// APIPrefix is the versioned API mount point (spec 02 section 5).
const APIPrefix = "/api/v1"

// BootstrapPath exchanges the launch token for a session cookie.
const BootstrapPath = "/bootstrap"

// Options configures a Server.
type Options struct {
	Sessions      *security.SessionManager
	Origins       *security.OriginPolicy
	Frontend      fs.FS
	FrontendBuilt bool
	Version       string
	WorkspaceName string
	Remote        bool
	// AllowRemoteAssets mirrors assets.allow_remote. It widens the image
	// policy in the Content-Security-Policy header (R3, N7).
	AllowRemoteAssets bool
	Logger            *slog.Logger

	// The fields below describe the workspace open at launch. They seed the
	// initial binding; after that the server answers from whatever Bind holds,
	// which is what lets a workspace be swapped without rebuilding the router
	// (ADR-0004).
	//
	// Workspace and Documents are nil only in tests that exercise the transport
	// layer alone, and in a process that started at the picker.
	Workspace *workspace.Workspace
	Documents *documents.Service
	// Recovery persists unsaved buffers against an abnormal exit (R13, E3).
	Recovery *session.RecoveryStore
	// Assets stores pasted and dropped images (R11).
	Assets *assets.Service
	// Watcher feeds the change stream. Nil disables /events.
	Watcher *watcher.Watcher
	// Search answers queries against the disposable FTS projection (R7).
	// Nil means search is disabled or unavailable; every other route still
	// works, because search is a projection and never a prerequisite (C1, C2).
	Search *search.Service
	// SessionState persists open tabs and layout (R13).
	SessionState *session.StateStore
	// Annotations reads and writes annotation sidecars (R8). Nil disables the
	// annotation routes.
	Annotations *annotations.Service
	// Notes reads and writes free-standing note files (R9). Nil disables the
	// note routes.
	Notes *notes.Service
	// Relationships computes outgoing links and backlinks (R10). Nil disables
	// the relationship route.
	Relationships *relationships.Service

	// Workspaces lists the registry and changes which workspace is open. Nil
	// disables the picker routes entirely, which is what every existing test
	// and any embedder that does not want a registry gets.
	Workspaces Workspaces
}

// Server routes API and frontend requests behind the session and origin
// controls required by R14 and acceptance A3.
type Server struct {
	opts Options
	mux  *http.ServeMux
	log  *slog.Logger

	// mu guards bound. Requests read it; a switch replaces it. An RWMutex
	// because reads are on every request and writes are a rare user action.
	mu    sync.RWMutex
	bound *Bound
}

// New builds the HTTP handler.
func New(opts Options) *Server {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	s := &Server{opts: opts, mux: http.NewServeMux(), log: log}
	s.bound = boundFromOptions(opts)
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc(BootstrapPath, s.handleBootstrap)
	s.mux.Handle(APIPrefix+"/health", s.guard(http.HandlerFunc(s.handleHealth)))
	s.mux.Handle("GET "+APIPrefix+"/workspace", s.guard(http.HandlerFunc(s.handleWorkspace)))
	// The registry launcher (ADR-0004). Listing is a read; opening and leaving
	// mutate which workspace is loaded and so sit behind the origin check too.
	s.mux.Handle("GET "+APIPrefix+"/workspaces", s.guard(http.HandlerFunc(s.handleWorkspaceList)))
	s.mux.Handle("POST "+APIPrefix+"/workspaces/open", s.guard(http.HandlerFunc(s.handleWorkspaceOpen)))
	s.mux.Handle("POST "+APIPrefix+"/workspaces/leave", s.guard(http.HandlerFunc(s.handleWorkspaceLeave)))
	s.mux.Handle("GET "+APIPrefix+"/documents", s.guard(http.HandlerFunc(s.handleDocumentList)))
	// The id wildcard spans the remaining path because a document ID contains
	// slashes, for example "docs/design/rendering.md".
	s.mux.Handle("GET "+APIPrefix+"/documents/{id...}", s.guard(http.HandlerFunc(s.handleDocumentRead)))
	s.mux.Handle("PUT "+APIPrefix+"/documents/{id...}", s.guard(http.HandlerFunc(s.handleDocumentSave)))
	s.mux.Handle("GET "+APIPrefix+"/events", s.guard(http.HandlerFunc(s.handleEvents)))
	s.mux.Handle("POST "+APIPrefix+"/assets", s.guard(http.HandlerFunc(s.handleAssetStore)))
	// Serves workspace images to the preview (R3, spec 03 section 9).
	s.mux.Handle("GET "+APIPrefix+"/assets/{id...}", s.guard(http.HandlerFunc(s.handleAssetServe)))
	// Search sits behind the same session and origin guard as every other route
	// (ADR-0002); the query string is never logged (spec 03 section 12).
	s.mux.Handle("GET "+APIPrefix+"/search", s.guard(http.HandlerFunc(s.handleSearch)))
	s.mux.Handle("GET "+APIPrefix+"/search/status", s.guard(http.HandlerFunc(s.handleSearchStatus)))
	s.mux.Handle("POST "+APIPrefix+"/search/rebuild", s.guard(http.HandlerFunc(s.handleSearchRebuild)))
	s.mux.Handle("GET "+APIPrefix+"/session", s.guard(http.HandlerFunc(s.handleSessionGet)))
	s.mux.Handle("PUT "+APIPrefix+"/session", s.guard(http.HandlerFunc(s.handleSessionPut)))
	s.mux.Handle("GET "+APIPrefix+"/recovery", s.guard(http.HandlerFunc(s.handleRecoveryList)))
	s.mux.Handle("PUT "+APIPrefix+"/recovery", s.guard(http.HandlerFunc(s.handleRecoveryPut)))
	s.mux.Handle("DELETE "+APIPrefix+"/recovery/{id...}", s.guard(http.HandlerFunc(s.handleRecoveryDelete)))
	s.mux.Handle("GET "+APIPrefix+"/annotations", s.guard(http.HandlerFunc(s.handleAnnotationList)))
	s.mux.Handle("POST "+APIPrefix+"/annotations", s.guard(http.HandlerFunc(s.handleAnnotationCreate)))
	s.mux.Handle("PATCH "+APIPrefix+"/annotations/{id}", s.guard(http.HandlerFunc(s.handleAnnotationUpdate)))
	s.mux.Handle("DELETE "+APIPrefix+"/annotations/{id}", s.guard(http.HandlerFunc(s.handleAnnotationDelete)))
	s.mux.Handle("GET "+APIPrefix+"/notes", s.guard(http.HandlerFunc(s.handleNoteList)))
	s.mux.Handle("POST "+APIPrefix+"/notes", s.guard(http.HandlerFunc(s.handleNoteCreate)))
	s.mux.Handle("GET "+APIPrefix+"/notes/{id}", s.guard(http.HandlerFunc(s.handleNoteRead)))
	s.mux.Handle("PUT "+APIPrefix+"/notes/{id}", s.guard(http.HandlerFunc(s.handleNoteUpdate)))
	s.mux.Handle("DELETE "+APIPrefix+"/notes/{id}", s.guard(http.HandlerFunc(s.handleNoteDelete)))
	s.mux.Handle("GET "+APIPrefix+"/relationships/{id...}", s.guard(http.HandlerFunc(s.handleRelationships)))
	s.mux.Handle("/", s.guard(http.HandlerFunc(s.handleFrontend)))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", s.contentSecurityPolicy())
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")

	// Reject traversal on the raw path before ServeMux cleans it, so a crafted
	// path returns the documented path-security error instead of a redirect
	// (acceptance B2).
	if code, ok := inspectRawPath(r.URL.EscapedPath()); !ok {
		s.writeErrorWithDetails(w, r, http.StatusBadRequest, code,
			"That request path is not valid for this workspace.", nil)
		return
	}

	s.mux.ServeHTTP(w, r)
}

// contentSecurityPolicy builds the policy header.
//
// Script, framing, and connections stay restricted to self (spec 03 section 9);
// only the image policy varies. R3 requires remote images to render with a
// visible indicator, and N7 permits exactly one kind of outbound request during
// normal use: a user-requested remote asset. A blanket img-src 'self' would
// block those before the browser even issued the request, silently making
// assets.allow_remote meaningless.
//
// When remote assets are disabled -- including under --safe-mode, which clears
// the flag -- the tighter policy applies and nothing off-origin can load.
func (s *Server) contentSecurityPolicy() string {
	imgSrc := "'self' data:"
	// The policy follows the open workspace, not the process. After a switch the
	// new workspace's assets.allow_remote governs; at the picker, where no
	// workspace is open, the tighter policy applies.
	allowRemote := false
	if b := s.current(); b != nil {
		allowRemote = b.AllowRemoteAssets
	}
	if allowRemote {
		// http: as well as https:, because Markdown in the wild carries both
		// and silently dropping one is the same class of failure. Remote images
		// are marked in the DOM and sent with no referrer and no credentials.
		imgSrc += " https: http:"
	}
	return "default-src 'self'; img-src " + imgSrc + "; style-src 'self' 'unsafe-inline'; " +
		"connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
}

// guard enforces session authentication on every route, and additionally
// enforces the origin allow-list on state-mutating methods.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.opts.Sessions.Validate(r) {
			s.writeError(w, r, http.StatusUnauthorized, "SESSION_REQUIRED",
				"This request carried no valid Athenaeum session. Open the launch URL printed by the server.")
			return
		}
		if security.IsMutating(r.Method) && !s.opts.Origins.Allows(r) {
			s.writeError(w, r, http.StatusForbidden, "ORIGIN_REJECTED",
				"The request origin is not allowed to modify this workspace.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Bootstrap accepts GET only.")
		return
	}
	sessionID, err := s.opts.Sessions.RedeemBootstrap(r.URL.Query().Get("t"))
	if err != nil {
		// Never echo the supplied token, and never log it (spec 03 section 12).
		s.log.Warn("bootstrap rejected", "remote_addr", r.RemoteAddr)
		s.writeError(w, r, http.StatusUnauthorized, "BOOTSTRAP_INVALID",
			"That launch token is not valid for this Athenaeum process.")
		return
	}
	s.opts.Sessions.IssueCookie(w, sessionID)
	// Redirect to the bare origin so the token leaves the address bar and the
	// browser history immediately.
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type healthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Workspace string `json:"workspace"`
	Remote    bool   `json:"remote"`
	Frontend  string `json:"frontend"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	frontend := "embedded"
	if !s.opts.FrontendBuilt {
		frontend = "missing"
	}
	name := ""
	if b := s.current(); b != nil {
		name = b.Name
	}
	s.writeJSON(w, http.StatusOK, healthResponse{
		Status:    "ok",
		Version:   s.opts.Version,
		Workspace: name,
		Remote:    s.opts.Remote,
		Frontend:  frontend,
	})
}

// handleFrontend serves the embedded SPA, falling back to index.html so that
// client-side routes resolve on a hard refresh.
func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, APIPrefix) {
		s.writeError(w, r, http.StatusNotFound, "NOT_FOUND", "No such API endpoint.")
		return
	}
	if !s.opts.FrontendBuilt {
		s.writeError(w, r, http.StatusInternalServerError, "FRONTEND_NOT_BUILT",
			"This binary contains no compiled frontend. Run `make build` to produce a release executable.")
		return
	}

	clean := path.Clean("/" + r.URL.Path)
	name := strings.TrimPrefix(clean, "/")
	if name == "" {
		name = "index.html"
	}
	if f, err := s.opts.Frontend.Open(name); err == nil {
		f.Close()
	} else {
		name = "index.html"
	}
	http.ServeFileFS(w, r, s.opts.Frontend, name)
}

type apiError struct {
	Error apiErrorBody `json:"error"`
}

type apiErrorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	s.writeErrorWithDetails(w, r, status, code, message, nil)
}

func (s *Server) writeErrorWithDetails(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]string) {
	if strings.HasPrefix(r.URL.Path, APIPrefix) || r.Header.Get("Accept") == "application/json" {
		s.writeJSON(w, status, apiError{Error: apiErrorBody{Code: code, Message: message, Details: details}})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(code + "\n\n" + message + "\n"))
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.log.Error("write response", "error", err)
	}
}
