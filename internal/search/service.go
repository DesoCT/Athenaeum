package search

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"athenaeum/internal/documents"
	"athenaeum/internal/security"
	"athenaeum/internal/watcher"
	"athenaeum/internal/workspace"
)

// Index states reported to the UI (spec 04 section 8).
const (
	// StateDisabled means `search.enabled = false`.
	StateDisabled = "disabled"
	// StateUnavailable means the projection could not be opened. Search is off;
	// everything else in the workspace still works (constitution C1).
	StateUnavailable = "unavailable"
	// StateBuilding means the first build for this cache is in progress.
	StateBuilding = "building"
	// StateRebuilding means an existing index is catching up.
	StateRebuilding = "rebuilding"
	// StateReady means the projection matches the workspace.
	StateReady = "ready"
)

// workerCount bounds the indexing pool (spec 02 section 6). Indexing is IO-bound
// on reads and serialised at the writer, so a small pool is the whole benefit.
const workerCount = 4

// batchDocuments and batchBytes bound one write transaction. The byte bound
// matters at the N3 ceiling: 64 ten-megabyte documents in one batch would hold
// 640 MB of text in memory at once.
const (
	batchDocuments = 64
	batchBytes     = 16 << 20
)

// locateBudget bounds the bytes read from disk to attribute match locations for
// one query. Ordinary Markdown never approaches it; a corpus of very large
// files degrades to "open at the top of the document" rather than to a slow
// query.
const locateBudget = 8 << 20

// GitStates reports per-document Git state for the search filter (R7).
//
// It is an interface so the search service does not depend on the Git adapter's
// lifecycle: when Git is absent or disabled the service is given nil and the
// filter reports itself unavailable rather than silently returning nothing.
type GitStates interface {
	// State returns the Git state of a document. ok is false when Git state is
	// not available at all.
	State(documentID string) (state string, ok bool)
	// Available reports whether Git state can be filtered on right now.
	Available() bool
}

// Options configure the search service.
type Options struct {
	Index     *Index
	Workspace *workspace.Workspace
	Documents *documents.Service
	Watcher   *watcher.Watcher
	Git       GitStates
	Logger    *slog.Logger
	// View controls what of each document is indexed (spec 05 `[search]`).
	View documents.IndexOptions
}

// Service maintains the disposable FTS projection and answers queries.
//
// Nothing it holds is authoritative. Deleting the file it writes loses no user
// data: the next start rebuilds it from the workspace (C2, D-014, acceptance F3).
type Service struct {
	index *Index
	ws    *workspace.Workspace
	docs  *documents.Service
	watch *watcher.Watcher
	git   GitStates
	log   *slog.Logger
	view  documents.IndexOptions

	// wake signals the coordinator that pending work exists. Buffered to one:
	// a queued wake is as good as many.
	wake chan struct{}

	// life guards the shutdown state below. It is deliberately not `mu`: Close
	// waits for goroutines that take `mu` themselves, so holding one lock for
	// both would deadlock the shutdown it is meant to perform.
	life sync.Mutex
	// ctx is the service's own context, derived from the caller's at Start.
	// Owning a context is what lets a service be stopped without ending the
	// process, which is what a workspace switch requires.
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
	// tasks covers every goroutine the service starts, so Close can prove they
	// have finished before the index is shut.
	tasks     sync.WaitGroup
	closeOnce sync.Once
	closeErr  error

	mu sync.Mutex
	// pending holds document IDs awaiting indexing. Membership is enough —
	// whether an entry is an update or a removal is decided at processing time
	// by asking the workspace, so the index can never disagree with it.
	pending map[string]struct{}
	status  Status
}

// Status describes the projection for the status bar and the search panel.
type Status struct {
	State string `json:"state"`
	// Indexed is the number of documents currently in the projection.
	Indexed int `json:"indexed"`
	// Total is the number of documents the workspace includes.
	Total int `json:"total"`
	// Pending is the number of documents queued for indexing. Non-zero means
	// results may be incomplete, which the UI shows as "stale".
	Pending int `json:"pending"`
	// GitFilter reports whether the Git-state filter can be offered.
	GitFilter bool `json:"git_filter"`
	// LastBuiltAt is when the projection last reached a settled state.
	LastBuiltAt string `json:"last_built_at,omitempty"`
	// LastDurationMs is how long the last catch-up took.
	LastDurationMs int64 `json:"last_duration_ms,omitempty"`
	// Error is a stable code, never a message containing document content
	// (spec 03 section 12).
	Error string `json:"error,omitempty"`
}

