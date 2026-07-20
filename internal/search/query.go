package search

import (
	"errors"
	"slices"
	"strings"
	"unicode"
)

// snippetWindow is the maximum number of runes in a result snippet, and lead is
// how much context to keep before the first matched term.
const (
	snippetWindow = 220
	snippetLead   = 60
)

// ErrNoSearchableTerms reports a query that contains nothing to search for,
// such as one made entirely of punctuation.
var ErrNoSearchableTerms = errors.New("the query contains no searchable words")

// maxQueryRunes bounds the query. A very long query is not a useful search and
// would only cost tokenizer time.
const maxQueryRunes = 512

// compiled is a user query turned into something safe to hand to FTS5.
type compiled struct {
	// Expression is the FTS5 MATCH expression.
	Expression string
	// Terms are the plain words the user asked for, used to locate the match
	// inside the authoritative file.
	Terms []string
}

// compile turns a user query into an FTS5 MATCH expression.
//
// FTS5 query syntax is a small language of its own: bare input containing `"`,
// `*`, `(`, `NEAR`, or a trailing `AND` is either a syntax error or means
// something the user did not intend. Rather than passing input through and
// hoping, every word is extracted and re-quoted, so the expression handed to
// SQLite is always well-formed. R7 asks for full-text search, not a query
// language (C10), so the only syntax honoured is a double-quoted phrase.
//
// Words are ANDed: adding a word should narrow the result set, which is what
// every search box in the world does.
func compile(raw string) (compiled, error) {
	if len([]rune(raw)) > maxQueryRunes {
		raw = string([]rune(raw)[:maxQueryRunes])
	}

	groups := splitQuery(raw)
	if len(groups) == 0 {
		return compiled{}, ErrNoSearchableTerms
	}

	var (
		clauses []string
		terms   []string
	)
	for i, group := range groups {
		quoted := make([]string, 0, len(group.words))
		for _, word := range group.words {
			quoted = append(quoted, `"`+strings.ReplaceAll(word, `"`, `""`)+`"`)
			terms = append(terms, word)
		}
		clause := strings.Join(quoted, " ")

		// The final bare word is prefix-matched so an incremental search shows
		// results while the user is still typing. A phrase is left exact,
		// because the user closed the quotes deliberately.
		last := i == len(groups)-1
		if last && !group.phrase && len(group.words) == 1 && len([]rune(group.words[0])) >= 2 {
			clause += "*"
		}
		clauses = append(clauses, clause)
	}

	return compiled{Expression: strings.Join(clauses, " AND "), Terms: terms}, nil
}

// queryGroup is one phrase or one bare word from the input.
type queryGroup struct {
	words  []string
	phrase bool
}

// splitQuery extracts words, treating a double-quoted run as one phrase.
//
// A word is a maximal run of letters and digits, so punctuation can never reach
// the FTS5 parser. An unterminated quote is treated as a phrase running to the
// end of the input rather than as an error: the user is simply mid-typing.
func splitQuery(raw string) []queryGroup {
	var (
		groups  []queryGroup
		current []rune
		phrase  []string
		inQuote bool
	)

	flushWord := func() {
		if len(current) == 0 {
			return
		}
		word := strings.ToLower(string(current))
		current = current[:0]
		if inQuote {
			phrase = append(phrase, word)
			return
		}
		groups = append(groups, queryGroup{words: []string{word}})
	}
	flushPhrase := func() {
		if len(phrase) > 0 {
			groups = append(groups, queryGroup{words: phrase, phrase: true})
			phrase = nil
		}
	}

	for _, r := range raw {
		switch {
		case r == '"':
			flushWord()
			if inQuote {
				flushPhrase()
			}
			inQuote = !inQuote
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			current = append(current, r)
		default:
			flushWord()
		}
	}
	flushWord()
	flushPhrase()

	return groups
}

// Segment is one run of snippet text, marked when it matched the query.
type Segment struct {
	Text string `json:"text"`
	// Match is true for a run the query matched, so the UI can highlight it
	// without the server building any markup (R7).
	Match bool `json:"match,omitempty"`
}

