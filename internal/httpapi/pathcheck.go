package httpapi

import (
	"net/url"
	"strings"

	"athenaeum/internal/security"
)

// inspectRawPath rejects a request URI that contains traversal or empty
// segments, before Go's ServeMux can clean it.
//
// This matters for acceptance B2, which requires a *stable path-security error*
// for absolute paths, encoded traversal, and `..` escapes. Without this check,
// ServeMux silently rewrites "/api/v1/documents/../../../etc/passwd" and
// answers with a 307 redirect or a bare 404. Neither leaks data — the path
// guard would reject the ID anyway — but neither is the documented error, and a
// redirect invites a client to retry against a different resource.
//
// escapedPath is r.URL.EscapedPath(): the raw, still-encoded path.
func inspectRawPath(escapedPath string) (code string, ok bool) {
	// Percent-decoding is applied once here so that "%2e%2e" is recognised as
	// "..". Decoding twice would itself be a vulnerability, so this is
	// deliberately a single pass.
	decoded, err := url.PathUnescape(escapedPath)
	if err != nil {
		return security.CodePathTraversal, false
	}

	for _, candidate := range []string{escapedPath, decoded} {
		for _, segment := range strings.Split(candidate, "/") {
			switch segment {
			case "..":
				return security.CodePathTraversal, false
			case "":
				// A leading empty segment is the normal "/" prefix; an interior
				// one means a doubled slash, which after cleaning would resolve
				// to a different resource than the client asked for.
				continue
			}
			// "...." and similar are not traversal by themselves, but a segment
			// consisting only of dots is never a legitimate document name.
			if len(segment) > 2 && strings.Trim(segment, ".") == "" {
				return security.CodePathTraversal, false
			}
		}
	}

	// An interior doubled slash inside the API surface would be cleaned by the
	// mux into a different path, so reject it rather than redirect.
	if idx := strings.Index(escapedPath, "//"); idx > 0 {
		return security.CodePathTraversal, false
	}

	if strings.ContainsRune(decoded, 0) {
		return security.CodePathTraversal, false
	}

	return "", true
}
