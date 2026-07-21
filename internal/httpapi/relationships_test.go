package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"athenaeum/internal/relationships"
)

func liveServerWithRelationships(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	srv, sessions, _ := liveServer(t, map[string]string{
		"docs/a.md": "# A\n\nsee [bee](b.md).\n",
		"docs/b.md": "# B\n",
	})
	cookie := bootstrap(t, srv, sessions)

	bound := *srv.current()
	bound.Relationships = relationships.NewService(relationships.Options{
		Workspace: bound.Workspace,
		Documents: bound.Documents,
	})
	srv.Bind(&bound)
	return srv, cookie
}

func getRelationships(t *testing.T, srv *Server, cookie *http.Cookie, path string) *relationships.Result {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200 (body %s)", path, w.Code, w.Body.String())
	}
	var result relationships.Result
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return &result
}

func TestRelationshipsOutgoingAndBacklinks(t *testing.T) {
	srv, cookie := liveServerWithRelationships(t)

	out := getRelationships(t, srv, cookie, APIPrefix+"/relationships/docs/a.md")
	if len(out.Outgoing) != 1 || out.Outgoing[0].DocumentID != "docs/b.md" || out.Outgoing[0].Source != relationships.SourceMarkdown {
		t.Fatalf("outgoing = %+v, want one markdown link to docs/b.md", out.Outgoing)
	}

	back := getRelationships(t, srv, cookie, APIPrefix+"/relationships/docs/b.md")
	if len(back.Backlinks) != 1 || back.Backlinks[0].DocumentID != "docs/a.md" {
		t.Fatalf("backlinks = %+v, want one from docs/a.md", back.Backlinks)
	}
}

func TestRelationshipsRequiresSession(t *testing.T) {
	srv, _ := liveServerWithRelationships(t)
	r := httptest.NewRequest(http.MethodGet, APIPrefix+"/relationships/docs/a.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d, want 401", w.Code)
	}
}
