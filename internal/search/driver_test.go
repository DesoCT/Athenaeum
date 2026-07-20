package search

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestFTS5IsAvailable guards the assumption the whole search phase rests on.
//
// The release builds with CGO_ENABLED=0 for a single cross-compilable static
// binary (constitution C6, requirement N4), which rules out the usual CGO
// SQLite driver. If a future dependency change quietly loses FTS5, search stops
// working entirely — so the capability is asserted rather than assumed.
func TestFTS5IsAvailable(t *testing.T) {
	db := openTemp(t)

	if _, err := db.Exec(
		`CREATE VIRTUAL TABLE docs USING fts5(path, title, headings, body, tokenize='porter unicode61')`,
	); err != nil {
		t.Fatalf("FTS5 is not available in this build: %v", err)
	}
}

// TestSnippetAndHighlight covers the result rendering R7 requires: snippets
// with matched terms marked.
func TestSnippetAndHighlight(t *testing.T) {
	db := openTemp(t)
	mustExec(t, db, `CREATE VIRTUAL TABLE docs USING fts5(path, title, headings, body, tokenize='porter unicode61')`)
	mustExec(t, db,
		`INSERT INTO docs VALUES (?,?,?,?)`,
		"docs/spec/02-SYSTEM-ARCHITECTURE.md",
		"System Architecture",
		"Concurrency model",
		"Indexing uses a bounded worker pool.",
	)

	var path, snippet string
	err := db.QueryRow(
		`SELECT path, snippet(docs, 3, '[', ']', '…', 8) FROM docs WHERE docs MATCH ? ORDER BY rank`,
		"bounded",
	).Scan(&path, &snippet)
	if err != nil {
		t.Fatalf("MATCH query failed: %v", err)
	}

	if path != "docs/spec/02-SYSTEM-ARCHITECTURE.md" {
		t.Errorf("path = %q", path)
	}
	if snippet != "Indexing uses a [bounded] worker pool." {
		t.Errorf("snippet = %q; highlight delimiters are needed for R7", snippet)
	}
}

// TestPorterStemming keeps "index" finding "Indexing", which is what makes
// full-text search feel usable rather than literal.
func TestPorterStemming(t *testing.T) {
	db := openTemp(t)
	mustExec(t, db, `CREATE VIRTUAL TABLE docs USING fts5(body, tokenize='porter unicode61')`)
	mustExec(t, db, `INSERT INTO docs VALUES (?)`, "Indexing uses a bounded worker pool.")

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM docs WHERE docs MATCH 'index'`).Scan(&count); err != nil {
		t.Fatalf("stemmed query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("stemmed match count = %d, want 1", count)
	}
}

// TestUnicodeTokenizer confirms non-ASCII content is searchable, since a
// Markdown corpus is not going to be English-only.
func TestUnicodeTokenizer(t *testing.T) {
	db := openTemp(t)
	mustExec(t, db, `CREATE VIRTUAL TABLE docs USING fts5(body, tokenize='porter unicode61')`)
	mustExec(t, db, `INSERT INTO docs VALUES (?)`, "Le café était très bon. 日本語のテキスト.")

	for _, term := range []string{"café", "très"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM docs WHERE docs MATCH ?`, term).Scan(&count); err != nil {
			t.Fatalf("query %q failed: %v", term, err)
		}
		if count != 1 {
			t.Errorf("match count for %q = %d, want 1", term, count)
		}
	}
}

func openTemp(t *testing.T) *sql.DB {
	t.Helper()
	// A file rather than :memory:, so this exercises the same path the real
	// index uses under the OS cache directory.
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "probe.sqlite"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
