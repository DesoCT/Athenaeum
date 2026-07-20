package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"athenaeum/internal/httpapi"
	"athenaeum/internal/security"
)

// The switching tests below are the enforcement of ADR-0004's central
// constraint. Multi-root is barred by D-006, R1, and spec 01 section 6, and the
// registry is allowed only as a launcher. The test ADR-0004 states is that no
// feature may make two roots visible at the same moment, so these tests assert
// the negative: after a switch, nothing whatsoever answers from the previous
// workspace.

const switchConfig = `
schema_version = 1
name = %q
root = "."
include = ["**/*.md"]

[search]
enabled = true

[security]
writable = ["docs/**/*.md"]
`

// switchFixture builds two separate workspaces and a registry naming both.
//
// Spec 07 section 5: temporary directories only. The registry path is injected
// so no test ever reads the developer's real ~/.config.
func switchFixture(t *testing.T) (*controller, *httpapi.Server, *http.Cookie, string, string) {
	t.Helper()

	alpha := t.TempDir()
	beta := t.TempDir()

	writeWorkspace(t, alpha, "Alpha", map[string]string{
		"docs/alpha-only.md": "# Alpha Only\n\nzebrafish appears only in alpha.\n",
		"docs/shared.md":     "# Shared Alpha\n\ncommon text\n",
	})
	writeWorkspace(t, beta, "Beta", map[string]string{
		"docs/beta-only.md": "# Beta Only\n\naardvark appears only in beta.\n",
		"docs/shared.md":    "# Shared Beta\n\ncommon text\n",
	})

	registryPath := filepath.Join(t.TempDir(), "workspaces.toml")
	body := fmt.Sprintf(`
[[workspace]]
name = "Alpha"
path = %q

[[workspace]]
name = "Beta"
path = %q
`, alpha, beta)
	if err := os.WriteFile(registryPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	control := &controller{
		opts:         Options{Logger: logger},
		registryPath: registryPath,
		ctx:          ctx,
	}

	sessions, err := security.NewSessionManager(time.Hour, false)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	api := httpapi.New(httpapi.Options{
		Sessions:   sessions,
		Origins:    security.NewOriginPolicy([]string{"http://127.0.0.1:7777"}),
		Logger:     logger,
		Workspaces: control,
	})
	control.server = api
	t.Cleanup(control.shutdown)

	return control, api, bootstrapCookie(t, api, sessions), alpha, beta
}

func writeWorkspace(t *testing.T, root, name string, files map[string]string) {
	t.Helper()

	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	configPath := filepath.Join(root, "athenaeum.toml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf(switchConfig, name)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func bootstrapCookie(t *testing.T, api *httpapi.Server, sessions *security.SessionManager) *http.Cookie {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, httpapi.BootstrapPath+"?t="+sessions.BootstrapToken(), nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)

	for _, cookie := range w.Result().Cookies() {
		if strings.Contains(cookie.Name, "session") {
			return cookie
		}
	}
	t.Fatalf("bootstrap issued no session cookie (status %d)", w.Code)
	return nil
}

func apiGet(t *testing.T, api *httpapi.Server, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, httpapi.APIPrefix+path, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	return w
}

// waitSearchReady blocks until the projection has settled, so a search
// assertion is about the index rather than about a race with it.
func waitSearchReady(t *testing.T, api *httpapi.Server, cookie *http.Cookie) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		w := apiGet(t, api, cookie, "/search/status")
		var status struct {
			State   string `json:"state"`
			Pending int    `json:"pending"`
		}
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &status); err == nil {
				if status.State == "ready" && status.Pending == 0 {
					return
				}
				if status.State == "disabled" || status.State == "unavailable" {
					t.Fatalf("search did not become usable: %s", w.Body.String())
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the search projection never settled")
}

func documentIDs(t *testing.T, api *httpapi.Server, cookie *http.Cookie) []string {
	t.Helper()

	w := apiGet(t, api, cookie, "/documents")
	if w.Code != http.StatusOK {
		t.Fatalf("list documents: status %d, body %s", w.Code, w.Body.String())
	}
	var body struct {
		Documents []struct {
			ID string `json:"id"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode documents: %v", err)
	}
	ids := make([]string, 0, len(body.Documents))
	for _, doc := range body.Documents {
		ids = append(ids, doc.ID)
	}
	return ids
}

func searchIDs(t *testing.T, api *httpapi.Server, cookie *http.Cookie, query string) []string {
	t.Helper()

	w := apiGet(t, api, cookie, "/search?q="+query)
	if w.Code != http.StatusOK {
		t.Fatalf("search %q: status %d, body %s", query, w.Code, w.Body.String())
	}
	var body struct {
		Results []struct {
			DocumentID string `json:"document_id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	ids := make([]string, 0, len(body.Results))
	for _, r := range body.Results {
		ids = append(ids, r.DocumentID)
	}
	return ids
}

// TestSwitchUnloadsThePreviousWorkspace is the central test of ADR-0004.
//
// It opens one workspace, switches to another, and asserts that every surface
// the ADR names — the tree, search, the write boundary, and the workspace
// summary — answers only from the new root. A stale service surviving here
// would be the silent multi-root bug the ADR exists to prevent.
func TestSwitchUnloadsThePreviousWorkspace(t *testing.T) {
	control, api, cookie, alpha, beta := switchFixture(t)

	if err := control.Open("Alpha"); err != nil {
		t.Fatalf("open Alpha: %v", err)
	}
	waitSearchReady(t, api, cookie)

	ids := documentIDs(t, api, cookie)
	if !contains(ids, "docs/alpha-only.md") {
		t.Fatalf("Alpha's tree does not list its own document: %v", ids)
	}
	if hits := searchIDs(t, api, cookie, "zebrafish"); len(hits) == 0 {
		t.Fatal("Alpha's index did not find Alpha's content")
	}

	// The switch.
	if err := control.Open("Beta"); err != nil {
		t.Fatalf("open Beta: %v", err)
	}
	waitSearchReady(t, api, cookie)

	// The tree is Beta's alone.
	ids = documentIDs(t, api, cookie)
	if !contains(ids, "docs/beta-only.md") {
		t.Fatalf("Beta's tree does not list its own document: %v", ids)
	}
	if contains(ids, "docs/alpha-only.md") {
		t.Fatalf("the tree still lists a document from the previous workspace: %v", ids)
	}

	// Search is Beta's alone. This is the assertion that matters most: a search
	// spanning workspaces is precisely the multi-root behaviour excluded by
	// D-006, R1, and spec 01 section 6.
	if hits := searchIDs(t, api, cookie, "aardvark"); len(hits) == 0 {
		t.Fatal("Beta's index did not find Beta's content")
	}
	if hits := searchIDs(t, api, cookie, "zebrafish"); len(hits) != 0 {
		t.Fatalf("search returned results from the previous workspace: %v", hits)
	}

	// The workspace summary names Beta and reports Beta's root.
	w := apiGet(t, api, cookie, "/workspace")
	var summary struct {
		Name string `json:"name"`
		Root string `json:"root"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode workspace: %v", err)
	}
	if summary.Name != "Beta" {
		t.Errorf("workspace name = %q, want Beta", summary.Name)
	}
	if !sameDir(summary.Root, beta) {
		t.Errorf("workspace root = %q, want %q", summary.Root, beta)
	}
	if sameDir(summary.Root, alpha) {
		t.Error("the workspace summary still points at the previous root")
	}

	// The write boundary moved with it. Alpha's document is not merely absent
	// from the tree; it cannot be written through the new session.
	if code := saveStatus(t, api, cookie, "docs/alpha-only.md"); code == http.StatusOK {
		t.Fatal("a document from the previous workspace was writable after the switch")
	}
}

// TestSwitchLeavesNoGoroutinesBehind proves the unloading is real rather than
// merely invisible.
//
// The tree and the index could each answer correctly while the previous
// workspace's watcher and indexer carried on running against a root the user
// has left. That is the leak that made switching impossible before the
// lifecycle fix, and it is silent from the API alone.
func TestSwitchLeavesNoGoroutinesBehind(t *testing.T) {
	control, api, cookie, _, _ := switchFixture(t)

	if err := control.Open("Alpha"); err != nil {
		t.Fatalf("open Alpha: %v", err)
	}
	waitSearchReady(t, api, cookie)

	before := workspaceGoroutines()
	if before == 0 {
		t.Fatal("expected the open workspace to be running background goroutines")
	}

	for i := range 3 {
		name := "Beta"
		if i%2 == 1 {
			name = "Alpha"
		}
		if err := control.Open(name); err != nil {
			t.Fatalf("switch to %s: %v", name, err)
		}
		waitSearchReady(t, api, cookie)
	}

	// One workspace is open, so one workspace's worth of goroutines is correct.
	// Repeated switching must not accumulate them.
	after := awaitStableGoroutines(t)
	if after > before {
		t.Fatalf("goroutines grew across repeated switches: %d before, %d after", before, after)
	}

	// Leaving must take the last of them with it.
	if err := control.Leave(); err != nil {
		t.Fatalf("leave: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if count := workspaceGoroutines(); count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d workspace goroutine(s) still running after leaving", workspaceGoroutines())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestLeaveReturnsToThePicker covers ADR-0004's "leaving a workspace returns to
// the picker; nothing from a previous workspace remains loaded".
func TestLeaveReturnsToThePicker(t *testing.T) {
	control, api, cookie, _, _ := switchFixture(t)

	if err := control.Open("Alpha"); err != nil {
		t.Fatalf("open Alpha: %v", err)
	}
	if err := control.Leave(); err != nil {
		t.Fatalf("leave: %v", err)
	}

	// Every workspace-scoped surface must now refuse, rather than serving the
	// workspace that was open a moment ago.
	for _, path := range []string{"/workspace", "/documents", "/documents/docs/alpha-only.md", "/search?q=zebrafish"} {
		w := apiGet(t, api, cookie, path)
		if w.Code == http.StatusOK {
			t.Errorf("%s still answered after leaving: %s", path, w.Body.String())
		}
	}

	// The registry itself is still listable: that is the picker.
	w := apiGet(t, api, cookie, "/workspaces")
	if w.Code != http.StatusOK {
		t.Fatalf("the picker is not available after leaving: status %d", w.Code)
	}
	var list struct {
		Active  *struct{} `json:"active"`
		Entries []struct {
			Name      string `json:"name"`
			Available bool   `json:"available"`
			Active    bool   `json:"active"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode picker: %v", err)
	}
	if list.Active != nil {
		t.Error("the picker reports an active workspace after leaving")
	}
	if len(list.Entries) != 2 {
		t.Fatalf("picker listed %d entries, want 2", len(list.Entries))
	}
	for _, entry := range list.Entries {
		if entry.Active {
			t.Errorf("entry %q is marked active after leaving", entry.Name)
		}
	}
}

// TestPickerMarksExactlyOneActiveEntry proves the list view cannot present two
// workspaces as open at once.
func TestPickerMarksExactlyOneActiveEntry(t *testing.T) {
	control, api, cookie, _, _ := switchFixture(t)

	for _, name := range []string{"Alpha", "Beta"} {
		if err := control.Open(name); err != nil {
			t.Fatalf("open %s: %v", name, err)
		}

		w := apiGet(t, api, cookie, "/workspaces")
		var list struct {
			Entries []struct {
				Name   string `json:"name"`
				Active bool   `json:"active"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode picker: %v", err)
		}

		active := []string{}
		for _, entry := range list.Entries {
			if entry.Active {
				active = append(active, entry.Name)
			}
		}
		if len(active) != 1 {
			t.Fatalf("with %s open, %d entries are marked active: %v", name, len(active), active)
		}
		if active[0] != name {
			t.Errorf("with %s open, the active entry is %q", name, active[0])
		}
	}
}

// TestOpenUnknownNameFails covers the lookup failures ADR-0004 requires to be
// explicit rather than guessed.
func TestOpenUnknownNameFails(t *testing.T) {
	control, _, _, _, _ := switchFixture(t)

	if err := control.Open("Gamma"); err == nil {
		t.Fatal("opening an unregistered name succeeded")
	}
	if control.active() != nil {
		t.Fatal("a failed open left a workspace loaded")
	}
}

// TestFailedSwitchKeepsTheCurrentWorkspace proves a switch that cannot complete
// does not strand the process with nothing open.
func TestFailedSwitchKeepsTheCurrentWorkspace(t *testing.T) {
	control, api, cookie, _, _ := switchFixture(t)

	if err := control.Open("Alpha"); err != nil {
		t.Fatalf("open Alpha: %v", err)
	}
	if err := control.Open("Gamma"); err == nil {
		t.Fatal("opening an unregistered name succeeded")
	}

	w := apiGet(t, api, cookie, "/workspace")
	if w.Code != http.StatusOK {
		t.Fatalf("the previously open workspace stopped answering after a failed switch: %d", w.Code)
	}
	var summary struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode workspace: %v", err)
	}
	if summary.Name != "Alpha" {
		t.Errorf("workspace name = %q, want Alpha to still be open", summary.Name)
	}
}

// saveStatus attempts a write and reports the status code.
func saveStatus(t *testing.T, api *httpapi.Server, cookie *http.Cookie, id string) int {
	t.Helper()

	body := strings.NewReader(`{"content":"# Edited\n","base_version":"sha256:unknown"}`)
	r := httptest.NewRequest(http.MethodPut, httpapi.APIPrefix+"/documents/"+id, body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://127.0.0.1:7777")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	return w.Code
}

// workspaceGoroutines counts goroutines belonging to a workspace's background
// services, whichever workspace they were started for.
func workspaceGoroutines() int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}

	frames := []string{
		"athenaeum/internal/search.(*Service).coordinate",
		"athenaeum/internal/search.(*Service).follow",
		"athenaeum/internal/watcher.(*Watcher).run",
		"athenaeum/internal/gitview.(*Adapter).Run",
		"athenaeum/internal/app.followWorkspace",
	}

	count := 0
	for _, stack := range strings.Split(string(buf), "\n\n") {
		for _, frame := range frames {
			if strings.Contains(stack, frame) {
				count++
				break
			}
		}
	}
	return count
}

// awaitStableGoroutines waits for the count to stop moving, so the assertion is
// not taken while a just-closed workspace is still winding down.
func awaitStableGoroutines(t *testing.T) int {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	last := -1
	stable := 0
	for time.Now().Before(deadline) {
		count := workspaceGoroutines()
		if count == last {
			stable++
			if stable >= 5 {
				return count
			}
		} else {
			stable = 0
			last = count
		}
		time.Sleep(20 * time.Millisecond)
	}
	return workspaceGoroutines()
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// sameDir compares two directory paths after resolving symlinks.
//
// Canonicalise before comparing: on macOS /var is a symlink to /private/var,
// and this project has been bitten twice by comparing a non-canonical path
// against a canonical root (see canonicalise in internal/security/paths.go).
func sameDir(a, b string) bool {
	resolvedA, err := filepath.EvalSymlinks(a)
	if err != nil {
		resolvedA = a
	}
	resolvedB, err := filepath.EvalSymlinks(b)
	if err != nil {
		resolvedB = b
	}
	return resolvedA == resolvedB
}
