package documents

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// IndexView is the projection the search index consumes (R7, spec 02 section 3.5).
//
// It exists so the index never parses Markdown itself. ADR-0003 makes this
// package the single source of truth for heading identity, and a second parser
// in the search package would be exactly the divergence that ADR forbids.
type IndexView struct {
	ID      string
	Title   string
	Version string
	Size    int64
	ModTime string
	Groups  []string
	// Headings is the authoritative outline (ADR-0003).
	Headings []Heading
	// Body is the indexable document text with front matter removed.
	Body string
	// Tags are front-matter tags or keywords, when front-matter indexing is on.
	Tags []string
}

// IndexOptions mirror the `[search]` configuration block (spec 05).
type IndexOptions struct {
	// IncludeCodeBlocks keeps fenced and indented code in the indexed body.
	IncludeCodeBlocks bool
	// IncludeFrontMatter indexes front-matter tags and keywords.
	IncludeFrontMatter bool
}

// IndexView reads one document and reduces it to indexable text.
//
// A document the workspace does not include returns the same not-found error a
// read does, so an excluded file can never reach the index (acceptance B1).
func (s *Service) IndexView(id string, opts IndexOptions) (*IndexView, error) {
	doc, err := s.Read(id)
	if err != nil {
		return nil, err
	}

	view := &IndexView{
		ID:       id,
		Title:    doc.Title,
		Version:  doc.Version,
		Size:     doc.Size,
		Groups:   doc.Groups,
		Headings: doc.Outline,
	}
	if entry, ok := s.ws.Lookup(id); ok {
		view.ModTime = entry.ModTime
	}

	// A non-UTF-8 document has no usable text. It stays in the index by path and
	// title so it remains findable, rather than vanishing from the workspace.
	if doc.Encoding != EncodingUTF8 {
		return view, nil
	}

	content := []byte(doc.Content)
	fm := parseFrontMatter(content, s.ws.Config().Documents.FrontMatter)
	body := content[fm.BodyOffset:]

	if opts.IncludeCodeBlocks {
		view.Body = string(body)
	} else {
		view.Body = proseOnly(body)
	}
	if opts.IncludeFrontMatter {
		view.Tags = tagsFrom(fm.Fields)
	}
	return view, nil
}

// proseOnly renders a body with fenced and indented code blocks removed.
//
// It collects inline text across the whole tree rather than scanning lines: a
// line scanner misreads a fence indented inside a list or blockquote, and
// walking inline nodes handles tables, quotes, and lists uniformly. Inline code
// spans are kept — `index_code_blocks` is about blocks, not spans.
func proseOnly(body []byte) string {
	doc := markdown.Parser().Parse(text.NewReader(body))

	var b strings.Builder
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := node.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			b.Write(typed.Segment.Value(body))
			b.WriteByte('\n')
		case *ast.String:
			b.Write(typed.Value)
			b.WriteByte('\n')
		case *ast.AutoLink:
			b.Write(typed.URL(body))
			b.WriteByte('\n')
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// tagsFrom extracts front-matter tags or keywords.
//
// Only these two fields are indexed. Indexing every scalar would put arbitrary
// metadata — dates, authors, internal identifiers — into full-text results
// where the user has no way to understand why a document matched.
func tagsFrom(fields map[string]any) []string {
	var tags []string
	for _, key := range []string{"tags", "keywords"} {
		value, ok := fields[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			tags = append(tags, typed)
		case []any:
			for _, item := range typed {
				if s, ok := item.(string); ok {
					tags = append(tags, s)
				}
			}
		case []string:
			tags = append(tags, typed...)
		}
	}
	return tags
}
