package search

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"athenaeum/internal/config"
	"athenaeum/internal/documents"
	"athenaeum/internal/watcher"
	"athenaeum/internal/workspace"
)

// serviceFrames names every long-lived goroutine the service starts.
//
// Scanning for these by name keeps the assertion specific. A bare
// runtime.NumGoroutine() comparison would be both flaky — other packages'
// goroutines come and go — and too weak, because it could be satisfied by a
// leaked indexing goroutine exiting while an unrelated one started.
var serviceFrames = []string{
	"athenaeum/internal/search.(*Service).coordinate",
	"athenaeum/internal/search.(*Service).follow",
	"athenaeum/internal/search.(*Service).diff",
	"athenaeum/internal/search.(*Service).drain",
	"athenaeum/internal/search.(*Service).process",
}

// serviceGoroutines counts goroutines currently inside a service loop.
func serviceGoroutines() (int, string) {
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
		for _, frame := range serviceFrames {
			if strings.Contains(stack, frame) {
				count++
				matched = append(matched, stack)
				break
			}
		}
	}
	return count, strings.Join(matched, "\n\n")
}

// awaitNoServiceGoroutines polls until nothing is left running, or gives up.
//
// Polling rather than asserting once tolerates the microseconds between a
// goroutine's final statement and the scheduler recording its exit; it does not
// tolerate a goroutine that is genuinely still running, because the deadline is
// far longer than any legitimate wind-down.
func awaitNoServiceGoroutines(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		count, stacks := serviceGoroutines()
		if count == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d search service goroutine(s) still running after Close:\n\n%s", count, stacks)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// lifecycleFixture builds a service with a watcher attached, without the
// automatic cancellation the shared fixture installs.
//
// The distinction is the whole point of these tests: the caller's context must
// stay alive, because that is precisely the situation a workspace switch
// creates — one workspace stops while the process carries on.
func lifecycleFixture(t *testing.T) (*Service, *watcher.Watcher) {
	t.Helper()

	root := t.TempDir()
	cache := t.TempDir()
	writeFiles(t, root, map[string]string{
		"docs/one.md": "# One\n\nalpha beta gamma\n",
		"docs/two.md": "# Two\n\ndelta epsilon\n",
	})

	configPath := filepath.Join(root, config.DefaultFileName)
	if err := os.WriteFile(configPath, []byte(defaultConfig), 0o644); err != nil {
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

	changeWatcher, err := watcher.New(ws, testLogger())
	if err != nil {
		t.Fatalf("watcher.New: %v", err)
	}

	index, err := Open(cache, ProjectionKey(cfg))
	if err != nil {
		t.Fatalf("search.Open: %v", err)
	}
	service := NewService(Options{
		Index:     index,
		Workspace: ws,
		Documents: documents.New(ws),
		Watcher:   changeWatcher,
		Logger:    testLogger(),
		View: documents.IndexOptions{
			IncludeCodeBlocks:  cfg.Search.IndexCodeBlocks,
			IncludeFrontMatter: cfg.Search.IndexFrontMatter,
		},
	})
	return service, changeWatcher
}

// TestCloseStopsBackgroundGoroutines is the regression test for the leak that
// made a workspace switch impossible.
//
// Start used to bind its goroutines to the caller's context and Close only shut
// the index, so nothing ever cancelled them. That went unnoticed because the
// only context in production was the process's own, cancelled at exit. The
// deliberate detail here is that the caller's context is never cancelled: a
// test that passed only because the process ended would prove nothing.
func TestCloseStopsBackgroundGoroutines(t *testing.T) {
	service, changeWatcher := lifecycleFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcherCtx, stopWatcher := context.WithCancel(context.Background())
	defer stopWatcher()
	changeWatcher.Start(watcherCtx)
	defer func() { _ = changeWatcher.Close() }()

	service.Start(ctx)
	waitReady(t, service, 2)

	if count, _ := serviceGoroutines(); count == 0 {
		t.Fatal("expected the service to be running goroutines before Close")
	}

	// Close must not merely return: it must have waited. A Close that returns
	// while its goroutines run on would still leak an index handle behind them.
	done := make(chan error, 1)
	go func() { done <- service.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return; the background goroutines are still running")
	}

	awaitNoServiceGoroutines(t)
}

// TestCloseIsIdempotent proves a second Close is safe.
//
// Switching workspaces closes a service on one path and a deferred shutdown may
// close it again on another. Closing the index twice would otherwise surface as
// an error the caller cannot act on.
func TestCloseIsIdempotent(t *testing.T) {
	service, _ := lifecycleFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx)
	waitReady(t, service, 2)

	if err := service.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	awaitNoServiceGoroutines(t)
}

// TestCloseWithoutStart proves a service that never started still closes.
//
// The application builds a service and only starts it once the listener is
// open, so an early failure in between must not make shutdown hang.
func TestCloseWithoutStart(t *testing.T) {
	service, _ := lifecycleFixture(t)

	done := make(chan error, 1)
	go func() { done <- service.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung on a service that was never started")
	}
}

// TestStartAfterCloseDoesNothing proves a closed service cannot be revived into
// leaking goroutines that no cancel function reaches.
func TestStartAfterCloseDoesNothing(t *testing.T) {
	service, _ := lifecycleFixture(t)

	if err := service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx)

	awaitNoServiceGoroutines(t)
}

// TestRebuildAfterCloseDoesNothing covers the other goroutine source: Rebuild
// spawns a scan of its own, and it must respect the same shutdown.
func TestRebuildAfterCloseDoesNothing(t *testing.T) {
	service, _ := lifecycleFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx)
	waitReady(t, service, 2)

	if err := service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	service.Rebuild()

	awaitNoServiceGoroutines(t)
}
