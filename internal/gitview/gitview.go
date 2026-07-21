// Package gitview provides read-only Git context (spec 02 section 3.10, D-019).
//
// Scope note: v0.1 Phase 3 needs only per-file state, because R7 requires a
// Git-state search filter. Diff, history, and blame belong to Phase 5 and are
// deliberately absent — this package is the seam they will extend, not a stub
// standing in for them.
//
// Athenaeum never mutates a repository. The command allow-list below is the
// mechanism, and acceptance J3 is the test: no mutating subcommand can be
// reached through this type because no mutating subcommand appears in it.
package gitview

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Per-file states (acceptance J1).
const (
	StateModified  = "modified"
	StateUntracked = "untracked"
	StateClean     = "clean"
)

// commandTimeout bounds every Git invocation. A repository on a slow or
// unresponsive filesystem must degrade to "Git unavailable", never hang a
// request (spec 02 section 3.10).
const commandTimeout = 10 * time.Second

// minRefreshInterval throttles status refreshes. The watcher already coalesces
// bursts, but a save storm should not fork one `git status` per batch.
const minRefreshInterval = time.Second

// staleAfter forces a refresh even without a filesystem event, so a change made
// through another tool that the watcher missed still shows up.
const staleAfter = 15 * time.Second

// Adapter runs an allow-listed set of read-only Git commands.
type Adapter struct {
	// workDir is the canonical workspace root; Git runs there and nowhere else.
	workDir string
	// prefix is the workspace root's path relative to the repository root, so
	// repository-relative paths can be turned into document IDs.
	prefix string
	log    *slog.Logger

	refresh chan struct{}

	mu          sync.RWMutex
	states      map[string]string
	available   bool
	lastRefresh time.Time
}

// ErrUnavailable reports that Git context cannot be provided.
var ErrUnavailable = errors.New("Git is not available for this workspace")

// New locates the repository containing root and returns an adapter.
//
// A workspace outside a repository, or a machine without `git`, is not an
// error: the rest of the product works unchanged (acceptance J4, constitution
// C1). The caller receives an adapter reporting itself unavailable.
func New(root string, log *slog.Logger) *Adapter {
	if log == nil {
		log = slog.Default()
	}
	a := &Adapter{
		workDir: root,
		log:     log,
		refresh: make(chan struct{}, 1),
		states:  map[string]string{},
	}

	top, err := a.run(context.Background(), "rev-parse", "--show-toplevel")
	if err != nil {
		log.Debug("git context unavailable", "error_code", "GIT_UNAVAILABLE")
		return a
	}
	repoRoot := strings.TrimSpace(string(top))
	if repoRoot == "" {
		return a
	}

	// Document IDs are relative to the workspace root, which may be a
	// subdirectory of the repository.
	// git reports a canonical toplevel, so the workspace root must be
	// canonicalised too. On macOS /var is a symlink to /private/var, and
	// comparing the two forms textually makes every status entry fail to match
	// its document — which reports the whole workspace as clean.
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(repoRoot); resolveErr == nil {
		repoRoot = resolved
	}

	rel, err := filepath.Rel(repoRoot, root)
	if err != nil {
		return a
	}
	if rel == "." {
		rel = ""
	}
	a.prefix = filepath.ToSlash(rel)
	a.available = true
	return a
}

// Available reports whether Git state can be read.
func (a *Adapter) Available() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.available
}

// State returns a document's Git state.
//
// ok is false only when Git is unavailable entirely, so a caller can tell
// "no Git" apart from "clean".
func (a *Adapter) State(documentID string) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.available {
		return "", false
	}
	if state, ok := a.states[documentID]; ok {
		return state, true
	}
	return StateClean, true
}

// States returns a copy of the current per-file states.
func (a *Adapter) States() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make(map[string]string, len(a.states))
	for id, state := range a.states {
		out[id] = state
	}
	return out
}

// Notify asks for a status refresh at the next opportunity.
func (a *Adapter) Notify() {
	select {
	case a.refresh <- struct{}{}:
	default:
	}
}