// NewService creates the service. It does no work until Start is called.
func NewService(opts Options) *Service {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		index:   opts.Index,
		ws:      opts.Workspace,
		docs:    opts.Documents,
		watch:   opts.Watcher,
		git:     opts.Git,
		log:     log,
		view:    opts.View,
		wake:    make(chan struct{}, 1),
		pending: make(map[string]struct{}),
		status:  Status{State: StateBuilding},
	}
}

// Start launches background indexing and returns immediately.
//
// Requirements N1 and N2 are the whole point of this signature: the HTTP
// listener must be accepting requests within two seconds of launch, and no
// interaction may wait on indexing. Nothing on the startup path opens a
// document.
// Start is safe to call once. A second call, or a call after Close, does
// nothing: a service whose goroutines outlive the cancel function that reaches
// them is precisely the leak this lifecycle exists to prevent.
func (s *Service) Start(ctx context.Context) {
	s.life.Lock()
	if s.closed || s.cancel != nil {
		s.life.Unlock()
		return
	}
	// The caller's context still stops the service — it is a parent — but the
	// service now also holds a cancel of its own, so Close can stop it while the
	// caller's context lives on.
	s.ctx, s.cancel = context.WithCancel(ctx)
	serviceCtx := s.ctx
	s.life.Unlock()

	s.spawn(func() { s.coordinate(serviceCtx) })

	if s.watch != nil {
		s.spawn(func() { s.follow(serviceCtx) })
	}

	// The initial diff runs in the background too. On a warm cache it is one
	// SQL query and a map comparison, but a cold cache would otherwise read the
	// whole corpus before the first request could be served.
	s.spawn(func() { s.scan(serviceCtx) })
}

// spawn runs fn as a tracked goroutine, refusing to start work on a closed
// service.
//
// The closed check and the WaitGroup increment share one lock, and Close marks
// the service closed under that same lock before it waits. That ordering is
// what makes it impossible for an Add to race a Wait already in progress.
func (s *Service) spawn(fn func()) {
	s.life.Lock()
	defer s.life.Unlock()

	if s.closed {
		return
	}
	s.tasks.Add(1)
	go func() {
		defer s.tasks.Done()
		fn()
	}()
}

// Close stops background work and releases the projection.
//
// The order matters and is the fix for a real defect: cancel, wait, then close
// the index. Closing the index first would let a goroutine still mid-batch
// write to a closed database. Not cancelling at all — the original behaviour —
// left the coordinator and the watcher follower running for the life of the
// process, which made stopping one workspace to open another impossible.
//
// Close is idempotent and safe to call concurrently. It blocks until every
// goroutine Start launched has returned, so a caller that has closed a service
// knows nothing of it is still touching the workspace.
func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		s.life.Lock()
		s.closed = true
		cancel := s.cancel
		s.life.Unlock()

		if cancel != nil {
			cancel()
		}
		s.tasks.Wait()

		if s.index != nil {
			s.closeErr = s.index.Close()
		}
	})
	return s.closeErr
}

// Status returns the current projection status.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.status
	status.Pending = len(s.pending)
	if s.ws != nil {
		status.Total = s.ws.Count()
	}
	status.GitFilter = s.git != nil && s.git.Available()
	if status.State == StateReady && status.Pending > 0 {
		status.State = StateRebuilding
	}
	return status
}

// Rebuild discards nothing on disk but re-examines every document.
//
// It is the "rebuild index" command of spec 04 section 4.3. A projection that
// has drifted is repaired by re-reading the workspace, which is always safe
// because the files are the source of truth.
func (s *Service) Rebuild() {
	s.life.Lock()
	if s.closed {
		s.life.Unlock()
		return
	}
	ctx := s.ctx
	s.life.Unlock()

	if ctx == nil {
		// Rebuild before Start has nothing to rebuild against; the initial scan
		// will cover the corpus anyway.
		return
	}

	s.mu.Lock()
	s.status.State = StateRebuilding
	s.mu.Unlock()
	s.spawn(func() { s.scanAll(ctx) })
}

// scan diffs the workspace against the projection and queues what differs.
func (s *Service) scan(ctx context.Context) { s.diff(ctx, false) }

// scanAll queues every document regardless of the stored fingerprint.
func (s *Service) scanAll(ctx context.Context) { s.diff(ctx, true) }

