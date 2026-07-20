package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"athenaeum/internal/config"
	"athenaeum/internal/workspace"
)

// pngBytes is a minimal valid PNG header; content is never interpreted.
var pngBytes = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3}

func newService(t *testing.T, configBody string, files map[string]string) *Service {
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
	return New(ws)
}

const assetConfig = `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]

[assets]
directory = "assets"
paste_naming = "date-hash"

[security]
writable = ["**/*.md", "assets/**"]
`

// TestStoreWritesUnderAssetDirectory covers acceptance I1.
func TestStoreWritesUnderAssetDirectory(t *testing.T) {
	s := newService(t, assetConfig, map[string]string{"docs/a.md": "# A\n"})

	result, err := s.Store(Request{DocumentID: "docs/a.md", FileName: "photo.png", Content: pngBytes})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	if !strings.HasPrefix(result.AssetID, "assets/") {
		t.Errorf("AssetID = %q, want it under assets/", result.AssetID)
	}
	full := filepath.Join(s.ws.Guard().Root(), filepath.FromSlash(result.AssetID))
	written, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read stored asset: %v", err)
	}
	if string(written) != string(pngBytes) {
		t.Error("stored bytes differ from the input")
	}
}

// TestMarkdownReferenceIsRelativeToDocument keeps links working when the
// workspace moves (constitution C2).
func TestMarkdownReferenceIsRelativeToDocument(t *testing.T) {
	s := newService(t, assetConfig, map[string]string{
		"docs/deep/nested/a.md": "# A\n",
		"top.md":                "# Top\n",
	})

	deep, err := s.Store(Request{DocumentID: "docs/deep/nested/a.md", FileName: "x.png", Content: pngBytes})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if !strings.HasPrefix(deep.RelativePath, "../../../assets/") {
		t.Errorf("RelativePath = %q, want it to climb out of docs/deep/nested", deep.RelativePath)
	}
	if !strings.HasPrefix(deep.Markdown, "![") || !strings.Contains(deep.Markdown, deep.RelativePath) {
		t.Errorf("Markdown = %q", deep.Markdown)
	}

	top, err := s.Store(Request{DocumentID: "top.md", FileName: "y.png", Content: []byte("different")})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if !strings.HasPrefix(top.RelativePath, "assets/") {
		t.Errorf("RelativePath = %q, want assets/... for a root document", top.RelativePath)
	}
}

// TestCollisionIsDetectedBeforeWriting covers acceptance I2. Silent overwrite
// is prohibited.
func TestCollisionIsDetectedBeforeWriting(t *testing.T) {
	s := newService(t, assetConfig, map[string]string{"docs/a.md": "# A\n"})

	first, err := s.Store(Request{
		DocumentID:    "docs/a.md",
		FileName:      "diagram.png",
		Content:       pngBytes,
		PreferredName: "diagram.png",
	})
	if err != nil {
		t.Fatalf("first Store: %v", err)
	}

	original, _ := os.ReadFile(filepath.Join(s.ws.Guard().Root(), filepath.FromSlash(first.AssetID)))

	_, err = s.Store(Request{
		DocumentID:    "docs/a.md",
		FileName:      "diagram.png",
		Content:       []byte("completely different bytes"),
		PreferredName: "diagram.png",
	})
	if err == nil {
		t.Fatal("a colliding asset was written silently")
	}
	if CodeOf(err) != CodeCollision {
		t.Fatalf("code = %q, want %q", CodeOf(err), CodeCollision)
	}

	// The existing file must be untouched.
	after, _ := os.ReadFile(filepath.Join(s.ws.Guard().Root(), filepath.FromSlash(first.AssetID)))
	if string(after) != string(original) {
		t.Fatal("the colliding write modified the existing asset")
	}

	var assetErr *Error
	if !AsError(err, &assetErr) || assetErr.Suggestion == "" {
		t.Fatal("no alternative name was suggested")
	}
	if assetErr.Suggestion == "diagram.png" {
		t.Error("the suggestion collides too")
	}
}

