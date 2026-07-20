package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) (*RecoveryStore, string) {
	t.Helper()
	dir := t.TempDir()
	dirs := Dirs{State: dir}
	store, err := NewRecoveryStore(dirs)
	if err != nil {
		t.Fatalf("NewRecoveryStore: %v", err)
	}
	return store, dirs.Recovery()
}

func TestPutAndList(t *testing.T) {
	store, _ := newStore(t)

	if err := store.Put(Buffer{DocumentID: "docs/a.md", Content: "# Unsaved\n", BaseVersion: "sha256:aaa"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	buffers, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(buffers) != 1 {
		t.Fatalf("got %d buffers, want 1", len(buffers))
	}
	got := buffers[0]
	if got.DocumentID != "docs/a.md" || got.Content != "# Unsaved\n" {
		t.Errorf("buffer = %+v", got)
	}
	if got.BaseVersion != "sha256:aaa" {
		t.Errorf("BaseVersion = %q", got.BaseVersion)
	}
	if got.UpdatedAt == "" {
		t.Error("UpdatedAt was not stamped")
	}
}

func TestPutReplacesPreviousBuffer(t *testing.T) {
	store, dir := newStore(t)

	for _, content := range []string{"first", "second", "third"} {
		if err := store.Put(Buffer{DocumentID: "a.md", Content: content}); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	buffers, _ := store.List()
	if len(buffers) != 1 {
		t.Fatalf("got %d buffers, want 1 (each Put should replace)", len(buffers))
	}
	if buffers[0].Content != "third" {
		t.Errorf("content = %q, want third", buffers[0].Content)
	}

	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".recovery-") {
			t.Errorf("temporary file left behind: %s", entry.Name())
		}
	}
}

// TestDiscardIsExplicit covers the core of acceptance E3: recovery data is
// removed only when something explicitly asks.
func TestDiscardIsExplicit(t *testing.T) {
	store, _ := newStore(t)
	if err := store.Put(Buffer{DocumentID: "a.md", Content: "x"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Listing must not consume the buffer, however many times it is called.
	for i := 0; i < 3; i++ {
		buffers, _ := store.List()
		if len(buffers) != 1 {
			t.Fatalf("List call %d returned %d buffers; listing must not consume", i, len(buffers))
		}
	}

	if err := store.Discard("a.md"); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	buffers, _ := store.List()
	if len(buffers) != 0 {
		t.Fatalf("got %d buffers after Discard, want 0", len(buffers))
	}
}

func TestDiscardUnknownIsNotAnError(t *testing.T) {
	store, _ := newStore(t)
	if err := store.Discard("never-existed.md"); err != nil {
		t.Fatalf("Discard of an absent buffer: %v", err)
	}
}

// TestBuffersSurviveRestart is the point of the whole store.
func TestBuffersSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	dirs := Dirs{State: dir}

	first, err := NewRecoveryStore(dirs)
	if err != nil {
		t.Fatalf("NewRecoveryStore: %v", err)
	}
	if err := first.Put(Buffer{DocumentID: "docs/a.md", Content: "work in progress"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A new process opens the same directory.
	second, err := NewRecoveryStore(dirs)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	buffers, _ := second.List()
	if len(buffers) != 1 || buffers[0].Content != "work in progress" {
		t.Fatalf("the buffer did not survive a restart: %+v", buffers)
	}
}

// TestCorruptBufferIsSkippedNotDeleted keeps damaged user text on disk.
func TestCorruptBufferIsSkippedNotDeleted(t *testing.T) {
	store, dir := newStore(t)
	if err := store.Put(Buffer{DocumentID: "good.md", Content: "fine"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	buffers, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(buffers) != 1 || buffers[0].DocumentID != "good.md" {
		t.Fatalf("buffers = %+v", buffers)
	}
	if _, err := os.Stat(corrupt); err != nil {
		t.Error("the corrupt buffer was deleted; unsaved text must never be removed silently")
	}
}

func TestPutRejectsOversizedBuffer(t *testing.T) {
	store, _ := newStore(t)
	err := store.Put(Buffer{DocumentID: "a.md", Content: strings.Repeat("x", maxRecoveryBytes+1)})
	if err == nil {
		t.Fatal("an oversized buffer was accepted")
	}
}

func TestPutRejectsEmptyDocumentID(t *testing.T) {
	store, _ := newStore(t)
	if err := store.Put(Buffer{Content: "x"}); err == nil {
		t.Fatal("a buffer with no document ID was accepted")
	}
}

// TestDocumentIDNeverAppearsInFilename guards spec 03 section 2.2: a path can
// itself be sensitive, so it must not leak into the recovery directory.
func TestDocumentIDNeverAppearsInFilename(t *testing.T) {
	store, dir := newStore(t)
	const id = "docs/secret-project/plan.md"
	if err := store.Put(Buffer{DocumentID: id, Content: "x"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		for _, fragment := range []string{"secret", "plan", "docs"} {
			if strings.Contains(entry.Name(), fragment) {
				t.Errorf("filename %q leaks part of the document ID", entry.Name())
			}
		}
	}
}

// TestNestedIDsDoNotEscape confirms hashing removes any separator risk.
func TestNestedIDsDoNotEscape(t *testing.T) {
	store, dir := newStore(t)
	for _, id := range []string{"a/b/c/deep.md", "../escape.md", "with space.md"} {
		if err := store.Put(Buffer{DocumentID: id, Content: "x"}); err != nil {
			t.Fatalf("Put(%q): %v", id, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d files, want 3 flat files", len(entries))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("a directory was created: %s", entry.Name())
		}
	}
}

func TestWorkspaceKeyIsStableAndOpaque(t *testing.T) {
	a := NewWorkspaceKey("/home/matt/dev/notes", "")
	b := NewWorkspaceKey("/home/matt/dev/notes", "")
	c := NewWorkspaceKey("/home/matt/dev/other", "")

	if a != b {
		t.Error("the same root produced different keys")
	}
	if a == c {
		t.Error("different roots produced the same key")
	}
	if strings.Contains(string(a), "matt") || strings.Contains(string(a), "notes") {
		t.Errorf("the key leaks the root path: %s", a)
	}
}

// TestWorkspaceUUIDSurvivesAMove addresses the instability raised in review:
// a path-derived key orphans personal state when a workspace moves.
func TestWorkspaceUUIDSurvivesAMove(t *testing.T) {
	before := NewWorkspaceKey("/old/location", "9f1c-workspace-uuid")
	after := NewWorkspaceKey("/a/completely/different/place", "9f1c-workspace-uuid")

	if before != after {
		t.Error("a configured workspace UUID did not survive a move")
	}
}
