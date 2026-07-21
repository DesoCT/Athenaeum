package annotations

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"athenaeum/internal/atomicfs"
)

// DocumentSource supplies the current body and outline of a document, so the
// store can stamp a source hash on creation and repair anchors on read. An
// interface keeps this package free of a documents import and easy to fake in
// tests.
type DocumentSource interface {
	// Source returns the LF-normalised body and heading outline for a document
	// id, or an error if the document cannot be read.
	Source(id string) (content string, headings []Heading, err error)
}

// Service reads and writes annotation sidecars for one workspace.
//
// Personal and shared annotations live in different files with independent
// revisions: a shared sidecar is committable and lives under the workspace,
// while a personal sidecar lives in the user data directory and never enters
// the repository (spec 03 sections 1 and 2, acceptance G1 and G2).
type Service struct {
	personalDir string // "" when personal storage is unavailable this session
	sharedDir   string
	docs        DocumentSource
	now         func() time.Time
}

// Options configures a Service.
type Options struct {
	// PersonalDir is the user-data annotations root (session Dirs.Data joined
	// with "annotations"). Empty disables personal annotations for this session
	// without affecting shared ones.
	PersonalDir string
	// SharedDir is the workspace's committable annotations root, normally
	// <root>/.athenaeum/shared/annotations.
	SharedDir string
	Docs      DocumentSource
}

// NewService binds a Service to a workspace's storage roots.
func NewService(opts Options) *Service {
	return &Service{
		personalDir: opts.PersonalDir,
		sharedDir:   opts.SharedDir,
		docs:        opts.Docs,
		now:         time.Now,
	}
}

// ConflictError reports a stale-revision write (spec 02 section 5). It carries
// the current revision and annotations so the client can reconcile without a
// second request that might race again, mirroring the document conflict shape.
type ConflictError struct {
	Visibility      string
	CurrentRevision int
	Current         []Annotation
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("annotation sidecar changed: %s revision is now %d", e.Visibility, e.CurrentRevision)
}

// NotFoundError reports an annotation id that is not in the addressed sidecar.
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string { return "annotation not found: " + e.ID }

// UnavailableError reports storage that is off this session (personal storage
// when the user data directory could not be resolved).
type UnavailableError struct{ Visibility string }

func (e *UnavailableError) Error() string {
	return e.Visibility + " annotations are unavailable in this session"
}

// SchemaError reports a sidecar written by a newer Athenaeum than this build
// understands. Reading it as if it were the current schema and writing it back
// would silently drop whatever the newer version added — a data-loss scenario
// that spec 08 lists as a release blocker — so such a file is refused rather
// than migrated in place (spec 03 section 3: migrations must be explicit).
type SchemaError struct {
	Found int
	Known int
}

func (e *SchemaError) Error() string {
	return fmt.Sprintf("annotation sidecar schema version %d is newer than this build understands (%d)", e.Found, e.Known)
}

// SourceError reports that the document an annotation targets could not be read
// on creation. It is a client error — a bad or stale document id — not a
// storage failure.
type SourceError struct{ Err error }

func (e *SourceError) Error() string { return "target document unreadable: " + e.Err.Error() }
func (e *SourceError) Unwrap() error { return e.Err }

