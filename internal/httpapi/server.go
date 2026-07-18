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

	"athenaeum/internal/security"
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
	Logger        *slog.Logger
}

// Server routes API and frontend requests behind the session and origin
// controls required by R14 and acceptance A3.
type Server struct {
	opts Options
	mux  *http.ServeMux
	log  *slog.Logger
}

// New builds the HTTP handler.
func New(opts Options) *Server {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	s := &Server{opts: opts, mux: http.NewServeMux(), log: log}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc(BootstrapPath, s.handleBootstrap)
	s.mux.Handle(APIPrefix+"/health", s.guard(http.HandlerFunc(s.handleHealth)))
	s.mux.Handle("/", s.guard(http.HandlerFunc(s.handleFrontend)))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A conservative default policy: no third-party script, no framing, and
	// connections restricted to self (spec 03 section 9).
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
			"connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	s.mux.ServeHTTP(w, r)
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
	s.writeJSON(w, http.StatusOK, healthResponse{
		Status:    "ok",
		Version:   s.opts.Version,
		Workspace: s.opts.WorkspaceName,
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
	if strings.HasPrefix(r.URL.Path, APIPrefix) || r.Header.Get("Accept") == "application/json" {
		s.writeJSON(w, status, apiError{Error: apiErrorBody{Code: code, Message: message}})
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
