package scale

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"athenaeum/internal/config"
	"athenaeum/internal/documents"
	"athenaeum/internal/search"
	"athenaeum/internal/workspace"
)

// The scale measurements are opt-in: generating and indexing a corpus of this
// size takes far longer than a unit-test run should, so `go test ./...` skips
// them and `make test-scale` runs them.
//
// ATHENAEUM_SCALE_DIR should be on a real filesystem. Pointing it at a tmpfs
// measures RAM, not the disk a user's workspace actually lives on.
func scaleConfig(t *testing.T) (dir string, docs int, target int64) {
	t.Helper()
	if os.Getenv("ATHENAEUM_SCALE") == "" {
		t.Skip("ATHENAEUM_SCALE is not set; run `make test-scale`")
	}

	dir = os.Getenv("ATHENAEUM_SCALE_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "athenaeum-scale")
	}

	docs = 5000
	if raw := os.Getenv("ATHENAEUM_SCALE_DOCS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("ATHENAEUM_SCALE_DOCS=%q: %v", raw, err)
		}
		docs = parsed
	}

	// Requirement N3's ceiling is 2 GB of included source content.
	target = 2 << 30
	if raw := os.Getenv("ATHENAEUM_SCALE_BYTES"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			t.Fatalf("ATHENAEUM_SCALE_BYTES=%q: %v", raw, err)
		}
		target = parsed
	}
	return dir, docs, target
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestScale measures N1, N2, and N3 against a generated corpus.
//
// It reports numbers rather than asserting tight thresholds on most of them:
// a wall-clock assertion is worth little on unknown hardware, and a measurement
// recorded honestly is worth a great deal. The two hard assertions are the ones
// the requirements state unambiguously — the corpus really is at the N3 scale,
// and indexing never blocks a query (N2).
func TestScale(t *testing.T) {
	dir, wantDocs, targetBytes := scaleConfig(t)

	generateStart := time.Now()
	corpus, err := Generate(GenerateOptions{
		Root: dir, Documents: wantDocs, TargetBytes: targetBytes, Seed: 20260720,
	})
	if err != nil {
		t.Fatalf("generate corpus: %v", err)
	}
	t.Logf("N3 corpus: %d documents, %.2f GB on disk, prepared in %s",
		corpus.Documents, float64(corpus.Bytes)/(1<<30), time.Since(generateStart).Round(time.Millisecond))

	if corpus.Documents < 5000 && wantDocs >= 5000 {
		t.Fatalf("corpus has %d documents, below the N3 target of 5,000", corpus.Documents)
	}

	cfg, err := config.Load(filepath.Join(dir, config.DefaultFileName))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	enumerateStart := time.Now()
	ws, err := workspace.Open(cfg)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	enumerate := time.Since(enumerateStart)
	t.Logf("workspace enumeration: %d documents in %s", ws.Count(), enumerate.Round(time.Millisecond))

	cache := filepath.Join(dir, ".cache")
	// The cache lives beside the fixture only so the measurement is repeatable
	// and self-cleaning; the product always uses the OS cache directory.
	t.Cleanup(func() { _ = os.RemoveAll(cache) })

	docs := documents.New(ws)
	view := documents.IndexOptions{
		IncludeCodeBlocks:  cfg.Search.IndexCodeBlocks,
		IncludeFrontMatter: cfg.Search.IndexFrontMatter,
	}

	// --- Cold build: an empty cache indexes the whole corpus ----------------

	_ = os.RemoveAll(cache)
	coldStart := time.Now()
	cold := start(t, cache, cfg, ws, docs, view)
	coldReady := time.Since(coldStart)

	waitSettled(t, cold, corpus.Documents, 30*time.Minute)
	coldBuild := time.Since(coldStart)
	t.Logf("N1 cold: service ready in %s; full index built in %s (%.0f docs/s)",
		coldReady.Round(time.Millisecond), coldBuild.Round(time.Millisecond),
		float64(corpus.Documents)/coldBuild.Seconds())

	indexSize, _ := fileSize(filepath.Join(cache, search.IndexFileName))
	t.Logf("index size: %.2f GB (%.0f%% of corpus)",
		float64(indexSize)/(1<<30), 100*float64(indexSize)/float64(corpus.Bytes))

	// --- Query latency on a settled index ----------------------------------

	measureQueries(t, cold, corpus.Documents)
	_ = cold.Close()

	// --- Warm start: an unchanged corpus must not be re-read ----------------

	warmStart := time.Now()
	warm := start(t, cache, cfg, ws, docs, view)
	warmReady := time.Since(warmStart)
	waitSettled(t, warm, corpus.Documents, 5*time.Minute)
	warmSettle := time.Since(warmStart)
	t.Logf("N1 warm: service ready in %s; index confirmed current in %s",
		warmReady.Round(time.Millisecond), warmSettle.Round(time.Millisecond))

	if warmReady > 2*time.Second {
		t.Errorf("N1: the search service took %s to become ready on a warm cache, above the two-second target",
			warmReady.Round(time.Millisecond))
	}
	_ = warm.Close()

	// --- N2: queries stay responsive while the index rebuilds --------------

	rebuilding := start(t, cache, cfg, ws, docs, view)
	defer rebuilding.Close()
	rebuilding.Rebuild()

	// Sample while the projection is demonstrably busy.
	busy := false
	deadline := time.Now().Add(30 * time.Second)
	var during []time.Duration
	for time.Now().Before(deadline) {
		state := rebuilding.Status().State
		if state == search.StateRebuilding || state == search.StateBuilding {
			busy = true
		}
		if busy && state == search.StateReady {
			break
		}
		if !busy {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		started := time.Now()
		if _, err := rebuilding.Search(search.Request{Query: "workspace document", Limit: 20}); err != nil {
			t.Fatalf("search during rebuild: %v", err)
		}
		during = append(during, time.Since(started))
	}

	if !busy {
		t.Log("N2: the rebuild completed too quickly to sample; treat the reading below as a floor")
	}
	if len(during) > 0 {
		slices.Sort(during)
		t.Logf("N2: %d queries served while indexing — p50 %s, p95 %s, max %s",
			len(during), during[len(during)/2].Round(time.Microsecond),
			during[len(during)*95/100].Round(time.Microsecond),
			during[len(during)-1].Round(time.Microsecond))

		// The hard part of N2: indexing must not block the query path. A
		// single-writer database that serialised readers behind the writer
		// would show up here as seconds, not milliseconds.
		if worst := during[len(during)-1]; worst > 2*time.Second {
			t.Errorf("N2: a query took %s while indexing; the UI would not stay responsive", worst)
		}
	}
}

// start builds and starts a search service, cancelling it when the test ends.
func start(
	t *testing.T,
	cache string,
	cfg *config.Config,
	ws *workspace.Workspace,
	docs *documents.Service,
	view documents.IndexOptions,
) *search.Service {
	t.Helper()

	index, err := search.Open(cache, search.ProjectionKey(cfg))
	if err != nil {
		t.Fatalf("search.Open: %v", err)
	}
	service := search.NewService(search.Options{
		Index: index, Workspace: ws, Documents: docs, Logger: quiet(), View: view,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	service.Start(ctx)
	return service
}

func waitSettled(t *testing.T, service *search.Service, want int, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		status := service.Status()
		if status.State == search.StateReady && status.Indexed >= want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the index did not settle at %d documents within %s; status = %+v",
		want, limit, service.Status())
}

// measureQueries reports latency for query shapes with very different result
// set sizes, because one number for "a search" would hide the difference.
func measureQueries(t *testing.T, service *search.Service, documents int) {
	t.Helper()

	cases := []struct {
		name  string
		query string
	}{
		{"single common term", "workspace"},
		{"two common terms", "workspace document"},
		{"rare exact token", fmt.Sprintf("zqx%05d", documents/2)},
		{"phrase", `"bounded worker pool"`},
		{"prefix while typing", "concur"},
	}

	for _, test := range cases {
		var samples []time.Duration
		var results int
		for range 20 {
			started := time.Now()
			response, err := service.Search(search.Request{Query: test.query, Limit: 25})
			if err != nil {
				t.Fatalf("search %q: %v", test.query, err)
			}
			samples = append(samples, time.Since(started))
			results = len(response.Results)
		}
		slices.Sort(samples)
		t.Logf("query %-22s p50 %-10s p95 %-10s (%d results)",
			test.name,
			samples[len(samples)/2].Round(time.Microsecond),
			samples[len(samples)*95/100].Round(time.Microsecond),
			results)
	}
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
