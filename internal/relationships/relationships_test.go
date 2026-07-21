package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"athenaeum/internal/config"
	"athenaeum/internal/documents"
	"athenaeum/internal/workspace"
)

const relConfig = `
schema_version = 1
name = "Fixture"
include = ["**/*.md"]

[documents]
wiki_links = true
front_matter = ["yaml", "toml"]

[relationships.front_matter]
fields = ["related", "implements"]
`

func build(t *testing.T, files map[string]string) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	configPath := filepath.Join(root, config.DefaultFileName)
	if err := os.WriteFile(configPath, []byte(relConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	ws, err := workspace.Open(cfg)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	svc := NewService(Options{
		Workspace:   ws,
		Documents:   documents.New(ws),
		WikiLinks:   cfg.Documents.WikiLinks,
		Fields:      cfg.Relationships.FrontMatter.Fields,
		SidecarPath: filepath.Join(root, ".athenaeum", "shared", "relationships.json"),
	})
	return svc, root
}

// sourcesTo returns the set of sources by which `from` links to `to`.
func sourcesTo(refs []Ref, to string) map[string]string {
	out := map[string]string{}
	for _, r := range refs {
		if r.DocumentID == to {
			out[r.Source] = r.Kind
		}
	}
	return out
}

func TestOutgoingFromAllFourSources(t *testing.T) {
	svc, root := build(t, map[string]string{
		"a.md": "---\nrelated:\n  - d.md\nimplements: e.md\n---\n" +
			"# A\n\nSee [the bee](b.md) and [[C]].\n",
		"b.md": "# B\n",
		"c.md": "# C\n",
		"d.md": "# D\n",
		"e.md": "# E\n",
		"f.md": "# F\n",
	})
	// A sidecar relationship from a.md to f.md.
	writeSidecar(t, root, `{"schema_version":1,"revision":1,"relationships":[
		{"id":"01X","from":"a.md","to":"f.md","kind":"supersedes","label":"A supersedes F"}]}`)

	res, err := svc.Get("a.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !_has(sourcesTo(res.Outgoing, "b.md"), SourceMarkdown) {
		t.Errorf("b.md not linked by markdown")
	}
	if !_has(sourcesTo(res.Outgoing, "c.md"), SourceWiki) {
		t.Errorf("c.md not linked by wiki")
	}
	if k := sourcesTo(res.Outgoing, "d.md"); k[SourceFrontMatter] != "related" {
		t.Errorf("d.md not linked by front_matter/related: %v", k)
	}
	if k := sourcesTo(res.Outgoing, "e.md"); k[SourceFrontMatter] != "implements" {
		t.Errorf("e.md not linked by front_matter/implements: %v", k)
	}
	if k := sourcesTo(res.Outgoing, "f.md"); k[SourceSidecar] != "supersedes" {
		t.Errorf("f.md not linked by sidecar/supersedes: %v", k)
	}
}

func TestBacklinksCarrySource(t *testing.T) {
	svc, _ := build(t, map[string]string{
		"hub.md":   "# Hub\n",
		"one.md":   "# One\n\nlinks to [hub](hub.md).\n",
		"two.md":   "# Two\n\nalso [[Hub]].\n",
		"three.md": "# Three\n",
	})
	res, err := svc.Get("hub.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !_has(sourcesTo(res.Backlinks, "one.md"), SourceMarkdown) {
		t.Errorf("missing markdown backlink from one.md: %+v", res.Backlinks)
	}
	if !_has(sourcesTo(res.Backlinks, "two.md"), SourceWiki) {
		t.Errorf("missing wiki backlink from two.md: %+v", res.Backlinks)
	}
	if len(res.Backlinks) != 2 {
		t.Errorf("backlink count = %d, want 2", len(res.Backlinks))
	}
}

// TestNoInferenceForSimilarButUnlinked is acceptance H2.
func TestNoInferenceForSimilarButUnlinked(t *testing.T) {
	svc, _ := build(t, map[string]string{
		"alpha.md": "# Databases\n\nSharding and replication and consistency.\n",
		"beta.md":  "# Databases\n\nSharding and replication and consistency.\n",
	})
	res, err := svc.Get("alpha.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(res.Outgoing) != 0 || len(res.Backlinks) != 0 {
		t.Fatalf("similar-but-unlinked docs produced relationships: %+v", res)
	}
}

func TestExternalAndAnchorLinksIgnored(t *testing.T) {
	svc, _ := build(t, map[string]string{
		"x.md": "# X\n\n[ext](https://example.com) and [frag](#section) and [y](y.md).\n",
		"y.md": "# Y\n",
	})
	res, _ := svc.Get("x.md")
	if len(res.Outgoing) != 1 || res.Outgoing[0].DocumentID != "y.md" {
		t.Fatalf("outgoing = %+v, want only y.md", res.Outgoing)
	}
}

func TestInvalidateRebuilds(t *testing.T) {
	svc, root := build(t, map[string]string{
		"a.md": "# A\n",
		"b.md": "# B\n",
	})
	if res, _ := svc.Get("b.md"); len(res.Backlinks) != 0 {
		t.Fatal("unexpected backlink before edit")
	}
	// Add a link and invalidate; the next query must see it.
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n\n[b](b.md)\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	svc.Invalidate()
	res, _ := svc.Get("b.md")
	if !_has(sourcesTo(res.Backlinks, "a.md"), SourceMarkdown) {
		t.Fatalf("backlink not seen after invalidate: %+v", res.Backlinks)
	}
}

// helpers

func writeSidecar(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".athenaeum", "shared")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sidecar: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "relationships.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

func _has(m map[string]string, key string) bool {
	_, ok := m[key]
	return ok
}
