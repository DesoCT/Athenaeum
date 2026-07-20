package security

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Path error codes. Every rejection carries one so the API can return a stable
// code (requirement N6) and acceptance B2 can assert on it.
const (
	CodePathAbsolute    = "PATH_ABSOLUTE"
	CodePathTraversal   = "PATH_TRAVERSAL"
	CodePathEscapesRoot = "PATH_ESCAPES_ROOT"
	CodePathNotRegular  = "PATH_NOT_REGULAR_FILE"
	CodePathNotFound    = "PATH_NOT_FOUND"
	CodePathEmpty       = "PATH_EMPTY"
	CodePathNotWritable = "PATH_NOT_WRITABLE"
)

// PathError reports a rejected path operation.
type PathError struct {
	Code       string
	DocumentID string
	Reason     string
}

func (e *PathError) Error() string {
	return fmt.Sprintf("%s: %s (%s)", e.Code, e.Reason, e.DocumentID)
}

func pathErr(code, id, reason string) error {
	return &PathError{Code: code, DocumentID: id, Reason: reason}
}

// CodeNotIncluded reports an ID that the workspace configuration does not
// include. It is deliberately indistinguishable from a missing file so a
// crafted path cannot probe for excluded documents (acceptance B1).
const CodeNotIncluded = CodePathNotFound

// NotIncluded reports a document ID outside the configured include set.
func NotIncluded(documentID string) error {
	return pathErr(CodeNotIncluded, documentID, "no such document in this workspace")
}

// NotWritable reports a document outside the configured write boundary.
func NotWritable(documentID string) error {
	return pathErr(CodePathNotWritable, documentID,
		"this document is outside the configured write boundary")
}

// CodeOf returns the stable code for a path error, or "" for other errors.
func CodeOf(err error) string {
	var pe *PathError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// PathGuard enforces the containment rules in spec 03 section 6. It is the only
// component permitted to turn a caller-supplied document ID into a filesystem
// path.
type PathGuard struct {
	// root is the canonical absolute workspace root, with symlinks resolved.
	root string
	// allowExternalReads permits reading symlinks that resolve outside the
	// root (spec 03 section 6). Such files always remain read-only in v0.1.
	allowExternalReads bool
}

// NewPathGuard canonicalises the root and returns a guard bound to it.
func NewPathGuard(root string, allowExternalReads bool) (*PathGuard, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %q: %w", root, err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("canonicalise workspace root %q: %w", abs, err)
	}
	return &PathGuard{root: canonical, allowExternalReads: allowExternalReads}, nil
}

// Root returns the canonical workspace root.
func (g *PathGuard) Root() string { return g.root }

// DocumentID converts an absolute path inside the workspace into the stable
// document ID defined by spec 02 section 3.2: the slash-normalised,
// case-preserving path relative to the canonical root.
func (g *PathGuard) DocumentID(absPath string) (string, error) {
	// The root is canonical, so the input must be too before they are compared.
	// On macOS /var is a symlink to /private/var, so a path under a temporary
	// directory and the canonical root disagree textually while naming the same
	// file — and filepath.Rel would then report a spurious escape.
	rel, err := filepath.Rel(g.root, canonicalise(absPath))
	if err != nil {
		return "", fmt.Errorf("relativise %q against %q: %w", absPath, g.root, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", pathErr(CodePathEscapesRoot, absPath, "path is outside the workspace root")
	}
	return filepath.ToSlash(rel), nil
}

// checkID applies the caller-supplied-input rules that do not need the
// filesystem: steps 2, 3, and 6 of spec 03 section 6.
func checkID(documentID string) (string, error) {
	if documentID == "" {
		return "", pathErr(CodePathEmpty, documentID, "a document ID is required")
	}

	// A backslash is a separator on Windows and a legal filename character on
	// Unix. Rejecting it outright keeps one ID meaning one file on every
	// platform, and stops "..\\escape" from bypassing the slash-based checks.
	if strings.ContainsRune(documentID, '\\') {
		return "", pathErr(CodePathTraversal, documentID, "backslashes are not allowed in a document ID")
	}
	if strings.ContainsRune(documentID, 0) {
		return "", pathErr(CodePathTraversal, documentID, "a document ID may not contain a null byte")
	}

	// Reject absolute paths from API callers (step 3). This covers both
	// "/etc/passwd" and a Windows-style "C:/..." volume prefix.
	if strings.HasPrefix(documentID, "/") || filepath.IsAbs(documentID) {
		return "", pathErr(CodePathAbsolute, documentID, "absolute paths are not accepted; use a workspace-relative document ID")
	}
	if volumeNamed(documentID) {
		return "", pathErr(CodePathAbsolute, documentID, "a volume-qualified path is not a valid document ID")
	}

	// path.Clean resolves "." and ".." lexically. If anything survives at the
	// front, the ID escapes the root (step 6).
	clean := path.Clean(documentID)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", pathErr(CodePathTraversal, documentID, "the path escapes the workspace root")
	}
	if clean == "." {
		return "", pathErr(CodePathEmpty, documentID, "a document ID must name a file")
	}
	return clean, nil
}

