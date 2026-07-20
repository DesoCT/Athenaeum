package search

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"athenaeum/internal/config"
	"athenaeum/internal/documents"
	"athenaeum/internal/workspace"
)

// fixture builds a temporary workspace and a running search service over it.
//
// Spec 07 section 5: every filesystem test uses a temporary workspace, never
// the developer's own repository.
func fixture(t *testing.T, files map[string]string) (*Service, *workspace.Workspace, string, string) {
	t.Helper()
	return fixtureWithConfig(t, defaultConfig, files)
}

const defaultConfig = `
schema_version = 1
name = "Fixture"
root = "."
include = ["**/*.md"]
exclude = ["docs/private/**"]

[search]
enabled = true
index_code_blocks = true
index_front_matter = true

[[groups]]
id = "design"
title = "Design"
patterns = ["docs/design/**/*.md"]
`

func fixtureWithConfig(t *testing.T, configBody string, files map[string]string) (*Service, *workspace.Workspace, string, string) {
	t.Helper()

	root := t.TempDir()
	cache := t.TempDir()
	writeFiles(t, root, files)

	configPath := filepath.Join(root, config.DefaultFileName)
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
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

	service := openService(t, cache, cfg, ws)
	return service, ws, root, cache
}

func openService(t *testing.T, cache string, cfg *config.Config, ws *workspace.Workspace) *Service {
	t.Helper()

	index, err := Open(cache, ProjectionKey(cfg))
	if err != nil {
		t.Fatalf("search.Open: %v", err)
	}
	service := NewService(Options{
		Index:     index,
		Workspace: ws,
		Documents: documents.New(ws),
		Logger:    testLogger(),
		View: documents.IndexOptions{
			IncludeCodeBlocks:  cfg.Search.IndexCodeBlocks,
			IncludeFrontMatter: cfg.Search.IndexFrontMatter,
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		service.Close()
	})
	service.Start(ctx)
	return service
}

// testLogger discards output. Tests assert on behaviour, and a chatty indexer
// would drown the failure that matters.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

// waitReady blocks until the projection has caught up, so a test asserts on a
// settled index rather than racing the background indexer.
func waitReady(t *testing.T, s *Service, want int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status := s.Status()
		if status.State == StateReady && status.Indexed == want {
			return
		}
		if status.State == StateUnavailable {
			t.Fatalf("index became unavailable: %s", status.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("index did not reach %d documents; status = %+v", want, s.Status())
}

func mustSearch(t *testing.T, s *Service, req Request) Response {
	t.Helper()
	response, err := s.Search(req)
	if err != nil {
		t.Fatalf("Search(%q): %v", req.Query, err)
	}
	return response
}

func ids(response Response) []string {
	out := make([]string, 0, len(response.Results))
	for _, r := range response.Results {
		out = append(out, r.DocumentID)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

var corpus = map[string]string{
	"README.md": "# Athenaeum\n\nA local-first command centre for Markdown.\n",
	"docs/design/rendering.md": "---\ntitle: Rendering design\ntags:\n  - renderer\n  - gfm\n---\n\n" +
		"# Rendering\n\nIntro paragraph.\n\n## Sanitisation\n\n" +
		"Raw HTML is disabled by default and the renderer escapes it.\n\n" +
		"## Mermaid\n\nDiagrams render in a restricted mode.\n",
	"docs/design/concurrency.md": "# Concurrency\n\n## Worker pool\n\n" +
		"Indexing uses a bounded worker pool so the interface stays responsive.\n",
	"docs/private/secret.md": "# Secret\n\nThe passphrase is hunter2 and must never be searchable.\n",
}

// TestInitialIndex covers acceptance F1: the fixture corpus indexes path,
// title, heading, and body content with correct result locations.
func TestInitialIndex(t *testing.T) {
	service, _, _, _ := fixture(t, corpus)
	waitReady(t, service, 3) // docs/private is excluded.

	t.Run("body match reports its line and heading", func(t *testing.T) {
		response := mustSearch(t, service, Request{Query: "bounded"})
		if len(response.Results) == 0 {
			t.Fatal("no result for a word that appears in a document body")
		}
		result := response.Results[0]
		if result.DocumentID != "docs/design/concurrency.md" {
			t.Fatalf("document = %q", result.DocumentID)
		}
		if result.Line != 5 {
			t.Errorf("line = %d, want 5 (the line the phrase is on)", result.Line)
		}
		if got := strings.Join(result.HeadingPath, " > "); got != "Concurrency > Worker pool" {
			t.Errorf("heading path = %q", got)
		}
		if result.Field != FieldBody {
			t.Errorf("field = %q, want %q", result.Field, FieldBody)
		}
		if !hasHighlight(result.Snippet) {
			t.Errorf("snippet has no highlighted run: %+v", result.Snippet)
		}
	})

	t.Run("heading text is searchable", func(t *testing.T) {
		response := mustSearch(t, service, Request{Query: "sanitisation"})
		if !contains(ids(response), "docs/design/rendering.md") {
			t.Fatalf("heading search missed the document; got %v", ids(response))
		}
	})

	t.Run("title from front matter is searchable", func(t *testing.T) {
		response := mustSearch(t, service, Request{Query: "rendering design"})
		if !contains(ids(response), "docs/design/rendering.md") {
			t.Fatalf("title search missed the document; got %v", ids(response))
		}
	})

	t.Run("path is searchable", func(t *testing.T) {
		response := mustSearch(t, service, Request{Query: "concurrency"})
		if !contains(ids(response), "docs/design/concurrency.md") {
			t.Fatalf("path search missed the document; got %v", ids(response))
		}
	})

	t.Run("front matter tags are searchable", func(t *testing.T) {
		response := mustSearch(t, service, Request{Query: "gfm"})
		if !contains(ids(response), "docs/design/rendering.md") {
			t.Fatalf("tag search missed the document; got %v", ids(response))
		}
	})
}

// TestExcludedDocumentsAreNeverSearchable is acceptance B1 applied to search.
// An excluded document must not appear, and a caller must not be able to tell
// "excluded" from "absent".
func TestExcludedDocumentsAreNeverSearchable(t *testing.T) {
	service, _, _, _ := fixture(t, corpus)
	waitReady(t, service, 3)

	for _, query := range []string{"hunter2", "passphrase", "secret"} {
		response := mustSearch(t, service, Request{Query: query})
		for _, result := range response.Results {
			if strings.HasPrefix(result.DocumentID, "docs/private/") {
				t.Fatalf("query %q returned excluded document %q", query, result.DocumentID)
			}
		}
	}
}

// TestStaleIndexCannotLeakAnExcludedDocument proves the live workspace, not the
// projection, decides what a caller may see. The index is deliberately poisoned
// with a document the workspace does not include.
func TestStaleIndexCannotLeakAnExcludedDocument(t *testing.T) {
	service, _, _, _ := fixture(t, corpus)
	waitReady(t, service, 3)

	if err := service.index.Put(&documents.IndexView{
		ID:      "docs/private/secret.md",
		Title:   "Secret",
		Version: "sha256:stale",
		Body:    "The passphrase is hunter2.",
	}); err != nil {
		t.Fatalf("poison the index: %v", err)
	}

	response := mustSearch(t, service, Request{Query: "hunter2"})
	if len(response.Results) != 0 {
		t.Fatalf("a stale index row leaked an excluded document: %v", ids(response))
	}
}

// TestIncrementalUpdate covers acceptance F2 without the watcher: the indexing
// path itself must make new text searchable. Watcher-driven latency is measured
// separately in TestWatcherLatency.
func TestIncrementalUpdate(t *testing.T) {
	service, ws, root, _ := fixture(t, corpus)
	waitReady(t, service, 3)

	if before := mustSearch(t, service, Request{Query: "photosynthesis"}); len(before.Results) != 0 {
		t.Fatalf("the word was already present: %v", ids(before))
	}

	writeFiles(t, root, map[string]string{
		"docs/design/concurrency.md": "# Concurrency\n\n## Worker pool\n\nNow mentions photosynthesis.\n",
	})
	if err := ws.Refresh(); err != nil {
		t.Fatalf("workspace refresh: %v", err)
	}
	service.enqueue("docs/design/concurrency.md")
	service.signal()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if response := mustSearch(t, service, Request{Query: "photosynthesis"}); len(response.Results) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("edited text did not become searchable")
}

// TestRemovedDocumentLeavesTheIndex proves a deleted file stops being a result.
func TestRemovedDocumentLeavesTheIndex(t *testing.T) {
	service, ws, root, _ := fixture(t, corpus)
	waitReady(t, service, 3)

	if err := os.Remove(filepath.Join(root, "docs", "design", "concurrency.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := ws.Refresh(); err != nil {
		t.Fatalf("workspace refresh: %v", err)
	}
	service.enqueue("docs/design/concurrency.md")
	service.signal()
	waitReady(t, service, 2)

	if response := mustSearch(t, service, Request{Query: "bounded"}); len(response.Results) != 0 {
		t.Fatalf("a removed document is still a result: %v", ids(response))
	}
}

// TestRebuildAfterCacheDeletion covers acceptance F3: deleting the cache and
// restarting rebuilds the index with no loss of authoritative data.
func TestRebuildAfterCacheDeletion(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	writeFiles(t, root, corpus)

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

	first := openService(t, cache, cfg, ws)
	waitReady(t, first, 3)
	before := ids(mustSearch(t, first, Request{Query: "bounded"}))
	if len(before) == 0 {
		t.Fatal("nothing indexed before the cache was deleted")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The whole cache directory goes, exactly as a user clearing caches would.
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("the index wrote nothing to the cache directory")
	}
	if err := os.RemoveAll(cache); err != nil {
		t.Fatalf("delete cache: %v", err)
	}

	// Every authoritative file must still be present and unchanged.
	for rel, want := range corpus {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("authoritative file %s was lost with the cache: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("authoritative file %s changed when the cache was deleted", rel)
		}
	}

	second := openService(t, cache, cfg, ws)
	waitReady(t, second, 3)
	after := ids(mustSearch(t, second, Request{Query: "bounded"}))
	if len(after) != len(before) || after[0] != before[0] {
		t.Fatalf("rebuild produced different results: before %v, after %v", before, after)
	}
}

// TestProjectionKeyChangeDiscardsTheIndex proves a narrowed include set cannot
// leave searchable rows behind.
func TestProjectionKeyChangeDiscardsTheIndex(t *testing.T) {
	cache := t.TempDir()

	index, err := Open(cache, "key-one")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := index.Put(&documents.IndexView{ID: "docs/a.md", Title: "A", Version: "v1", Body: "alpha"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if count, _ := index.Count(); count != 1 {
		t.Fatalf("count before = %d, want 1", count)
	}
	index.Close()

	reopened, err := Open(cache, "key-two")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	count, err := reopened.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Fatalf("a changed projection key left %d rows behind; the index must be discarded", count)
	}
}

// TestCorruptIndexIsDiscardedNotFatal proves a damaged cache costs a rebuild,
// never a failed launch.
func TestCorruptIndexIsDiscardedNotFatal(t *testing.T) {
	cache := t.TempDir()
	if err := os.WriteFile(filepath.Join(cache, IndexFileName),
		[]byte("this is not a database"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	index, err := Open(cache, "key")
	if err != nil {
		t.Fatalf("a corrupt cache must be discarded, not fatal: %v", err)
	}
	defer index.Close()

	if count, err := index.Count(); err != nil || count != 0 {
		t.Fatalf("count = %d, err = %v; want an empty usable index", count, err)
	}
}

// TestIndexLivesOutsideTheWorkspace guards constitution C2 and spec 03
// section 2.3: the projection must never be written into the repository.
func TestIndexLivesOutsideTheWorkspace(t *testing.T) {
	service, _, root, cache := fixture(t, corpus)
	waitReady(t, service, 3)

	if !strings.HasPrefix(service.index.Path(), cache) {
		t.Fatalf("index path %q is not inside the cache directory %q", service.index.Path(), cache)
	}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if strings.Contains(entry.Name(), "search.sqlite") {
			t.Errorf("the search index was written inside the workspace at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// TestFilters covers the path and group filters R7 requires.
func TestFilters(t *testing.T) {
	service, _, _, _ := fixture(t, corpus)
	waitReady(t, service, 3)

	t.Run("path filter narrows to a subtree", func(t *testing.T) {
		response := mustSearch(t, service, Request{
			Query:   "markdown",
			Filters: Filters{Path: "docs/"},
		})
		for _, id := range ids(response) {
			if !strings.HasPrefix(id, "docs/") {
				t.Errorf("path filter returned %q", id)
			}
		}
	})

	t.Run("group filter narrows to configured members", func(t *testing.T) {
		response := mustSearch(t, service, Request{
			Query:   "the",
			Filters: Filters{Group: "design"},
		})
		if len(response.Results) == 0 {
			t.Fatal("group filter returned nothing")
		}
		for _, id := range ids(response) {
			if !strings.HasPrefix(id, "docs/design/") {
				t.Errorf("group filter returned %q, which is not in the group", id)
			}
		}
	})

	t.Run("an unknown group is an explicit error", func(t *testing.T) {
		_, err := service.Search(Request{Query: "the", Filters: Filters{Group: "nope"}})
		if err == nil {
			t.Fatal("an unknown group must be rejected, not silently empty")
		}
	})

	t.Run("the Git filter reports itself unavailable without Git", func(t *testing.T) {
		_, err := service.Search(Request{Query: "the", Filters: Filters{Git: "modified"}})
		if err == nil {
			t.Fatal("filtering on Git state without a Git adapter must be an error")
		}
	})

	t.Run("an unknown Git state is rejected", func(t *testing.T) {
		_, err := service.Search(Request{Query: "the", Filters: Filters{Git: "exploded"}})
		if err == nil {
			t.Fatal("an unrecognised Git state must be rejected")
		}
	})
}

// TestMalformedQueriesAreCleanErrors proves FTS5 match syntax cannot be used to
// provoke a fault. Every one of these is either a normal search or a stable
// 400-class error, never a panic and never a driver error.
func TestMalformedQueriesAreCleanErrors(t *testing.T) {
	service, _, _, _ := fixture(t, corpus)
	waitReady(t, service, 3)

	// Inputs chosen because each is either invalid FTS5 syntax or means
	// something the user did not ask for.
	inputs := []string{
		`"`, `""`, `AND`, `OR`, `NOT`, `NEAR(a b)`, `a NEAR b`,
		`(`, `)`, `()`, `*`, `**`, `^`, `-`, `a AND`, `AND a`,
		`{`, `}`, `[`, `]`, `:`, `a:b`, `"unterminated`,
		`'; DROP TABLE documents; --`, `%`, `_`, `\`,
		"a\x00b", strings.Repeat("x", 5000),
	}

	for _, input := range inputs {
		response, err := service.Search(Request{Query: input})
		if err != nil {
			// The only acceptable failure is "no searchable words".
			if err != ErrNoSearchableTerms {
				t.Errorf("query %q returned unexpected error %v", input, err)
			}
			continue
		}
		_ = response
	}
}

// TestSnippetsCarryNoMarkup proves highlighting cannot inject markup: the
// server ships typed segments, so the frontend never builds HTML from document
// text (spec 03 section 9).
func TestSnippetsCarryNoMarkup(t *testing.T) {
	service, _, _, _ := fixture(t, map[string]string{
		"docs/xss.md": "# Payload\n\nA line with <script>alert(1)</script> injected markup.\n",
	})
	waitReady(t, service, 1)

	response := mustSearch(t, service, Request{Query: "injected"})
	if len(response.Results) == 0 {
		t.Fatal("no result")
	}
	// Highlighting is structural: a marked run is a field on a segment, never a
	// delimiter or a tag embedded in text. There is therefore nothing in a
	// snippet a frontend could be tricked into rendering as markup.
	joined := joinSegments(response.Results[0].Snippet)
	if !strings.Contains(joined, "<script>") {
		t.Errorf("the snippet silently altered document text: %q", joined)
	}
	if strings.Contains(joined, "<mark>") || strings.Contains(joined, "\x02") {
		t.Errorf("the snippet carries markup or delimiters: %q", joined)
	}
}

// TestLimitAndTruncation proves the caller learns that more matched.
func TestLimitAndTruncation(t *testing.T) {
	files := map[string]string{}
	for i := range 10 {
		files[filepath.Join("docs", string(rune('a'+i))+".md")] = "# Doc\n\nshared vocabulary here.\n"
	}
	service, _, _, _ := fixture(t, files)
	waitReady(t, service, 10)

	response := mustSearch(t, service, Request{Query: "vocabulary", Limit: 3})
	if len(response.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(response.Results))
	}
	if !response.Truncated {
		t.Error("truncated = false, but more documents matched than were returned")
	}
}

func hasHighlight(segments []Segment) bool {
	for _, segment := range segments {
		if segment.Match {
			return true
		}
	}
	return false
}

func joinSegments(segments []Segment) string {
	var b strings.Builder
	for _, segment := range segments {
		b.WriteString(segment.Text)
	}
	return b.String()
}
