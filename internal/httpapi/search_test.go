package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"athenaeum/internal/config"
	"athenaeum/internal/documents"
	"athenaeum/internal/search"
	"athenaeum/internal/security"
	"athenaeum/internal/session"
	"athenaeum/internal/workspace"
)

// liveServerWithSearch builds a server with a real index over a temporary
// workspace and a temporary cache. Neither the developer's repository nor their
// real cache directory is touched (spec 07 section 5).
func liveServerWithSearch(t *testing.T, files map[string]string) (*Server, *security.SessionManager) {
	t.Helper()

	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	configPath := filepath.Join(root, config.DefaultFileName)
	body := `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]
exclude = ["private/**"]

[search]
enabled = true

[[groups]]
id = "design"
title = "Design"
patterns = ["docs/**/*.md"]
`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
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
	docs := documents.New(ws)

	index, err := search.Open(t.TempDir(), search.ProjectionKey(cfg))
	if err != nil {
		t.Fatalf("search.Open: %v", err)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	searchService := search.NewService(search.Options{
		Index:     index,
		Workspace: ws,
		Documents: docs,
		Logger:    quiet,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		searchService.Close()
	})
	searchService.Start(ctx)

	stateStore, err := session.NewStateStore(session.Dirs{State: t.TempDir()})
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}

	sessions, err := security.NewSessionManager(time.Hour, false)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	srv := New(Options{
		Sessions:      sessions,
		Origins:       security.NewOriginPolicy([]string{testOrigin}),
		FrontendBuilt: false,
		Version:       "test",
		Logger:        quiet,
		Workspace:     ws,
		Documents:     docs,
		Search:        searchService,
		SessionState:  stateStore,
	})

	waitIndexed(t, srv, sessions)
	return srv, sessions
}

// waitIndexed blocks until the projection has settled, through the API, so the
// status endpoint is exercised as well.
func waitIndexed(t *testing.T, srv *Server, sessions *security.SessionManager) {
	t.Helper()
	cookie := bootstrap(t, srv, sessions)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var status search.Status
		decodeJSON(t, get(t, srv, cookie, "/search/status"), &status)
		if status.State == search.StateReady {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the index never reached a ready state")
}

func get(t *testing.T, srv *Server, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, APIPrefix+path, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
		t.Fatalf("decode response: %v (body %q)", err, w.Body.String())
	}
}

func searchFor(t *testing.T, srv *Server, cookie *http.Cookie, query string, params ...string) *httptest.ResponseRecorder {
	t.Helper()
	values := url.Values{"q": {query}}
	for i := 0; i+1 < len(params); i += 2 {
		values.Set(params[i], params[i+1])
	}
	return get(t, srv, cookie, "/search?"+values.Encode())
}

var searchCorpus = map[string]string{
	"README.md":       "# Athenaeum\n\nA command centre.\n",
	"docs/design.md":  "# Design\n\n## Indexing\n\nIndexing uses a bounded worker pool.\n",
	"private/keys.md": "# Keys\n\nThe passphrase is hunter2.\n",
}

func TestSearchRequiresASession(t *testing.T) {
	srv, _ := liveServerWithSearch(t, searchCorpus)

	r := httptest.NewRequest(http.MethodGet, APIPrefix+"/search?q=bounded", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; search must sit behind the session guard",
			w.Code, http.StatusUnauthorized)
	}
}

func TestSearchReturnsRankedResults(t *testing.T) {
	srv, sessions := liveServerWithSearch(t, searchCorpus)
	cookie := bootstrap(t, srv, sessions)

	w := searchFor(t, srv, cookie, "bounded")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}

	var response search.Response
	decodeJSON(t, w, &response)
	if len(response.Results) == 0 {
		t.Fatal("no results")
	}
	result := response.Results[0]
	if result.DocumentID != "docs/design.md" {
		t.Errorf("document = %q", result.DocumentID)
	}
	if result.Line != 5 {
		t.Errorf("line = %d, want 5", result.Line)
	}
	if strings.Join(result.HeadingPath, " > ") != "Design > Indexing" {
		t.Errorf("heading path = %v", result.HeadingPath)
	}
	if len(result.Snippet) == 0 {
		t.Error("the result carries no snippet")
	}
}

