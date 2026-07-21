package documents

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"unicode/utf8"

	"athenaeum/internal/atomicfs"
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

// atomicWrite performs steps 2 to 8 of spec 03 section 8, delegating the
// mechanism to internal/atomicfs and mapping its failure kinds to the document
// write vocabulary so callers keep their stable codes (N6). The shared helper
// exists so sidecar writers use the same policy rather than a divergent copy.
func atomicWrite(target string, payload []byte) error {
	err := atomicfs.Write(target, payload)
	if err == nil {
		return nil
	}
	var ae *atomicfs.Error
	if errors.As(err, &ae) {
		switch ae.Kind {
		case atomicfs.KindNotAtomic:
			return &WriteError{
				Code:    CodeNotAtomic,
				Message: "The document could not be replaced atomically, so it was left unchanged.",
				Err:     ae.Err,
			}
		case atomicfs.KindVerifyFailed:
			return &WriteError{
				Code:    CodeVerifyFailed,
				Message: "The document was written but could not be verified on disk.",
				Err:     ae.Err,
			}
		}
	}
	return &WriteError{Code: CodeWriteFailed, Message: "The document could not be written.", Err: err}
}
