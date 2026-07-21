// Package notes manages free-standing workspace notes: Markdown files with a
// small YAML front matter, kept in personal or shared storage (R9, spec 02
// section 3.7, spec 03 section 4).
//
// Notes are not document annotations. They stand on their own, may link to
// documents, headings, annotations, and other notes, and are edited like any
// Markdown file. Shared notes live under the workspace and are meant to be
// committed; personal notes live in the user data directory and never enter the
// repository (acceptance G1's rule, applied to notes).
package notes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Visibility decides where a note is stored, and therefore whether it can be
// committed.
const (
	VisibilityPersonal = "personal"
	VisibilityShared   = "shared"
)

// Link is one typed relationship from a note (R9). Exactly one target field is
// normally set; a document link may additionally name a heading.
type Link struct {
	Document   string `yaml:"document,omitempty" json:"document,omitempty"`
	Heading    string `yaml:"heading,omitempty" json:"heading,omitempty"`
	Note       string `yaml:"note,omitempty" json:"note,omitempty"`
	Annotation string `yaml:"annotation,omitempty" json:"annotation,omitempty"`
}

// meta is the YAML front matter of a note file (spec 03 section 4).
type meta struct {
	ID         string `yaml:"id"`
	Title      string `yaml:"title"`
	Visibility string `yaml:"visibility"`
	CreatedAt  string `yaml:"created_at"`
	UpdatedAt  string `yaml:"updated_at"`
	Links      []Link `yaml:"links,omitempty"`
}

// Note is a free-standing note with its Markdown body.
type Note struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Visibility string `json:"visibility"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	Links      []Link `json:"links,omitempty"`
	Body       string `json:"body"`
	// Version fingerprints the file for optimistic concurrency (spec 02 section
	// 5). It is computed on read and required on write. Empty in a summary.
	Version string `json:"version,omitempty"`
}

// Summary is a note without its body, for the list view.
type Summary struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Visibility string `json:"visibility"`
	UpdatedAt  string `json:"updated_at"`
	Links      []Link `json:"links,omitempty"`
}

// ValidationError reports a rejected field so the transport returns a 400.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

func invalid(field, msg string) error { return &ValidationError{Field: field, Message: msg} }

func validVisibility(v string) bool {
	return v == VisibilityPersonal || v == VisibilityShared
}

var yamlFence = []byte("---")

// encode serialises a note to its on-disk form: a YAML front matter block
// followed by the Markdown body. Writing YAML (never TOML) keeps the format
// single and predictable, matching the spec 03 section 4 example.
func encode(n *Note) ([]byte, error) {
	m := meta{
		ID:         n.ID,
		Title:      n.Title,
		Visibility: n.Visibility,
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
		Links:      n.Links,
	}
	front, err := yaml.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode note front matter: %w", err)
	}
	var buf bytes.Buffer
	buf.Write(yamlFence)
	buf.WriteByte('\n')
	buf.Write(front)
	buf.Write(yamlFence)
	buf.WriteByte('\n')
	body := n.Body
	if body != "" {
		buf.WriteByte('\n')
		buf.WriteString(body)
		if body[len(body)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}

// decode parses a note file. A note without a front matter block is malformed:
// unlike a document, a note is something Athenaeum wrote, so a missing header is
// corruption to report rather than body to render.
func decode(data []byte) (*Note, error) {
	source := data
	firstLineEnd := bytes.IndexByte(source, '\n')
	if firstLineEnd < 0 || !bytes.Equal(bytes.TrimRight(source[:firstLineEnd], "\r"), yamlFence) {
		return nil, fmt.Errorf("note has no front matter")
	}
	rest := source[firstLineEnd+1:]
	close := findClosingFence(rest)
	if close < 0 {
		return nil, fmt.Errorf("note front matter is not terminated")
	}
	raw := rest[:close]

	var m meta
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse note front matter: %w", err)
	}

	// Body starts after the closing fence's line.
	afterClose := rest[close:]
	bodyStart := bytes.IndexByte(afterClose, '\n')
	body := ""
	if bodyStart >= 0 {
		body = string(bytes.TrimPrefix(afterClose[bodyStart+1:], []byte("\n")))
	}

	return &Note{
		ID:         m.ID,
		Title:      m.Title,
		Visibility: m.Visibility,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
		Links:      m.Links,
		Body:       body,
		Version:    fingerprint(data),
	}, nil
}

// findClosingFence returns the offset of a line that is exactly the fence, or
// -1. The offset is the start of that line.
func findClosingFence(rest []byte) int {
	offset := 0
	for offset < len(rest) {
		lineEnd := bytes.IndexByte(rest[offset:], '\n')
		var line []byte
		if lineEnd < 0 {
			line = rest[offset:]
		} else {
			line = rest[offset : offset+lineEnd]
		}
		if bytes.Equal(bytes.TrimRight(line, "\r"), yamlFence) {
			return offset
		}
		if lineEnd < 0 {
			break
		}
		offset += lineEnd + 1
	}
	return -1
}

func fingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
