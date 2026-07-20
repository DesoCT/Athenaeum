package httpapi

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"athenaeum/internal/security"
)

const testOrigin = "http://127.0.0.1:7777"

func newTestServer(t *testing.T) (*Server, *security.SessionManager) {
	t.Helper()
	sessions, err := security.NewSessionManager(time.Hour, false)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	frontend := fstest.MapFS{
		"index.html":     &fstest.MapFile{Data: []byte("<!doctype html><title>Map Room</title>")},
		"assets/app.css": &fstest.MapFile{Data: []byte("body{}")},
	}
	srv := New(Options{
		Sessions:      sessions,
		Origins:       security.NewOriginPolicy([]string{testOrigin}),
		Frontend:      fs.FS(frontend),
		FrontendBuilt: true,
		Version:       "test",
		WorkspaceName: "Fixture",
	})
	return srv, sessions
}

// bootstrap performs the launch-token exchange and returns the session cookie.
func bootstrap(t *testing.T, srv *Server, sessions *security.SessionManager) *http.Cookie {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, BootstrapPath+"?t="+sessions.BootstrapToken(), nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("bootstrap status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if loc := w.Header().Get("Location"); loc != "/" {
		t.Fatalf("bootstrap redirected to %q, want / so the token leaves the address bar", loc)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("bootstrap issued no session cookie")
	}
	return cookies[0]
}

// TestUnauthenticatedRequestsRejected covers acceptance A3: the server rejects
// requests that carry no valid session.
func TestUnauthenticatedRequestsRejected(t *testing.T) {
	srv, _ := newTestServer(t)

	for _, path := range []string{"/", "/index.html", APIPrefix + "/health"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestBootstrapRejectsBadToken(t *testing.T) {
	srv, _ := newTestServer(t)

	r := httptest.NewRequest(http.MethodGet, BootstrapPath+"?t=not-the-token", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("a rejected bootstrap issued a cookie")
	}
}

func TestBootstrapDoesNotEchoToken(t *testing.T) {
	srv, _ := newTestServer(t)
	const supplied = "sensitive-token-value"

	r := httptest.NewRequest(http.MethodGet, BootstrapPath+"?t="+supplied, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if strings.Contains(w.Body.String(), supplied) {
		t.Fatal("the rejection response echoed the supplied token")
	}
}

func TestHealthAfterBootstrap(t *testing.T) {
	srv, sessions := newTestServer(t)
	cookie := bootstrap(t, srv, sessions)

	r := httptest.NewRequest(http.MethodGet, APIPrefix+"/health", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, http.StatusOK, w.Body.String())
	}
	var got healthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if got.Status != "ok" || got.Workspace != "Fixture" || got.Version != "test" {
		t.Fatalf("health response = %+v", got)
	}
	if got.Remote {
		t.Error("health reported remote mode for a local server")
	}
}

func TestFrontendServedAfterBootstrap(t *testing.T) {
	srv, sessions := newTestServer(t)
	cookie := bootstrap(t, srv, sessions)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "Map Room") {
		t.Fatalf("index.html was not served; body = %q", w.Body.String())
	}
}

// TestUnknownRouteFallsBackToIndex keeps client-side routes working on a hard
// refresh without exposing anything outside the embedded filesystem.
func TestUnknownRouteFallsBackToIndex(t *testing.T) {
	srv, sessions := newTestServer(t)
	cookie := bootstrap(t, srv, sessions)

	r := httptest.NewRequest(http.MethodGet, "/some/client/route", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "Map Room") {
		t.Error("client route did not fall back to index.html")
	}
}

func TestUnknownAPIRouteIs404(t *testing.T) {
	srv, sessions := newTestServer(t)
	cookie := bootstrap(t, srv, sessions)

	r := httptest.NewRequest(http.MethodGet, APIPrefix+"/nope", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	var body apiError
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "NOT_FOUND" {
		t.Fatalf("error code = %q, want NOT_FOUND", body.Error.Code)
	}
}

// TestMutatingRequestRequiresAllowedOrigin covers the CSRF control in
// spec 03 section 10.
func TestMutatingRequestRequiresAllowedOrigin(t *testing.T) {
	srv, sessions := newTestServer(t)
	cookie := bootstrap(t, srv, sessions)

	r := httptest.NewRequest(http.MethodPost, APIPrefix+"/health", nil)
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestSecurityHeadersAlwaysPresent(t *testing.T) {
	srv, _ := newTestServer(t)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no Content-Security-Policy header")
	}
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'", "connect-src 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP is missing %q; got %q", want, csp)
		}
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options: nosniff")
	}
}

func TestFrontendNotBuiltIsExplicit(t *testing.T) {
	sessions, err := security.NewSessionManager(time.Hour, false)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	srv := New(Options{
		Sessions:      sessions,
		Origins:       security.NewOriginPolicy([]string{testOrigin}),
		Frontend:      fs.FS(fstest.MapFS{}),
		FrontendBuilt: false,
		Version:       "test",
	})
	cookie := bootstrap(t, srv, sessions)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(w.Body.String(), "FRONTEND_NOT_BUILT") {
		t.Fatalf("unhelpful body: %q", w.Body.String())
	}
}

// TestRemoteImagesAllowedWhenConfigured is the regression test for a bug that
// made every remote image in every document fail to load.
//
// The Content-Security-Policy hard-coded img-src 'self' data:, so the browser
// refused remote images before issuing a request — which also made opening one
// directly fail, and made assets.allow_remote silently meaningless. R3 requires
// remote images to render with a visible indicator.
func TestRemoteImagesAllowedWhenConfigured(t *testing.T) {
	sessions, err := security.NewSessionManager(time.Hour, false)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	srv := New(Options{
		Sessions:          sessions,
		Origins:           security.NewOriginPolicy([]string{testOrigin}),
		Frontend:          fs.FS(fstest.MapFS{}),
		AllowRemoteAssets: true,
	})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "img-src 'self' data: https: http:") {
		t.Fatalf("remote images are still blocked by the policy: %q", csp)
	}
	// Widening images must not have widened anything else.
	for _, directive := range []string{
		"default-src 'self'", "connect-src 'self'", "frame-ancestors 'none'",
	} {
		if !strings.Contains(csp, directive) {
			t.Errorf("policy lost %q: %q", directive, csp)
		}
	}
}

// TestRemoteImagesBlockedWhenDisabled covers --safe-mode, which clears the
// flag, and any workspace setting allow_remote = false.
func TestRemoteImagesBlockedWhenDisabled(t *testing.T) {
	sessions, err := security.NewSessionManager(time.Hour, false)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	srv := New(Options{
		Sessions:          sessions,
		Origins:           security.NewOriginPolicy([]string{testOrigin}),
		Frontend:          fs.FS(fstest.MapFS{}),
		AllowRemoteAssets: false,
	})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	csp := w.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "http:") || strings.Contains(csp, "https:") {
		t.Fatalf("remote images are permitted although they are disabled: %q", csp)
	}
	if !strings.Contains(csp, "img-src 'self' data:") {
		t.Fatalf("local and inline images should still load: %q", csp)
	}
}
