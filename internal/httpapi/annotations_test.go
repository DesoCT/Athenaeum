package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"athenaeum/internal/annotations"
	"athenaeum/internal/documents"
)

// docAdapter mirrors the app's documents-to-annotations adapter so handler
// tests exercise the real store against real document content (repair included).
type docAdapter struct{ docs *documents.Service }

func (d docAdapter) Source(id string) (string, []annotations.Heading, error) {
	doc, err := d.docs.Read(id)
	if err != nil {
		return "", nil, err
	}
	hs := make([]annotations.Heading, len(doc.Outline))
	for i, h := range doc.Outline {
		hs[i] = annotations.Heading{Path: h.Path, Line: h.Line}
	}
	return doc.Content, hs, nil
}

// liveServerWithAnnotations is liveServer plus an annotation service whose two
// storage roots are temporary, so tests never touch the developer's real data.
func liveServerWithAnnotations(t *testing.T, files map[string]string) (*Server, *http.Cookie, string) {
	t.Helper()
	srv, sessions, dir := liveServer(t, files)
	cookie := bootstrap(t, srv, sessions)

	bound := *srv.current()
	bound.Annotations = annotations.NewService(annotations.Options{
		PersonalDir: filepath.Join(t.TempDir(), "annotations"),
		SharedDir:   filepath.Join(dir, ".athenaeum", "shared", "annotations"),
		Docs:        docAdapter{docs: bound.Documents},
	})
	srv.Bind(&bound)
	return srv, cookie, dir
}

// annReq issues an authenticated, same-origin annotation request.
func annReq(t *testing.T, srv *Server, cookie *http.Cookie, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = strings.NewReader(string(data))
	} else {
		reader = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, reader)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func textAnchorFor() annotations.Anchor {
	return annotations.Anchor{
		Type:      annotations.AnchorText,
		Exact:     "disposable cache",
		StartLine: 2,
		EndLine:   2,
		Prefix:    "a ",
		Suffix:    ".",
	}
}

func TestAnnotationCreateAndList(t *testing.T) {
	srv, cookie, _ := liveServerWithAnnotations(t, map[string]string{
		"docs/a.md": "# Title\nThe index is a disposable cache.\n",
	})

	w := annReq(t, srv, cookie, http.MethodPost, APIPrefix+"/annotations", annotationCreateRequest{
		DocumentID: "docs/a.md",
		Kind:       annotations.KindComment,
		Visibility: annotations.VisibilityPersonal,
		Status:     annotations.StatusOpen,
		Body:       "clarify authority",
		Anchor:     textAnchorFor(),
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body %s)", w.Code, w.Body.String())
	}
	var created annotationResult
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Revision != 1 || created.Annotation.ID == "" {
		t.Fatalf("create result = %+v", created)
	}

	lw := annReq(t, srv, cookie, http.MethodGet, APIPrefix+"/annotations?document=docs/a.md", nil)
	if lw.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", lw.Code)
	}
	var list annotations.ListResult
	if err := json.Unmarshal(lw.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Annotations) != 1 {
		t.Fatalf("list count = %d, want 1", len(list.Annotations))
	}
	if got := list.Annotations[0].Anchor.State; got != annotations.StateAnchored {
		t.Fatalf("anchor state = %s, want anchored", got)
	}
}

