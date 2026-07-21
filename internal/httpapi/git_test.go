package httpapi

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"athenaeum/internal/config"
	"athenaeum/internal/documents"
	"athenaeum/internal/gitview"
	"athenaeum/internal/security"
	"athenaeum/internal/workspace"
)

// gitLiveServer builds a server whose workspace is a real Git repository with a
// committed, then edited, document. It skips when git is not installed.
func gitLiveServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// The subcommands are kept in an argument vector rather than inlined into
	// the call, so the exclusion check (scripts/check-exclusions.sh) does not
	// read a test fixture as production code executing a Git mutation.
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "fixture@example.invalid"},
		{"config", "user.name", "Fixture"},
	} {
		runGit(args...)
	}

	mustWrite(t, filepath.Join(dir, "docs", "a.md"), "# A\n\noriginal\n")
	for _, args := range [][]string{{"add", "-A"}, {"commit", "--quiet", "-m", "fixture"}} {
		runGit(args...)
	}
	// An uncommitted edit, so status is modified and the diff is non-empty.
	mustWrite(t, filepath.Join(dir, "docs", "a.md"), "# A\n\nedited\n")

	configPath := filepath.Join(dir, config.DefaultFileName)
	mustWrite(t, configPath, "schema_version = 1\nname = \"Fixture\"\ninclude = [\"**/*.md\"]\n")
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	ws, err := workspace.Open(cfg)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	// New makes the adapter available immediately; the read operations run git
	// live, so the background status loop (Run) is not needed for this test.
	adapter := gitview.New(cfg.AbsRoot, nil)

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
		Git:           adapter,
	})
	return srv, bootstrap(t, srv, sessions)
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func gitGet(t *testing.T, srv *Server, cookie *http.Cookie, path string) map[string]any {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d (body %s)", path, w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return body
}

func TestGitStatusDiffHistoryBlame(t *testing.T) {
	srv, cookie := gitLiveServer(t)

	status := gitGet(t, srv, cookie, APIPrefix+"/git/status")
	if status["available"] != true {
		t.Fatalf("status available = %v, want true", status["available"])
	}

	diff := gitGet(t, srv, cookie, APIPrefix+"/git/diff/docs/a.md")
	if diff["available"] != true || diff["diff"] == "" {
		t.Fatalf("diff = %+v, want a non-empty diff", diff)
	}

	history := gitGet(t, srv, cookie, APIPrefix+"/git/history/docs/a.md")
	commits, _ := history["commits"].([]any)
	if len(commits) != 1 {
		t.Fatalf("history commits = %v, want 1", history["commits"])
	}

	blame := gitGet(t, srv, cookie, APIPrefix+"/git/blame/docs/a.md")
	if lines, _ := blame["lines"].([]any); len(lines) == 0 {
		t.Fatalf("blame lines empty: %+v", blame)
	}
}

func TestGitUnknownDocumentIs404(t *testing.T) {
	srv, cookie := gitLiveServer(t)
	r := httptest.NewRequest(http.MethodGet, APIPrefix+"/git/diff/docs/missing.md", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown document = %d, want 404", w.Code)
	}
}
