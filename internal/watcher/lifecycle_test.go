package watcher

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"athenaeum/internal/config"
	"athenaeum/internal/workspace"
)

// watcherGoroutines counts goroutines inside the watch loop.
//
// Named frames rather than a total goroutine count: the assertion has to be
// about this watcher, not about whatever else the test binary is doing.
func watcherGoroutines() (int, string) {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}

	var (
		count   int
		matched []string
	)
	for _, stack := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(stack, "athenaeum/internal/watcher.(*Watcher).run") {
			count++
			matched = append(matched, stack)
		}
	}
	return count, strings.Join(matched, "\n\n")
}

func awaitNoWatcherGoroutines(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		count, stacks := watcherGoroutines()
		if count == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d watcher goroutine(s) still running after Close:\n\n%s", count, stacks)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestCloseStopsTheWatchLoop proves Close ends the watcher without the caller's
// context being cancelled.
//
// This is the watcher half of the switching requirement: a workspace being
// unloaded must release its watch registrations there and then, not at process
// exit.
func TestCloseStopsTheWatchLoop(t *testing.T) {
	w, dir, _ := newWatcher(t, map[string]string{"docs/seed.md": "# Seed\n"})

	// Prove it is genuinely running before asserting that it stops.
	changes, cancelSub := w.Subscribe()
	writeFile(t, filepath.Join(dir, "docs", "started.md"), "# Started\n")
	await(t, changes, "the watcher should report a new document")
	cancelSub()

	if count, _ := watcherGoroutines(); count == 0 {
		t.Fatal("expected a running watch loop before Close")
	}

	done := make(chan error, 1)
	go func() { done <- w.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return; the watch loop is still running")
	}

	awaitNoWatcherGoroutines(t)
}

// TestCloseEndsSubscriptions proves a subscriber learns the watcher has gone.
//
// A subscriber left blocked on a channel nobody will ever send to is a leak in
// the consumer rather than the watcher, and just as fatal to a switch.
func TestCloseEndsSubscriptions(t *testing.T) {
	w, _, _ := newWatcher(t, map[string]string{"docs/seed.md": "# Seed\n"})

	changes, cancelSub := w.Subscribe()
	defer cancelSub()

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case _, open := <-changes:
		if open {
			t.Fatal("expected the subscription channel to be closed, not to deliver a batch")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close left a subscriber blocked on a channel that will never deliver")
	}
}

// TestSubscribeAfterCloseDoesNotBlock covers a real leak on the switching path.
//
// Subscription used to register a channel regardless of shutdown state. After
// Close the watch loop is gone, so nothing would ever broadcast to that channel
// and nothing would ever close it: a subscriber — the /events stream is one —
// that arrived during teardown blocked forever, holding the unloaded
// workspace's watcher alive. Verified to fail without the guard in add().
func TestSubscribeAfterCloseDoesNotBlock(t *testing.T) {
	w, _, _ := newWatcher(t, map[string]string{"docs/seed.md": "# Seed\n"})

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	changes, cancelSub := w.Subscribe()
	defer cancelSub()

	select {
	case _, open := <-changes:
		if open {
			t.Fatal("a batch was delivered by a closed watcher")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a subscriber registered after Close blocks forever")
	}
}

// TestCloseDuringDebounceDeliversNothing shuts the watcher down with a change
// still inside its debounce window.
//
// A workspace switched while a file is being written must not deliver that
// file's change to the next workspace's listeners, and the pending timer must
// not keep anything alive.
func TestCloseDuringDebounceDeliversNothing(t *testing.T) {
	w, dir, _ := newWatcher(t, map[string]string{"docs/seed.md": "# Seed\n"})

	changes, cancelSub := w.Subscribe()
	defer cancelSub()

	// Write, then wait for the event to have been absorbed into a debounce
	// window that has not yet closed. Merely writing and closing immediately
	// proves nothing: fsnotify may not have delivered the event at all, and the
	// test would pass without ever creating the pending timer it is about.
	writeFile(t, filepath.Join(dir, "docs", "racing.md"), "# Racing\n")
	awaitPendingDebounce(t, w)

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Outlive the debounce window, so a timer that survived teardown has every
	// chance to fire before the assertions below.
	time.Sleep(4 * debounce)

	select {
	case _, open := <-changes:
		if open {
			t.Fatal("a batch was delivered after Close")
		}
	default:
		t.Fatal("expected the subscription to have been closed")
	}

	awaitNoWatcherGoroutines(t)
}

// TestCloseIsIdempotent proves a second Close is safe, as it is for the search
// service: shutdown paths overlap when a workspace is swapped.
func TestCloseIsIdempotent(t *testing.T) {
	w, _, _ := newWatcher(t, map[string]string{"docs/seed.md": "# Seed\n"})

	if err := w.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	awaitNoWatcherGoroutines(t)
}

// TestCloseWithoutStart proves an unstarted watcher still releases its handle.
func TestCloseWithoutStart(t *testing.T) {
	w := unstartedWatcher(t)

	done := make(chan error, 1)
	go func() { done <- w.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung on a watcher that was never started")
	}
}

// TestStartAfterCloseDoesNothing proves a closed watcher cannot be restarted
// into a goroutine no cancel function reaches.
func TestStartAfterCloseDoesNothing(t *testing.T) {
	w := unstartedWatcher(t)

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	awaitNoWatcherGoroutines(t)
}

// awaitPendingDebounce blocks until the watcher holds an unfired debounce
// timer, so a test about shutting down mid-window really is in one.
func awaitPendingDebounce(t *testing.T, w *Watcher) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		w.mu.Lock()
		pending := w.timer != nil
		w.mu.Unlock()
		if pending {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no debounce timer became pending; the test cannot exercise shutdown mid-window")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// unstartedWatcher builds a watcher over a temporary workspace and deliberately
// does not start it.
func unstartedWatcher(t *testing.T) *Watcher {
	t.Helper()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docs", "seed.md"), "# Seed\n")

	configPath := filepath.Join(dir, config.DefaultFileName)
	writeFile(t, configPath, "schema_version = 1\nname = \"Fixture\"\ninclude = [\"**/*.md\"]\n")

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	ws, err := workspace.Open(cfg)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}

	w, err := New(ws, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("watcher.New: %v", err)
	}
	return w
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
