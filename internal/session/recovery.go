package session

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// recoverySchemaVersion guards the on-disk format.
const recoverySchemaVersion = 1

// maxRecoveryBytes bounds a single recovery buffer.
const maxRecoveryBytes = 16 << 20

// Buffer is an unsaved editor buffer preserved against an abnormal exit
// (requirement R13, acceptance E3).
type Buffer struct {
	SchemaVersion int `json:"schema_version"`
	// DocumentID is the document the buffer belongs to.
	DocumentID string `json:"document_id"`
	// Content is the unsaved text.
	Content string `json:"content"`
	// BaseVersion is the document version the buffer was edited from, so the
	// UI can tell whether the file has since changed underneath it.
	BaseVersion string `json:"base_version"`
	// UpdatedAt is when the buffer was last recorded, in UTC RFC 3339.
	UpdatedAt string `json:"updated_at"`
}

// RecoveryStore persists unsaved buffers.
//
// Spec 02 section 3.11 is explicit that stale recovery data expires only after
// a clean close or an explicit user action. Nothing here deletes a buffer on
// its own, and nothing here ever applies one: recovery is offered, and the
// user decides (acceptance E3).
type RecoveryStore struct {
	dir string
	mu  sync.Mutex
}

// NewRecoveryStore opens the store for a workspace.
func NewRecoveryStore(dirs Dirs) (*RecoveryStore, error) {
	if err := os.MkdirAll(dirs.Recovery(), 0o700); err != nil {
		return nil, fmt.Errorf("create recovery directory: %w", err)
	}
	return &RecoveryStore{dir: dirs.Recovery()}, nil
}

// fileName maps a document ID onto a flat, filename-safe name.
//
// The ID is hashed rather than escaped so that nested paths do not need
// directories and no separator can escape the recovery directory.
func fileName(documentID string) string {
	sum := sha256.Sum256([]byte(documentID))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
	return strings.ToLower(encoded[:26]) + ".json"
}

// Put records an unsaved buffer, replacing any previous one for the document.
//
// The write is atomic so an interrupted recovery write cannot leave a truncated
// buffer that would look like valid but corrupted recovery data.
func (s *RecoveryStore) Put(buffer Buffer) error {
	if buffer.DocumentID == "" {
		return errors.New("a recovery buffer needs a document ID")
	}
	if len(buffer.Content) > maxRecoveryBytes {
		return fmt.Errorf("the buffer is %d bytes, above the %d byte recovery limit",
			len(buffer.Content), maxRecoveryBytes)
	}

	buffer.SchemaVersion = recoverySchemaVersion
	buffer.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	payload, err := json.Marshal(buffer)
	if err != nil {
		return fmt.Errorf("encode recovery buffer: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	target := filepath.Join(s.dir, fileName(buffer.DocumentID))
	temp, err := os.CreateTemp(s.dir, ".recovery-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary recovery file: %w", err)
	}
	tempName := temp.Name()

	if _, err := temp.Write(payload); err != nil {
		temp.Close()
		os.Remove(tempName)
		return fmt.Errorf("write recovery buffer: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		os.Remove(tempName)
		return fmt.Errorf("flush recovery buffer: %w", err)
	}
	if err := temp.Close(); err != nil {
		os.Remove(tempName)
		return fmt.Errorf("close recovery buffer: %w", err)
	}
	if err := os.Chmod(tempName, 0o600); err != nil {
		// Not fatal; the directory is already 0700.
		_ = err
	}
	if err := os.Rename(tempName, target); err != nil {
		os.Remove(tempName)
		return fmt.Errorf("replace recovery buffer: %w", err)
	}
	return nil
}

// List returns every pending recovery buffer, newest first.
func (s *RecoveryStore) List() ([]Buffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read recovery directory: %w", err)
	}

	var buffers []Buffer
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var buffer Buffer
		if err := json.Unmarshal(raw, &buffer); err != nil {
			// A corrupted buffer is skipped rather than deleted: it is the
			// user's unsaved text, and removing it silently is exactly what
			// acceptance E3 forbids.
			continue
		}
		if buffer.SchemaVersion != recoverySchemaVersion || buffer.DocumentID == "" {
			continue
		}
		buffers = append(buffers, buffer)
	}

	sort.Slice(buffers, func(i, j int) bool {
		if buffers[i].UpdatedAt != buffers[j].UpdatedAt {
			return buffers[i].UpdatedAt > buffers[j].UpdatedAt
		}
		return buffers[i].DocumentID < buffers[j].DocumentID
	})
	return buffers, nil
}

// Discard removes one recovery buffer.
//
// This is only ever called for an explicit user action: saving the buffer,
// choosing to discard it, or closing the document cleanly.
func (s *RecoveryStore) Discard(documentID string) error {
	if documentID == "" {
		return errors.New("a document ID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(filepath.Join(s.dir, fileName(documentID)))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("discard recovery buffer: %w", err)
	}
	return nil
}

// Count returns the number of pending buffers, for the launch banner.
func (s *RecoveryStore) Count() int {
	buffers, err := s.List()
	if err != nil {
		return 0
	}
	return len(buffers)
}
