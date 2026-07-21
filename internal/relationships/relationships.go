// Package relationships computes the explicit links between documents and the
// backlinks to each — never an inferred or similarity relationship (R10,
// acceptance H1 and H2, spec 02 section 3.8).
//
// Four sources are recognised, each carried through with a label so the UI can
// say where a relationship came from (H1):
//
//   - markdown     — a relative Markdown link to another document;
//   - wiki         — a [[wiki link]] when the workspace enables them;
//   - front_matter — a configured front-matter field (relationships.front_matter);
//   - sidecar      — a user-authored entry in .athenaeum/shared/relationships.json.
//
// Backlinks are a projection: for document X they are every edge whose target
// is X, assembled from the same forward scan. Nothing here reads document text
// for similarity; H2 holds by construction.
package relationships

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"

	"athenaeum/internal/documents"
	"athenaeum/internal/watcher"
	"athenaeum/internal/workspace"
)

// Relationship sources (H1 labels).
const (
	SourceMarkdown    = "markdown"
	SourceWiki        = "wiki"
	SourceFrontMatter = "front_matter"
	SourceSidecar     = "sidecar"
)

// Ref is one end of a relationship as seen from a document, with the source and
// label needed to render it (H1).
type Ref struct {
	DocumentID string `json:"document_id"`
	Title      string `json:"title"`
	Source     string `json:"source"`
	Kind       string `json:"kind,omitempty"`
	Label      string `json:"label,omitempty"`
}

// Result is a document's outgoing links and its backlinks.
type Result struct {
	DocumentID string `json:"document_id"`
	Outgoing   []Ref  `json:"outgoing"`
	Backlinks  []Ref  `json:"backlinks"`
}

// edge is one directed relationship in the corpus.
type edge struct {
	from, to, kind, label, source string
}

// Service builds and answers relationship queries for one workspace.
type Service struct {
	ws     *workspace.Workspace
	docs   *documents.Service
	wiki   bool
	fields []string
	// sidecarPath is the shared relationships file, hand-authored and read only.
	sidecarPath string

	mu     sync.Mutex
	edges  []edge
	titles map[string]string
	ids    map[string]struct{}
	built  bool
	dirty  bool
}

// Options configures a Service.
type Options struct {
	Workspace   *workspace.Workspace
	Documents   *documents.Service
	WikiLinks   bool
	Fields      []string
	SidecarPath string
}

// NewService binds a Service to a workspace.
func NewService(opts Options) *Service {
	return &Service{
		ws:          opts.Workspace,
		docs:        opts.Documents,
		wiki:        opts.WikiLinks,
		fields:      opts.Fields,
		sidecarPath: opts.SidecarPath,
	}
}

// Invalidate marks the projection stale so the next query rebuilds it. The
// index is a projection, so a rebuild is always safe and never authoritative.
func (s *Service) Invalidate() {
	s.mu.Lock()
	s.dirty = true
	s.mu.Unlock()
}

// Follow invalidates the projection on any workspace change, so backlinks stay
// current without a full rebuild per event (the rebuild is deferred to the next
// query). It returns when ctx is cancelled.
func (s *Service) Follow(ctx context.Context, w *watcher.Watcher) {
	if w == nil {
		return
	}
	changes, cancel := w.Subscribe()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-changes:
			if !ok {
				return
			}
			s.Invalidate()
		}
	}
}

// Get returns a document's outgoing links and backlinks, rebuilding the
// projection first if it is stale.
func (s *Service) Get(id string) (*Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.built || s.dirty {
		s.rebuild()
	}

	result := &Result{DocumentID: id, Outgoing: []Ref{}, Backlinks: []Ref{}}
	for _, e := range s.edges {
		switch {
		case e.from == id:
			result.Outgoing = append(result.Outgoing, Ref{
				DocumentID: e.to, Title: s.titleOf(e.to), Source: e.source, Kind: e.kind, Label: e.label,
			})
		case e.to == id:
			result.Backlinks = append(result.Backlinks, Ref{
				DocumentID: e.from, Title: s.titleOf(e.from), Source: e.source, Kind: e.kind, Label: e.label,
			})
		}
	}
	return result, nil
}

func (s *Service) titleOf(id string) string {
	if t, ok := s.titles[id]; ok && t != "" {
		return t
	}
	return id
}

// rawEdge is an extracted relationship before its target is resolved to a
// document id. Resolution is deferred to a second pass so a bare-name or title
// match can see every document's title, not just those scanned so far.
type rawEdge struct {
	from, target, kind, label, source string
}

// rebuild scans every document and the sidecar file into a fresh edge list. The
// caller holds s.mu.
//
// It runs in two passes over a single read per document: the first collects
// titles and unresolved edges, the second resolves each target now that every
// title is known.
func (s *Service) rebuild() {
	docs := s.ws.Documents()
	s.ids = make(map[string]struct{}, len(docs))
	s.titles = make(map[string]string, len(docs))
	for _, d := range docs {
		s.ids[d.ID] = struct{}{}
	}

	var raw []rawEdge
	for _, d := range docs {
		doc, err := s.docs.Read(d.ID)
		if err != nil {
			continue // an unreadable document contributes no edges, never an error
		}
		s.titles[d.ID] = doc.Title
		raw = append(raw, s.rawEdgesFrom(d.ID, doc)...)
	}

	edges := make([]edge, 0, len(raw))
	for _, r := range raw {
		if to := s.resolve(r.target, r.from); to != "" && to != r.from {
			edges = append(edges, edge{from: r.from, to: to, kind: r.kind, label: r.label, source: r.source})
		}
	}
	edges = append(edges, s.sidecarEdges()...)

	s.edges = edges
	s.built = true
	s.dirty = false
}

