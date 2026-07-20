package search

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestSnippetFor(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		terms []string
		want  []Segment
	}{
		{
			"marks the matched word",
			"Indexing uses a bounded worker pool.",
			[]string{"bounded"},
			[]Segment{
				{Text: "Indexing uses a "},
				{Text: "bounded", Match: true},
				{Text: " worker pool."},
			},
		},
		{
			"marks every term",
			"a worker and a pool",
			[]string{"worker", "pool"},
			[]Segment{
				{Text: "a "},
				{Text: "worker", Match: true},
				{Text: " and a "},
				{Text: "pool", Match: true},
			},
		},
		{
			"a stemmed term highlights the whole word",
			"Indexing happens in the background.",
			[]string{"indexed"},
			[]Segment{
				{Text: "Indexing", Match: true},
				{Text: " happens in the background."},
			},
		},
		{
			"matching is case-insensitive but text is preserved",
			"The Workspace is authoritative.",
			[]string{"workspace"},
			[]Segment{
				{Text: "The "},
				{Text: "Workspace", Match: true},
				{Text: " is authoritative."},
			},
		},
		{
			"an unmatched line is returned as plain text",
			"Nothing to see here.",
			[]string{"absent"},
			[]Segment{{Text: "Nothing to see here."}},
		},
		{"empty text yields no snippet", "", []string{"anything"}, nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := snippetFor(test.text, test.terms)
			if len(got) != len(test.want) {
				t.Fatalf("snippet = %+v, want %+v", got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Errorf("segment %d = %+v, want %+v", i, got[i], test.want[i])
				}
			}
		})
	}
}

// TestSnippetWindow proves a long line is trimmed around the match rather than
// returned whole, and that the elision is visible.
func TestSnippetWindow(t *testing.T) {
	prefix := strings.Repeat("filler ", 200)
	text := prefix + "the bounded worker pool " + strings.Repeat("tail ", 200)

	got := snippetFor(text, []string{"bounded"})
	if len(got) == 0 {
		t.Fatal("no snippet")
	}
	if got[0].Text != "…" {
		t.Errorf("a trimmed snippet should open with an ellipsis, got %q", got[0].Text)
	}
	if got[len(got)-1].Text != "…" {
		t.Errorf("a trimmed snippet should close with an ellipsis, got %q", got[len(got)-1].Text)
	}

	var total int
	var marked bool
	for _, segment := range got {
		total += len([]rune(segment.Text))
		if segment.Match {
			marked = true
			if segment.Text != "bounded" {
				t.Errorf("marked run = %q, want %q", segment.Text, "bounded")
			}
		}
	}
	if !marked {
		t.Error("the matched term was not marked")
	}
	if total > snippetWindow+8 {
		t.Errorf("snippet is %d runes, above the %d window", total, snippetWindow)
	}
}

// TestSnippetHandlesMultibyteText guards against slicing a rune in half, which
// would corrupt the text the user is shown.
func TestSnippetHandlesMultibyteText(t *testing.T) {
	text := strings.Repeat("日本語のテキスト ", 40) + "café bounded " + strings.Repeat("さらに ", 40)

	got := snippetFor(text, []string{"bounded"})
	var joined strings.Builder
	for _, segment := range got {
		joined.WriteString(segment.Text)
	}
	if !utf8.ValidString(joined.String()) {
		t.Fatal("the snippet is not valid UTF-8; a rune was split")
	}
	if !strings.Contains(joined.String(), "bounded") {
		t.Error("the match is missing from the snippet")
	}
}

func TestLocate(t *testing.T) {
	content := "# Title\n\nFirst paragraph.\n\n## Section\n\nThe bounded worker pool lives here.\n"

	tests := []struct {
		name  string
		terms []string
		want  int
		text  string
	}{
		{"exact word", []string{"bounded"}, 7, "The bounded worker pool lives here."},
		{"two words on one line beat one", []string{"worker", "pool"}, 7, "The bounded worker pool lives here."},
		{"heading text", []string{"section"}, 5, "## Section"},
		{"stemmed term still finds a line", []string{"paragraphs"}, 3, "First paragraph."},
		{"absent term attributes nothing", []string{"absent"}, 0, ""},
		{"no terms attributes nothing", nil, 0, ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			line, text := locate(content, test.terms)
			if line != test.want {
				t.Errorf("locate line = %d, want %d", line, test.want)
			}
			if text != test.text {
				t.Errorf("locate text = %q, want %q", text, test.text)
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
