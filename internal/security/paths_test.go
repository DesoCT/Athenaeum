package security

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// newWorkspace builds a temporary workspace with a couple of documents and an
// "outside" directory used to test escapes.
func newWorkspace(t *testing.T) (root string, outside string) {
	t.Helper()
	base := t.TempDir()

	root = filepath.Join(base, "workspace")
	outside = filepath.Join(base, "outside")
	for _, dir := range []string{
		filepath.Join(root, "docs", "design"),
		outside,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	write(filepath.Join(root, "README.md"), "# Readme\n")
	write(filepath.Join(root, "docs", "design", "rendering.md"), "# Rendering\n")
	write(filepath.Join(outside, "secret.md"), "# Secret\n")

	return root, outside
}

func newGuard(t *testing.T, allowExternal bool) (*PathGuard, string, string) {
	t.Helper()
	root, outside := newWorkspace(t)
	g, err := NewPathGuard(root, allowExternal)
	if err != nil {
		t.Fatalf("NewPathGuard: %v", err)
	}
	return g, root, outside
}

func TestResolveReadAcceptsIncludedDocuments(t *testing.T) {
	g, root, _ := newGuard(t, false)

	for _, id := range []string{"README.md", "docs/design/rendering.md", "./README.md", "docs/../README.md"} {
		t.Run(id, func(t *testing.T) {
			got, err := g.ResolveRead(id)
			if err != nil {
				t.Fatalf("ResolveRead(%q): %v", id, err)
			}
			if !filepath.IsAbs(got) {
				t.Errorf("resolved path %q is not absolute", got)
			}
			// EvalSymlinks is applied to the root, so compare against the
			// guard's canonical root rather than the raw temp path.
			if rel, err := filepath.Rel(g.Root(), got); err != nil || rel == ".." {
				t.Errorf("resolved path %q escaped root %q (raw root %q)", got, g.Root(), root)
			}
		})
	}
}

// TestResolveReadRejectsTraversal covers acceptance B2.
func TestResolveReadRejectsTraversal(t *testing.T) {
	g, _, _ := newGuard(t, false)

	tests := []struct {
		name     string
		id       string
		wantCode string
	}{
		{"parent escape", "../outside/secret.md", CodePathTraversal},
		{"deep parent escape", "docs/../../outside/secret.md", CodePathTraversal},
		{"bare parent", "..", CodePathTraversal},
		{"absolute unix", "/etc/passwd", CodePathAbsolute},
		{"absolute workspace path", "/tmp/whatever.md", CodePathAbsolute},
		{"windows volume", "C:/Windows/system.ini", CodePathAbsolute},
		{"backslash traversal", `..\outside\secret.md`, CodePathTraversal},
		{"backslash separator", `docs\design\rendering.md`, CodePathTraversal},
		{"null byte", "README.md\x00.txt", CodePathTraversal},
		{"empty", "", CodePathEmpty},
		{"dot", ".", CodePathEmpty},
		{"directory", "docs", CodePathNotRegular},
		{"missing", "nope.md", CodePathNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := g.ResolveRead(tc.id)
			if err == nil {
				t.Fatalf("ResolveRead(%q) succeeded, want rejection", tc.id)
			}
			if got := CodeOf(err); got != tc.wantCode {
				t.Errorf("code = %q, want %q (err: %v)", got, tc.wantCode, err)
			}
		})
	}
}

// TestPercentEncodedTraversalIsCallerDecoded documents an important boundary:
// the guard operates on decoded IDs. The HTTP layer decodes before calling, so
// an encoded "%2e%2e" arrives here as "..", which is rejected above. A literal
// percent sequence is just an unusual filename and must not resolve to a
// parent directory.
func TestPercentEncodedTraversalIsNotDecodedHere(t *testing.T) {
	g, _, _ := newGuard(t, false)

	_, err := g.ResolveRead("%2e%2e/outside/secret.md")
	if err == nil {
		t.Fatal("an encoded traversal string resolved to a file")
	}
	// It should fail as a missing file, never as a successful escape.
	if code := CodeOf(err); code != CodePathNotFound {
		t.Errorf("code = %q, want %q", code, CodePathNotFound)
	}
}

// TestSymlinkEscapeRejected covers the symlink half of acceptance B2.
func TestSymlinkEscapeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	g, root, outside := newGuard(t, false)

	link := filepath.Join(root, "escape.md")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := g.ResolveRead("escape.md")
	if err == nil {
		t.Fatal("a symlink escaping the workspace was read")
	}
	if code := CodeOf(err); code != CodePathEscapesRoot {
		t.Errorf("code = %q, want %q", code, CodePathEscapesRoot)
	}
}

// TestSymlinkEscapeAllowedForReadsWhenConfigured covers the documented opt-in.
func TestSymlinkEscapeAllowedForReadsWhenConfigured(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	g, root, outside := newGuard(t, true)

	link := filepath.Join(root, "escape.md")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := g.ResolveRead("escape.md"); err != nil {
		t.Fatalf("allow_external_reads did not permit the read: %v", err)
	}
}

// TestExternalSymlinkStaysReadOnly is the important half of the opt-in: even
// with allow_external_reads, writes must not follow a link out of the
// workspace (spec 03 section 6).
func TestExternalSymlinkStaysReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	g, root, outside := newGuard(t, true)

	link := filepath.Join(root, "escape.md")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := g.ResolveWrite("escape.md")
	if err == nil {
		t.Fatal("a write followed a symlink out of the workspace")
	}
	if code := CodeOf(err); code != CodePathEscapesRoot {
		t.Errorf("code = %q, want %q", code, CodePathEscapesRoot)
	}
}

