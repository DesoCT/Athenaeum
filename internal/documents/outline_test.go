package documents

import (
	"slices"
	"testing"
)

func outlineOf(t *testing.T, source string) []Heading {
	t.Helper()
	fm := parseFrontMatter([]byte(source), []string{"yaml", "toml"})
	return buildOutline([]byte(source)[fm.BodyOffset:], fm.BodyLine)
}

func TestOutlineLevelsAndText(t *testing.T) {
	got := outlineOf(t, `# Title

Body text.

## Section one

### Nested

## Section two
`)

	want := []struct {
		level int
		text  string
	}{
		{1, "Title"},
		{2, "Section one"},
		{3, "Nested"},
		{2, "Section two"},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d headings, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Level != w.level || got[i].Text != w.text {
			t.Errorf("heading %d = (%d, %q), want (%d, %q)", i, got[i].Level, got[i].Text, w.level, w.text)
		}
	}
}

// TestHashInFencedCodeIsNotAHeading is the specific failure ADR-0003 exists to
// prevent: a regex-based extractor treats these as headings and every
// subsequent annotation anchor shifts.
func TestHashInFencedCodeIsNotAHeading(t *testing.T) {
	got := outlineOf(t, "# Real heading\n\n```bash\n# This is a shell comment\necho hi\n```\n\n## Second real heading\n")

	if len(got) != 2 {
		t.Fatalf("got %d headings, want 2: %+v", len(got), got)
	}
	if got[0].Text != "Real heading" || got[1].Text != "Second real heading" {
		t.Errorf("headings = %q, %q", got[0].Text, got[1].Text)
	}
}

func TestHashInIndentedCodeIsNotAHeading(t *testing.T) {
	got := outlineOf(t, "# Real\n\nParagraph.\n\n    # indented code, not a heading\n\n## Also real\n")

	if len(got) != 2 {
		t.Fatalf("got %d headings, want 2: %+v", len(got), got)
	}
}

func TestTildeFencedCodeIsNotAHeading(t *testing.T) {
	got := outlineOf(t, "# Real\n\n~~~\n# not a heading\n~~~\n")

	if len(got) != 1 {
		t.Fatalf("got %d headings, want 1: %+v", len(got), got)
	}
}

func TestSetextHeadingsRecognised(t *testing.T) {
	got := outlineOf(t, "Title\n=====\n\nSubtitle\n--------\n")

	if len(got) != 2 {
		t.Fatalf("got %d headings, want 2: %+v", len(got), got)
	}
	if got[0].Level != 1 || got[0].Text != "Title" {
		t.Errorf("first heading = (%d, %q), want (1, \"Title\")", got[0].Level, got[0].Text)
	}
	if got[1].Level != 2 || got[1].Text != "Subtitle" {
		t.Errorf("second heading = (%d, %q), want (2, \"Subtitle\")", got[1].Level, got[1].Text)
	}
}

// TestHeadingLines are what the frontend matches against (ADR-0003).
func TestHeadingLines(t *testing.T) {
	got := outlineOf(t, "# One\n\nText.\n\n## Two\n\nMore.\n\n### Three\n")

	wantLines := []int{1, 5, 9}
	for i, want := range wantLines {
		if got[i].Line != want {
			t.Errorf("heading %d (%q) line = %d, want %d", i, got[i].Text, got[i].Line, want)
		}
	}
}

// TestHeadingLinesAccountForFrontMatter keeps anchors correct in documents that
// open with a front matter block.
func TestHeadingLinesAccountForFrontMatter(t *testing.T) {
	got := outlineOf(t, "---\ntitle: Example\n---\n\n# First\n\n## Second\n")

	if len(got) != 2 {
		t.Fatalf("got %d headings, want 2: %+v", len(got), got)
	}
	if got[0].Line != 5 {
		t.Errorf("first heading line = %d, want 5", got[0].Line)
	}
	if got[1].Line != 7 {
		t.Errorf("second heading line = %d, want 7", got[1].Line)
	}
}

func TestHeadingPaths(t *testing.T) {
	got := outlineOf(t, `# System architecture

## Search

### Index

## Storage
`)

	tests := []struct {
		index int
		want  []string
	}{
		{0, []string{"System architecture"}},
		{1, []string{"System architecture", "Search"}},
		{2, []string{"System architecture", "Search", "Index"}},
		{3, []string{"System architecture", "Storage"}},
	}
	for _, tc := range tests {
		if !slices.Equal(got[tc.index].Path, tc.want) {
			t.Errorf("heading %d path = %v, want %v", tc.index, got[tc.index].Path, tc.want)
		}
	}
}

// TestHeadingPathHandlesLevelSkips keeps the chain sane when an author jumps
// from h1 straight to h3.
func TestHeadingPathHandlesLevelSkips(t *testing.T) {
	got := outlineOf(t, "# One\n\n### Three\n\n## Two\n")

	if !slices.Equal(got[1].Path, []string{"One", "Three"}) {
		t.Errorf("skipped-level path = %v, want [One Three]", got[1].Path)
	}
	if !slices.Equal(got[2].Path, []string{"One", "Two"}) {
		t.Errorf("path after skip = %v, want [One Two]", got[2].Path)
	}
}

func TestSlugsAreUniqueAndStable(t *testing.T) {
	got := outlineOf(t, "# Overview\n\n## Overview\n\n## Overview\n")

	slugs := []string{got[0].Slug, got[1].Slug, got[2].Slug}
	want := []string{"overview", "overview-1", "overview-2"}
	if !slices.Equal(slugs, want) {
		t.Errorf("slugs = %v, want %v", slugs, want)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Simple", "simple"},
		{"Two Words", "two-words"},
		{"Punctuation! Here?", "punctuation-here"},
		{"already-hyphenated", "already-hyphenated"},
		{"snake_case_name", "snake-case-name"},
		{"  Leading and trailing  ", "leading-and-trailing"},
		{"Numbers 123", "numbers-123"},
		{"Multiple   spaces", "multiple-spaces"},
		{"", ""},
		{"!!!", ""},
	}
	for _, tc := range tests {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHeadingTextStripsInlineMarkup(t *testing.T) {
	got := outlineOf(t, "# A *bold* and `code` heading\n\n## A [link](http://example.com) heading\n")

	if got[0].Text != "A bold and code heading" {
		t.Errorf("text = %q, want %q", got[0].Text, "A bold and code heading")
	}
	if got[1].Text != "A link heading" {
		t.Errorf("text = %q, want %q", got[1].Text, "A link heading")
	}
}

func TestEmptyDocumentHasNoOutline(t *testing.T) {
	if got := outlineOf(t, ""); len(got) != 0 {
		t.Errorf("empty document produced %d headings", len(got))
	}
	if got := outlineOf(t, "Just a paragraph.\n"); len(got) != 0 {
		t.Errorf("headingless document produced %d headings", len(got))
	}
}

func TestHeadingInsideBlockquoteIsStillAHeading(t *testing.T) {
	// CommonMark treats "> # Title" as a heading inside a quote. The outline
	// should report it, because the renderer will render it as a heading.
	got := outlineOf(t, "> # Quoted heading\n")

	if len(got) != 1 {
		t.Fatalf("got %d headings, want 1: %+v", len(got), got)
	}
}