// volumeNamed reports a Windows volume prefix such as "C:" appearing in an ID.
// filepath.VolumeName only detects this when running on Windows, so the check
// is explicit in order to behave identically on every platform.
func volumeNamed(id string) bool {
	if len(id) < 2 || id[1] != ':' {
		return false
	}
	c := id[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// ResolveRead validates a document ID and returns the absolute path to read.
//
// The file must exist, must be a regular file, and must resolve inside the
// workspace root unless external reads are explicitly enabled.
func (g *PathGuard) ResolveRead(documentID string) (string, error) {
	clean, err := checkID(documentID)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(g.root, filepath.FromSlash(clean))

	// Resolve symlinks before deciding anything (step 4). Lstat first so a
	// dangling symlink reports as missing rather than as a traversal attempt.
	if _, err := os.Lstat(candidate); err != nil {
		if os.IsNotExist(err) {
			return "", pathErr(CodePathNotFound, documentID, "no such file in this workspace")
		}
		return "", pathErr(CodePathNotFound, documentID, err.Error())
	}

	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", pathErr(CodePathNotFound, documentID, "the path could not be resolved")
	}

	inside := g.contains(resolved)
	if !inside && !g.allowExternalReads {
		// Covers both a symlink pointing outside and any residual escape.
		return "", pathErr(CodePathEscapesRoot, documentID,
			"the path resolves outside the workspace; set security.allow_external_reads to permit this")
	}

	if err := checkRegularFile(resolved, documentID); err != nil {
		return "", err
	}
	return resolved, nil
}

// ResolveWrite validates a document ID for writing.
//
// In addition to the read rules, the target must sit inside the workspace root
// even when external reads are permitted: spec 03 section 6 keeps externally
// resolved files read-only in v0.1. The target need not already exist, but its
// parent directory must.
func (g *PathGuard) ResolveWrite(documentID string) (string, error) {
	clean, err := checkID(documentID)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(g.root, filepath.FromSlash(clean))

	// The parent must exist and must itself resolve inside the root, so a
	// symlinked directory cannot be used to land a write outside the workspace.
	parent := filepath.Dir(candidate)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", pathErr(CodePathNotFound, documentID, "the containing directory does not exist")
	}
	if !g.contains(resolvedParent) {
		return "", pathErr(CodePathEscapesRoot, documentID,
			"the containing directory resolves outside the workspace")
	}

	target := filepath.Join(resolvedParent, filepath.Base(candidate))

	// If the target exists it must be a regular file, and a symlink target must
	// also stay inside the root.
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(target)
			if err != nil {
				return "", pathErr(CodePathNotFound, documentID, "the symlink could not be resolved")
			}
			if !g.contains(resolved) {
				return "", pathErr(CodePathEscapesRoot, documentID,
					"the path is a symlink resolving outside the workspace and is read-only")
			}
			target = resolved
		}
		if err := checkRegularFile(target, documentID); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", pathErr(CodePathNotFound, documentID, err.Error())
	}

	return target, nil
}

// Canonicalise resolves symlinks in a path, falling back gracefully.
//
// It is exported for callers outside this package that must compare paths —
// the workspace registry, for one — because comparing a non-canonical path
// against a canonical root has broken this project twice: on macOS /var is a
// symlink to /private/var, so two names for one directory disagree textually.
func Canonicalise(absPath string) string { return canonicalise(absPath) }

// canonicalise resolves symlinks in a path, falling back gracefully.
//
// The path may not exist — a removal event names a file that has just gone —
// so when the full path cannot be resolved the parent directory is resolved
// instead, which still yields a canonical prefix.
func canonicalise(absPath string) string {
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		return resolved
	}
	dir, base := filepath.Split(absPath)
	if resolvedDir, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(resolvedDir, base)
	}
	return absPath
}

// contains reports whether an already-canonical path is inside the root.
func (g *PathGuard) contains(canonical string) bool {
	if canonical == g.root {
		return true
	}
	rel, err := filepath.Rel(g.root, canonical)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// checkRegularFile rejects directories, device files, sockets, and named pipes
// (spec 03 section 6 step 7).
func checkRegularFile(absPath, documentID string) error {
	info, err := os.Stat(absPath)
	if err != nil {
		return pathErr(CodePathNotFound, documentID, err.Error())
	}
	if info.IsDir() {
		return pathErr(CodePathNotRegular, documentID, "the path is a directory")
	}
	if !info.Mode().IsRegular() {
		return pathErr(CodePathNotRegular, documentID, describeIrregular(info.Mode()))
	}
	return nil
}

func describeIrregular(mode fs.FileMode) string {
	switch {
	case mode&os.ModeDevice != 0:
		return "the path is a device file"
	case mode&os.ModeSocket != 0:
		return "the path is a socket"
	case mode&os.ModeNamedPipe != 0:
		return "the path is a named pipe"
	case mode&os.ModeSymlink != 0:
		return "the path is a symlink"
	default:
		return "the path is not a regular file"
	}
}
