// Package documents reads workspace files and produces document metadata
// (spec 02 section 3.3).
//
// v0.1 supports UTF-8 Markdown. Non-UTF-8 files open read-only with an
// explanatory warning rather than being silently transcoded.
package documents

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"athenaeum/internal/workspace"
)

// Line ending kinds (acceptance D3).
const (
	LineEndingLF    = "lf"
	LineEndingCRLF  = "crlf"
	LineEndingMixed = "mixed"
)

// Encoding kinds.
const (
	EncodingUTF8    = "utf-8"
	EncodingUnknown = "unknown"
)

// Document is a fully read document with its metadata.
type Document struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Content is the document body including front matter, normalised to LF
	// for transport. LineEnding records what the file actually uses so a save
	// can restore it (acceptance D3).
	Content string `json:"content"`
	// Version is a content fingerprint used for optimistic concurrency. A
	// write must present the version it last observed (spec 02 section 5).
	Version string `json:"version"`

	Encoding   string `json:"encoding"`
	LineEnding string `json:"line_ending"`
	// HasBOM records a UTF-8 byte order mark so a save can restore it.
	HasBOM bool `json:"has_bom"`

	FrontMatter       map[string]any `json:"front_matter,omitempty"`
	FrontMatterFormat string         `json:"front_matter_format"`
	// Outline is the authoritative heading structure (ADR-0003).
	Outline []Heading `json:"outline"`

	Size         int64    `json:"size"`
	Writable     bool     `json:"writable"`
	ReadOnly     bool     `json:"read_only"`
	TooLarge     bool     `json:"too_large"`
	LargeWarning bool     `json:"large_warning"`
	Groups       []string `json:"groups,omitempty"`
	// Warnings explain any degraded state, such as a non-UTF-8 encoding or
	// malformed front matter.
	Warnings []string `json:"warnings,omitempty"`
}

// Service reads documents from a workspace.
type Service struct {
	ws *workspace.Workspace
}

// New returns a document service bound to a workspace.
func New(ws *workspace.Workspace) *Service {
	return &Service{ws: ws}
}

// Read loads one document by ID.
func (s *Service) Read(id string) (*Document, error) {
	entry, ok := s.ws.Lookup(id)
	if !ok {
		// Indistinguishable from absent, so excluded files cannot be probed.
		return nil, s.notFound(id)
	}

	absPath, err := s.ws.ResolveRead(id)
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", id, err)
	}

	doc := &Document{
		ID:           id,
		Size:         int64(len(raw)),
		Groups:       entry.Groups,
		Writable:     entry.Writable,
		TooLarge:     entry.TooLarge,
		LargeWarning: entry.LargeWarning,
		Version:      fingerprint(raw),
	}

	// A UTF-8 BOM is stripped for processing and recorded so a save restores it.
	if bom := []byte{0xEF, 0xBB, 0xBF}; bytes.HasPrefix(raw, bom) {
		doc.HasBOM = true
		raw = raw[len(bom):]
	}

	if !utf8.Valid(raw) {
		// Spec 02 section 3.3: non-UTF-8 files open read-only with an
		// explanation. The bytes are not transcoded or discarded.
		doc.Encoding = EncodingUnknown
		doc.ReadOnly = true
		doc.Writable = false
		doc.LineEnding = LineEndingLF
		doc.FrontMatterFormat = FrontMatterNone
		doc.Title = defaultTitle(id)
		doc.Warnings = append(doc.Warnings,
			"This file is not valid UTF-8, so it is open read-only. Athenaeum v0.1 edits UTF-8 Markdown only.")
		return doc, nil
	}

	doc.Encoding = EncodingUTF8
	doc.LineEnding = detectLineEnding(raw)
	if doc.TooLarge {
		doc.ReadOnly = true
		doc.Writable = false
		doc.Warnings = append(doc.Warnings, fmt.Sprintf(
			"This file is %d bytes, above the configured editable limit, so it is open read-only.", doc.Size))
	}

	// Normalise to LF for transport; the original ending is recorded above.
	normalised := normaliseNewlines(raw)
	doc.Content = string(normalised)

	cfg := s.ws.Config()
	fm := parseFrontMatter(normalised, cfg.Documents.FrontMatter)
	doc.FrontMatterFormat = fm.Format
	if len(fm.Fields) > 0 {
		doc.FrontMatter = fm.Fields
	}
	if fm.Err != nil {
		doc.Warnings = append(doc.Warnings, fm.Err.Error())
	}

	body := normalised[fm.BodyOffset:]
	doc.Outline = buildOutline(body, fm.BodyLine)
	doc.Title = resolveTitle(id, fm, doc.Outline)

	return doc, nil
}

// notFound produces the same error shape the workspace uses, so callers see a
// consistent code.
func (s *Service) notFound(id string) error {
	_, err := s.ws.ResolveRead(id)
	if err != nil {
		return err
	}
	return fmt.Errorf("document %s not found", id)
}

// resolveTitle picks the best human label: front matter title, then the first
// level-1 heading, then the first heading, then the file name.
func resolveTitle(id string, fm frontMatter, outline []Heading) string {
	if title := fm.Title(); title != "" {
		return title
	}
	for _, h := range outline {
		if h.Level == 1 {
			return h.Text
		}
	}
	if len(outline) > 0 {
		return outline[0].Text
	}
	return defaultTitle(id)
}

func defaultTitle(id string) string {
	base := path.Base(id)
	return strings.TrimSuffix(base, path.Ext(base))
}

// detectLineEnding reports the dominant line ending so a save can preserve it.
func detectLineEnding(source []byte) string {
	crlf := bytes.Count(source, []byte("\r\n"))
	total := bytes.Count(source, []byte("\n"))
	lf := total - crlf

	switch {
	case crlf > 0 && lf > 0:
		return LineEndingMixed
	case crlf > 0:
		return LineEndingCRLF
	default:
		return LineEndingLF
	}
}

// normaliseNewlines converts CRLF to LF without touching lone CR characters,
// which are legal inside content.
func normaliseNewlines(source []byte) []byte {
	if !bytes.Contains(source, []byte("\r\n")) {
		return source
	}
	return bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
}

// fingerprint hashes the exact bytes on disk. It is the document version used
// for stale-write detection.
func fingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