// snippetFor builds a highlighted snippet from a line of authoritative text.
//
// FTS5's own snippet() was measured at roughly six milliseconds per row on a
// hundred-kilobyte document — around 99% of total query time — because it
// re-tokenises the whole column to choose a window. The match location is
// already known here, and the file has already been read to find it, so the
// snippet costs a scan of one line instead.
//
// Building it from the file rather than from the index also means the snippet
// shows what the document says now, not what the projection last recorded (C2).
func snippetFor(text string, terms []string) []Segment {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	spans := matchSpans(runes, terms)

	// Centre the window on the first match, keeping a little context before it.
	start := 0
	if len(spans) > 0 && spans[0].start > snippetLead {
		start = spans[0].start - snippetLead
	}
	end := min(start+snippetWindow, len(runes))

	var out []Segment
	appendText := func(from, to int) {
		if from < to {
			out = append(out, Segment{Text: string(runes[from:to])})
		}
	}

	if start > 0 {
		out = append(out, Segment{Text: "…"})
	}
	cursor := start
	for _, span := range spans {
		if span.start >= end {
			break
		}
		if span.start < cursor {
			continue // Overlapping match already covered.
		}
		appendText(cursor, span.start)
		out = append(out, Segment{Text: string(runes[span.start:min(span.end, end)]), Match: true})
		cursor = span.end
	}
	appendText(cursor, end)
	if end < len(runes) {
		out = append(out, Segment{Text: "…"})
	}
	return out
}

// span is a matched range within a line, in rune offsets.
type span struct{ start, end int }

// matchSpans finds every term occurrence in a line, merged and ordered.
//
// Matching is by the same truncated stem the locator uses, so what is
// highlighted is what caused the line to be chosen.
func matchSpans(runes []rune, terms []string) []span {
	lower := []rune(strings.ToLower(string(runes)))
	var spans []span

	for _, term := range terms {
		needle := []rune(stemPrefix(term))
		if len(needle) == 0 {
			continue
		}
		for i := 0; i+len(needle) <= len(lower); i++ {
			if !equalAt(lower, needle, i) {
				continue
			}
			// Extend over the rest of the word, so "index" highlights the whole
			// of "indexing" rather than a fragment of it.
			end := i + len(needle)
			for end < len(lower) && isWordRune(lower[end]) {
				end++
			}
			spans = append(spans, span{start: i, end: end})
			i = end - 1
		}
	}

	slices.SortFunc(spans, func(a, b span) int { return a.start - b.start })

	// Merge overlaps so a segment is never emitted twice.
	merged := spans[:0]
	for _, s := range spans {
		if len(merged) > 0 && s.start <= merged[len(merged)-1].end {
			if s.end > merged[len(merged)-1].end {
				merged[len(merged)-1].end = s.end
			}
			continue
		}
		merged = append(merged, s)
	}
	return merged
}

func equalAt(haystack, needle []rune, at int) bool {
	for i, r := range needle {
		if haystack[at+i] != r {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

// locate finds the best line for a match inside a document body.
//
// The index knows a document matched; only the authoritative file knows where.
// Scanning it here means the reported line is always correct for the file as it
// is now, even when the projection has not caught up yet (C2).
//
// Returns a 1-based line number and that line's text, or 0 and "" when no line
// can be attributed.
func locate(content string, terms []string) (int, string) {
	if len(terms) == 0 {
		return 0, ""
	}
	needles := make([]string, 0, len(terms))
	for _, term := range terms {
		needles = append(needles, stemPrefix(term))
	}

	best, bestScore, bestText := 0, 0, ""
	for i, line := range strings.Split(content, "\n") {
		lower := strings.ToLower(line)
		score := 0
		for _, needle := range needles {
			if strings.Contains(lower, needle) {
				score++
			}
		}
		if score > bestScore {
			best, bestScore, bestText = i+1, score, line
			if score == len(needles) {
				break // Every term on one line; nothing will beat this.
			}
		}
	}
	return best, bestText
}

// stemPrefix approximates the porter stemmer for line location.
//
// The index matches stems, so a document containing "indexing" is a result for
// "indexed" — but a literal substring scan for "indexed" would then find no
// line at all. Truncating the term is a deliberate over-match: pointing at a
// nearby line beats pointing at nothing, and the highlight is temporary.
func stemPrefix(term string) string {
	runes := []rune(strings.ToLower(term))
	if len(runes) <= 4 {
		return string(runes)
	}
	cut := len(runes) - 3
	if cut < 4 {
		cut = 4
	}
	return string(runes[:cut])
}
