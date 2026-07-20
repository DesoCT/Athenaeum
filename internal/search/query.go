package search

import (
	"errors"
	"strings"
	"unicode"
)

// Snippet highlight delimiters.
//
// STX and ETX rather than markup: the snippet is turned into typed segments
// before it leaves the server, so no HTML is ever constructed from document
// text and the frontend cannot be made to render an injected tag
// (spec 03 section 9).
const (
	highlightOpen  = "\x02"
	highlightClose = "\x03"
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

// segments splits a delimited FTS5 snippet into typed runs.
func segments(snippet string) []Segment {
	if snippet == "" {
		return nil
	}
	var out []Segment
	rest := snippet
	for {
		open := strings.Index(rest, highlightOpen)
		if open < 0 {
			break
		}
		close := strings.Index(rest[open:], highlightClose)
		if close < 0 {
			break
		}
		close += open

		if before := rest[:open]; before != "" {
			out = append(out, Segment{Text: before})
		}
		if match := rest[open+len(highlightOpen) : close]; match != "" {
			out = append(out, Segment{Text: match, Match: true})
		}
		rest = rest[close+len(highlightClose):]
	}
	if rest != "" {
		out = append(out, Segment{Text: rest})
	}
	return out
}

// locate finds the best line for a match inside a document body.
//
// The index knows a document matched; only the authoritative file knows where.
// Scanning it here means the reported line is always correct for the file as it
// is now, even when the projection has not caught up yet (C2).
//
// Returns a 1-based line number, or 0 when no line can be attributed.
func locate(content string, terms []string) int {
	if len(terms) == 0 {
		return 0
	}
	needles := make([]string, 0, len(terms))
	for _, term := range terms {
		needles = append(needles, stemPrefix(term))
	}

	best, bestScore := 0, 0
	for i, line := range strings.Split(content, "\n") {
		lower := strings.ToLower(line)
		score := 0
		for _, needle := range needles {
			if strings.Contains(lower, needle) {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = i+1, score
			if score == len(needles) {
				break // Every term on one line; nothing will beat this.
			}
		}
	}
	return best
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
