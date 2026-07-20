package documents

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"athenaeum/internal/security"
)

// Write error codes (requirement N6, spec 02 section 5).
const (
	CodeConflict      = "DOCUMENT_CONFLICT"
	CodeReadOnly      = "DOCUMENT_READ_ONLY"
	CodeTooLarge      = "DOCUMENT_TOO_LARGE"
	CodeInvalidUTF8   = "DOCUMENT_INVALID_UTF8"
	CodeWriteFailed   = "DOCUMENT_WRITE_FAILED"
	CodeNotAtomic     = "DOCUMENT_WRITE_NOT_ATOMIC"
	CodeVerifyFailed  = "DOCUMENT_VERIFY_FAILED"
	CodeMissingParent = "DOCUMENT_PARENT_MISSING"
)

// WriteError reports a failed save with a stable code.
//
// A write failure MUST NOT discard the editor buffer (R5 step 5), so the error
// carries enough context for the UI to explain what happened and keep the
// user's text.
type WriteError struct {
	Code    string
	Message string
	// CurrentVersion is set on a conflict so the client can compare.
	CurrentVersion string
	// CurrentContent is set on a conflict so the client can show both sides
	// without a second request that might race again.
	CurrentContent string
	Err            error
}

func (e *WriteError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *WriteError) Unwrap() error { return e.Err }

// WriteCodeOf returns the stable code for a write error, or "".
func WriteCodeOf(err error) string {
	var we *WriteError
	if errors.As(err, &we) {
		return we.Code
	}
	return ""
}

// WriteRequest is a save.
type WriteRequest struct {
	ID string
	// Content is LF-normalised text as held by the editor.
	Content string
	// ExpectedVersion is the version the editor last observed. A mismatch means
	// the file changed underneath and the save is refused (spec 02 section 5).
	ExpectedVersion string
	// LineEnding is the format to write. Empty means "keep what is on disk",
	// which is what preserves CRLF through an ordinary save (acceptance D3).
	LineEnding string
	// KeepBOM preserves a byte order mark that was present when read.
	KeepBOM bool
}

// WriteResult reports a successful save.
type WriteResult struct {
	ID         string `json:"id"`
	Version    string `json:"version"`
	Size       int64  `json:"size"`
	LineEnding string `json:"line_ending"`
}