// TestSymlinkedDirectoryEscapeRejectedOnWrite closes the parent-directory
// variant of the same attack.
func TestSymlinkedDirectoryEscapeRejectedOnWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	g, root, outside := newGuard(t, true)

	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := g.ResolveWrite("linked/new.md")
	if err == nil {
		t.Fatal("a write landed inside a symlinked directory outside the workspace")
	}
	if code := CodeOf(err); code != CodePathEscapesRoot {
		t.Errorf("code = %q, want %q", code, CodePathEscapesRoot)
	}
}

func TestResolveWriteAllowsNewFileInExistingDirectory(t *testing.T) {
	g, _, _ := newGuard(t, false)

	got, err := g.ResolveWrite("docs/design/new.md")
	if err != nil {
		t.Fatalf("ResolveWrite: %v", err)
	}
	if filepath.Base(got) != "new.md" {
		t.Errorf("resolved %q, want it to end with new.md", got)
	}
}

func TestResolveWriteRejectsMissingDirectory(t *testing.T) {
	g, _, _ := newGuard(t, false)

	_, err := g.ResolveWrite("no/such/dir/file.md")
	if err == nil {
		t.Fatal("a write into a missing directory was accepted")
	}
	if code := CodeOf(err); code != CodePathNotFound {
		t.Errorf("code = %q, want %q", code, CodePathNotFound)
	}
}

// TestResolveRejectsIrregularFiles covers spec 03 section 6 step 7.
func TestResolveRejectsIrregularFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("named pipes behave differently on Windows")
	}
	g, root, _ := newGuard(t, false)

	fifo := filepath.Join(root, "pipe.md")
	if err := makeFIFO(fifo); err != nil {
		t.Skipf("cannot create a named pipe here: %v", err)
	}

	_, err := g.ResolveRead("pipe.md")
	if err == nil {
		t.Fatal("a named pipe was accepted as a document")
	}
	if code := CodeOf(err); code != CodePathNotRegular {
		t.Errorf("code = %q, want %q", code, CodePathNotRegular)
	}
}

func TestDocumentIDIsSlashNormalised(t *testing.T) {
	g, root, _ := newGuard(t, false)

	id, err := g.DocumentID(filepath.Join(root, "docs", "design", "rendering.md"))
	if err != nil {
		t.Fatalf("DocumentID: %v", err)
	}
	if id != "docs/design/rendering.md" {
		t.Errorf("DocumentID = %q, want docs/design/rendering.md", id)
	}
}

func TestDocumentIDRejectsOutsidePaths(t *testing.T) {
	g, _, outside := newGuard(t, false)

	_, err := g.DocumentID(filepath.Join(outside, "secret.md"))
	if err == nil {
		t.Fatal("a path outside the workspace produced a document ID")
	}
	if code := CodeOf(err); code != CodePathEscapesRoot {
		t.Errorf("code = %q, want %q", code, CodePathEscapesRoot)
	}
}

// TestDocumentIDPreservesCase guards spec 02 section 3.2.
func TestDocumentIDPreservesCase(t *testing.T) {
	g, root, _ := newGuard(t, false)

	mixed := filepath.Join(root, "MixedCase.md")
	if err := os.WriteFile(mixed, []byte("# Mixed\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	id, err := g.DocumentID(mixed)
	if err != nil {
		t.Fatalf("DocumentID: %v", err)
	}
	if id != "MixedCase.md" {
		t.Errorf("DocumentID = %q, want MixedCase.md", id)
	}
}

// TestDocumentIDAcceptsSymlinkedPrefix is the regression test for a macOS-only
// CI failure.
//
// On macOS /var is a symlink to /private/var, so t.TempDir() returns a path
// that names the same directory as the canonical root by a different route.
// DocumentID compared the caller's path against the canonical root without
// canonicalising it first, and reported a spurious escape for every document.
// A symlinked prefix reproduces the same condition on any platform.
func TestDocumentIDAcceptsSymlinkedPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	base := t.TempDir()

	real := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "docs", "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// "link" stands in for /var pointing at /private/var.
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	g, err := NewPathGuard(link, false)
	if err != nil {
		t.Fatalf("NewPathGuard: %v", err)
	}

	// A caller hands back a path through the symlinked prefix, which is what
	// the filesystem walk and the watcher both do on macOS.
	id, err := g.DocumentID(filepath.Join(link, "docs", "a.md"))
	if err != nil {
		t.Fatalf("DocumentID via a symlinked prefix: %v", err)
	}
	if id != "docs/a.md" {
		t.Errorf("DocumentID = %q, want docs/a.md", id)
	}
}

// TestDocumentIDHandlesRemovedFile keeps the watcher working: a removal event
// names a file that no longer exists, so the full path cannot be resolved.
func TestDocumentIDHandlesRemovedFile(t *testing.T) {
	g, root, _ := newGuard(t, false)

	id, err := g.DocumentID(filepath.Join(root, "docs", "design", "deleted.md"))
	if err != nil {
		t.Fatalf("DocumentID for an absent file: %v", err)
	}
	if id != "docs/design/deleted.md" {
		t.Errorf("DocumentID = %q, want docs/design/deleted.md", id)
	}
}

// TestDocumentIDStillRejectsEscape confirms canonicalising the input did not
// weaken containment.
func TestDocumentIDStillRejectsEscape(t *testing.T) {
	g, _, outside := newGuard(t, false)

	if _, err := g.DocumentID(filepath.Join(outside, "secret.md")); err == nil {
		t.Fatal("a path outside the workspace produced a document ID")
	}
}
