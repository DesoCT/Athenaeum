package documents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"athenaeum/internal/config"
	"athenaeum/internal/workspace"
)

func service(t *testing.T, files map[string]string) *Service {
	t.Helper()
	return serviceWithConfig(t, `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]
`, files)
}

func serviceWithConfig(t *testing.T, configBody string, files map[string]string) *Service {
	t.Helper()
	dir := t.TempDir()

	for rel, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
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

// writeRaw writes bytes that must not pass through the string fixtures, such as
// CRLF content or invalid UTF-8.
func serviceRaw(t *testing.T, files map[string][]byte) *Service {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	configPath := filepath.Join(dir, config.DefaultFileName)
	if err := os.WriteFile(configPath, []byte("schema_version = 1\nname = \"Fixture\"\ninclude = [\"**/*.md\"]\n"), 0o644); err != nil {
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

func TestReadBasicDocument(t *testing.T) {
	s := service(t, map[string]string{"a.md": "# Title\n\nBody.\n"})

	doc, err := s.Read("a.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if doc.Title != "Title" {
		t.Errorf("Title = %q, want Title", doc.Title)
	}
	if doc.Encoding != EncodingUTF8 {
		t.Errorf("Encoding = %q", doc.Encoding)
	}
	if doc.LineEnding != LineEndingLF {
		t.Errorf("LineEnding = %q, want lf", doc.LineEnding)
	}
	if !strings.HasPrefix(doc.Version, "sha256:") {
		t.Errorf("Version = %q, want a sha256 fingerprint", doc.Version)
	}
	if len(doc.Outline) != 1 {
		t.Errorf("outline has %d headings, want 1", len(doc.Outline))
	}
}

func TestTitlePrecedence(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"front matter wins", "---\ntitle: From front matter\n---\n\n# From heading\n", "From front matter"},
		{"first h1", "## Second level\n\n# First level\n", "First level"},
		{"first heading when no h1", "## Only heading\n", "Only heading"},
		{"file name when headingless", "Just text.\n", "doc"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := service(t, map[string]string{"doc.md": tc.source})
			doc, err := s.Read("doc.md")
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if doc.Title != tc.want {
				t.Errorf("Title = %q, want %q", doc.Title, tc.want)
			}
		})
	}
}

func TestYAMLFrontMatterParsed(t *testing.T) {
	s := service(t, map[string]string{
		"a.md": "---\ntitle: Example\nrelated:\n  - docs/b.md\n---\n\n# Body\n",
	})

	doc, err := s.Read("a.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if doc.FrontMatterFormat != FrontMatterYAML {
		t.Errorf("format = %q, want yaml", doc.FrontMatterFormat)
	}
	if doc.FrontMatter["title"] != "Example" {
		t.Errorf("front matter title = %v", doc.FrontMatter["title"])
	}
	if _, ok := doc.FrontMatter["related"]; !ok {
		t.Error("related field missing from front matter")
	}
}

func TestTOMLFrontMatterParsed(t *testing.T) {
	s := service(t, map[string]string{
		"a.md": "+++\ntitle = \"Example\"\n+++\n\n# Body\n",
	})

	doc, err := s.Read("a.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if doc.FrontMatterFormat != FrontMatterTOML {
		t.Errorf("format = %q, want toml", doc.FrontMatterFormat)
	}
	if doc.FrontMatter["title"] != "Example" {
		t.Errorf("title = %v", doc.FrontMatter["title"])
	}
}

// TestMalformedFrontMatterWarnsButStillReads keeps a typo from making a
// document unopenable.
func TestMalformedFrontMatterWarnsButStillReads(t *testing.T) {
	s := service(t, map[string]string{
		"a.md": "---\ntitle: [unclosed\n---\n\n# Body\n",
	})

	doc, err := s.Read("a.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(doc.Warnings) == 0 {
		t.Error("malformed front matter produced no warning")
	}
	if len(doc.Outline) != 1 || doc.Outline[0].Text != "Body" {
		t.Errorf("body was not parsed after malformed front matter: %+v", doc.Outline)
	}
}

// TestUnterminatedFrontMatterIsBody stops a stray leading "---" swallowing the
// whole document.
func TestUnterminatedFrontMatterIsBody(t *testing.T) {
	s := service(t, map[string]string{"a.md": "---\n\n# Real heading\n"})

	doc, err := s.Read("a.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if doc.FrontMatterFormat != FrontMatterNone {
		t.Errorf("format = %q, want none", doc.FrontMatterFormat)
	}
	if len(doc.Outline) != 1 {
		t.Errorf("outline = %+v, want the heading to survive", doc.Outline)
	}
}

