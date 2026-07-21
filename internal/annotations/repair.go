package annotations

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Heading is the minimum a repair needs from the document outline: the heading
// path (ADR-0003 makes it authoritative) and its source line. The store adapts
// documents.Heading into this so repair stays free of the documents package and
// trivially unit-testable.
type Heading struct {
	Path []string
	Line int
}

// sourceHash fingerprints a document body so an unchanged document can skip the
// quote search entirely (the common case). It matches the "sha256:" prefix used
// on disk (spec 03 section 3).
func sourceHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// resolveAnchor computes an anchor's current state against the live document
// without mutating it (ADR-0005). It returns the state and the line range to
// display now; a detached anchor reports no lines.
//
// content is the document body exactly as the document API delivers it
// (LF-normalised, front matter included), so its line numbers and hash agree
// with what the frontend rendered.
func resolveAnchor(a Anchor, content string, headings []Heading) (state string, startLine, endLine int) {
	switch a.Type {
	case AnchorDocument:
		// A document anchor detaches only if the document itself is gone, which
		// the caller signals by never calling this with empty content plus a
		// document anchor. Present document, present anchor.
		return StateAnchored, 0, 0
	case AnchorHeading:
		if h := findHeading(headings, a.HeadingPath); h != nil {
			return StateAnchored, h.Line, h.Line
		}
		return StateDetached, 0, 0
	case AnchorText:
		// Fast path: an untouched document keeps its stored lines. Most reads
		// take this branch, so repair never runs on a document nobody edited.
		if a.SourceHash != "" && a.SourceHash == sourceHash(content) {
			return StateAnchored, a.StartLine, a.EndLine
		}
		return repairQuote(a, content)
	default:
		return StateDetached, 0, 0
	}
}

// findHeading returns the outline heading whose path equals want, or nil.
func findHeading(headings []Heading, want []string) *Heading {
	for i := range headings {
		if slicesEqual(headings[i].Path, want) {
			return &headings[i]
		}
	}
	return nil
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// repairQuote re-locates a text_quote anchor after the document changed
// (acceptance G3). It searches for the exact quote and disambiguates competing
// occurrences with the stored prefix and suffix context (a W3C
// TextQuoteSelector). A single confident match repairs; zero matches or an
// unresolved tie detaches — the anchor is never moved to a guess.
func repairQuote(a Anchor, content string) (state string, startLine, endLine int) {
	if a.Exact == "" {
		return StateDetached, 0, 0
	}

	offsets := allIndices(content, a.Exact)
	if len(offsets) == 0 {
		return StateDetached, 0, 0
	}

	chosen := -1
	if len(offsets) == 1 {
		chosen = offsets[0]
	} else {
		// Several copies of the quote: keep only those whose surrounding text
		// matches the recorded context. A unique survivor wins; anything else
		// is ambiguous and must detach rather than jump to the wrong copy.
		var matched []int
		for _, off := range offsets {
			if contextMatches(content, off, len(a.Exact), a.Prefix, a.Suffix) {
				matched = append(matched, off)
			}
		}
		if len(matched) == 1 {
			chosen = matched[0]
		} else {
			return StateDetached, 0, 0
		}
	}

	start := lineAt(content, chosen)
	end := lineAt(content, chosen+len(a.Exact)-1)
	return StateAnchored, start, end
}

// allIndices returns every start offset of needle in haystack, allowing
// overlaps so two adjacent copies are both considered.
func allIndices(haystack, needle string) []int {
	var out []int
	for i := 0; i+len(needle) <= len(haystack); {
		j := strings.Index(haystack[i:], needle)
		if j < 0 {
			break
		}
		out = append(out, i+j)
		i += j + 1
	}
	return out
}

// contextMatches reports whether the text immediately before an occurrence ends
// with prefix and the text immediately after begins with suffix. An empty
// prefix or suffix imposes no constraint on that side.
func contextMatches(content string, off, length int, prefix, suffix string) bool {
	before := content[:off]
	after := content[off+length:]
	if prefix != "" && !strings.HasSuffix(before, prefix) {
		return false
	}
	if suffix != "" && !strings.HasPrefix(after, suffix) {
		return false
	}
	return true
}

// lineAt returns the 1-based line number containing byte offset off.
func lineAt(content string, off int) int {
	if off < 0 {
		off = 0
	}
	if off > len(content) {
		off = len(content)
	}
	return strings.Count(content[:off], "\n") + 1
}
