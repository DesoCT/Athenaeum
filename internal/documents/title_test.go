package documents

import "testing"

// TestEnrichTitlesUsesRealTitles is the regression test for a bug found by
// screenshotting the Map Room: the listing showed file names such as
// "02-SYSTEM-ARCHITECTURE" because enumeration never parsed the documents,
// while opening the same document showed its real title.
func TestEnrichTitlesUsesRealTitles(t *testing.T) {
	s := service(t, map[string]string{
		"docs/02-SYSTEM-ARCHITECTURE.md": "# Athenaeum v0.1 System Architecture\n\nBody.\n",
		"docs/with-front-matter.md":      "---\ntitle: From Front Matter\n---\n\n# Ignored heading\n",
		"docs/no-heading.md":             "Just a paragraph.\n",
		"docs/h2-only.md":                "## Only a second level\n",
	})

	docs := s.ws.Documents()
	s.EnrichTitles(docs)

	want := map[string]string{
		"docs/02-SYSTEM-ARCHITECTURE.md": "Athenaeum v0.1 System Architecture",
		"docs/with-front-matter.md":      "From Front Matter",
		"docs/no-heading.md":             "no-heading",
		"docs/h2-only.md":                "Only a second level",
	}
	for _, doc := range docs {
		if got := want[doc.ID]; got != "" && doc.Title != got {
			t.Errorf("%s title = %q, want %q", doc.ID, doc.Title, got)
		}
	}
}

// TestEnrichTitlesMatchesRead keeps the listing and the document view from ever
// disagreeing about a title.
func TestEnrichTitlesMatchesRead(t *testing.T) {
	s := service(t, map[string]string{
		"a.md": "---\ntitle: Canonical\n---\n\n# Different\n",
		"b.md": "# From Heading\n",
		"c.md": "no headings here\n",
	})

	docs := s.ws.Documents()
	s.EnrichTitles(docs)

	for _, doc := range docs {
		full, err := s.Read(doc.ID)
		if err != nil {
			t.Fatalf("Read %s: %v", doc.ID, err)
		}
		if doc.Title != full.Title {
			t.Errorf("%s: listing says %q but Read says %q", doc.ID, doc.Title, full.Title)
		}
	}
}

// TestEnrichTitlesCacheInvalidates keeps a stale title from surviving an edit.
func TestEnrichTitlesCacheInvalidates(t *testing.T) {
	s := writableService(t, map[string]string{"a.md": "# First\n"})

	docs := s.ws.Documents()
	s.EnrichTitles(docs)
	if docs[0].Title != "First" {
		t.Fatalf("title = %q, want First", docs[0].Title)
	}

	// Rewrite with different content and a different size, then re-enumerate.
	if _, err := s.Write(WriteRequest{ID: "a.md", Content: "# Second heading\n"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	refreshed := s.ws.Documents()
	s.EnrichTitles(refreshed)
	if refreshed[0].Title != "Second heading" {
		t.Errorf("title = %q, want Second heading; the cache did not invalidate", refreshed[0].Title)
	}
}
