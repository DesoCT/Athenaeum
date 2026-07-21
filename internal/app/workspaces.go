package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"athenaeum/internal/annotations"
	"athenaeum/internal/assets"
	"athenaeum/internal/config"
	"athenaeum/internal/documents"
	"athenaeum/internal/gitview"
	"athenaeum/internal/httpapi"
	"athenaeum/internal/notes"
	"athenaeum/internal/registry"
	"athenaeum/internal/search"
	"athenaeum/internal/session"
	"athenaeum/internal/watcher"
	"athenaeum/internal/workspace"
)

// loaded is one open workspace and everything built for it.
//
// Every field belongs to a single root. Nothing here is shared with another
// workspace and nothing outside it refers to these services, so closing this
// value is the whole of unloading a workspace (ADR-0004).
type loaded struct {
	bound *httpapi.Bound
	// cancel stops the goroutines scoped to this workspace: the Git adapter's
	// refresh loop and the watcher-to-Git bridge. The watcher and the search
	// service own their own shutdown and are closed explicitly.
	cancel  context.CancelFunc
	watcher *watcher.Watcher
	search  *search.Service
	// documentCount and pendingRecovery feed the launch banner.
	documentCount   int
	pendingRecovery int
}

// close stops everything this workspace started.
//
// Order matters. The context goes first so the Git adapter stops issuing
// commands against a root that is about to be abandoned. Search is closed
// before the watcher because search subscribes to it, and closing the consumer
// first means the producer never broadcasts into a channel nobody is reading.
// Both Close calls wait for their goroutines, so when this returns nothing from
// this workspace is still running.
func (l *loaded) close() {
	if l == nil {
		return
	}
	if l.cancel != nil {
		l.cancel()
	}
	if l.search != nil {
		_ = l.search.Close()
	}
	if l.watcher != nil {
		_ = l.watcher.Close()
	}
}

// controller owns which workspace is open and is the only thing that may change
// it.
//
// It holds one *loaded at a time. That is not an incidental detail of the
// implementation but the enforcement point for D-006 and R1: there is no field
// here capable of holding two workspaces, so no request can be answered from
// two roots and no feature built on this can make two roots visible at once.
type controller struct {
	opts         Options
	registryPath string
	server       *httpapi.Server

	// ctx is the process context; each workspace derives its own from it.
	ctx context.Context

	// mu serialises switching. A switch tears down and rebuilds several
	// services, and two overlapping switches would leak whichever loaded value
	// lost the race.
	mu      sync.Mutex
	current *loaded
}

// List re-reads the registry from disk on every call.
//
// Not cached: the file is hand-edited, and a user who adds an entry expects to
// see it without restarting the process (C8, ADR-0004).
func (c *controller) List() (*registry.Registry, error) {
	return registry.Load(c.registryPath)
}

// Open unloads the current workspace and opens the named registry entry.
//
// The previous workspace is closed before the new one is built. Doing it in
// that order costs a moment where nothing is open, and buys the guarantee that
// two roots are never loaded at the same time — which is the line ADR-0004
// draws and the reason this is a launcher rather than multi-root.
func (c *controller) Open(name string) error {
	reg, err := c.List()
	if err != nil {
		return err
	}
	entry, err := reg.Lookup(name)
	if err != nil {
		return err
	}
	if !entry.Available {
		return &httpapi.EntryUnavailableError{
			Name:   entry.Name,
			Code:   entry.Code,
			Reason: entry.Reason,
			Remedy: entry.Remedy,
		}
	}

	cfg, err := config.Load(entry.ConfigPath)
	if err != nil {
		return err
	}
	if diags := cfg.Validate(); diags.HasErrors() {
		count, _ := diags.Counts()
		return &httpapi.EntryUnavailableError{
			Name:   entry.Name,
			Code:   registry.CodeConfigInvalid,
			Reason: fmt.Sprintf("the workspace configuration has %d error(s)", count),
			Remedy: "run `athenaeum validate` against this workspace to see the detail",
		}
	}
	if c.opts.SafeMode {
		cfg.ApplySafeMode()
	}

	return c.switchTo(cfg)
}

