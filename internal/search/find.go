package search

import (
	"encoding/json"
	"fmt"
	"strings"

	"athenaeum/internal/documents"
)

// bm25 column weights, in the order the FTS5 table declares them:
// path, title, headings, body, tags.
//
// A title hit is the strongest signal that a document is the one being looked
// for; a body hit is the weakest because a long document matches many words.
// The weights are deliberately blunt: R7 asks for lexical search, and an
// elaborate scoring model would be unexplainable to the user (C8).
const rankExpression = `bm25(documents_fts, 4.0, 10.0, 6.0, 1.0, 3.0)`

// scanCap bounds how many ranked rows a single query will walk while applying
// the Git filter, which cannot be expressed in SQL. It keeps a filter that
// matches almost nothing from turning into a full-table scan.
const scanCap = 2000

// hit is one ranked row, before the authoritative file is consulted.
type hit struct {
	documentID  string
	title       string
	groups      []string
	outline     []documents.Heading
	bodySnippet string
	anySnippet  string
	score       float64
}

// find runs the ranked query and applies the filters SQL can express.
//
// The Git filter is applied by the caller because Git state lives in memory,
// not in the projection: putting it in the index would make the index stale
// every time the working tree changed.
func (s *Service) find(expr string, filters Filters, limit int, allow func(string) bool) ([]hit, error) {
	var (
		where = []string{"documents_fts MATCH ?"}
		args  = []any{expr}
	)

	// A path filter is a plain substring of the document ID. R7 asks for a path
	// filter, not a glob language (C10). LIKE wildcards in user input are
	// escaped so `%` filters for a literal per cent sign.
	if filters.Path != "" {
		where = append(where, `d.id LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(strings.ToLower(filters.Path))+"%")
	}
	// Group membership is stored space-separated, so the match is on a padded
	// string and cannot match a group whose ID is merely a prefix of another.
	if filters.Group != "" {
		where = append(where, `(' ' || d.groups || ' ') LIKE ? ESCAPE '\'`)
		args = append(args, "% "+escapeLike(filters.Group)+" %")
	}

	query := fmt.Sprintf(`
		SELECT d.id, d.title, d.groups, d.outline,
		       snippet(documents_fts, 3, char(2), char(3), '…', 14),
		       snippet(documents_fts, -1, char(2), char(3), '…', 14),
		       %[1]s AS score
		FROM documents_fts
		JOIN documents d ON d.rowid = documents_fts.rowid
		WHERE %[2]s
		ORDER BY score
		LIMIT %[3]d`, rankExpression, strings.Join(where, " AND "), scanCap)

	rows, err := s.index.reader.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hits := make([]hit, 0, limit)
	for rows.Next() {
		var (
			h       hit
			groups  string
			outline string
		)
		if err := rows.Scan(&h.documentID, &h.title, &groups, &outline,
			&h.bodySnippet, &h.anySnippet, &h.score); err != nil {
			return nil, err
		}
		if groups != "" {
			h.groups = strings.Fields(groups)
		}
		if outline != "" {
			// A projection written by an older build could fail to decode. It
			// costs a heading label, never a result.
			_ = json.Unmarshal([]byte(outline), &h.outline)
		}
		if !allow(h.documentID) {
			continue
		}
		hits = append(hits, h)
		if len(hits) >= limit {
			break
		}
	}
	return hits, rows.Err()
}

// escapeLike neutralises LIKE wildcards in user input.
func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

// headingFor returns the heading containing a source line.
//
// Headings are in document order, so the enclosing heading is the last one
// declared at or above the line.
func headingFor(outline []documents.Heading, line int) (path []string, slug string) {
	if line <= 0 {
		return nil, ""
	}
	for i := len(outline) - 1; i >= 0; i-- {
		if outline[i].Line <= line {
			return outline[i].Path, outline[i].Slug
		}
	}
	return nil, ""
}