// rawEdgesFrom extracts every explicit outgoing edge from one document, leaving
// targets unresolved.
func (s *Service) rawEdgesFrom(from string, doc *documents.Document) []rawEdge {
	body := stripFrontMatter(doc.Content, doc.FrontMatterFormat)
	var raw []rawEdge

	for _, link := range markdownLinks(body) {
		raw = append(raw, rawEdge{from: from, target: link.target, source: SourceMarkdown, label: link.text})
	}
	if s.wiki {
		for _, target := range wikiTargets(body) {
			raw = append(raw, rawEdge{from: from, target: target, source: SourceWiki, label: target})
		}
	}
	for _, field := range s.fields {
		for _, target := range frontMatterTargets(doc.FrontMatter[field]) {
			raw = append(raw, rawEdge{from: from, target: target, source: SourceFrontMatter, kind: field, label: target})
		}
	}
	return raw
}

// sidecarEdges reads the hand-authored relationships file, if present. It is
// read only in v0.1 (spec 03 section 5); a malformed file yields no edges rather
// than an error, so one bad entry cannot hide every real relationship.
func (s *Service) sidecarEdges() []edge {
	if s.sidecarPath == "" {
		return nil
	}
	data, err := os.ReadFile(s.sidecarPath)
	if err != nil {
		return nil
	}
	var file struct {
		Relationships []struct {
			From  string `json:"from"`
			To    string `json:"to"`
			Kind  string `json:"kind"`
			Label string `json:"label"`
		} `json:"relationships"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil
	}
	var edges []edge
	for _, r := range file.Relationships {
		if r.From == "" || r.To == "" {
			continue
		}
		edges = append(edges, edge{from: r.From, to: r.To, kind: r.Kind, label: r.Label, source: SourceSidecar})
	}
	return edges
}

// resolve maps a link target to a workspace document id, or "" when it points
// outside the workspace. It mirrors the frontend resolution (renderer/links.ts):
// relative path first, then common extensions, then a bare file name or title.
func (s *Service) resolve(target, from string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	// Drop any heading fragment: a link may point at a heading in another doc.
	if i := strings.IndexByte(target, '#'); i >= 0 {
		target = target[:i]
	}
	if target == "" {
		return ""
	}

	baseDir := ""
	if i := strings.LastIndexByte(from, '/'); i >= 0 {
		baseDir = from[:i]
	}

	candidates := []string{}
	add := func(c string) {
		c = path.Clean(c)
		c = strings.TrimPrefix(c, "./")
		if c != "" && c != "." {
			candidates = append(candidates, c)
		}
	}
	if baseDir != "" {
		add(path.Join(baseDir, target))
	}
	add(target)
	for _, c := range append([]string{}, candidates...) {
		if !hasExtension(c) {
			candidates = append(candidates, c+".md", c+".markdown")
		}
	}
	for _, c := range candidates {
		if _, ok := s.ids[c]; ok {
			return c
		}
	}

	// Last resort: a wiki link or field value naming a document by bare file
	// name or title alone.
	bare := target
	if i := strings.LastIndexByte(bare, '/'); i >= 0 {
		bare = bare[i+1:]
	}
	lower := strings.ToLower(bare)
	for id := range s.ids {
		if id == bare+".md" || strings.HasSuffix(id, "/"+bare+".md") {
			return id
		}
		if strings.ToLower(s.titles[id]) == lower {
			return id
		}
	}
	return ""
}

func hasExtension(p string) bool {
	base := p
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	return strings.LastIndexByte(base, '.') > 0
}

// markdown is a parser used only for structure, matching outline extraction so
// a link inside a fenced code block is not mistaken for a real link.
var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

type mdLink struct {
	target string
	text   string
}

// markdownLinks returns the destination and text of every real Markdown link in
// a body, skipping links with a URL scheme and bare fragments.
func markdownLinks(body string) []mdLink {
	src := []byte(body)
	reader := text.NewReader(src)
	doc := markdown.Parser().Parse(reader)

	var links []mdLink
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		link, ok := node.(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}
		dest := string(link.Destination)
		if dest == "" || strings.HasPrefix(dest, "#") || hasScheme(dest) {
			return ast.WalkContinue, nil
		}
		links = append(links, mdLink{target: dest, text: nodeText(link, src)})
		return ast.WalkContinue, nil
	})
	return links
}

func hasScheme(dest string) bool {
	for i, r := range dest {
		if r == ':' {
			return i > 0
		}
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '+' || r == '.' || r == '-') {
			return false
		}
	}
	return false
}

// nodeText concatenates the text under an inline node, used for a link's label.
func nodeText(n ast.Node, source []byte) string {
	var buf bytes.Buffer
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			buf.Write(t.Segment.Value(source))
		}
	}
	return strings.TrimSpace(buf.String())
}

// wikiPattern matches [[Target]] and [[Target|alias]], capturing the target.
var wikiPattern = regexp.MustCompile(`\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]`)

func wikiTargets(body string) []string {
	matches := wikiPattern.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if t := strings.TrimSpace(m[1]); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// frontMatterTargets normalises a front-matter value into a list of targets. A
// relationship field may hold a single string or a list of strings.
func frontMatterTargets(value any) []string {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	default:
		return nil
	}
}

// stripFrontMatter removes a leading YAML or TOML front-matter block so link
// parsing sees only the body, matching the renderer (Preview.svelte).
func stripFrontMatter(content, format string) string {
	fence := ""
	switch format {
	case documents.FrontMatterYAML:
		fence = "---"
	case documents.FrontMatterTOML:
		fence = "+++"
	default:
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != fence {
		return content
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == fence {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return content
}
