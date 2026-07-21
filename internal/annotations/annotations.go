// Package annotations reads and writes the personal and shared annotation
// sidecars that let a developer attach context to a document without altering
// its content (spec 02 section 3.6, spec 03 section 3, requirement R8).
//
// Two rules shape everything here:
//
//   - The document source is never touched. An annotation lives in a sidecar,
//     so an annotated document is byte-for-byte what it was.
//   - A broken anchor is shown as detached, never deleted (R8). Repair is
//     computed on read from the stored selector; the sidecar is not rewritten
//     just because a document moved underneath it. See repair.go and ADR-0005.
package annotations

import (
	"fmt"
	"slices"
)

// SchemaVersion is the on-disk sidecar version (spec 03 section 3).
const SchemaVersion = 1

// Visibility decides where an annotation is stored, and therefore whether it can
// be committed. Personal annotations never enter the repository (G1); shared
// annotations live under the workspace and are meant to be committed (G2).
const (
	VisibilityPersonal = "personal"
	VisibilityShared   = "shared"
)

// Kind distinguishes a comment from a bookmark/pin (R8).
const (
	KindComment = "comment"
	KindPin     = "pin"
)

// Status is the resolved/unresolved lifecycle (R8).
const (
	StatusOpen     = "open"
	StatusResolved = "resolved"
)

// Anchor types (spec 03 section 3).
const (
	AnchorText     = "text_quote"
	AnchorHeading  = "heading"
	AnchorDocument = "document"
)

// Anchor state is computed on read and never persisted (ADR-0005).
const (
	StateAnchored = "anchored"
	StateDetached = "detached"
)

// Anchor stores both a structural position and a quoted context so it can be
// repaired after edits (R8). Every field except State round-trips to the
// sidecar exactly as spec 03 section 3 defines it.
type Anchor struct {
	Type        string   `json:"type"`
	HeadingPath []string `json:"heading_path,omitempty"`
	StartLine   int      `json:"start_line,omitempty"`
	EndLine     int      `json:"end_line,omitempty"`
	Exact       string   `json:"exact,omitempty"`
	Prefix      string   `json:"prefix,omitempty"`
	Suffix      string   `json:"suffix,omitempty"`
	SourceHash  string   `json:"source_hash,omitempty"`
	// State is the repair result (anchored|detached) for the current document.
	// It is computed on every read and cleared before writing, so it never
	// pollutes the sidecar on disk.
	State string `json:"state,omitempty"`
}

// Annotation is one comment or pin (spec 03 section 3).
type Annotation struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Visibility string `json:"visibility"`
	Status     string `json:"status"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	Anchor     Anchor `json:"anchor"`
}

// Sidecar is one JSON file: all annotations of one visibility for one document
// (spec 03 section 3).
type Sidecar struct {
	SchemaVersion int          `json:"schema_version"`
	DocumentID    string       `json:"document_id"`
	Revision      int          `json:"revision"`
	Annotations   []Annotation `json:"annotations"`
}

// ValidationError reports a rejected annotation field with a stable code so the
// transport layer can return a 400 rather than a 500.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

func invalid(field, msg string) error { return &ValidationError{Field: field, Message: msg} }

// validVisibility reports whether v is a storable visibility.
func validVisibility(v string) bool {
	return v == VisibilityPersonal || v == VisibilityShared
}

// validateNew checks the caller-supplied fields of a create request. The server
// stamps id, timestamps, and source_hash, so those are not checked here.
func validateNew(kind, visibility, status, body string, anchor Anchor) error {
	switch kind {
	case KindComment:
		if body == "" {
			return invalid("body", "a comment needs a body")
		}
	case KindPin:
		// A pin may carry an empty body: it is a bookmark, not a note.
	default:
		return invalid("kind", fmt.Sprintf("unknown kind %q", kind))
	}
	if !validVisibility(visibility) {
		return invalid("visibility", fmt.Sprintf("unknown visibility %q", visibility))
	}
	if status != StatusOpen && status != StatusResolved {
		return invalid("status", fmt.Sprintf("unknown status %q", status))
	}
	return validateAnchor(anchor)
}

// validateAnchor rejects an anchor the repair engine could never resolve.
func validateAnchor(a Anchor) error {
	switch a.Type {
	case AnchorDocument:
		// No selector needed: the whole document is the target.
	case AnchorHeading:
		if len(a.HeadingPath) == 0 {
			return invalid("anchor.heading_path", "a heading anchor needs a heading path")
		}
	case AnchorText:
		if a.Exact == "" {
			return invalid("anchor.exact", "a text anchor needs the quoted text")
		}
		if a.StartLine < 1 || a.EndLine < a.StartLine {
			return invalid("anchor.start_line", "a text anchor needs a valid line range")
		}
	default:
		return invalid("anchor.type", fmt.Sprintf("unknown anchor type %q", a.Type))
	}
	return nil
}

// sortByID orders annotations by their sortable ULID, which is creation order
// (spec 03 section 3). Merging personal and shared files therefore yields a
// stable, chronological list.
func sortByID(list []Annotation) {
	slices.SortFunc(list, func(a, b Annotation) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
}
