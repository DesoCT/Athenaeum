package search

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompileProducesValidFTS5 is the load-bearing test for the whole query
// path: whatever the user types, the expression handed to SQLite must parse.
//
// It runs every candidate against a real FTS5 table rather than asserting on
// the string, because "valid FTS5" is defined by SQLite and not by this file.
func TestCompileProducesValidFTS5(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "probe.sqlite"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`CREATE VIRTUAL TABLE f USING fts5(path, title, headings, body, tags, tokenize='porter unicode61')`,
	); err != nil {
		t.Fatalf("create: %v", err)
	}

	inputs := []string{
		"simple", "two words", "  padded  ",
		`"`, `""`, `"""`, `AND`, `OR`, `NOT`, `NEAR`, `a NEAR b`, `NEAR(a b, 3)`,
		`(`, `)`, `()`, `(a`, `a)`, `*`, `**`, `a*`, `*a`,
		`^caret`, `-minus`, `+plus`, `a AND`, `AND a`, `a OR OR b`,
		`{brace}`, `[bracket]`, `col:on`, `semi;colon`, `back\slash`,
		`'; DROP TABLE f; --`, `" OR 1=1 --`, `%wild_card%`,
		`"quoted phrase"`, `"unterminated phrase`, `mixed "and quoted" terms`,
		"tab\there", "new\nline", "null\x00byte",
		"café", "日本語", "Ünïcödé",
		strings.Repeat("long ", 300),
	}

	for _, input := range inputs {
		compiled, err := compile(input)
		if errors.Is(err, ErrNoSearchableTerms) {
			continue // A query with no words is refused before it reaches SQLite.
		}
		if err != nil {
			t.Errorf("compile(%q) returned %v", input, err)
			continue
		}
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM f WHERE f MATCH ?`, compiled.Expression).Scan(&count); err != nil {
			t.Errorf("compile(%q) produced %q, which SQLite rejected: %v",
				input, compiled.Expression, err)
		}
	}
}

func TestCompileShape(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		terms []string
	}{
		{"single word is prefix matched", "index", `"index"*`, []string{"index"}},
		{"words are ANDed", "worker pool", `"worker" AND "pool"*`, []string{"worker", "pool"}},
		{"punctuation is dropped", "worker-pool!", `"worker" AND "pool"*`, []string{"worker", "pool"}},
		{"a quoted phrase stays exact", `"worker pool"`, `"worker" "pool"`, []string{"worker", "pool"}},
		{"a one-letter word is not prefix matched", "a", `"a"`, []string{"a"}},
		{"case is folded", "Worker POOL", `"worker" AND "pool"*`, []string{"worker", "pool"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiled, err := compile(test.input)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if compiled.Expression != test.want {
				t.Errorf("expression = %q, want %q", compiled.Expression, test.want)
			}
			if strings.Join(compiled.Terms, ",") != strings.Join(test.terms, ",") {
				t.Errorf("terms = %v, want %v", compiled.Terms, test.terms)
			}
		})
	}
}

func TestCompileRejectsWordlessQueries(t *testing.T) {
	for _, input := range []string{"", "   ", "!!!", "-", "()", "***"} {
		if _, err := compile(input); !errors.Is(err, ErrNoSearchableTerms) {
			t.Errorf("compile(%q) error = %v, want ErrNoSearchableTerms", input, err)
		}
	}
}

func TestSegments(t *testing.T) {
	tests := []struct {
		name    string
		snippet string
		want    []Segment
	}{
		{"plain text", "no match here", []Segment{{Text: "no match here"}}},
		{
			"one highlight",
			"a " + highlightOpen + "hit" + highlightClose + " b",
			[]Segment{{Text: "a "}, {Text: "hit", Match: true}, {Text: " b"}},
		},
		{
			"two highlights",
			highlightOpen + "one" + highlightClose + " and " + highlightOpen + "two" + highlightClose,
			[]Segment{{Text: "one", Match: true}, {Text: " and "}, {Text: "two", Match: true}},
		},
		{"empty", "", nil},
		{
			"unbalanced delimiter is passed through as text",
			"dangling " + highlightOpen + "open",
			[]Segment{{Text: "dangling " + highlightOpen + "open"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := segments(test.snippet)
			if len(got) != len(test.want) {
				t.Fatalf("segments = %+v, want %+v", got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Errorf("segment %d = %+v, want %+v", i, got[i], test.want[i])
				}
			}
		})
	}
}

func TestLocate(t *testing.T) {
	content := "# Title\n\nFirst paragraph.\n\n## Section\n\nThe bounded worker pool lives here.\n"

	tests := []struct {
		name  string
		terms []string
		want  int
	}{
		{"exact word", []string{"bounded"}, 7},
		{"two words on one line beat one", []string{"worker", "pool"}, 7},
		{"heading text", []string{"section"}, 5},
		{"stemmed term still finds a line", []string{"paragraphs"}, 3},
		{"absent term attributes nothing", []string{"absent"}, 0},
		{"no terms attributes nothing", nil, 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := locate(content, test.terms); got != test.want {
				t.Errorf("locate = %d, want %d", got, test.want)
			}
		})
	}
}

func TestStemPrefix(t *testing.T) {
	tests := map[string]string{
		"a":        "a",
		"the":      "the",
		"spec":     "spec",
		"specs":    "spec",
		"indexing": "index",
		"document": "docum",
	}
	for input, want := range tests {
		if got := stemPrefix(input); got != want {
			t.Errorf("stemPrefix(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEscapeLike(t *testing.T) {
	// A user filtering for a literal per cent sign must not get a wildcard.
	if got := escapeLike("50%_x"); got != `50\%\_x` {
		t.Errorf("escapeLike = %q", got)
	}
}
