package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"athenaeum/internal/registry"
	"athenaeum/internal/security"
)

// fakeWorkspaces stands in for the application's controller.
//
// The routes are tested against it rather than against real workspaces because
// what is under test here is the contract — codes, statuses, and shapes — not
// the construction of services, which internal/app covers end to end.
type fakeWorkspaces struct {
	reg      *registry.Registry
	listErr  error
	openErr  error
	leaveErr error
	opened   []string
	left     int
}

func (f *fakeWorkspaces) List() (*registry.Registry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.reg, nil
}

func (f *fakeWorkspaces) Open(name string) error {
	f.opened = append(f.opened, name)
	return f.openErr
}

func (f *fakeWorkspaces) Leave() error {
	f.left++
	return f.leaveErr
}

func workspacesServer(t *testing.T, fake *fakeWorkspaces) (*Server, *http.Cookie) {
	t.Helper()

	srv, sessions, _ := liveServer(t, map[string]string{"docs/a.md": "# A\n"})
	srv.opts.Workspaces = fake
	srv.mux = http.NewServeMux()
	srv.routes()
	return srv, bootstrap(t, srv, sessions)
}

// pickerServer builds a server with no workspace at all, as `--pick` produces.
func pickerServer(t *testing.T, fake *fakeWorkspaces) (*Server, *security.SessionManager, string) {
	t.Helper()

	sessions, err := security.NewSessionManager(time.Hour, false)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	srv := New(Options{
		Sessions:   sessions,
		Origins:    security.NewOriginPolicy([]string{testOrigin}),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Workspaces: fake,
	})
	return srv, sessions, ""
}

func sampleRegistry() *registry.Registry {
	return &registry.Registry{
		SourcePath: "/tmp/workspaces.toml",
		Present:    true,
		Entries: []registry.Entry{
			{Name: "Alpha", Path: "/tmp/alpha", ConfigPath: "/tmp/alpha/athenaeum.toml", Available: true},
			{
				Name:      "Broken",
				Path:      "/tmp/missing",
				Available: false,
				Code:      registry.CodeConfigMissing,
				Reason:    "this directory holds no athenaeum.toml",
				Remedy:    "create athenaeum.toml in the directory",
			},
		},
	}
}

func getJSON(t *testing.T, srv *Server, cookie *http.Cookie, path string, into any) int {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, APIPrefix+path, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if into != nil && w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatalf("decode %s: %v (body %s)", path, err, w.Body.String())
		}
	}
	return w.Code
}