// Leave unloads the current workspace and returns to the picker.
func (c *controller) Leave() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// The server stops answering from this workspace before its services are
	// stopped, so no request can be mid-flight against a closing index.
	c.server.Bind(nil)
	previous := c.current
	c.current = nil
	previous.close()
	return nil
}

// switchTo replaces the open workspace with one built from cfg.
func (c *controller) switchTo(cfg *config.Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	next, err := c.build(cfg)
	if err != nil {
		// Nothing was swapped, so whatever was open stays open and usable. A
		// failed switch must not leave the process with no workspace and no
		// explanation.
		return err
	}

	c.server.Bind(nil)
	previous := c.current
	c.current = next
	// Unload before binding the new workspace: at no instant are two roots
	// reachable, and the previous index and watcher are released before the new
	// ones start competing for the same cache directory.
	previous.close()
	c.server.Bind(next.bound)

	c.opts.Logger.Info("workspace opened", "workspace", cfg.Name, "documents", next.documentCount)
	return nil
}

// build constructs every service for one workspace without touching what is
// currently open.
//
// A failure here leaves nothing behind: anything already started is closed
// before the error is returned.
func (c *controller) build(cfg *config.Config) (*loaded, error) {
	// Launch-mode checks are per workspace, not per process. A workspace that
	// enables raw HTML must be refused in remote mode however it was reached,
	// including by switching into it (spec 05 section 6).
	if diags := cfg.ValidateRuntime(c.opts.Remote); diags.HasErrors() {
		count, _ := diags.Counts()
		return nil, &httpapi.EntryUnavailableError{
			Name:   cfg.Name,
			Code:   "WORKSPACE_UNSAFE_FOR_MODE",
			Reason: fmt.Sprintf("this workspace's configuration is not safe for this launch mode (%d error(s))", count),
			Remedy: "start without --remote, or change the workspace configuration",
		}
	}

	ws, err := workspace.Open(cfg)
	if err != nil {
		return nil, err
	}
	for _, d := range ws.Diagnostics() {
		c.opts.Logger.Warn("workspace", "field", d.Field, "detail", d.Message)
	}

	ctx, cancel := context.WithCancel(c.ctx)
	entry := &loaded{
		cancel:        cancel,
		documentCount: ws.Count(),
		bound: &httpapi.Bound{
			Name:              cfg.Name,
			Root:              cfg.AbsRoot,
			AllowRemoteAssets: cfg.Assets.AllowRemote,
			Workspace:         ws,
			Documents:         documents.New(ws),
			Assets:            assets.New(ws),
		},
	}

	// Personal state lives outside the workspace and is keyed on this root, so
	// switching gets a different store rather than inheriting the last one
	// (spec 03 section 1). A failure degrades recovery but must not stop a
	// workspace opening.
	var dirs session.Dirs
	dirsReady := false
	key := session.NewWorkspaceKey(cfg.AbsRoot, "")
	if resolved, err := session.ResolveDirs(key); err != nil {
		c.opts.Logger.Warn("crash recovery unavailable", "error", err)
	} else {
		dirs, dirsReady = resolved, true
		if store, err := session.NewRecoveryStore(dirs); err != nil {
			c.opts.Logger.Warn("crash recovery unavailable", "error", err)
		} else {
			entry.bound.Recovery = store
			if pending := store.Count(); pending > 0 {
				c.opts.Logger.Info("unsaved buffers are available to recover", "count", pending)
				entry.pendingRecovery = pending
			}
		}
		if store, err := session.NewStateStore(dirs); err != nil {
			c.opts.Logger.Warn("session restoration unavailable", "error", err)
		} else {
			entry.bound.SessionState = store
		}
	}

	// Annotations (R8). Shared sidecars live under the workspace and are always
	// available; personal sidecars need the user data directory, so they are
	// enabled only when it resolved. Either store being off degrades that
	// visibility alone, never document reading or editing.
	personalAnnotations := ""
	if dirsReady {
		personalAnnotations = filepath.Join(dirs.Data, "annotations")
	}
	entry.bound.Annotations = annotations.NewService(annotations.Options{
		PersonalDir: personalAnnotations,
		SharedDir:   filepath.Join(cfg.AbsRoot, ".athenaeum", "shared", "annotations"),
		Docs:        documentSource{docs: entry.bound.Documents},
	})

	// Notes (R9), the same personal/shared split as annotations.
	personalNotes := ""
	if dirsReady {
		personalNotes = filepath.Join(dirs.Data, "notes")
	}
	entry.bound.Notes = notes.NewService(notes.Options{
		PersonalDir: personalNotes,
		SharedDir:   filepath.Join(cfg.AbsRoot, ".athenaeum", "shared", "notes"),
	})

	// The watcher is advisory: a failure costs live updates, never correctness,
	// so it must not stop a workspace opening (spec 02 section 3.4).
	if w, err := watcher.New(ws, c.opts.Logger); err != nil {
		c.opts.Logger.Warn("live change notifications unavailable", "error", err)
	} else {
		entry.watcher = w
		entry.bound.Watcher = w
	}

	// Read-only Git context. Absent Git is not an error: the panel and the
	// search filter simply report themselves unavailable (acceptance J4, C1).
	var gitAdapter *gitview.Adapter
	if cfg.Git.Enabled {
		gitAdapter = gitview.New(cfg.AbsRoot, c.opts.Logger)
	}

	// The disposable FTS projection (R7, D-014). It lives under the OS cache
	// directory, never inside the workspace, and a failure to open it costs
	// search alone (C1, C2).
	if cfg.Search.Enabled && dirsReady {
		index, err := search.Open(dirs.Cache, search.ProjectionKey(cfg))
		if err != nil {
			c.opts.Logger.Warn("search is unavailable", "error", err)
		} else {
			svc := search.NewService(search.Options{
				Index:     index,
				Workspace: ws,
				Documents: entry.bound.Documents,
				Watcher:   entry.watcher,
				Git:       gitStates(gitAdapter),
				Logger:    c.opts.Logger,
				View: documents.IndexOptions{
					IncludeCodeBlocks:  cfg.Search.IndexCodeBlocks,
					IncludeFrontMatter: cfg.Search.IndexFrontMatter,
				},
			})
			entry.search = svc
			entry.bound.Search = svc
		}
	}

	// Start everything only once construction has succeeded, so a failure above
	// never leaves a half-started workspace running.
	if entry.watcher != nil {
		entry.watcher.Start(ctx)
	}
	if gitAdapter != nil {
		go gitAdapter.Run(ctx)
		if entry.watcher != nil {
			go followWorkspace(ctx, entry.watcher, gitAdapter)
		}
	}
	// Indexing starts in the background and never blocks a request: the server
	// must stay responsive regardless of corpus size (requirements N1 and N2).
	if entry.search != nil {
		entry.search.Start(ctx)
	}

	return entry, nil
}

// active returns the open workspace, or nil at the picker.
func (c *controller) active() *loaded {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// shutdown unloads whatever is open. It is called once, as the process exits.
func (c *controller) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.server != nil {
		c.server.Bind(nil)
	}
	c.current.close()
	c.current = nil
}

// documentSource adapts the document service to annotations.DocumentSource,
// supplying the current body and authoritative outline (ADR-0003) that anchor
// repair needs while keeping the annotations package free of a documents
// import.
type documentSource struct{ docs *documents.Service }

func (d documentSource) Source(id string) (string, []annotations.Heading, error) {
	doc, err := d.docs.Read(id)
	if err != nil {
		return "", nil, err
	}
	headings := make([]annotations.Heading, len(doc.Outline))
	for i, h := range doc.Outline {
		headings[i] = annotations.Heading{Path: h.Path, Line: h.Line}
	}
	return doc.Content, headings, nil
}
