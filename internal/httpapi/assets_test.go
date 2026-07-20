package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"athenaeum/internal/assets"
)

func postAsset(t *testing.T, srv *Server, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, APIPrefix+"/assets", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

// TestAssetStoreRoundTrip covers acceptance I1 through the API.
func TestAssetStoreRoundTrip(t *testing.T) {
	srv, sessions, dir := liveServerWithAssets(t)
	cookie := bootstrap(t, srv, sessions)

	content := base64.StdEncoding.EncodeToString([]byte("fake png bytes"))
	body, _ := json.Marshal(assetRequest{
		DocumentID: "docs/a.md", FileName: "photo.png", Content: content,
	})

	w := postAsset(t, srv, cookie, string(body))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", w.Code, w.Body.String())
	}

	var result assets.Result
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(result.AssetID, "assets/") {
		t.Errorf("AssetID = %q", result.AssetID)
	}
	if !strings.Contains(result.Markdown, result.RelativePath) {
		t.Errorf("Markdown %q does not reference %q", result.Markdown, result.RelativePath)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(result.AssetID))); err != nil {
		t.Fatalf("asset not on disk: %v", err)
	}
}

// TestAssetCollisionReturnsSuggestion covers acceptance I2.
func TestAssetCollisionReturnsSuggestion(t *testing.T) {
	srv, sessions, dir := liveServerWithAssets(t)
	cookie := bootstrap(t, srv, sessions)

	first, _ := json.Marshal(assetRequest{
		DocumentID: "docs/a.md", FileName: "x.png", PreferredName: "x.png",
		Content: base64.StdEncoding.EncodeToString([]byte("original")),
	})
	if w := postAsset(t, srv, cookie, string(first)); w.Code != http.StatusCreated {
		t.Fatalf("first status = %d", w.Code)
	}

	second, _ := json.Marshal(assetRequest{
		DocumentID: "docs/a.md", FileName: "x.png", PreferredName: "x.png",
		Content: base64.StdEncoding.EncodeToString([]byte("replacement")),
	})
	w := postAsset(t, srv, cookie, string(second))
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}

	var conflict assetConflictResponse
	if err := json.Unmarshal(w.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if conflict.Suggestion == "" || conflict.Suggestion == "x.png" {
		t.Errorf("suggestion = %q, want a free alternative", conflict.Suggestion)
	}

	// The original must be untouched.
	onDisk, _ := os.ReadFile(filepath.Join(dir, "assets", "x.png"))
	if string(onDisk) != "original" {
		t.Fatalf("the colliding request modified the asset: %q", onDisk)
	}
}

func TestAssetRequiresSessionAndOrigin(t *testing.T) {
	srv, sessions, _ := liveServerWithAssets(t)
	cookie := bootstrap(t, srv, sessions)
	body, _ := json.Marshal(assetRequest{
		DocumentID: "docs/a.md", FileName: "x.png",
		Content: base64.StdEncoding.EncodeToString([]byte("x")),
	})

	r := httptest.NewRequest(http.MethodPost, APIPrefix+"/assets", strings.NewReader(string(body)))
	r.Header.Set("Origin", testOrigin)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated = %d, want 401", w.Code)
	}

	r2 := httptest.NewRequest(http.MethodPost, APIPrefix+"/assets", strings.NewReader(string(body)))
	r2.Header.Set("Origin", "https://evil.example")
	r2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, r2)
	if w2.Code != http.StatusForbidden {
		t.Errorf("cross-origin = %d, want 403", w2.Code)
	}
}

func TestAssetRejectsUnsupportedType(t *testing.T) {
	srv, sessions, _ := liveServerWithAssets(t)
	cookie := bootstrap(t, srv, sessions)

	body, _ := json.Marshal(assetRequest{
		DocumentID: "docs/a.md", FileName: "payload.exe",
		Content: base64.StdEncoding.EncodeToString([]byte("MZ")),
	})
	if w := postAsset(t, srv, cookie, string(body)); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestAssetRejectsUnknownDocument(t *testing.T) {
	srv, sessions, _ := liveServerWithAssets(t)
	cookie := bootstrap(t, srv, sessions)

	body, _ := json.Marshal(assetRequest{
		DocumentID: "nowhere.md", FileName: "x.png",
		Content: base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if w := postAsset(t, srv, cookie, string(body)); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}
