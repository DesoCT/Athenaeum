// Package watcher reports filesystem changes to the rest of the application
// (spec 02 section 3.4).
//
// The watcher is advisory. It exists to make the UI feel live, not to
// establish truth: correctness always comes from file metadata and content
// hashes checked at read and write time. A missed event costs freshness, never
// data.
package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"athenaeum/internal/workspace"
)

// debounce coalesces the burst of events an editor produces when saving.
const debounce = 250 * time.Millisecond

// Change kinds.
const (
	KindModified = "modified"
	KindCreated  = "created"
	KindRemoved  = "removed"
)

// Change is one observed document change.
type Change struct {
	DocumentID string `json:"document_id"`
	Kind       string `json:"kind"`
	// Version is the new content fingerprint, absent for a removal. The client
	// compares it against the version it holds to decide whether it is stale.
	Version string `json:"version,omitempty"`
}

// Watcher observes a workspace and emits coalesced changes.
type Watcher struct {
	ws   *workspace.Workspace
	log  *slog.Logger
	fsw  *fsnotify.Watcher
	subs *subscribers

	mu sync.Mutex
	// pending collects touched paths until the debounce window closes.
	pending map[string]string
	timer   *time.Timer
	// selfWrites records fingerprints Athenaeum just wrote, so its own saves
	// are not reported back to the editor that made them.
	selfWrites map[string]string
}

// New creates a watcher over the workspace root.
func New(ws *workspace.Workspace, log *slog.Logger) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		ws:         ws,
		log:        log,
		fsw:        fsw,
		subs:       newSubscribers(),
		pending:    make(map[string]string),
		selfWrites: make(map[string]string),
	}
	if err := w.addDirs(); err != nil {
		fsw.Close()
		return nil, err
	}
	return w, nil
}

// addDirs registers every directory under the root. fsnotify watches
// directories, not files, so new files are noticed without re-registering.
func (w *Watcher) addDirs() error {
	root := w.ws.Guard().Root()
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil // Unreadable subtrees are skipped, not fatal.
		}
		if !entry.IsDir() {
			return nil
		}
		if name := entry.Name(); name == ".git" || name == "node_modules" {
			return filepath.SkipDir
		}
		if err := w.fsw.Add(path); err != nil {
			w.log.Debug("watch directory", "path", path, "error", err)
		}
		return nil
	})
}

// Subscribe returns a channel of change batches and a cancel function.
func (w *Watcher) Subscribe() (<-chan []Change, func()) {
	return w.subs.add()
}

// NoteSelfWrite records a fingerprint Athenaeum itself just wrote, so the
// resulting event is recognised as its own and not reported as external.
func (w *Watcher) NoteSelfWrite(documentID, version string) {
	w.mu.Lock()
	w.selfWrites[documentID] = version
	w.mu.Unlock()
}

// Run watches until the context is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	defer w.fsw.Close()
	defer w.subs.closeAll()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(event)

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			// A watcher error degrades liveness only.
			w.log.Debug("watcher error", "error", err)
		}
	}
}

func (w *Watcher) handle(event fsnotify.Event) {
	// A newly created directory must be watched too, or files inside it are
	// invisible.
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			_ = w.fsw.Add(event.Name)
			return
		}
	}

	id, err := w.ws.Guard().DocumentID(event.Name)
	if err != nil {
		return
	}

	kind := KindModified
	switch {
	case event.Has(fsnotify.Remove), event.Has(fsnotify.Rename):
		kind = KindRemoved
	case event.Has(fsnotify.Create):
		kind = KindCreated
	case event.Has(fsnotify.Write):
		kind = KindModified
	default:
		return // Chmod and similar are not content changes.
	}

	w.mu.Lock()
	w.pending[id] = mergeKind(w.pending[id], kind)
	if w.timer == nil {
		w.timer = time.AfterFunc(debounce, w.flush)
	} else {
		w.timer.Reset(debounce)
	}
	w.mu.Unlock()
}

// mergeKind combines two kinds observed for one document inside a single
// debounce window.
//
// Creating a file emits Create immediately followed by Write, so plain
// last-write-wins reported it as a mere modification and the client never
// learned the tree had gained a document. Precedence is
// removed > created > modified: a file created and then deleted in the same
// window is gone, and a file created and then written is still new.
func mergeKind(existing, incoming string) string {
	if existing == "" {
		return incoming
	}
	if existing == KindRemoved || incoming == KindRemoved {
		return KindRemoved
	}
	if existing == KindCreated || incoming == KindCreated {
		return KindCreated
	}
	return KindModified
}

// flush emits the coalesced batch once the debounce window closes.
func (w *Watcher) flush() {
	w.mu.Lock()
	pending := w.pending
	w.pending = make(map[string]string)
	w.timer = nil
	selfWrites := w.selfWrites
	w.selfWrites = make(map[string]string)
	w.mu.Unlock()

	if len(pending) == 0 {
		return
	}

	// Re-enumerate so the tree and the change batch agree.
	_ = w.ws.Refresh()

	var batch []Change
	for id, kind := range pending {
		change := Change{DocumentID: id, Kind: kind}

		if kind != KindRemoved {
			version, err := w.fingerprint(id)
			if err != nil {
				// The file vanished between the event and the read.
				change.Kind = KindRemoved
			} else {
				change.Version = version
				// Spec 02 section 3.4: classify self-writes versus external
				// writes by comparing content, not by trusting event order.
				if expected, ok := selfWrites[id]; ok && expected == version {
					continue
				}
			}
		}
		batch = append(batch, change)
	}

	if len(batch) > 0 {
		w.subs.broadcast(batch)
	}
}

// fingerprint hashes a document exactly as the document service does, so the
// versions the client compares are interchangeable.
func (w *Watcher) fingerprint(id string) (string, error) {
	absPath, err := w.ws.ResolveRead(id)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	return hashBytes(raw), nil
}

// hashBytes mirrors the document service's fingerprint exactly, so the versions
// a client compares across the two are interchangeable.
func hashBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// subscribers fans one batch out to every listener.
type subscribers struct {
	mu   sync.Mutex
	next int
	subs map[int]chan []Change
}

func newSubscribers() *subscribers {
	return &subscribers{subs: make(map[int]chan []Change)}
}

func (s *subscribers) add() (<-chan []Change, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.next
	s.next++
	// Buffered so a slow client cannot stall the watcher.
	ch := make(chan []Change, 8)
	s.subs[id] = ch

	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if existing, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(existing)
		}
	}
}

func (s *subscribers) broadcast(batch []Change) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- batch:
		default:
			// A client that cannot keep up misses this batch. It will still
			// detect staleness on its next read or save, because correctness
			// never depends on these events.
		}
	}
}

func (s *subscribers) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ch := range s.subs {
		delete(s.subs, id)
		close(ch)
	}
}
