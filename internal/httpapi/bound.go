package httpapi

import (
	"net/http"

	"athenaeum/internal/annotations"
	"athenaeum/internal/assets"
	"athenaeum/internal/documents"
	"athenaeum/internal/notes"
	"athenaeum/internal/relationships"
	"athenaeum/internal/search"
	"athenaeum/internal/session"
	"athenaeum/internal/watcher"
	"athenaeum/internal/workspace"
)

// Bound is every service scoped to one open workspace.
//
// Grouping them is what makes a workspace switch total. The server holds
// exactly one *Bound at a time and replaces it as a unit, so there is no path
// by which a service from the previous workspace can answer a request against
// the new one: the tree, quick open, search, the write boundary, and the
// session store all come from the same value or from none at all.
//
// A nil *Bound means no workspace is open and the picker is showing. That is a
// legitimate state, not an error state — it is where `athenaeum open` lands
// when it has no path to open (ADR-0004).
//
// This is not a mount table. Nothing here is a collection, and there is no
// operation anywhere that produces two of these at once; D-006 and R1 stand.
type Bound struct {
	// Name and Root describe the workspace for the command bar and the banner.
	Name string
	// Root is the canonical absolute root. It is exposed to the UI, which needs
	// to show which directory is open, but never written to logs
	// (spec 03 section 12).
	Root string
	// AllowRemoteAssets mirrors this workspace's assets.allow_remote, so the
	// Content-Security-Policy follows the workspace rather than the process
	// (R3, N7).
	AllowRemoteAssets bool

	Workspace *workspace.Workspace
	Documents *documents.Service
	// Recovery persists unsaved buffers against an abnormal exit (R13, E3).
	Recovery *session.RecoveryStore
	// Assets stores pasted and dropped images (R11).
	Assets *assets.Service
	// Watcher feeds the change stream. Nil disables /events.
	Watcher *watcher.Watcher
	// Search answers queries against the disposable FTS projection (R7). Nil
	// means search is disabled or unavailable; every other route still works,
	// because search is a projection and never a prerequisite (C1, C2).
	Search *search.Service
	// SessionState persists open tabs and layout (R13).
	SessionState *session.StateStore
	// Annotations reads and writes the personal and shared annotation sidecars
	// (R8). Nil disables the annotation routes; every other route still works,
	// because annotations are sidecar context and never a prerequisite.
	Annotations *annotations.Service
	// Notes reads and writes free-standing note files (R9). Nil disables the
	// note routes; like annotations, notes are never a prerequisite for the
	// document routes.
	Notes *notes.Service
	// Relationships computes outgoing links and backlinks as a projection over
	// the corpus (R10). Nil disables the relationship route.
	Relationships *relationships.Service
}

// current returns the workspace bound right now, or nil when the picker is
// showing.
//
// Handlers call this once and use the result throughout, so a switch arriving
// mid-request cannot make one handler read two workspaces.
func (s *Server) current() *Bound {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bound
}

// open reports whether a workspace is actually loaded.
//
// A non-nil binding is not enough. A process launched at the picker holds a
// binding seeded from options that names no workspace, so "is there a binding"
// and "is a workspace open" are different questions; conflating them reported
// an empty active workspace to the picker instead of none.
func (b *Bound) open() bool {
	return b != nil && b.Workspace != nil
}

// Bind replaces the open workspace, or clears it when b is nil.
//
// The caller is responsible for shutting down the services it displaces; this
// only changes what requests are answered from.
func (s *Server) Bind(b *Bound) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bound = b
}

// boundFromOptions seeds the initial binding from the launch options.
//
// It always returns a binding, even one whose services are all nil. Deriving
// "no workspace" from a nil Workspace field would silently discard the other
// fields — a server given only a recovery store, as several transport tests
// are, would lose it. Emptiness is decided per service by the handlers, and the
// picker state is entered explicitly through Bind(nil).
func boundFromOptions(opts Options) *Bound {
	root := ""
	if opts.Workspace != nil {
		if cfg := opts.Workspace.Config(); cfg != nil {
			root = cfg.AbsRoot
		}
	}
	return &Bound{
		Name:              opts.WorkspaceName,
		Root:              root,
		AllowRemoteAssets: opts.AllowRemoteAssets,
		Workspace:         opts.Workspace,
		Documents:         opts.Documents,
		Recovery:          opts.Recovery,
		Assets:            opts.Assets,
		Watcher:           opts.Watcher,
		Search:            opts.Search,
		SessionState:      opts.SessionState,
		Annotations:       opts.Annotations,
		Notes:             opts.Notes,
		Relationships:     opts.Relationships,
	}
}

// noWorkspace writes the standard "no workspace is open" response.
//
// It deliberately keeps the existing WORKSPACE_UNAVAILABLE code rather than
// minting a new one. The code already meant exactly this, and changing a stable
// error code is a breaking API change that would need an ADR of its own
// (requirement N6, spec 07 section 6). The message now names the remedy, which
// is what tells the frontend's user where to go.
func (s *Server) noWorkspace(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, http.StatusServiceUnavailable, "WORKSPACE_UNAVAILABLE",
		"No workspace is open in this process. Choose one from the workspace picker.")
}