// Write saves a document using the atomic algorithm in spec 03 section 8.
//
// The sequence is: verify version, write a sibling temporary file, copy mode
// bits, flush, fsync, rename over the target, verify the result, and only then
// discard the recovery copy. If atomic replacement is unavailable the operation
// fails rather than degrading to a truncating write.
func (s *Service) Write(req WriteRequest) (*WriteResult, error) {
	entry, ok := s.ws.Lookup(req.ID)
	if !ok {
		return nil, s.notFound(req.ID)
	}

	absPath, err := s.ws.ResolveWrite(req.ID)
	if err != nil {
		// Outside the write boundary, or not a legal document ID.
		if security.CodeOf(err) == security.CodePathNotWritable {
			return nil, &WriteError{
				Code:    CodeReadOnly,
				Message: "This document is outside the configured write boundary.",
				Err:     err,
			}
		}
		return nil, err
	}

	cfg := s.ws.Config()

	// Content must be valid UTF-8: v0.1 edits UTF-8 Markdown only, and writing
	// anything else would corrupt a file we cannot faithfully round-trip.
	if !utf8.ValidString(req.Content) {
		return nil, &WriteError{
			Code:    CodeInvalidUTF8,
			Message: "The submitted content is not valid UTF-8.",
		}
	}

	// Read the current bytes once: they establish the on-disk version, the mode
	// bits, and the line ending to preserve.
	current, err := os.ReadFile(absPath)
	if err != nil {
		return nil, &WriteError{
			Code:    CodeWriteFailed,
			Message: "The document could not be read before saving.",
			Err:     err,
		}
	}

	// Step 1: verify the expected source version.
	currentVersion := fingerprint(current)
	if req.ExpectedVersion != "" && req.ExpectedVersion != currentVersion {
		body := current
		if bytes.HasPrefix(body, utf8BOM) {
			body = body[len(utf8BOM):]
		}
		return nil, &WriteError{
			Code:           CodeConflict,
			Message:        "The file changed on disk while local edits were unsaved.",
			CurrentVersion: currentVersion,
			CurrentContent: string(normaliseNewlines(body)),
		}
	}

	// Encode the buffer back to its on-disk form: restore the line ending and
	// any byte order mark, so an ordinary save is byte-faithful (D3).
	lineEnding := req.LineEnding
	if lineEnding == "" {
		lineEnding = detectLineEnding(current)
	}
	payload := encode(req.Content, lineEnding, req.KeepBOM)

	if int64(len(payload)) > cfg.Documents.MaxEditableBytes {
		return nil, &WriteError{
			Code: CodeTooLarge,
			Message: fmt.Sprintf("The document would be %d bytes, above the configured limit of %d.",
				len(payload), cfg.Documents.MaxEditableBytes),
		}
	}

	if err := atomicWrite(absPath, payload); err != nil {
		return nil, err
	}

	// Refresh the enumeration so size and timestamps stay accurate. A failure
	// here does not invalidate the write that already succeeded.
	_ = s.ws.Refresh()
	_ = entry

	return &WriteResult{
		ID:         req.ID,
		Version:    fingerprint(payload),
		Size:       int64(len(payload)),
		LineEnding: lineEnding,
	}, nil
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// encode converts editor text back to on-disk bytes.
func encode(content, lineEnding string, keepBOM bool) []byte {
	body := content
	if lineEnding == LineEndingCRLF {
		// Normalise first so an already-CRLF buffer cannot become CRCRLF.
		body = string(normaliseNewlines([]byte(body)))
		body = string(bytes.ReplaceAll([]byte(body), []byte("\n"), []byte("\r\n")))
	}
	out := []byte(body)
	if keepBOM {
		out = append(append([]byte{}, utf8BOM...), out...)
	}
	return out
}

// atomicWrite performs steps 2 to 8 of spec 03 section 8.
func atomicWrite(target string, payload []byte) error {
	dir := filepath.Dir(target)

	// Preserve the original mode where practical (R5 step 1). A file that does
	// not exist yet takes a conservative default.
	mode := os.FileMode(0o644)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return &WriteError{Code: CodeWriteFailed, Message: "The target could not be inspected.", Err: err}
	}

	// Step 2: a temporary file in the same directory, therefore the same
	// filesystem, so the rename in step 7 is atomic rather than a copy.
	temp, err := os.CreateTemp(dir, ".athenaeum-*.tmp")
	if err != nil {
		return &WriteError{
			Code:    CodeWriteFailed,
			Message: "A temporary file could not be created beside the document.",
			Err:     err,
		}
	}
	tempName := temp.Name()

	// Any failure from here on must leave the original untouched and remove the
	// temporary file.
	cleanup := func() {
		temp.Close()
		os.Remove(tempName)
	}

	// Step 3: copy relevant mode bits.
	if err := temp.Chmod(mode); err != nil {
		// Not fatal on filesystems without permission support; the rename still
		// produces a correct file, it just may not carry the original mode.
		_ = err
	}

	// Step 4: write the complete content.
	if _, err := temp.Write(payload); err != nil {
		cleanup()
		return &WriteError{Code: CodeWriteFailed, Message: "The document could not be written.", Err: err}
	}

	// Step 5: flush to disk before the rename, so a crash cannot leave the
	// target pointing at a file whose contents were never persisted.
	if err := temp.Sync(); err != nil {
		cleanup()
		return &WriteError{Code: CodeWriteFailed, Message: "The document could not be flushed to disk.", Err: err}
	}
	if err := temp.Close(); err != nil {
		os.Remove(tempName)
		return &WriteError{Code: CodeWriteFailed, Message: "The temporary file could not be closed.", Err: err}
	}

	// Step 7: atomic replace.
	if err := os.Rename(tempName, target); err != nil {
		os.Remove(tempName)
		// Spec 03 section 8: if atomic replace is unavailable, fail rather than
		// degrade silently to a truncate-and-write.
		return &WriteError{
			Code:    CodeNotAtomic,
			Message: "The document could not be replaced atomically, so it was left unchanged.",
			Err:     err,
		}
	}

	// Step 6, applied after the rename: fsync the directory so the rename
	// itself is durable. Not supported everywhere, so a failure is tolerated.
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		handle.Close()
	}

	// Step 8: verify the bytes that actually landed.
	written, err := os.ReadFile(target)
	if err != nil {
		return &WriteError{
			Code:    CodeVerifyFailed,
			Message: "The document was written but could not be read back for verification.",
			Err:     err,
		}
	}
	if !bytes.Equal(written, payload) {
		return &WriteError{
			Code:    CodeVerifyFailed,
			Message: "The document on disk does not match what was written.",
		}
	}

	return nil
}