// TestSearchCannotReachExcludedDocuments is acceptance B1 through the API.
func TestSearchCannotReachExcludedDocuments(t *testing.T) {
	srv, sessions := liveServerWithSearch(t, searchCorpus)
	cookie := bootstrap(t, srv, sessions)

	for _, query := range []string{"hunter2", "passphrase", "keys"} {
		w := searchFor(t, srv, cookie, query)
		if w.Code != http.StatusOK {
			continue
		}
		var response search.Response
		decodeJSON(t, w, &response)
		for _, result := range response.Results {
			if strings.HasPrefix(result.DocumentID, "private/") {
				t.Fatalf("query %q exposed an excluded document", query)
			}
		}
	}
}

// TestMalformedQueriesReturnCleanErrors is the requirement that FTS5 syntax in
// user input cannot produce a 500 or a panic.
func TestMalformedQueriesReturnCleanErrors(t *testing.T) {
	srv, sessions := liveServerWithSearch(t, searchCorpus)
	cookie := bootstrap(t, srv, sessions)

	inputs := []string{
		"", "   ", `"`, `AND`, `NEAR(a b)`, `(`, `*`, `^`, `-`,
		`'; DROP TABLE documents; --`, `%`, `\`, strings.Repeat("x", 4000),
		"!!!", "@@@",
	}
	for _, input := range inputs {
		w := searchFor(t, srv, cookie, input)
		if w.Code >= 500 {
			t.Errorf("query %q returned %d: %s", input, w.Code, w.Body.String())
			continue
		}
		if w.Code == http.StatusBadRequest {
			var body apiError
			decodeJSON(t, w, &body)
			if body.Error.Code == "" {
				t.Errorf("query %q returned a 400 with no stable code", input)
			}
		}
	}
}

func TestSearchFilters(t *testing.T) {
	srv, sessions := liveServerWithSearch(t, searchCorpus)
	cookie := bootstrap(t, srv, sessions)

	t.Run("path", func(t *testing.T) {
		var response search.Response
		decodeJSON(t, searchFor(t, srv, cookie, "a", "path", "docs/"), &response)
		for _, result := range response.Results {
			if !strings.HasPrefix(result.DocumentID, "docs/") {
				t.Errorf("path filter returned %q", result.DocumentID)
			}
		}
	})

	t.Run("group", func(t *testing.T) {
		var response search.Response
		decodeJSON(t, searchFor(t, srv, cookie, "indexing", "group", "design"), &response)
		if len(response.Results) == 0 {
			t.Fatal("group filter returned nothing")
		}
	})

	t.Run("unknown group is a 400 with a stable code", func(t *testing.T) {
		w := searchFor(t, srv, cookie, "indexing", "group", "nonexistent")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
		var body apiError
		decodeJSON(t, w, &body)
		if body.Error.Code != "SEARCH_FILTER_INVALID" {
			t.Errorf("code = %q", body.Error.Code)
		}
	})

	t.Run("git filter without git is a 409, not a silent empty list", func(t *testing.T) {
		w := searchFor(t, srv, cookie, "indexing", "git", "modified")
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
		}
		var body apiError
		decodeJSON(t, w, &body)
		if body.Error.Code != "SEARCH_GIT_UNAVAILABLE" {
			t.Errorf("code = %q", body.Error.Code)
		}
	})

	t.Run("a negative limit is rejected", func(t *testing.T) {
		w := searchFor(t, srv, cookie, "indexing", "limit", "-3")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})
}

func TestSearchStatusAndRebuild(t *testing.T) {
	srv, sessions := liveServerWithSearch(t, searchCorpus)
	cookie := bootstrap(t, srv, sessions)

	var status search.Status
	decodeJSON(t, get(t, srv, cookie, "/search/status"), &status)
	if status.State != search.StateReady {
		t.Fatalf("state = %q", status.State)
	}
	if status.Indexed != 2 {
		t.Errorf("indexed = %d, want 2 (the excluded document must not be counted)", status.Indexed)
	}

	r := httptest.NewRequest(http.MethodPost, APIPrefix+"/search/rebuild", nil)
	r.Header.Set("Origin", testOrigin)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("rebuild status = %d, want %d", w.Code, http.StatusAccepted)
	}
}

// TestRebuildRejectsAForeignOrigin proves the mutating route is behind the
// origin allow-list like every other one (ADR-0002).
func TestRebuildRejectsAForeignOrigin(t *testing.T) {
	srv, sessions := liveServerWithSearch(t, searchCorpus)
	cookie := bootstrap(t, srv, sessions)

	r := httptest.NewRequest(http.MethodPost, APIPrefix+"/search/rebuild", nil)
	r.Header.Set("Origin", "http://evil.example")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestSessionStateRoundTrip(t *testing.T) {
	srv, sessions := liveServerWithSearch(t, searchCorpus)
	cookie := bootstrap(t, srv, sessions)

	payload := `{
		"schema_version": 1,
		"tabs": [{"document_id": "docs/design.md", "mode": "source", "preview_scroll": 0.4, "source_line": 3}],
		"active_document": "docs/design.md",
		"recent": ["docs/design.md"],
		"layout": {"navigation": true, "context": false, "search": true}
	}`
	r := httptest.NewRequest(http.MethodPut, APIPrefix+"/session", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("put status = %d, body %s", w.Code, w.Body.String())
	}

	var state session.State
	decodeJSON(t, get(t, srv, cookie, "/session"), &state)
	if len(state.Tabs) != 1 || state.Tabs[0].DocumentID != "docs/design.md" {
		t.Fatalf("tabs = %+v", state.Tabs)
	}
	if state.Tabs[0].Mode != session.ModeSource || state.Tabs[0].SourceLine != 3 {
		t.Errorf("tab = %+v", state.Tabs[0])
	}
	if state.Layout.Search != true || state.Layout.Context != false {
		t.Errorf("layout = %+v", state.Layout)
	}
}

// TestSessionStateCannotReferenceExcludedDocuments is acceptance B1 applied to
// R13: a crafted session cannot be used to confirm an excluded file exists.
func TestSessionStateCannotReferenceExcludedDocuments(t *testing.T) {
	srv, sessions := liveServerWithSearch(t, searchCorpus)
	cookie := bootstrap(t, srv, sessions)

	payload := `{
		"schema_version": 1,
		"tabs": [{"document_id": "private/keys.md", "mode": "split"}],
		"active_document": "private/keys.md",
		"recent": ["private/keys.md"],
		"layout": {"navigation": true, "context": true, "search": false}
	}`
	r := httptest.NewRequest(http.MethodPut, APIPrefix+"/session", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("put status = %d", w.Code)
	}

	var state session.State
	decodeJSON(t, get(t, srv, cookie, "/session"), &state)
	if len(state.Tabs) != 0 {
		t.Errorf("an excluded document survived into session state: %+v", state.Tabs)
	}
	if state.ActiveDocument != "" {
		t.Errorf("active document = %q, want it cleared", state.ActiveDocument)
	}
	if len(state.Recent) != 0 {
		t.Errorf("recent = %v, want empty", state.Recent)
	}
}

func TestSessionStateRejectsUnknownFields(t *testing.T) {
	srv, sessions := liveServerWithSearch(t, searchCorpus)
	cookie := bootstrap(t, srv, sessions)

	r := httptest.NewRequest(http.MethodPut, APIPrefix+"/session",
		strings.NewReader(`{"schema_version": 1, "surprise": true}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestSearchDisabledIsGraceful proves a workspace without search still serves
// every other route (constitution C1).
func TestSearchDisabledIsGraceful(t *testing.T) {
	srv, sessions, _ := liveServer(t, map[string]string{"docs/a.md": "# A\n"})
	cookie := bootstrap(t, srv, sessions)

	w := searchFor(t, srv, cookie, "anything")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var status search.Status
	decodeJSON(t, get(t, srv, cookie, "/search/status"), &status)
	if status.State != search.StateDisabled {
		t.Errorf("state = %q, want %q", status.State, search.StateDisabled)
	}

	// Reading a document is unaffected.
	if got := get(t, srv, cookie, "/documents/docs/a.md"); got.Code != http.StatusOK {
		t.Errorf("document read status = %d with search off", got.Code)
	}
}
