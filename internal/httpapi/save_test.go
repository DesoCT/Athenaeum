package httpapi

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"athenaeum/internal/config"
	"athenaeum/internal/documents"
	"athenaeum/internal/security"
	"athenaeum/internal/workspace"
)

// liveServer builds a server backed by a real temporary workspace.
func liveServer(t *testing.T, files map[string]string) (*Server, *security.SessionManager, string) {
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
	configBody := `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]

[security]
writable = ["docs/**/*.md"]
`
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

	sessions, err := security.NewSessionManager(time.Hour, false)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	srv := New(Options{
		Sessions:      sessions,
		Origins:       security.NewOriginPolicy([]string{testOrigin}),
		Frontend:      fs.FS(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}}),
		FrontendBuilt: true,
		Version:       "test",
		Workspace:     ws,
		Documents:     documents.New(ws),
	})
	return srv, sessions, dir
}

// save issues an authenticated, same-origin PUT.
func save(t *testing.T, srv *Server, cookie *http.Cookie, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, APIPrefix+"/documents/"+id, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func readDoc(t *testing.T, srv *Server, cookie *http.Cookie, id string) documents.Document {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, APIPrefix+"/documents/"+id, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("read %s: status %d", id, w.Code)
	}
	var doc documents.Document
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	return doc
}

func TestSaveRoundTrip(t *testing.T) {
	srv, sessions, dir := liveServer(t, map[string]string{"docs/a.md": "# One\n"})
	cookie := bootstrap(t, srv, sessions)

	doc := readDoc(t, srv, cookie, "docs/a.md")
	body, _ := json.Marshal(saveRequest{Content: "# Two\n", Version: doc.Version})

	w := save(t, srv, cookie, "docs/a.md", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	onDisk, err := os.ReadFile(filepath.Join(dir, "docs", "a.md"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(onDisk) != "# Two\n" {
		t.Fatalf("on-disk content = %q", onDisk)
	}
}

// TestSaveConflictReturnsBothSides covers R6 through the API.
func TestSaveConflictReturnsBothSides(t *testing.T) {
	srv, sessions, dir := liveServer(t, map[string]string{"docs/a.md": "# One\n"})
	cookie := bootstrap(t, srv, sessions)

	doc := readDoc(t, srv, cookie, "docs/a.md")

	// Something changes the file behind the editor's back.
	if err := os.WriteFile(filepath.Join(dir, "docs", "a.md"), []byte("# Disk\n"), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	body, _ := json.Marshal(saveRequest{Content: "# Local\n", Version: doc.Version})
	w := save(t, srv, cookie, "docs/a.md", string(body))

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	var got conflictResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if got.Error.Code != documents.CodeConflict {
		t.Errorf("code = %q", got.Error.Code)
	}
	if got.Conflict.CurrentContent != "# Disk\n" {
		t.Errorf("current_content = %q, want the disk version", got.Conflict.CurrentContent)
	}
	if got.Conflict.CurrentVersion == "" {
		t.Error("current_version missing")
	}

	// The disk version must be untouched by the refused save.
	onDisk, _ := os.ReadFile(filepath.Join(dir, "docs", "a.md"))
	if string(onDisk) != "# Disk\n" {
		t.Fatalf("the refused save modified the file: %q", onDisk)
	}
}

// TestForceSaveOverwritesAfterConflict is how "keep local" resolves.
func TestForceSaveOverwritesAfterConflict(t *testing.T) {
	srv, sessions, dir := liveServer(t, map[string]string{"docs/a.md": "# One\n"})
	cookie := bootstrap(t, srv, sessions)

	body, _ := json.Marshal(saveRequest{Content: "# Local wins\n", Force: true})
	w := save(t, srv, cookie, "docs/a.md", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "docs", "a.md"))
	if string(onDisk) != "# Local wins\n" {
		t.Fatalf("content = %q", onDisk)
	}
}

// TestSaveWithoutVersionRefused stops an omitted field silently clobbering.
func TestSaveWithoutVersionRefused(t *testing.T) {
	srv, sessions, dir := liveServer(t, map[string]string{"docs/a.md": "# One\n"})
	cookie := bootstrap(t, srv, sessions)

	w := save(t, srv, cookie, "docs/a.md", `{"content":"# No version\n"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "docs", "a.md"))
	if string(onDisk) != "# One\n" {
		t.Fatalf("the refused save modified the file: %q", onDisk)
	}
}

// TestSaveOutsideWriteBoundary covers acceptance B3 through the API.
func TestSaveOutsideWriteBoundary(t *testing.T) {
	srv, sessions, dir := liveServer(t, map[string]string{
		"docs/a.md":   "# Writable\n",
		"readonly.md": "# Read only\n",
	})
	cookie := bootstrap(t, srv, sessions)

	body, _ := json.Marshal(saveRequest{Content: "# Changed\n", Force: true})
	w := save(t, srv, cookie, "readonly.md", string(body))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", w.Code, w.Body.String())
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "readonly.md"))
	if string(onDisk) != "# Read only\n" {
		t.Fatalf("the refused save modified the file: %q", onDisk)
	}
}

// TestSaveRequiresAllowedOrigin is the CSRF guard on the write path
// (spec 03 section 10). A cookie alone must not be enough.
func TestSaveRequiresAllowedOrigin(t *testing.T) {
	srv, sessions, dir := liveServer(t, map[string]string{"docs/a.md": "# One\n"})
	cookie := bootstrap(t, srv, sessions)

	body, _ := json.Marshal(saveRequest{Content: "# Injected\n", Force: true})
	r := httptest.NewRequest(http.MethodPut, APIPrefix+"/documents/docs/a.md", strings.NewReader(string(body)))
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "docs", "a.md"))
	if string(onDisk) != "# One\n" {
		t.Fatalf("a cross-origin save modified the file: %q", onDisk)
	}
}

func TestSaveRequiresSession(t *testing.T) {
	srv, _, dir := liveServer(t, map[string]string{"docs/a.md": "# One\n"})

	body, _ := json.Marshal(saveRequest{Content: "# Injected\n", Force: true})
	r := httptest.NewRequest(http.MethodPut, APIPrefix+"/documents/docs/a.md", strings.NewReader(string(body)))
	r.Header.Set("Origin", testOrigin)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "docs", "a.md"))
	if string(onDisk) != "# One\n" {
		t.Fatalf("an unauthenticated save modified the file: %q", onDisk)
	}
}

func TestSaveRejectsTraversal(t *testing.T) {
	srv, sessions, _ := liveServer(t, map[string]string{"docs/a.md": "# One\n"})
	cookie := bootstrap(t, srv, sessions)

	body, _ := json.Marshal(saveRequest{Content: "x", Force: true})
	w := save(t, srv, cookie, "../escape.md", string(body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestSaveRejectsUnknownFields(t *testing.T) {
	srv, sessions, _ := liveServer(t, map[string]string{"docs/a.md": "# One\n"})
	cookie := bootstrap(t, srv, sessions)

	w := save(t, srv, cookie, "docs/a.md", `{"content":"x","force":true,"nonsense":1}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown field", w.Code)
	}
}