// TestDisabledFrontMatterFormatIsBody honours documents.front_matter.
func TestDisabledFrontMatterFormatIsBody(t *testing.T) {
	s := serviceWithConfig(t, `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]

[documents]
front_matter = ["toml"]
`, map[string]string{"a.md": "---\ntitle: Ignored\n---\n\n# Heading\n"})

	doc, err := s.Read("a.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if doc.FrontMatterFormat != FrontMatterNone {
		t.Errorf("YAML front matter was parsed although only TOML is enabled")
	}
	if doc.Title != "Heading" {
		t.Errorf("Title = %q, want Heading", doc.Title)
	}
}

// TestCRLFDetectedAndNormalised covers the read half of acceptance D3.
func TestCRLFDetectedAndNormalised(t *testing.T) {
	s := serviceRaw(t, map[string][]byte{
		"crlf.md": []byte("# Title\r\n\r\nBody line.\r\n"),
	})

	doc, err := s.Read("crlf.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if doc.LineEnding != LineEndingCRLF {
		t.Errorf("LineEnding = %q, want crlf", doc.LineEnding)
	}
	if strings.Contains(doc.Content, "\r\n") {
		t.Error("content was not normalised to LF for transport")
	}
	if doc.Outline[0].Line != 1 {
		t.Errorf("heading line = %d, want 1", doc.Outline[0].Line)
	}
}

func TestMixedLineEndingsDetected(t *testing.T) {
	s := serviceRaw(t, map[string][]byte{
		"mixed.md": []byte("# Title\r\n\nBody.\n"),
	})

	doc, err := s.Read("mixed.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if doc.LineEnding != LineEndingMixed {
		t.Errorf("LineEnding = %q, want mixed", doc.LineEnding)
	}
}

// TestNonUTF8OpensReadOnly covers acceptance D4.
func TestNonUTF8OpensReadOnly(t *testing.T) {
	// 0xFF is never valid in UTF-8.
	s := serviceRaw(t, map[string][]byte{
		"latin1.md": {0x23, 0x20, 0x54, 0x69, 0x74, 0x6C, 0x65, 0x0A, 0xFF, 0xFE, 0x0A},
	})

	doc, err := s.Read("latin1.md")
	if err != nil {
		t.Fatalf("Read should succeed read-only, got: %v", err)
	}
	if doc.Encoding != EncodingUnknown {
		t.Errorf("Encoding = %q, want unknown", doc.Encoding)
	}
	if !doc.ReadOnly {
		t.Error("a non-UTF-8 file is not marked read-only")
	}
	if doc.Writable {
		t.Error("a non-UTF-8 file is marked writable")
	}
	if len(doc.Warnings) == 0 {
		t.Error("no explanation was attached to the non-UTF-8 document")
	}
}

func TestBOMRecordedAndStripped(t *testing.T) {
	s := serviceRaw(t, map[string][]byte{
		"bom.md": append([]byte{0xEF, 0xBB, 0xBF}, []byte("# Title\n")...),
	})

	doc, err := s.Read("bom.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !doc.HasBOM {
		t.Error("the byte order mark was not recorded")
	}
	if strings.HasPrefix(doc.Content, "\uFEFF") {
		t.Error("the byte order mark was left in the content")
	}
	if doc.Title != "Title" {
		t.Errorf("Title = %q; the BOM broke heading parsing", doc.Title)
	}
}

func TestTooLargeOpensReadOnly(t *testing.T) {
	s := serviceWithConfig(t, `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]

[documents]
max_editable_bytes = 20
`, map[string]string{"big.md": "# Heading\n" + strings.Repeat("x", 100)})

	doc, err := s.Read("big.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !doc.TooLarge || !doc.ReadOnly {
		t.Errorf("too_large=%v read_only=%v, want both true", doc.TooLarge, doc.ReadOnly)
	}
	if len(doc.Warnings) == 0 {
		t.Error("no warning explaining the read-only state")
	}
}

func TestReadRejectsExcludedDocument(t *testing.T) {
	s := serviceWithConfig(t, `
schema_version = 1
name = "Fixture"
include = ["docs/**/*.md"]
`, map[string]string{
		"docs/a.md": "# A\n",
		"secret.md": "# Secret\n",
	})

	if _, err := s.Read("secret.md"); err == nil {
		t.Fatal("an excluded document was read")
	}
	if _, err := s.Read("../outside.md"); err == nil {
		t.Fatal("a traversal ID was read")
	}
}

func TestVersionChangesWithContent(t *testing.T) {
	s := service(t, map[string]string{"a.md": "# One\n"})

	first, err := s.Read("a.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	path := filepath.Join(s.ws.Guard().Root(), "a.md")
	if err := os.WriteFile(path, []byte("# Two\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	second, err := s.Read("a.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if first.Version == second.Version {
		t.Error("the version did not change after the content changed")
	}
}