// TestExplicitOverwriteIsHonoured is the other half of I2.
func TestExplicitOverwriteIsHonoured(t *testing.T) {
	s := newService(t, assetConfig, map[string]string{"docs/a.md": "# A\n"})

	if _, err := s.Store(Request{
		DocumentID: "docs/a.md", FileName: "x.png", Content: pngBytes, PreferredName: "x.png",
	}); err != nil {
		t.Fatalf("first Store: %v", err)
	}

	replacement := []byte("replacement bytes")
	result, err := s.Store(Request{
		DocumentID: "docs/a.md", FileName: "x.png", Content: replacement,
		PreferredName: "x.png", Overwrite: true,
	})
	if err != nil {
		t.Fatalf("overwrite Store: %v", err)
	}

	written, _ := os.ReadFile(filepath.Join(s.ws.Guard().Root(), filepath.FromSlash(result.AssetID)))
	if string(written) != string(replacement) {
		t.Errorf("content = %q, want the replacement", written)
	}
}

func TestUnsupportedTypeRejected(t *testing.T) {
	s := newService(t, assetConfig, map[string]string{"docs/a.md": "# A\n"})

	for _, name := range []string{"payload.exe", "script.sh", "notes.pdf", "noextension"} {
		_, err := s.Store(Request{DocumentID: "docs/a.md", FileName: name, Content: pngBytes})
		if err == nil {
			t.Errorf("Store(%q) was accepted", name)
			continue
		}
		if CodeOf(err) != CodeUnsupported {
			t.Errorf("Store(%q) code = %q, want %q", name, CodeOf(err), CodeUnsupported)
		}
	}
}

// TestPreferredNameCannotEscape guards the boundary on a crafted name.
func TestPreferredNameCannotEscape(t *testing.T) {
	s := newService(t, assetConfig, map[string]string{"docs/a.md": "# A\n"})

	for _, name := range []string{"../escape.png", "../../etc/evil.png", "/absolute.png", "sub/dir.png"} {
		result, err := s.Store(Request{
			DocumentID: "docs/a.md", FileName: "x.png", Content: pngBytes, PreferredName: name,
		})
		if err != nil {
			continue // Rejected outright is fine.
		}
		// If accepted, the name must have been flattened into the directory.
		if strings.Contains(result.AssetID, "..") {
			t.Errorf("PreferredName %q produced escaping asset ID %q", name, result.AssetID)
		}
		if !strings.HasPrefix(result.AssetID, "assets/") {
			t.Errorf("PreferredName %q escaped the asset directory: %q", name, result.AssetID)
		}
	}
}

// TestOutsideWriteBoundaryRejected covers the case where the asset directory
// is not writable by configuration.
func TestOutsideWriteBoundaryRejected(t *testing.T) {
	s := newService(t, `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]

[assets]
directory = "assets"

[security]
writable = ["docs/**/*.md"]
`, map[string]string{"docs/a.md": "# A\n"})

	_, err := s.Store(Request{DocumentID: "docs/a.md", FileName: "x.png", Content: pngBytes})
	if err == nil {
		t.Fatal("an asset was written outside the configured write boundary")
	}
	if CodeOf(err) != CodeOutsideBoundary {
		t.Fatalf("code = %q, want %q", CodeOf(err), CodeOutsideBoundary)
	}
}

func TestSanitiseName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Photo Of Thing.png", "photo-of-thing.png"},
		{"already-fine.png", "already-fine.png"},
		{"UPPER.PNG", "upper.png"},
		{"weird!!chars??.png", "weirdchars.png"},
		{"../../escape.png", "escape.png"},
		{"/absolute/path.png", "path.png"},
		{"!!!.png", "asset.png"},
	}
	for _, tc := range tests {
		if got := sanitiseName(tc.in, ".png"); got != tc.want {
			t.Errorf("sanitiseName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDateHashNamesAreStableForSameContent(t *testing.T) {
	first := generateName("date-hash", "a.png", ".png", pngBytes)
	second := generateName("date-hash", "b.png", ".png", pngBytes)
	if first != second {
		t.Errorf("identical content produced different names: %q and %q", first, second)
	}

	different := generateName("date-hash", "a.png", ".png", []byte("other"))
	if different == first {
		t.Error("different content produced the same name")
	}
}

func TestEmptyAssetRejected(t *testing.T) {
	s := newService(t, assetConfig, map[string]string{"docs/a.md": "# A\n"})
	if _, err := s.Store(Request{DocumentID: "docs/a.md", FileName: "x.png"}); err == nil {
		t.Fatal("an empty asset was accepted")
	}
}
