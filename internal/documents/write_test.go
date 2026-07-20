package documents

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writableService(t *testing.T, files map[string]string) *Service {
	t.Helper()
	return serviceWithConfig(t, `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]

[security]
writable = ["**/*.md"]
`, files)
}

func onDisk(t *testing.T, s *Service, id string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(s.ws.Guard().Root(), filepath.FromSlash(id)))
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return string(raw)
}

// TestWriteRoundTrip covers the persistence half of acceptance D1.
func TestWriteRoundTrip(t *testing.T) {
	s := writableService(t, map[string]string{"a.md": "# One\n"})

	before, err := s.Read("a.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	result, err := s.Write(WriteRequest{
		ID:              "a.md",
		Content:         "# One\n\nAdded a paragraph.\n",
		ExpectedVersion: before.Version,
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := onDisk(t, s, "a.md"); got != "# One\n\nAdded a paragraph.\n" {
		t.Fatalf("on-disk content = %q", got)
	}

	after, err := s.Read("a.md")
	if err != nil {
		t.Fatalf("Read after write: %v", err)
	}
	if after.Version != result.Version {
		t.Errorf("returned version %q but a fresh read reports %q", result.Version, after.Version)
	}
	if after.Version == before.Version {
		t.Error("the version did not change after a write")
	}
}

// TestStaleVersionRejected covers the optimistic concurrency rule in
// spec 02 section 5.
func TestStaleVersionRejected(t *testing.T) {
	s := writableService(t, map[string]string{"a.md": "# One\n"})

	before, err := s.Read("a.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	// Something else changes the file.
	path := filepath.Join(s.ws.Guard().Root(), "a.md")
	if err := os.WriteFile(path, []byte("# Changed externally\n"), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	_, err = s.Write(WriteRequest{
		ID:              "a.md",
		Content:         "# My local edit\n",
		ExpectedVersion: before.Version,
	})
	if err == nil {
		t.Fatal("a stale write was accepted")
	}
	if WriteCodeOf(err) != CodeConflict {
		t.Fatalf("code = %q, want %q", WriteCodeOf(err), CodeConflict)
	}

	// The external content must survive untouched (R6).
	if got := onDisk(t, s, "a.md"); got != "# Changed externally\n" {
		t.Fatalf("the conflicting write modified the file: %q", got)
	}
}

// TestConflictCarriesBothSides lets the UI show a comparison without a second
// request that could race again (R6).
func TestConflictCarriesBothSides(t *testing.T) {
	s := writableService(t, map[string]string{"a.md": "# One\n"})
	before, _ := s.Read("a.md")

	path := filepath.Join(s.ws.Guard().Root(), "a.md")
	if err := os.WriteFile(path, []byte("# Disk version\n"), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	_, err := s.Write(WriteRequest{ID: "a.md", Content: "# Local\n", ExpectedVersion: before.Version})
	we, ok := err.(*WriteError)
	if !ok {
		t.Fatalf("error type = %T, want *WriteError", err)
	}
	if we.CurrentContent != "# Disk version\n" {
		t.Errorf("CurrentContent = %q, want the disk version", we.CurrentContent)
	}
	if we.CurrentVersion == "" {
		t.Error("CurrentVersion was not reported")
	}
}

// TestWriteWithoutVersionIsAllowed supports a deliberate overwrite after the
// user has resolved a conflict.
func TestWriteWithoutVersionIsAllowed(t *testing.T) {
	s := writableService(t, map[string]string{"a.md": "# One\n"})

	if _, err := s.Write(WriteRequest{ID: "a.md", Content: "# Forced\n"}); err != nil {
		t.Fatalf("Write without expected version: %v", err)
	}
	if got := onDisk(t, s, "a.md"); got != "# Forced\n" {
		t.Fatalf("content = %q", got)
	}
}

// TestCRLFPreserved covers acceptance D3.
func TestCRLFPreserved(t *testing.T) {
	s := serviceRawWritable(t, map[string][]byte{
		"crlf.md": []byte("# Title\r\n\r\nBody.\r\n"),
	})

	doc, err := s.Read("crlf.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if doc.LineEnding != LineEndingCRLF {
		t.Fatalf("line ending = %q, want crlf", doc.LineEnding)
	}

	// The editor holds LF text and saves it back unchanged.
	_, err = s.Write(WriteRequest{
		ID:              "crlf.md",
		Content:         doc.Content,
		ExpectedVersion: doc.Version,
		LineEnding:      doc.LineEnding,
		KeepBOM:         doc.HasBOM,
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := onDisk(t, s, "crlf.md")
	if !strings.Contains(got, "\r\n") {
		t.Fatalf("CRLF was lost on save: %q", got)
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("mixed endings after save: %q", got)
	}
}

// TestLineEndingCanBeChangedExplicitly is the "unless the user explicitly
// changes the format" half of D3.
func TestLineEndingCanBeChangedExplicitly(t *testing.T) {
	s := serviceRawWritable(t, map[string][]byte{"crlf.md": []byte("# Title\r\nBody.\r\n")})

	doc, _ := s.Read("crlf.md")
	_, err := s.Write(WriteRequest{
		ID:              "crlf.md",
		Content:         doc.Content,
		ExpectedVersion: doc.Version,
		LineEnding:      LineEndingLF,
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if strings.Contains(onDisk(t, s, "crlf.md"), "\r\n") {
		t.Error("an explicit switch to LF did not take effect")
	}
}

func TestBOMPreserved(t *testing.T) {
	s := serviceRawWritable(t, map[string][]byte{
		"bom.md": append([]byte{0xEF, 0xBB, 0xBF}, []byte("# Title\n")...),
	})

	doc, _ := s.Read("bom.md")
	if !doc.HasBOM {
		t.Fatal("the BOM was not detected")
	}

	_, err := s.Write(WriteRequest{
		ID:              "bom.md",
		Content:         doc.Content + "\nMore.\n",
		ExpectedVersion: doc.Version,
		LineEnding:      doc.LineEnding,
		KeepBOM:         doc.HasBOM,
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(s.ws.Guard().Root(), "bom.md"))
	if len(raw) < 3 || raw[0] != 0xEF || raw[1] != 0xBB || raw[2] != 0xBF {
		t.Fatalf("the BOM was not restored: % x", raw[:min(6, len(raw))])
	}
}

// TestWriteRejectedOutsideBoundary covers acceptance B3 on the write path.
func TestWriteRejectedOutsideBoundary(t *testing.T) {
	s := serviceWithConfig(t, `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]

[security]
writable = ["docs/**/*.md"]
`, map[string]string{
		"docs/ok.md":  "# Writable\n",
		"readonly.md": "# Read only\n",
	})

	if _, err := s.Write(WriteRequest{ID: "docs/ok.md", Content: "# Changed\n"}); err != nil {
		t.Fatalf("a writable document was rejected: %v", err)
	}

	_, err := s.Write(WriteRequest{ID: "readonly.md", Content: "# Changed\n"})
	if err == nil {
		t.Fatal("a document outside the write boundary was saved")
	}
	if WriteCodeOf(err) != CodeReadOnly {
		t.Fatalf("code = %q, want %q", WriteCodeOf(err), CodeReadOnly)
	}
	if got := onDisk(t, s, "readonly.md"); got != "# Read only\n" {
		t.Fatalf("the rejected write still modified the file: %q", got)
	}
}

func TestWriteRejectsTraversal(t *testing.T) {
	s := writableService(t, map[string]string{"a.md": "# A\n"})

	for _, id := range []string{"../escape.md", "/etc/passwd", "a.md/../../x.md"} {
		if _, err := s.Write(WriteRequest{ID: id, Content: "x"}); err == nil {
			t.Errorf("Write(%q) was accepted", id)
		}
	}
}

func TestWriteRejectsInvalidUTF8(t *testing.T) {
	s := writableService(t, map[string]string{"a.md": "# A\n"})

	_, err := s.Write(WriteRequest{ID: "a.md", Content: string([]byte{0x23, 0xFF, 0xFE})})
	if err == nil {
		t.Fatal("invalid UTF-8 was written")
	}
	if WriteCodeOf(err) != CodeInvalidUTF8 {
		t.Fatalf("code = %q, want %q", WriteCodeOf(err), CodeInvalidUTF8)
	}
}

func TestWriteRejectsOversizedContent(t *testing.T) {
	s := serviceWithConfig(t, `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]

[documents]
max_editable_bytes = 32

[security]
writable = ["**/*.md"]
`, map[string]string{"a.md": "# A\n"})

	_, err := s.Write(WriteRequest{ID: "a.md", Content: strings.Repeat("x", 200)})
	if err == nil {
		t.Fatal("an oversized write was accepted")
	}
	if WriteCodeOf(err) != CodeTooLarge {
		t.Fatalf("code = %q, want %q", WriteCodeOf(err), CodeTooLarge)
	}
	if got := onDisk(t, s, "a.md"); got != "# A\n" {
		t.Fatalf("the rejected write modified the file: %q", got)
	}
}

// TestFilePermissionsPreserved covers R5 step 1.
func TestFilePermissionsPreserved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply on Windows")
	}
	s := writableService(t, map[string]string{"a.md": "# A\n"})

	path := filepath.Join(s.ws.Guard().Root(), "a.md")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if _, err := s.Write(WriteRequest{ID: "a.md", Content: "# Changed\n"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o640 {
		t.Errorf("permissions = %o, want 640", perm)
	}
}

// TestNoTemporaryFilesLeftBehind keeps the workspace clean after success.
func TestNoTemporaryFilesLeftBehind(t *testing.T) {
	s := writableService(t, map[string]string{"a.md": "# A\n"})

	if _, err := s.Write(WriteRequest{ID: "a.md", Content: "# Changed\n"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	entries, err := os.ReadDir(s.ws.Guard().Root())
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".athenaeum-") {
			t.Errorf("temporary file left behind: %s", entry.Name())
		}
	}
}

// TestAtomicFailureLeavesOriginalIntact covers acceptance D2.
//
// The directory is made read-only so the temporary file cannot be created,
// which is the realistic failure mode: a full disk or a permission change
// between opening a document and saving it.
func TestAtomicFailureLeavesOriginalIntact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions behave differently on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	s := writableService(t, map[string]string{"a.md": "# Original\n"})
	root := s.ws.Guard().Root()

	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	_, err := s.Write(WriteRequest{ID: "a.md", Content: "# Replacement\n"})
	if err == nil {
		t.Fatal("the write succeeded although the directory is not writable")
	}
	if code := WriteCodeOf(err); code != CodeWriteFailed {
		t.Errorf("code = %q, want %q", code, CodeWriteFailed)
	}

	// The original must be byte-identical (acceptance D2).
	if got := onDisk(t, s, "a.md"); got != "# Original\n" {
		t.Fatalf("the original was modified by a failed write: %q", got)
	}
}

// TestEncodePreservesContentExactly guards the round-trip encoder.
func TestEncodePreservesContentExactly(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		lineEnding string
		bom        bool
		want       string
	}{
		{"lf unchanged", "a\nb\n", LineEndingLF, false, "a\nb\n"},
		{"lf to crlf", "a\nb\n", LineEndingCRLF, false, "a\r\nb\r\n"},
		{"already crlf is not doubled", "a\r\nb\r\n", LineEndingCRLF, false, "a\r\nb\r\n"},
		{"bom prepended", "a\n", LineEndingLF, true, "\uFEFFa\n"},
		{"no trailing newline preserved", "a", LineEndingLF, false, "a"},
		{"empty stays empty", "", LineEndingLF, false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(encode(tc.content, tc.lineEnding, tc.bom)); got != tc.want {
				t.Errorf("encode() = %q, want %q", got, tc.want)
			}
		})
	}
}

// serviceRawWritable builds a writable workspace from raw bytes.
func serviceRawWritable(t *testing.T, files map[string][]byte) *Service {
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
	configPath := filepath.Join(dir, "athenaeum.toml")
	body := "schema_version = 1\nname = \"Fixture\"\ninclude = [\"**/*.md\"]\n\n[security]\nwritable = [\"**/*.md\"]\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return serviceFromConfigPath(t, configPath)
}
