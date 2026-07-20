package watcher

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"athenaeum/internal/config"
	"athenaeum/internal/workspace"
)

func newWatcher(t *testing.T, files map[string]string) (*Watcher, string, context.CancelFunc) {
	t.Helper()
	dir := t.TempDir()

	for rel, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	configPath := filepath.Join(dir, config.DefaultFileName)
	if err := os.WriteFile(configPath, []byte("schema_version = 1\nname = \"Fixture\"\ninclude = [\"**/*.md\"]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	ws, err := workspace.Open(cfg)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w, err := New(ws, log)
	if err != nil {
		t.Fatalf("watcher.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go w.Run(ctx)
	t.Cleanup(cancel)

	return w, dir, cancel
}

// await waits for a change batch, failing the test on timeout.
func await(t *testing.T, changes <-chan []Change, why string) []Change {
	t.Helper()
	select {
	case batch, ok := <-changes:
		if !ok {
			t.Fatalf("%s: the channel closed", why)
		}
		return batch
	case <-time.After(4 * time.Second):
		t.Fatalf("%s: no change observed within 4s", why)
		return nil
	}
}

// TestExternalWriteIsReported is the signal acceptance E1 depends on.
func TestExternalWriteIsReported(t *testing.T) {
	w, dir, _ := newWatcher(t, map[string]string{"a.md": "# One\n"})
	changes, cancel := w.Subscribe()
	defer cancel()

	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Changed externally\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	batch := await(t, changes, "external write")
	var found bool
	for _, change := range batch {
		if change.DocumentID == "a.md" && change.Kind == KindModified {
			found = true
			if change.Version == "" {
				t.Error("no version reported, so the client cannot tell whether it is stale")
			}
		}
	}
	if !found {
		t.Fatalf("a.md was not reported: %+v", batch)
	}
}

// TestSelfWriteIsNotReported covers the classification rule in spec 02
// section 3.4: Athenaeum's own saves must not come back as external changes.
func TestSelfWriteIsNotReported(t *testing.T) {
	w, dir, _ := newWatcher(t, map[string]string{"a.md": "# One\n"})
	changes, cancel := w.Subscribe()
	defer cancel()

	content := []byte("# Written by Athenaeum\n")
	// The application records the fingerprint it is about to write.
	w.NoteSelfWrite("a.md", fingerprintOf(content))
	if err := os.WriteFile(filepath.Join(dir, "a.md"), content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case batch := <-changes:
		for _, change := range batch {
			if change.DocumentID == "a.md" {
				t.Fatalf("a self-write was reported as an external change: %+v", change)
			}
		}
	case <-time.After(1500 * time.Millisecond):
		// Nothing reported, which is the expected outcome.
	}
}

// TestSelfWriteFollowedByExternalIsStillReported guards against the self-write
// record swallowing a genuine later change.
func TestSelfWriteFollowedByExternalIsStillReported(t *testing.T) {
	w, dir, _ := newWatcher(t, map[string]string{"a.md": "# One\n"})
	changes, cancel := w.Subscribe()
	defer cancel()

	ours := []byte("# Ours\n")
	w.NoteSelfWrite("a.md", fingerprintOf(ours))
	if err := os.WriteFile(filepath.Join(dir, "a.md"), ours, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(600 * time.Millisecond)

	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Theirs\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	batch := await(t, changes, "external write after a self-write")
	var found bool
	for _, change := range batch {
		if change.DocumentID == "a.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the later external change was swallowed: %+v", batch)
	}
}

func TestRemovalIsReported(t *testing.T) {
	w, dir, _ := newWatcher(t, map[string]string{"a.md": "# One\n", "b.md": "# Two\n"})
	changes, cancel := w.Subscribe()
	defer cancel()

	if err := os.Remove(filepath.Join(dir, "a.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	batch := await(t, changes, "removal")
	for _, change := range batch {
		if change.DocumentID == "a.md" {
			if change.Kind != KindRemoved {
				t.Errorf("kind = %q, want %q", change.Kind, KindRemoved)
			}
			return
		}
	}
	t.Fatalf("the removal was not reported: %+v", batch)
}

// TestBurstIsCoalesced keeps a save storm from flooding the client.
func TestBurstIsCoalesced(t *testing.T) {
	w, dir, _ := newWatcher(t, map[string]string{"a.md": "# One\n"})
	changes, cancel := w.Subscribe()
	defer cancel()

	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Edit\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	batch := await(t, changes, "burst")
	count := 0
	for _, change := range batch {
		if change.DocumentID == "a.md" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("a.md appeared %d times in one batch, want 1", count)
	}
}

func TestMultipleSubscribersEachReceive(t *testing.T) {
	w, dir, _ := newWatcher(t, map[string]string{"a.md": "# One\n"})
	first, cancelFirst := w.Subscribe()
	defer cancelFirst()
	second, cancelSecond := w.Subscribe()
	defer cancelSecond()

	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Changed\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	await(t, first, "first subscriber")
	await(t, second, "second subscriber")
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	w, dir, _ := newWatcher(t, map[string]string{"a.md": "# One\n"})
	changes, cancel := w.Subscribe()
	cancel()

	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Changed\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case _, open := <-changes:
		if open {
			t.Error("a cancelled subscriber still received a batch")
		}
	case <-time.After(1200 * time.Millisecond):
		// Also acceptable: nothing arrived at all.
	}
}

// TestNewFileIsReported confirms directory-level watching notices creations.
func TestNewFileIsReported(t *testing.T) {
	w, dir, _ := newWatcher(t, map[string]string{"a.md": "# One\n"})
	changes, cancel := w.Subscribe()
	defer cancel()

	if err := os.WriteFile(filepath.Join(dir, "new.md"), []byte("# New\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	batch := await(t, changes, "new file")
	for _, change := range batch {
		if change.DocumentID == "new.md" {
			return
		}
	}
	t.Fatalf("the new file was not reported: %+v", batch)
}

// fingerprintOf mirrors the document service's hashing for test setup.
func fingerprintOf(content []byte) string {
	return hashBytes(content)
}

// TestCreateIsNotDowngradedToModified is the regression test for a bug that
// stopped externally created documents appearing in the tree: creating a file
// emits Create then Write, and last-write-wins reported only "modified".
func TestCreateIsNotDowngradedToModified(t *testing.T) {
	w, dir, _ := newWatcher(t, map[string]string{"a.md": "# One\n"})
	changes, cancel := w.Subscribe()
	defer cancel()

	if err := os.WriteFile(filepath.Join(dir, "fresh.md"), []byte("# Fresh\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	batch := await(t, changes, "created file")
	for _, change := range batch {
		if change.DocumentID == "fresh.md" {
			if change.Kind != KindCreated {
				t.Fatalf("kind = %q, want %q; the tree will not refresh otherwise",
					change.Kind, KindCreated)
			}
			return
		}
	}
	t.Fatalf("fresh.md was not reported: %+v", batch)
}

func TestMergeKindPrecedence(t *testing.T) {
	tests := []struct{ existing, incoming, want string }{
		{"", KindModified, KindModified},
		{KindCreated, KindModified, KindCreated},
		{KindModified, KindCreated, KindCreated},
		{KindCreated, KindRemoved, KindRemoved},
		{KindRemoved, KindCreated, KindRemoved},
		{KindModified, KindModified, KindModified},
	}
	for _, tc := range tests {
		if got := mergeKind(tc.existing, tc.incoming); got != tc.want {
			t.Errorf("mergeKind(%q, %q) = %q, want %q", tc.existing, tc.incoming, got, tc.want)
		}
	}
}
