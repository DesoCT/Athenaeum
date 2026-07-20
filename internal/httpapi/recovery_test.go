package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func putRecovery(t *testing.T, srv *Server, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, APIPrefix+"/recovery", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func listRecovery(t *testing.T, srv *Server, cookie *http.Cookie) recoveryListResponse {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, APIPrefix+"/recovery", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	var body recoveryListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// TestRecoveryRoundTrip covers acceptance E3 through the API: a buffer is
// stored, offered on a later request, and removed only when asked.
func TestRecoveryRoundTrip(t *testing.T) {
	srv, sessions, _ := liveServerWithRecovery(t, map[string]string{"docs/a.md": "# One\n"})
	cookie := bootstrap(t, srv, sessions)

	w := putRecovery(t, srv, cookie, `{"document_id":"docs/a.md","content":"# Unsaved\n","base_version":"sha256:x"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("put status = %d, want 204 (body %s)", w.Code, w.Body.String())
	}

	// Offered, and repeated listing must not consume it.
	for i := 0; i < 2; i++ {
		body := listRecovery(t, srv, cookie)
		if len(body.Buffers) != 1 || body.Buffers[0].Content != "# Unsaved\n" {
			t.Fatalf("list %d returned %+v", i, body.Buffers)
		}
	}

	r := httptest.NewRequest(http.MethodDelete, APIPrefix+"/recovery/docs/a.md", nil)
	r.Header.Set("Origin", testOrigin)
	r.AddCookie(cookie)
	dw := httptest.NewRecorder()
	srv.ServeHTTP(dw, r)
	if dw.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", dw.Code)
	}
	if body := listRecovery(t, srv, cookie); len(body.Buffers) != 0 {
		t.Fatalf("buffers remain after an explicit discard: %+v", body.Buffers)
	}
}

// TestRecoveryRejectsUnknownDocument stops recovery being used to write
// arbitrary text into user state.
func TestRecoveryRejectsUnknownDocument(t *testing.T) {
	srv, sessions, _ := liveServerWithRecovery(t, map[string]string{"docs/a.md": "# One\n"})
	cookie := bootstrap(t, srv, sessions)

	w := putRecovery(t, srv, cookie, `{"document_id":"nowhere.md","content":"x"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestRecoveryRequiresSessionAndOrigin(t *testing.T) {
	srv, sessions, _ := liveServerWithRecovery(t, map[string]string{"docs/a.md": "# One\n"})
	cookie := bootstrap(t, srv, sessions)

	// No session.
	r := httptest.NewRequest(http.MethodPut, APIPrefix+"/recovery", strings.NewReader(`{"document_id":"docs/a.md","content":"x"}`))
	r.Header.Set("Origin", testOrigin)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated put = %d, want 401", w.Code)
	}

	// Session but foreign origin.
	r2 := httptest.NewRequest(http.MethodPut, APIPrefix+"/recovery", strings.NewReader(`{"document_id":"docs/a.md","content":"x"}`))
	r2.Header.Set("Origin", "https://evil.example")
	r2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, r2)
	if w2.Code != http.StatusForbidden {
		t.Errorf("cross-origin put = %d, want 403", w2.Code)
	}
}

func TestRecoveryRejectsUnknownFields(t *testing.T) {
	srv, sessions, _ := liveServerWithRecovery(t, map[string]string{"docs/a.md": "# One\n"})
	cookie := bootstrap(t, srv, sessions)

	w := putRecovery(t, srv, cookie, `{"document_id":"docs/a.md","content":"x","nonsense":true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