// Run keeps the status snapshot current until the context is cancelled.
//
// Status is refreshed in the background rather than on the request path:
// `git status` on a large repository takes long enough that a search request
// waiting for it would violate requirement N2.
func (a *Adapter) Run(ctx context.Context) {
	if !a.Available() {
		return
	}
	a.reload(ctx)

	ticker := time.NewTicker(staleAfter)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.reload(ctx)
		case <-a.refresh:
			a.mu.RLock()
			since := time.Since(a.lastRefresh)
			a.mu.RUnlock()
			if since < minRefreshInterval {
				// Coalesce: wait out the throttle, then refresh once.
				select {
				case <-ctx.Done():
					return
				case <-time.After(minRefreshInterval - since):
				}
			}
			a.reload(ctx)
		}
	}
}

func (a *Adapter) reload(ctx context.Context) {
	states, err := a.status(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		a.log.Debug("git status failed", "error_code", "GIT_STATUS_FAILED")
		return
	}
	a.mu.Lock()
	a.states = states
	a.lastRefresh = time.Now()
	a.mu.Unlock()
}

// status runs `git status --porcelain=v2` and parses per-file state.
func (a *Adapter) status(ctx context.Context) (map[string]string, error) {
	// -z gives NUL-separated, unquoted paths, so a filename containing a quote,
	// a backslash, or a newline parses correctly instead of being mangled.
	out, err := a.run(ctx, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}

	states := make(map[string]string)
	fields := bytes.Split(out, []byte{0})

	for i := 0; i < len(fields); i++ {
		entry := string(fields[i])
		if entry == "" {
			continue
		}
		switch entry[0] {
		case '1':
			// "1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>"
			if path, ok := fieldAfter(entry, 8); ok {
				a.record(states, path, StateModified)
			}
		case '2':
			// A rename or copy. The original path follows as its own field.
			if path, ok := fieldAfter(entry, 9); ok {
				a.record(states, path, StateModified)
			}
			i++
		case 'u':
			// Unmerged: "u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>"
			if path, ok := fieldAfter(entry, 10); ok {
				a.record(states, path, StateModified)
			}
		case '?':
			if path, ok := fieldAfter(entry, 1); ok {
				a.record(states, path, StateUntracked)
			}
		}
	}
	return states, nil
}

// fieldAfter returns the remainder of a porcelain line after n space-separated
// fields, which is the path. Splitting the whole line would break a path
// containing a space.
func fieldAfter(entry string, n int) (string, bool) {
	rest := entry
	for range n {
		space := strings.IndexByte(rest, ' ')
		if space < 0 {
			return "", false
		}
		rest = rest[space+1:]
	}
	if rest == "" {
		return "", false
	}
	return rest, true
}

// record maps a repository-relative path onto a document ID.
func (a *Adapter) record(states map[string]string, repoPath, state string) {
	id := repoPath
	if a.prefix != "" {
		trimmed, ok := strings.CutPrefix(repoPath, a.prefix+"/")
		if !ok {
			return // Outside the workspace root; not a document here.
		}
		id = trimmed
	}
	states[id] = state
}

// allowed is the complete set of Git subcommands this package may execute.
//
// Spec 02 section 3.10 and D-019 restrict v0.1 to read-only operations. The
// list is enforced rather than documented so a future edit cannot widen it by
// accident: `run` rejects anything absent from it.
// Every entry is read-only. Spec 02 section 3.10 restricts v0.1 to inspection,
// and acceptance J3 proves it: a test asserts no mutating subcommand is present,
// and run rejects anything absent here, so no write can be reached.
var allowed = map[string]bool{
	"rev-parse": true,
	"status":    true,
	"diff":      true,
	"log":       true,
	"blame":     true,
}

// run executes one allow-listed Git command.
//
// exec.Command with an argument vector, never a shell: no part of a path or a
// document ID can be interpreted as a command (spec 02 section 3.10).
func (a *Adapter) run(ctx context.Context, subcommand string, args ...string) ([]byte, error) {
	if !allowed[subcommand] {
		return nil, ErrUnavailable
	}

	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{subcommand}, args...)...)
	cmd.Dir = a.workDir
	// A clean environment: no pager, no prompting, no locale-dependent output.
	cmd.Env = append(cmd.Environ(),
		"GIT_PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, ErrUnavailable
	}
	return stdout.Bytes(), nil
}
