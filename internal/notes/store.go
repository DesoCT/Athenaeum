package notes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"athenaeum/internal/atomicfs"
	"athenaeum/internal/ulid"
)

// Service reads and writes note files for one workspace.
type Service struct {
	personalDir string // "" when personal storage is unavailable this session
	sharedDir   string
	now         func() time.Time
}

// Options configures a Service.
type Options struct {
	// PersonalDir is the user-data notes root (session Dirs.Data joined with
	// "notes"). Empty disables personal notes for this session.
	PersonalDir string
	// SharedDir is the workspace's committable notes root, normally
	// <root>/.athenaeum/shared/notes.
	SharedDir string
}

// NewService binds a Service to a workspace's storage roots.
func NewService(opts Options) *Service {
	return &Service{personalDir: opts.PersonalDir, sharedDir: opts.SharedDir, now: time.Now}
}

// NotFoundError reports a note id that does not exist.
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string { return "note not found: " + e.ID }

// ConflictError reports a stale-version write (spec 02 section 5).
type ConflictError struct {
	ID      string
	Current *Note
}

func (e *ConflictError) Error() string { return "note changed on disk: " + e.ID }

// UnavailableError reports storage that is off this session.
type UnavailableError struct{ Visibility string }

func (e *UnavailableError) Error() string {
	return e.Visibility + " notes are unavailable in this session"
}

// List returns summaries of every note in both stores, newest first. A store
// that is off this session simply contributes nothing.
func (s *Service) List() ([]Summary, error) {
	var out []Summary
	for _, visibility := range []string{VisibilityPersonal, VisibilityShared} {
		dir, err := s.dir(visibility)
		if err != nil {
			continue // unavailable store
		}
		notes, err := readDir(dir, visibility)
		if err != nil {
			return nil, err
		}
		for _, n := range notes {
			out = append(out, Summary{
				ID:         n.ID,
				Title:      n.Title,
				Visibility: n.Visibility,
				UpdatedAt:  n.UpdatedAt,
				Links:      n.Links,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// Read returns one note in full.
func (s *Service) Read(visibility, id string) (*Note, error) {
	p, err := s.path(visibility, id)
	if err != nil {
		return nil, err
	}
	return readFile(p, id, visibility)
}

// CreateRequest creates a note.
type CreateRequest struct {
	Title      string
	Visibility string
	Body       string
	Links      []Link
}

// Create writes a new note and returns it.
func (s *Service) Create(req CreateRequest) (*Note, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, invalid("title", "a note needs a title")
	}
	if !validVisibility(req.Visibility) {
		return nil, invalid("visibility", "unknown visibility")
	}
	now := s.now().UTC().Format(time.RFC3339)
	n := &Note{
		ID:         ulid.New(),
		Title:      req.Title,
		Visibility: req.Visibility,
		CreatedAt:  now,
		UpdatedAt:  now,
		Links:      req.Links,
		Body:       req.Body,
	}
	if err := s.write(n); err != nil {
		return nil, err
	}
	return s.Read(req.Visibility, n.ID)
}

// UpdateRequest changes a note. Visibility is fixed at creation, so it names the
// store rather than moving the note.
type UpdateRequest struct {
	ID              string
	Visibility      string
	Title           *string
	Body            *string
	Links           *[]Link
	ExpectedVersion string
}

// Update rewrites a note after a version check and returns it.
func (s *Service) Update(req UpdateRequest) (*Note, error) {
	current, err := s.Read(req.Visibility, req.ID)
	if err != nil {
		return nil, err
	}
	if req.ExpectedVersion != "" && req.ExpectedVersion != current.Version {
		return nil, &ConflictError{ID: req.ID, Current: current}
	}
	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			return nil, invalid("title", "a note needs a title")
		}
		current.Title = *req.Title
	}
	if req.Body != nil {
		current.Body = *req.Body
	}
	if req.Links != nil {
		current.Links = *req.Links
	}
	current.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	if err := s.write(current); err != nil {
		return nil, err
	}
	return s.Read(req.Visibility, req.ID)
}

// Delete removes a note.
func (s *Service) Delete(visibility, id string) error {
	p, err := s.path(visibility, id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &NotFoundError{ID: id}
		}
		return fmt.Errorf("delete note: %w", err)
	}
	return nil
}

// write serialises and atomically persists a note (spec 03 section 8).
func (s *Service) write(n *Note) error {
	p, err := s.path(n.Visibility, n.ID)
	if err != nil {
		return err
	}
	data, err := encode(n)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create notes directory: %w", err)
	}
	if err := atomicfs.Write(p, data); err != nil {
		return fmt.Errorf("write note: %w", err)
	}
	return nil
}

func (s *Service) dir(visibility string) (string, error) {
	switch visibility {
	case VisibilityPersonal:
		if s.personalDir == "" {
			return "", &UnavailableError{Visibility: VisibilityPersonal}
		}
		return s.personalDir, nil
	case VisibilityShared:
		if s.sharedDir == "" {
			return "", &UnavailableError{Visibility: VisibilityShared}
		}
		return s.sharedDir, nil
	default:
		return "", invalid("visibility", "unknown visibility")
	}
}

// path resolves a note file, rejecting any id that is not a bare identifier so
// nothing can escape the notes directory (spec 03 section 6).
func (s *Service) path(visibility, id string) (string, error) {
	dir, err := s.dir(visibility)
	if err != nil {
		return "", err
	}
	if !safeID(id) {
		return "", invalid("id", "a note id must be a bare identifier")
	}
	return filepath.Join(dir, id+".md"), nil
}

// safeID accepts only the character set a generated ulid uses, so an id can
// never carry a separator or a parent reference.
func safeID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		ok := (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

// readDir parses every note file in a directory. A missing directory is an
// empty store, not an error.
func readDir(dir, visibility string) ([]*Note, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read notes directory: %w", err)
	}
	var out []*Note
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".md")
		n, err := readFile(filepath.Join(dir, entry.Name()), id, visibility)
		if err != nil {
			// A single corrupt file must not hide every other note.
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func readFile(path, id, visibility string) (*Note, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, &NotFoundError{ID: id}
	}
	if err != nil {
		return nil, fmt.Errorf("read note: %w", err)
	}
	n, err := decode(data)
	if err != nil {
		return nil, err
	}
	// The filename is the authority on id and the directory on visibility, so a
	// hand-edited front matter cannot misfile a note.
	n.ID = id
	n.Visibility = visibility
	return n, nil
}