func (s *Service) diff(ctx context.Context, force bool) {
	if s.index == nil || s.ws == nil {
		return
	}
	started := time.Now()

	stored, err := s.index.Snapshot()
	if err != nil {
		if ctx.Err() != nil {
			// Shutting down: the read failed because the service is stopping,
			// which is not a projection fault worth reporting.
			return
		}
		s.fail("INDEX_READ_FAILED", err)
		return
	}
	if ctx.Err() != nil {
		return
	}

	live := s.ws.Documents()
	queued := 0
	for _, doc := range live {
		entry, indexed := stored[doc.ID]
		delete(stored, doc.ID)
		if !force && indexed && entry.Size == doc.Size && entry.ModTime == doc.ModTime {
			// Unchanged since the last index: no file is opened at all, which
			// is what makes a warm start cheap (requirement N1).
			continue
		}
		s.enqueue(doc.ID)
		queued++
	}
	// Whatever is left in `stored` is in the projection but no longer in the
	// workspace: deleted, renamed, or newly excluded. Queueing it makes the
	// coordinator remove it (acceptance B1).
	for id := range stored {
		s.enqueue(id)
		queued++
	}

	s.log.Debug("search index scan", "documents", len(live), "queued", queued,
		"duration_ms", time.Since(started).Milliseconds())

	if ctx.Err() != nil {
		return
	}
	if queued == 0 {
		s.settle(0)
		return
	}
	s.signal()
}

// follow subscribes to watcher change batches (R7's two-second target).
//
// The watcher already coalesces bursts and filters out Athenaeum's own writes,
// so this is a queue-and-wake rather than any logic of its own.
func (s *Service) follow(ctx context.Context) {
	changes, cancel := s.watch.Subscribe()
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case batch, open := <-changes:
			if !open {
				return
			}
			for _, change := range batch {
				s.enqueue(change.DocumentID)
			}
			s.signal()
		}
	}
}

func (s *Service) enqueue(id string) {
	s.mu.Lock()
	s.pending[id] = struct{}{}
	s.mu.Unlock()
}

func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
		// A wake is already queued; the coordinator drains everything pending.
	}
}

// coordinate is the single goroutine that drives indexing.
func (s *Service) coordinate(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			s.drain(ctx)
		}
	}
}

// maxWriteAttempts bounds how long one drain keeps retrying a failing write
// before it gives up and waits for the next change to try again.
const maxWriteAttempts = 5

// drain processes everything pending, then reports the projection settled.
func (s *Service) drain(ctx context.Context) {
	started := time.Now()
	failures := 0

	for {
		batch := s.take(batchDocuments * workerCount)
		if len(batch) == 0 {
			break
		}
		if err := s.process(ctx, batch); err != nil {
			if ctx.Err() != nil {
				return
			}
			// A write failure is almost always contention: another Athenaeum
			// process holding the same cache file, or a slow disk. The work
			// goes back on the queue and is retried, because the index is a
			// cache — being late is a far better answer than giving up on it.
			for _, id := range batch {
				s.enqueue(id)
			}
			failures++
			if failures >= maxWriteAttempts {
				// Search stays available on whatever is already indexed. The
				// documents remain queued, so the next change retries them.
				s.degrade("INDEX_WRITE_FAILED", err)
				return
			}
			s.backOff(ctx, failures)
			continue
		}
		failures = 0
		if ctx.Err() != nil {
			return
		}
	}
	s.settle(time.Since(started))
}

// backOff waits between write attempts, giving up promptly on shutdown.
func (s *Service) backOff(ctx context.Context, attempt int) {
	select {
	case <-ctx.Done():
	case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
	}
}

// take removes up to n pending IDs.
func (s *Service) take(n int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pending) == 0 {
		return nil
	}
	if s.status.State == StateReady {
		s.status.State = StateRebuilding
	}
	out := make([]string, 0, n)
	for id := range s.pending {
		out = append(out, id)
		delete(s.pending, id)
		if len(out) >= n {
			break
		}
	}
	return out
}