// Ref is a lightweight pointer to an annotation for the Map Room home, without
// the full anchor selector.
type Ref struct {
	ID         string `json:"id"`
	DocumentID string `json:"document_id"`
	Visibility string `json:"visibility"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Body       string `json:"body"`
	// Line is the stored anchor line, a hint for opening the document. Zero for
	// a document-level anchor.
	Line int `json:"line,omitempty"`
}

// Overview summarises annotations across the whole workspace for the Map Room
// home (spec 04 section 3): pinned documents and unresolved comments.
type Overview struct {
	Pins       []Ref `json:"pins"`
	Unresolved []Ref `json:"unresolved"`
}

// Overview walks both annotation stores and collects pins (bookmarks) and
// unresolved comments across every document. It reads sidecars directly rather
// than repairing anchors: the home only needs to point at a document, and a
// full corpus repair would be far more work than the summary warrants.
func (s *Service) Overview() (*Overview, error) {
	ov := &Overview{Pins: []Ref{}, Unresolved: []Ref{}}
	for _, store := range []struct{ dir, visibility string }{
		{s.personalDir, VisibilityPersonal},
		{s.sharedDir, VisibilityShared},
	} {
		if store.dir == "" {
			continue
		}
		if err := collect(store.dir, store.visibility, ov); err != nil {
			return nil, err
		}
	}
	sortRefsByID(ov.Pins)
	sortRefsByID(ov.Unresolved)
	return ov, nil
}

// collect walks one annotation store, adding pins and unresolved comments to ov.
func collect(dir, visibility string, ov *Overview) error {
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil // an absent store is empty, not an error
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		documentID := filepath.ToSlash(strings.TrimSuffix(rel, ".json"))

		data, err := os.ReadFile(p)
		if err != nil {
			return nil // a sidecar that cannot be read is skipped, never fatal
		}
		var sc Sidecar
		if err := json.Unmarshal(data, &sc); err != nil {
			return nil
		}
		for _, a := range sc.Annotations {
			ref := Ref{
				ID: a.ID, DocumentID: documentID, Visibility: visibility,
				Kind: a.Kind, Status: a.Status, Body: a.Body, Line: a.Anchor.StartLine,
			}
			if a.Kind == KindPin {
				ov.Pins = append(ov.Pins, ref)
			}
			if a.Status == StatusOpen && a.Kind == KindComment {
				ov.Unresolved = append(ov.Unresolved, ref)
			}
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// sortRefsByID orders refs chronologically by their sortable ULID.
func sortRefsByID(refs []Ref) {
	slices.SortFunc(refs, func(a, b Ref) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
}

// ListResult is every annotation for one document, from both sidecars, with
// each anchor's live state resolved.
type ListResult struct {
	DocumentID       string       `json:"document_id"`
	PersonalRevision int          `json:"personal_revision"`
	SharedRevision   int          `json:"shared_revision"`
	Annotations      []Annotation `json:"annotations"`
}

// List returns the merged, repair-resolved annotations for a document. A
// missing sidecar is an empty one at revision 0, never an error. A document
// that cannot be read still returns its annotations, all detached, so context
// is preserved rather than lost (R8).
func (s *Service) List(documentID string) (*ListResult, error) {
	// A store that is off this session contributes no annotations rather than
	// failing the whole read: a shared-only workspace still lists its shared
	// annotations, and vice versa.
	personal, err := s.readForList(VisibilityPersonal, documentID)
	if err != nil {
		return nil, err
	}
	shared, err := s.readForList(VisibilityShared, documentID)
	if err != nil {
		return nil, err
	}

	content, headings, docErr := s.source(documentID)

	merged := make([]Annotation, 0, len(personal.Annotations)+len(shared.Annotations))
	merged = append(merged, personal.Annotations...)
	merged = append(merged, shared.Annotations...)
	for i := range merged {
		resolve(&merged[i], content, headings, docErr != nil)
	}
	sortByID(merged)

	return &ListResult{
		DocumentID:       documentID,
		PersonalRevision: personal.Revision,
		SharedRevision:   shared.Revision,
		Annotations:      merged,
	}, nil
}

// CreateRequest creates a new annotation.
type CreateRequest struct {
	DocumentID       string
	Kind             string
	Visibility       string
	Status           string
	Body             string
	Anchor           Anchor
	ExpectedRevision int
}

// Create appends an annotation to the sidecar for its visibility and returns it
// with its anchor resolved, plus the sidecar's new revision.
func (s *Service) Create(req CreateRequest) (*Annotation, int, error) {
	if req.Status == "" {
		req.Status = StatusOpen
	}
	if err := validateNew(req.Kind, req.Visibility, req.Status, req.Body, req.Anchor); err != nil {
		return nil, 0, err
	}

	// A document must be readable to anchor against it: this is where the
	// source hash for a text anchor comes from, and it confirms the target
	// exists before we write context about it.
	content, headings, err := s.source(req.DocumentID)
	if err != nil {
		return nil, 0, &SourceError{Err: err}
	}

	sc, err := s.read(req.Visibility, req.DocumentID)
	if err != nil {
		return nil, 0, err
	}
	if req.ExpectedRevision != sc.Revision {
		return nil, 0, s.conflict(req.Visibility, sc)
	}

	now := s.now().UTC().Format(time.RFC3339)
	ann := Annotation{
		ID:         newID(),
		Kind:       req.Kind,
		Visibility: req.Visibility,
		Status:     req.Status,
		Body:       req.Body,
		CreatedAt:  now,
		UpdatedAt:  now,
		Anchor:     req.Anchor,
	}
	if ann.Anchor.Type == AnchorText {
		ann.Anchor.SourceHash = sourceHash(content)
	}

	sc.Annotations = append(sc.Annotations, ann)
	sc.Revision++
	if err := s.write(req.Visibility, sc); err != nil {
		return nil, 0, err
	}

	resolve(&ann, content, headings, false)
	return &ann, sc.Revision, nil
}

// UpdateRequest changes an existing annotation's body and/or status. Visibility
// is fixed at creation in this version (moving between personal and shared is a
// later slice), so the addressed sidecar is unambiguous.
type UpdateRequest struct {
	DocumentID       string
	Visibility       string
	ID               string
	Body             *string
	Status           *string
	ExpectedRevision int
}

// Update modifies an annotation in place and returns it resolved.
func (s *Service) Update(req UpdateRequest) (*Annotation, int, error) {
	if !validVisibility(req.Visibility) {
		return nil, 0, invalid("visibility", "unknown visibility")
	}
	sc, err := s.read(req.Visibility, req.DocumentID)
	if err != nil {
		return nil, 0, err
	}
	if req.ExpectedRevision != sc.Revision {
		return nil, 0, s.conflict(req.Visibility, sc)
	}

	idx := indexOf(sc.Annotations, req.ID)
	if idx < 0 {
		return nil, 0, &NotFoundError{ID: req.ID}
	}
	ann := &sc.Annotations[idx]
	if req.Status != nil {
		if *req.Status != StatusOpen && *req.Status != StatusResolved {
			return nil, 0, invalid("status", "unknown status")
		}
		ann.Status = *req.Status
	}
	if req.Body != nil {
		if ann.Kind == KindComment && *req.Body == "" {
			return nil, 0, invalid("body", "a comment needs a body")
		}
		ann.Body = *req.Body
	}
	ann.UpdatedAt = s.now().UTC().Format(time.RFC3339)

	sc.Revision++
	if err := s.write(req.Visibility, sc); err != nil {
		return nil, 0, err
	}

	updated := sc.Annotations[idx]
	content, headings, docErr := s.source(req.DocumentID)
	resolve(&updated, content, headings, docErr != nil)
	return &updated, sc.Revision, nil
}

// DeleteRequest removes an annotation.
type DeleteRequest struct {
	DocumentID       string
	Visibility       string
	ID               string
	ExpectedRevision int
}

// Delete removes an annotation and returns the sidecar's new revision.
func (s *Service) Delete(req DeleteRequest) (int, error) {
	if !validVisibility(req.Visibility) {
		return 0, invalid("visibility", "unknown visibility")
	}
	sc, err := s.read(req.Visibility, req.DocumentID)
	if err != nil {
		return 0, err
	}
	if req.ExpectedRevision != sc.Revision {
		return 0, s.conflict(req.Visibility, sc)
	}
	idx := indexOf(sc.Annotations, req.ID)
	if idx < 0 {
		return 0, &NotFoundError{ID: req.ID}
	}
	sc.Annotations = append(sc.Annotations[:idx], sc.Annotations[idx+1:]...)
	sc.Revision++
	if err := s.write(req.Visibility, sc); err != nil {
		return 0, err
	}
	return sc.Revision, nil
}

// resolve fills in an annotation's live anchor state. A document that could not
// be read detaches every anchor rather than dropping the annotations.
func resolve(ann *Annotation, content string, headings []Heading, docMissing bool) {
	if docMissing {
		ann.Anchor.State = StateDetached
		ann.Anchor.StartLine, ann.Anchor.EndLine = 0, 0
		return
	}
	state, start, end := resolveAnchor(ann.Anchor, content, headings)
	ann.Anchor.State = state
	ann.Anchor.StartLine, ann.Anchor.EndLine = start, end
}

func (s *Service) source(id string) (string, []Heading, error) {
	if s.docs == nil {
		return "", nil, errors.New("no document source configured")
	}
	return s.docs.Source(id)
}

func (s *Service) conflict(visibility string, sc *Sidecar) error {
	return &ConflictError{
		Visibility:      visibility,
		CurrentRevision: sc.Revision,
		Current:         sc.Annotations,
	}
}

// readForList is read for the merge path: a store that is unavailable this
// session, or a sidecar from a newer Athenaeum, contributes nothing rather than
// failing the whole read. A newer sidecar is skipped, never rewritten, so its
// data is preserved even though this build cannot display it; a mutation would
// still be refused by read (below), which is where the data-loss guard bites.
func (s *Service) readForList(visibility, documentID string) (*Sidecar, error) {
	sc, err := s.read(visibility, documentID)
	var un *UnavailableError
	var se *SchemaError
	if errors.As(err, &un) || errors.As(err, &se) {
		return &Sidecar{SchemaVersion: SchemaVersion, DocumentID: documentID}, nil
	}
	return sc, err
}

// read loads a sidecar, returning an empty revision-0 sidecar when the file
// does not exist yet.
func (s *Service) read(visibility, documentID string) (*Sidecar, error) {
	p, err := s.path(visibility, documentID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Sidecar{SchemaVersion: SchemaVersion, DocumentID: documentID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read annotation sidecar: %w", err)
	}
	var sc Sidecar
	if err := json.Unmarshal(data, &sc); err != nil {
		return nil, fmt.Errorf("parse annotation sidecar %s: %w", p, err)
	}
	sc.DocumentID = documentID
	if sc.SchemaVersion == 0 {
		sc.SchemaVersion = SchemaVersion
	}
	// A sidecar from a newer Athenaeum must never be rewritten by this one, or
	// its added fields would be lost. Refuse it here, so neither a read nor a
	// write can silently downgrade it.
	if sc.SchemaVersion > SchemaVersion {
		return nil, &SchemaError{Found: sc.SchemaVersion, Known: SchemaVersion}
	}
	return &sc, nil
}

// write persists a sidecar atomically. It clears every computed anchor state
// first, so the on-disk file matches spec 03 section 3 exactly.
func (s *Service) write(visibility string, sc *Sidecar) error {
	p, err := s.path(visibility, documentIDOf(sc))
	if err != nil {
		return err
	}
	sc.SchemaVersion = SchemaVersion
	for i := range sc.Annotations {
		sc.Annotations[i].Anchor.State = ""
	}
	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode annotation sidecar: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create annotation directory: %w", err)
	}
	if err := atomicfs.Write(p, data); err != nil {
		return fmt.Errorf("write annotation sidecar: %w", err)
	}
	return nil
}

func documentIDOf(sc *Sidecar) string { return sc.DocumentID }

// path resolves the sidecar file for a visibility and document, refusing any id
// that would escape the storage root (spec 03 section 6). A personal path when
// personal storage is off is an explicit unavailable error, not a panic.
func (s *Service) path(visibility, documentID string) (string, error) {
	var root string
	switch visibility {
	case VisibilityPersonal:
		if s.personalDir == "" {
			return "", &UnavailableError{Visibility: VisibilityPersonal}
		}
		root = s.personalDir
	case VisibilityShared:
		if s.sharedDir == "" {
			return "", &UnavailableError{Visibility: VisibilityShared}
		}
		root = s.sharedDir
	default:
		return "", invalid("visibility", "unknown visibility")
	}
	return safeChild(root, documentID, ".json")
}

// safeChild joins a workspace-relative document id under root and appends a
// suffix, rejecting absolute paths and any traversal that would escape root.
func safeChild(root, rel, suffix string) (string, error) {
	if rel == "" {
		return "", invalid("document", "a document id is required")
	}
	if path.IsAbs(rel) || filepath.IsAbs(rel) {
		return "", invalid("document", "document id must be relative")
	}
	// A legitimate document id never contains a parent reference, so reject any
	// ".." rather than silently clamping it into the root (spec 03 section 6).
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", invalid("document", "document id must not contain ..")
	}
	full := filepath.Join(root, filepath.FromSlash(clean)+suffix)
	base := filepath.Clean(root)
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		return "", invalid("document", "document id escapes the storage root")
	}
	return full, nil
}

func indexOf(list []Annotation, id string) int {
	for i := range list {
		if list[i].ID == id {
			return i
		}
	}
	return -1
}
