package httpapi

import (
	"testing"

	"athenaeum/internal/security"
)

// TestInspectRawPathRejectsTraversal is the regression test for a bug in which
// a crafted path was cleaned by ServeMux and answered with a 307 redirect or a
// bare 404, rather than the stable path-security error acceptance B2 requires.
func TestInspectRawPathRejectsTraversal(t *testing.T) {
	bad := []string{
		"/api/v1/documents/../../../etc/passwd",
		"/api/v1/documents/%2e%2e/%2e%2e/etc/passwd",
		"/api/v1/documents/%2E%2E/etc/passwd",
		"/api/v1/documents/docs/../../athenaeum.toml",
		"/api/v1/documents/....//....//etc/passwd",
		"/api/v1/documents//etc/passwd",
		"/api/v1/documents/..",
	}

	for _, path := range bad {
		t.Run(path, func(t *testing.T) {
			code, ok := inspectRawPath(path)
			if ok {
				t.Fatalf("inspectRawPath(%q) accepted a traversal path", path)
			}
			if code != security.CodePathTraversal {
				t.Errorf("code = %q, want %q", code, security.CodePathTraversal)
			}
		})
	}
}

func TestInspectRawPathAcceptsOrdinaryPaths(t *testing.T) {
	good := []string{
		"/",
		"/index.html",
		"/api/v1/health",
		"/api/v1/workspace",
		"/api/v1/documents",
		"/api/v1/documents/README.md",
		"/api/v1/documents/docs/design/rendering.md",
		"/api/v1/documents/docs/a.b.c.md",
		"/api/v1/documents/file%20with%20spaces.md",
		"/assets/index-abc123.js",
	}

	for _, path := range good {
		t.Run(path, func(t *testing.T) {
			if _, ok := inspectRawPath(path); !ok {
				t.Fatalf("inspectRawPath(%q) rejected a legitimate path", path)
			}
		})
	}
}

// TestSingleDotSegmentIsNotTraversal keeps the check from over-reaching: "." is
// harmless and a filename may legitimately contain dots.
func TestSingleDotSegmentIsNotTraversal(t *testing.T) {
	for _, path := range []string{"/api/v1/documents/a..b.md", "/api/v1/documents/..hidden.md"} {
		if _, ok := inspectRawPath(path); !ok {
			t.Errorf("inspectRawPath(%q) rejected a legitimate filename", path)
		}
	}
}
