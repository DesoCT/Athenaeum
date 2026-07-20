package documents

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// Heading is one entry in a document's authoritative outline.
//
// ADR-0003 makes the backend the single source of truth for heading identity.
// The frontend renders headings but adopts the Slug and Path published here,
// matching by Line.
type Heading struct {
	// Level is 1 for `#` through 6 for `######`.
	Level int `json:"level"`
	// Text is the rendered heading text with inline markup removed.
	Text string `json:"text"`
	// Slug is the stable, unique anchor for this heading within the document.
	Slug string `json:"slug"`
	// Path is the enclosing heading chain, ending with this heading's own text.
	Path []string `json:"path"`
	// Line is the 1-based source line of the heading, used to match rendered
	// headings back to this outline.
	Line int `json:"line"`
}

// markdown is the parser used for structure extraction. It enables the GFM
// extensions that affect block structure so that, for example, a `#` inside a
// fenced code block is not mistaken for a heading.
var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// buildOutline extracts the heading structure from a document body.
//
// body must be the document with any front matter removed; bodyLine is the
// 1-based source line at which body begins, so reported lines refer to the
// original file.
func buildOutline(body []byte, bodyLine int) []Heading {
	reader := text.NewReader(body)
	doc := markdown.Parser().Parse(reader)

	var headings []Heading
	slugs := newSlugSet()
	// stack holds the current heading text at each level, so a heading path can
	// be assembled without a second pass.
	var stack []Heading

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		heading, ok := node.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}

		txt := headingText(heading, body)
		line := bodyLine + offsetToLine(body, headingOffset(heading, body))

		// Pop any headings at or below this level; what remains is the parent
		// chain.
		for len(stack) > 0 && stack[len(stack)-1].Level >= heading.Level {
			stack = stack[:len(stack)-1]
		}
		path := make([]string, 0, len(stack)+1)
		for _, ancestor := range stack {
			path = append(path, ancestor.Text)
		}
		path = append(path, txt)

		h := Heading{
			Level: heading.Level,
			Text:  txt,
			Slug:  slugs.add(txt),
			Path:  path,
			Line:  line,
		}
		headings = append(headings, h)
		stack = append(stack, h)

		return ast.WalkSkipChildren, nil
	})

	return headings
}

// headingOffset returns the byte offset of a heading within the body.
//
// An ATX heading ("# Title") reports its text segment, which starts after the
// marker but on the same line. A setext heading ("Title\n=====") reports the
// text line, which is what should be pointed at.
func headingOffset(heading *ast.Heading, body []byte) int {
	if heading.Lines().Len() > 0 {
		return heading.Lines().At(0).Start
	}
	// A heading with no text segment (for example "#" alone) still occupies a
	// line; fall back to the start of the document rather than panicking.
	_ = body
	return 0
}

// offsetToLine converts a byte offset into a 0-based line count.
func offsetToLine(body []byte, offset int) int {
	if offset > len(body) {
		offset = len(body)
	}
	return bytes.Count(body[:offset], []byte("\n"))
}

// headingText renders a heading's inline content as plain text, dropping
// emphasis, code spans, and link syntax so the outline reads naturally.
func headingText(heading *ast.Heading, source []byte) string {
	var b strings.Builder
	collectText(heading, source, &b)
	return strings.TrimSpace(b.String())
}

func collectText(node ast.Node, source []byte, b *strings.Builder) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch typed := child.(type) {
		case *ast.Text:
			b.Write(typed.Segment.Value(source))
			if typed.SoftLineBreak() || typed.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(typed.Value)
		case *ast.CodeSpan:
			collectText(typed, source, b)
		case *ast.AutoLink:
			b.Write(typed.URL(source))
		default:
			collectText(child, source, b)
		}
	}
}

// slugSet generates unique, stable anchors.
type slugSet struct {
	seen map[string]int
}

func newSlugSet() *slugSet {
	return &slugSet{seen: map[string]int{}}
}

// add returns a unique slug for a heading, disambiguating repeats with a
// numeric suffix in the same way GitHub does.
func (s *slugSet) add(textValue string) string {
	base := slugify(textValue)
	if base == "" {
		base = "section"
	}
	count, exists := s.seen[base]
	s.seen[base] = count + 1
	if !exists {
		return base
	}
	return fmt.Sprintf("%s-%d", base, count)
}

// slugify lowercases, drops punctuation, and joins words with hyphens.
func slugify(value string) string {
	var b strings.Builder
	var lastHyphen bool

	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			lastHyphen = false
		case r == ' ' || r == '-' || r == '_':
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		default:
			// Punctuation is dropped entirely.
		}
	}
	return strings.TrimRight(b.String(), "-")
}