func TestAnnotationStaleRevisionConflicts(t *testing.T) {
	srv, cookie, _ := liveServerWithAnnotations(t, map[string]string{
		"docs/a.md": "# Title\nThe index is a disposable cache.\n",
	})
	first := annReq(t, srv, cookie, http.MethodPost, APIPrefix+"/annotations", annotationCreateRequest{
		DocumentID: "docs/a.md", Kind: annotations.KindComment, Visibility: annotations.VisibilityShared,
		Body: "one", Anchor: textAnchorFor(),
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("first create = %d", first.Code)
	}

	// Same expected revision as the first create (0): the sidecar has already
	// advanced, so this must conflict rather than overwrite.
	second := annReq(t, srv, cookie, http.MethodPost, APIPrefix+"/annotations", annotationCreateRequest{
		DocumentID: "docs/a.md", Kind: annotations.KindComment, Visibility: annotations.VisibilityShared,
		Body: "two", Anchor: textAnchorFor(), ExpectedRevision: 0,
	})
	if second.Code != http.StatusConflict {
		t.Fatalf("stale create status = %d, want 409 (body %s)", second.Code, second.Body.String())
	}
	var conflict annotationConflictResponse
	if err := json.Unmarshal(second.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conflict.Error.Code != "ANNOTATION_CONFLICT" || conflict.Conflict.CurrentRevision != 1 {
		t.Fatalf("conflict payload = %+v", conflict)
	}
}

func TestAnnotationValidationRejected(t *testing.T) {
	srv, cookie, _ := liveServerWithAnnotations(t, map[string]string{"docs/a.md": "# Title\n"})
	w := annReq(t, srv, cookie, http.MethodPost, APIPrefix+"/annotations", annotationCreateRequest{
		DocumentID: "docs/a.md", Kind: "scribble", Visibility: annotations.VisibilityShared,
		Body: "x", Anchor: annotations.Anchor{Type: annotations.AnchorDocument},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_ANNOTATION") {
		t.Fatalf("body = %s, want INVALID_ANNOTATION", w.Body.String())
	}
}

func TestAnnotationUpdateAndDelete(t *testing.T) {
	srv, cookie, _ := liveServerWithAnnotations(t, map[string]string{
		"docs/a.md": "# Title\nThe index is a disposable cache.\n",
	})
	cw := annReq(t, srv, cookie, http.MethodPost, APIPrefix+"/annotations", annotationCreateRequest{
		DocumentID: "docs/a.md", Kind: annotations.KindComment, Visibility: annotations.VisibilityShared,
		Body: "open", Anchor: textAnchorFor(),
	})
	var created annotationResult
	_ = json.Unmarshal(cw.Body.Bytes(), &created)

	resolved := annotations.StatusResolved
	uw := annReq(t, srv, cookie, http.MethodPatch, APIPrefix+"/annotations/"+created.Annotation.ID, annotationUpdateRequest{
		DocumentID: "docs/a.md", Visibility: annotations.VisibilityShared, Status: &resolved, ExpectedRevision: created.Revision,
	})
	if uw.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 (body %s)", uw.Code, uw.Body.String())
	}
	var updated annotationResult
	_ = json.Unmarshal(uw.Body.Bytes(), &updated)
	if updated.Annotation.Status != annotations.StatusResolved {
		t.Fatalf("status not resolved: %+v", updated.Annotation)
	}

	dw := annReq(t, srv, cookie, http.MethodDelete, APIPrefix+"/annotations/"+created.Annotation.ID, annotationDeleteRequest{
		DocumentID: "docs/a.md", Visibility: annotations.VisibilityShared, ExpectedRevision: updated.Revision,
	})
	if dw.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", dw.Code)
	}
}

func TestAnnotationOverview(t *testing.T) {
	srv, cookie, _ := liveServerWithAnnotations(t, map[string]string{
		"docs/a.md": "# Title\nThe index is a disposable cache.\n",
	})
	// One open comment and one pin.
	annReq(t, srv, cookie, http.MethodPost, APIPrefix+"/annotations", annotationCreateRequest{
		DocumentID: "docs/a.md", Kind: annotations.KindComment, Visibility: annotations.VisibilityShared,
		Body: "open item", Anchor: textAnchorFor(),
	})
	annReq(t, srv, cookie, http.MethodPost, APIPrefix+"/annotations", annotationCreateRequest{
		DocumentID: "docs/a.md", Kind: annotations.KindPin, Visibility: annotations.VisibilityPersonal,
		Anchor: annotations.Anchor{Type: annotations.AnchorDocument},
	})

	w := annReq(t, srv, cookie, http.MethodGet, APIPrefix+"/annotations/overview", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("overview status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var ov annotations.Overview
	if err := json.Unmarshal(w.Body.Bytes(), &ov); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if len(ov.Pins) != 1 || len(ov.Unresolved) != 1 {
		t.Fatalf("overview = %+v, want 1 pin and 1 unresolved", ov)
	}
}

func TestAnnotationRequiresSession(t *testing.T) {
	srv, _, _ := liveServerWithAnnotations(t, map[string]string{"docs/a.md": "# Title\n"})
	// No cookie: the guard must reject before any handler runs.
	r := httptest.NewRequest(http.MethodGet, APIPrefix+"/annotations?document=docs/a.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", w.Code)
	}
}