func postJSON(t *testing.T, srv *Server, cookie *http.Cookie, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, APIPrefix+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

// TestWorkspaceListShowsUnavailableEntriesWithReasons covers ADR-0004: a
// registered path that cannot be opened is shown as unavailable with the
// reason, rather than silently omitted.
func TestWorkspaceListShowsUnavailableEntriesWithReasons(t *testing.T) {
	srv, cookie := workspacesServer(t, &fakeWorkspaces{reg: sampleRegistry()})

	var body workspaceListResponse
	if code := getJSON(t, srv, cookie, "/workspaces", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}

	if len(body.Entries) != 2 {
		t.Fatalf("listed %d entries, want 2", len(body.Entries))
	}
	broken := body.Entries[1]
	if broken.Available {
		t.Error("the broken entry is reported as available")
	}
	if broken.Code != registry.CodeConfigMissing {
		t.Errorf("code = %q, want %q", broken.Code, registry.CodeConfigMissing)
	}
	if broken.Reason == "" || broken.Remedy == "" {
		t.Errorf("an unavailable entry must state a reason and a remedy: %+v", broken)
	}
	if !body.Present {
		t.Error("present = false for a registry that exists")
	}
	if body.RegistryPath == "" {
		t.Error("the response does not name the registry file")
	}
}

// TestPickerReportsNoActiveWorkspaceAtLaunch is a regression test.
//
// A process launched at the picker still holds a binding seeded from its
// options; treating "the binding is non-nil" as "a workspace is open" reported
// an empty active workspace — a nameless, rootless entry — instead of none.
// Found by running the real binary with --pick, not by a unit test.
func TestPickerReportsNoActiveWorkspaceAtLaunch(t *testing.T) {
	srv, sessions, _ := pickerServer(t, &fakeWorkspaces{reg: sampleRegistry()})
	cookie := bootstrap(t, srv, sessions)

	var body workspaceListResponse
	if code := getJSON(t, srv, cookie, "/workspaces", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body.Active != nil {
		t.Fatalf("a process with no workspace reports one as active: %+v", body.Active)
	}
	for _, entry := range body.Entries {
		if entry.Active {
			t.Errorf("entry %q is marked active with no workspace open", entry.Name)
		}
	}
}

// TestWorkspaceListReportsAnUnreadableRegistry proves a broken registry file
// produces a stable code rather than an empty list that looks like "you have no
// workspaces".
func TestWorkspaceListReportsAnUnreadableRegistry(t *testing.T) {
	srv, cookie := workspacesServer(t, &fakeWorkspaces{listErr: errUnreadable{}})

	code := getJSON(t, srv, cookie, "/workspaces", nil)
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", code)
	}
}

type errUnreadable struct{}

func (errUnreadable) Error() string { return "unreadable" }

// TestWorkspaceOpenPassesTheName covers the happy path of the switch route.
func TestWorkspaceOpenPassesTheName(t *testing.T) {
	fake := &fakeWorkspaces{reg: sampleRegistry()}
	srv, cookie := workspacesServer(t, fake)

	w := postJSON(t, srv, cookie, "/workspaces/open", `{"name":"Alpha"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if len(fake.opened) != 1 || fake.opened[0] != "Alpha" {
		t.Fatalf("opened = %v, want [Alpha]", fake.opened)
	}
}

// TestWorkspaceOpenRequiresAName covers the actionable-error requirement: the
// message names the field and the remedy (requirement N6).
func TestWorkspaceOpenRequiresAName(t *testing.T) {
	srv, cookie := workspacesServer(t, &fakeWorkspaces{reg: sampleRegistry()})

	w := postJSON(t, srv, cookie, "/workspaces/open", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var body apiError
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Details["field"] != "name" {
		t.Errorf("the error does not name the field: %+v", body.Error)
	}
	if body.Error.Details["remedy"] == "" {
		t.Errorf("the error does not state a remedy: %+v", body.Error)
	}
}

// TestWorkspaceOpenSurfacesRegistryCodes proves the picker can explain a failed
// open using the same code and words it already shows in the list.
func TestWorkspaceOpenSurfacesRegistryCodes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{
			name:   "unknown name",
			err:    &registry.LookupError{Code: registry.CodeNameUnknown, Name: "Gamma", Reason: "no workspace with that name is registered"},
			status: http.StatusNotFound,
			code:   registry.CodeNameUnknown,
		},
		{
			// Ambiguity is refused rather than guessed: silently opening the
			// first of two identically named workspaces is exactly the hidden
			// behaviour C8 prohibits.
			name:   "ambiguous name",
			err:    &registry.LookupError{Code: registry.CodeNameAmbiguous, Name: "Twin", Reason: "2 registered workspaces share that name"},
			status: http.StatusConflict,
			code:   registry.CodeNameAmbiguous,
		},
		{
			name: "unavailable entry",
			err: &EntryUnavailableError{
				Name: "Broken", Code: registry.CodeConfigMissing,
				Reason: "this directory holds no athenaeum.toml",
				Remedy: "create athenaeum.toml in the directory",
			},
			status: http.StatusConflict,
			code:   registry.CodeConfigMissing,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, cookie := workspacesServer(t, &fakeWorkspaces{reg: sampleRegistry(), openErr: tc.err})

			w := postJSON(t, srv, cookie, "/workspaces/open", `{"name":"X"}`)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tc.status, w.Body.String())
			}
			var body apiError
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Error.Code != tc.code {
				t.Errorf("code = %q, want %q", body.Error.Code, tc.code)
			}
			if body.Error.Details["reason"] == "" {
				t.Errorf("the error does not explain itself: %+v", body.Error)
			}
		})
	}
}

// TestWorkspaceLeaveReturnsToThePicker covers the leave route.
func TestWorkspaceLeaveReturnsToThePicker(t *testing.T) {
	fake := &fakeWorkspaces{reg: sampleRegistry()}
	srv, cookie := workspacesServer(t, fake)

	w := postJSON(t, srv, cookie, "/workspaces/leave", ``)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", w.Code, w.Body.String())
	}
	if fake.left != 1 {
		t.Fatalf("Leave called %d times, want 1", fake.left)
	}
}

// TestWorkspaceRoutesRequireAnOrigin proves switching is treated as the
// state-mutating operation it is (R14, acceptance A3).
func TestWorkspaceRoutesRequireAnOrigin(t *testing.T) {
	srv, cookie := workspacesServer(t, &fakeWorkspaces{reg: sampleRegistry()})

	for _, path := range []string{"/workspaces/open", "/workspaces/leave"} {
		r := httptest.NewRequest(http.MethodPost, APIPrefix+path, strings.NewReader(`{"name":"Alpha"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://evil.example")
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden {
			t.Errorf("%s accepted a foreign origin: status %d", path, w.Code)
		}
	}
}

// TestWorkspaceRoutesRequireASession proves the picker is behind the same
// session guard as everything else (ADR-0002).
func TestWorkspaceRoutesRequireASession(t *testing.T) {
	srv, _ := workspacesServer(t, &fakeWorkspaces{reg: sampleRegistry()})

	r := httptest.NewRequest(http.MethodGet, APIPrefix+"/workspaces", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// TestWorkspaceRoutesAbsentWithoutARegistry proves a process started without a
// registry says so, rather than presenting an empty picker that looks like a
// user with no workspaces.
func TestWorkspaceRoutesAbsentWithoutARegistry(t *testing.T) {
	srv, sessions, _ := liveServer(t, map[string]string{"docs/a.md": "# A\n"})
	cookie := bootstrap(t, srv, sessions)

	if code := getJSON(t, srv, cookie, "/workspaces", nil); code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
}
