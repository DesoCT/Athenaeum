package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"athenaeum/internal/notes"
)

func liveServerWithNotes(t *testing.T) (*Server, *http.Cookie, string) {
	t.Helper()
	srv, sessions, dir := liveServer(t, map[string]string{"docs/a.md": "# A\n"})
	cookie := bootstrap(t, srv, sessions)

	bound := *srv.current()
	bound.Notes = notes.NewService(notes.Options{
		PersonalDir: filepath.Join(t.TempDir(), "notes"),
		SharedDir:   filepath.Join(dir, ".athenaeum", "shared", "notes"),
	})
	srv.Bind(&bound)
	return srv, cookie, dir
}

func noteReq(t *testing.T, srv *Server, cookie *http.Cookie, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	reader := strings.NewReader("")
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = strings.NewReader(string(data))
	}
	r := httptest.NewRequest(method, path, reader)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func TestNoteCreateReadListUpdateDelete(t *testing.T) {
	srv, cookie, dir := liveServerWithNotes(t)

	cw := noteReq(t, srv, cookie, http.MethodPost, APIPrefix+"/notes", noteCreateRequest{
		Title:      "Design review",
		Visibility: notes.VisibilityShared,
		Body:       "Body here.",
		Links:      []notes.Link{{Document: "docs/a.md", Heading: "A"}},
	})
	if cw.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body %s)", cw.Code, cw.Body.String())
	}
	var created notes.Note
	if err := json.Unmarshal(cw.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == "" || created.Version == "" {
		t.Fatalf("create result missing id/version: %+v", created)
	}

	// The shared note is committable and lives under the workspace (G2 rule).
	if _, err := os.Stat(filepath.Join(dir, ".athenaeum", "shared", "notes", created.ID+".md")); err != nil {
		t.Fatalf("shared note not under workspace: %v", err)
	}

	// List returns it.
	lw := noteReq(t, srv, cookie, http.MethodGet, APIPrefix+"/notes", nil)
	var listed struct {
		Notes []notes.Summary `json:"notes"`
	}
	_ = json.Unmarshal(lw.Body.Bytes(), &listed)
	if len(listed.Notes) != 1 || listed.Notes[0].Title != "Design review" {
		t.Fatalf("list = %+v", listed.Notes)
	}

	// Update the body with the correct version.
	newBody := "Revised."
	uw := noteReq(t, srv, cookie, http.MethodPut, APIPrefix+"/notes/"+created.ID, noteUpdateRequest{
		Visibility: notes.VisibilityShared, Body: &newBody, ExpectedVersion: created.Version,
	})
	if uw.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 (body %s)", uw.Code, uw.Body.String())
	}

	// Delete it.
	dw := noteReq(t, srv, cookie, http.MethodDelete,
		APIPrefix+"/notes/"+created.ID+"?visibility=shared", nil)
	if dw.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", dw.Code)
	}
	rw := noteReq(t, srv, cookie, http.MethodGet,
		APIPrefix+"/notes/"+created.ID+"?visibility=shared", nil)
	if rw.Code != http.StatusNotFound {
		t.Fatalf("read after delete = %d, want 404", rw.Code)
	}
}

func TestNoteStaleUpdateConflicts(t *testing.T) {
	srv, cookie, _ := liveServerWithNotes(t)
	cw := noteReq(t, srv, cookie, http.MethodPost, APIPrefix+"/notes", noteCreateRequest{
		Title: "T", Visibility: notes.VisibilityShared, Body: "one",
	})
	var created notes.Note
	_ = json.Unmarshal(cw.Body.Bytes(), &created)

	body := "two"
	w := noteReq(t, srv, cookie, http.MethodPut, APIPrefix+"/notes/"+created.ID, noteUpdateRequest{
		Visibility: notes.VisibilityShared, Body: &body, ExpectedVersion: "sha256:stale",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("stale update = %d, want 409", w.Code)
	}
	if !strings.Contains(w.Body.String(), "NOTE_CONFLICT") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestNoteValidationAndAuth(t *testing.T) {
	srv, cookie, _ := liveServerWithNotes(t)
	// Missing title → 400.
	w := noteReq(t, srv, cookie, http.MethodPost, APIPrefix+"/notes", noteCreateRequest{
		Title: "  ", Visibility: notes.VisibilityShared, Body: "x",
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_NOTE") {
		t.Fatalf("blank title = %d body %s", w.Code, w.Body.String())
	}

	// Unauthenticated list → 401.
	r := httptest.NewRequest(http.MethodGet, APIPrefix+"/notes", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth list = %d, want 401", rec.Code)
	}
}