// process reads a set of documents through a bounded worker pool and writes the
// results through the single writer.
func (s *Service) process(ctx context.Context, ids []string) error {
	type outcome struct {
		view *documents.IndexView
		id   string
		gone bool
	}

	work := make(chan string)
	results := make(chan outcome, workerCount)

	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range work {
				view, err := s.docs.IndexView(id, s.view)
				if err != nil {
					// A document the workspace no longer includes — deleted,
					// renamed, or newly excluded — is removed from the
					// projection. Any other read failure is also treated as
					// "not indexable now": the file is authoritative and the
					// next change re-queues it.
					if security.CodeOf(err) == "" && !os.IsNotExist(err) {
						s.log.Debug("index document", "document_id", id,
							"error_code", "DOCUMENT_READ_FAILED")
					}
					results <- outcome{id: id, gone: true}
					continue
				}
				results <- outcome{view: view, id: id}
			}
		}()
	}

	go func() {
		defer close(work)
		for _, id := range ids {
			select {
			case <-ctx.Done():
				return
			case work <- id:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var (
		writes  []*documents.IndexView
		removes []string
		bytes   int64
		errs    []error
	)
	flush := func() {
		if err := s.index.PutBatch(writes); err != nil {
			errs = append(errs, err)
		}
		if err := s.index.DeleteBatch(removes); err != nil {
			errs = append(errs, err)
		}
		writes, removes, bytes = writes[:0], removes[:0], 0
	}

	for result := range results {
		if result.gone {
			removes = append(removes, result.id)
		} else {
			writes = append(writes, result.view)
			bytes += result.view.Size
		}
		if len(writes)+len(removes) >= batchDocuments || bytes >= batchBytes {
			flush()
		}
	}
	flush()

	return errors.Join(errs...)
}

// settle records that the projection has caught up.
func (s *Service) settle(took time.Duration) {
	indexed := 0
	if s.index != nil {
		if count, err := s.index.Count(); err == nil {
			indexed = count
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pending) > 0 {
		// More arrived while the last batch was being written.
		s.status.State = StateRebuilding
		return
	}
	s.status.State = StateReady
	s.status.Indexed = indexed
	s.status.Error = ""
	s.status.LastBuiltAt = time.Now().UTC().Format(time.RFC3339)
	if took > 0 {
		s.status.LastDurationMs = took.Milliseconds()
	}
}

// fail marks the projection unusable.
//
// Reserved for failures that make search itself impossible, such as not being
// able to read the index at all. The underlying error is logged at debug level
// only and never carries document text; the status the UI sees is a stable code
// (spec 03 section 12, requirement N6).
func (s *Service) fail(code string, err error) {
	s.log.Warn("search index unavailable", "error_code", code)
	s.log.Debug("search index failure detail", "error", err)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.State = StateUnavailable
	s.status.Error = code
}

// degrade records a failure that left the projection usable but incomplete.
//
// A write failure is exactly this case: everything already indexed is still
// searchable and still correct. Declaring search dead because one batch could
// not be written would throw away working capability over a transient fault,
// which is the opposite of what a disposable cache should do (C1, C2).
func (s *Service) degrade(code string, err error) {
	s.log.Warn("search index is behind", "error_code", code)
	s.log.Debug("search index failure detail", "error", err)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Error = code
	if s.status.State != StateUnavailable {
		s.status.State = StateRebuilding
	}
}

// Filters narrow a search (R7).
type Filters struct {
	// Path is a substring of the document ID.
	Path string
	// Group is a configured document group ID.
	Group string
	// Git is a Git state: "modified", "untracked", or "clean".
	Git string
}

// Request is one search.
type Request struct {
	Query   string
	Filters Filters
	Limit   int
}

// Result is one ranked match.
type Result struct {
	DocumentID string   `json:"document_id"`
	Title      string   `json:"title"`
	Groups     []string `json:"groups,omitempty"`
	// HeadingPath is the enclosing heading chain of the matched line, taken
	// from the backend's authoritative outline (ADR-0003).
	HeadingPath []string `json:"heading_path,omitempty"`
	HeadingSlug string   `json:"heading_slug,omitempty"`
	// Line is the 1-based source line of the match, or 0 when the match was in
	// the path or title rather than in the text.
	Line int `json:"line,omitempty"`
	// Snippet is the matched text split into plain and highlighted runs.
	Snippet []Segment `json:"snippet,omitempty"`
	// Field explains what matched, so a result is never unexplained
	// (constitution C8).
	Field string `json:"field"`
}

// Match fields.
const (
	FieldBody    = "body"
	FieldHeading = "heading"
	FieldTitle   = "title"
	FieldPath    = "path"
)

// Response is a completed search.
type Response struct {
	Results []Result `json:"results"`
	// Truncated reports that more documents matched than were returned.
	Truncated bool   `json:"truncated"`
	Status    Status `json:"status"`
}

// Search errors, mapped to stable API codes by the HTTP layer.
var (
	// ErrUnavailable means the projection is not usable.
	ErrUnavailable = errors.New("the search index is not available")
	// ErrGitFilterUnavailable means a Git filter was asked for without Git.
	ErrGitFilterUnavailable = errors.New("Git state is not available for filtering")
	// ErrUnknownFilter means a filter value is not one this build understands.
	ErrUnknownFilter = errors.New("that filter value is not recognised")
)

const (
	defaultLimit = 25
	maxLimit     = 100
)

// Git states accepted by the filter.
var gitStates = map[string]bool{"modified": true, "untracked": true, "clean": true}

// Search runs one query.
//
// The projection ranks; the authoritative files are consulted for the match
// location. A document the workspace no longer includes is dropped even when
// the projection still holds it, so a stale index can never expose an excluded
// file (acceptance B1).
func (s *Service) Search(req Request) (Response, error) {
	if s.index == nil || s.ws == nil {
		return Response{}, ErrUnavailable
	}
	if status := s.Status(); status.State == StateUnavailable {
		return Response{Status: status}, ErrUnavailable
	}

	compiled, err := compile(req.Query)
	if err != nil {
		return Response{}, err
	}

	if req.Filters.Git != "" {
		if !gitStates[req.Filters.Git] {
			return Response{}, ErrUnknownFilter
		}
		if s.git == nil || !s.git.Available() {
			return Response{}, ErrGitFilterUnavailable
		}
	}
	if req.Filters.Group != "" && !s.knownGroup(req.Filters.Group) {
		return Response{}, ErrUnknownFilter
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	// A row is allowed through only when the live workspace still includes it
	// and it passes the Git filter.
	allow := func(id string) bool {
		if _, ok := s.ws.Lookup(id); !ok {
			return false
		}
		if req.Filters.Git == "" {
			return true
		}
		state, ok := s.git.State(id)
		return ok && state == req.Filters.Git
	}

	// One extra row tells the UI whether more matched without a second query.
	hits, err := s.find(compiled.Expression, req.Filters, limit+1, allow)
	if err != nil {
		s.log.Warn("search query failed", "error_code", "SEARCH_QUERY_FAILED")
		s.log.Debug("search query failure detail", "error", err)
		return Response{}, ErrUnavailable
	}

	truncated := len(hits) > limit
	if truncated {
		hits = hits[:limit]
	}

	results := make([]Result, 0, len(hits))
	budget := locateBudget
	for _, h := range hits {
		results = append(results, s.resolve(h, compiled.Terms, &budget))
	}

	return Response{Results: results, Truncated: truncated, Status: s.Status()}, nil
}

// resolve turns a ranked row into a result, attributing the match to a line in
// the authoritative file.
func (s *Service) resolve(h hit, terms []string, budget *int) Result {
	result := Result{
		DocumentID: h.documentID,
		Title:      h.title,
		Groups:     h.groups,
	}

	// Reading the file is what makes the reported line and the snippet correct
	// for the document as it is now, rather than as the projection last
	// recorded it (constitution C2).
	if *budget > 0 {
		if content, size, ok := s.readForLocation(h.documentID, *budget); ok {
			*budget -= size
			if line, text := locate(content, terms); line > 0 {
				result.Line = line
				result.Snippet = snippetFor(text, terms)
				result.HeadingPath, result.HeadingSlug = headingFor(h.outline, line)
				result.Field = FieldBody
				if insideHeading(h.outline, line) {
					result.Field = FieldHeading
				}
				return result
			}
		}
	}

	// Nothing in the text matched, so the document was found by its title or
	// its path. Saying which, and showing it, is what keeps a result from being
	// unexplained (constitution C8).
	result.Field = metadataField(h, terms)
	if result.Field == FieldTitle {
		result.Snippet = snippetFor(h.title, terms)
	} else {
		result.Snippet = snippetFor(h.documentID, terms)
	}
	return result
}

// readForLocation reads a document through the path guard, refusing files that
// would exhaust the per-query budget.
func (s *Service) readForLocation(id string, budget int) (string, int, bool) {
	absPath, err := s.ws.ResolveRead(id)
	if err != nil {
		return "", 0, false
	}
	info, err := os.Stat(absPath)
	if err != nil || info.Size() > int64(budget) {
		return "", 0, false
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return "", 0, false
	}
	return string(raw), len(raw), true
}

// insideHeading reports whether a line is itself a heading line.
func insideHeading(outline []documents.Heading, line int) bool {
	for _, h := range outline {
		if h.Line == line {
			return true
		}
	}
	return false
}

// metadataField attributes a non-body match to the path or the title.
func metadataField(h hit, terms []string) string {
	lowerID := strings.ToLower(h.documentID)
	lowerTitle := strings.ToLower(h.title)
	for _, term := range terms {
		needle := stemPrefix(term)
		if strings.Contains(lowerTitle, needle) {
			return FieldTitle
		}
		if strings.Contains(lowerID, needle) {
			return FieldPath
		}
	}
	return FieldHeading
}

// knownGroup reports whether a group ID is configured, so an unknown filter is
// an explicit error rather than a silently empty result set.
func (s *Service) knownGroup(id string) bool {
	for _, group := range s.ws.Config().Groups {
		if group.ID == id {
			return true
		}
	}
	return false
}
