package documents

import (
	"os"
	"sync"

	"athenaeum/internal/workspace"
)

// titlePrefixBytes bounds how much of a file is read to determine its title.
//
// Enumeration must stay fast at the 5,000-document scale target (N3), so the
// title is derived from a bounded prefix rather than the whole file. Front
// matter and the first heading are both near the top of any real document.
const titlePrefixBytes = 8192

// titleEntry caches a resolved title against the file identity it came from.
type titleEntry struct {
	modUnix int64
	size    int64
	title   string
}

// titleCache memoises resolved titles so repeated listings do not re-read the
// corpus. It is invalidated per file by modification time and size.
type titleCache struct {
	mu      sync.RWMutex
	entries map[string]titleEntry
}

func newTitleCache() *titleCache {
	return &titleCache{entries: make(map[string]titleEntry)}
}

func (c *titleCache) get(id string, modUnix, size int64) (string, bool) {
	c.mu.RLock()
	entry, ok := c.entries[id]
	c.mu.RUnlock()
	if !ok || entry.modUnix != modUnix || entry.size != size {
		return "", false
	}
	return entry.title, true
}

func (c *titleCache) put(id string, modUnix, size int64, title string) {
	c.mu.Lock()
	c.entries[id] = titleEntry{modUnix: modUnix, size: size, title: title}
	c.mu.Unlock()
}

// EnrichTitles replaces the file-name placeholder on each document with its
// real title: front matter `title`, else the first level-1 heading, else the
// first heading, else the file name.
//
// Requirement R2 and spec 04 section 4.2 both present titles rather than file
// names, and quick open searches titles, so the listing cannot use the file
// name as a stand-in.
//
// Resolution deliberately reuses the same front-matter parser and heading
// extractor as Read, so a document's title never depends on which endpoint
// asked for it.
func (s *Service) EnrichTitles(docs []*workspace.Document) {
	formats := s.ws.Config().Documents.FrontMatter

	for _, doc := range docs {
		absPath, err := s.ws.ResolveRead(doc.ID)
		if err != nil {
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}
		modUnix, size := info.ModTime().UnixNano(), info.Size()

		if cached, ok := s.titles.get(doc.ID, modUnix, size); ok {
			doc.Title = cached
			continue
		}

		title := resolveTitleFromPrefix(absPath, doc.ID, formats)
		s.titles.put(doc.ID, modUnix, size, title)
		doc.Title = title
	}
}

// resolveTitleFromPrefix reads a bounded prefix and derives the display title.
func resolveTitleFromPrefix(absPath, id string, formats []string) string {
	handle, err := os.Open(absPath)
	if err != nil {
		return defaultTitle(id)
	}
	defer handle.Close()

	buffer := make([]byte, titlePrefixBytes)
	n, err := handle.Read(buffer)
	if n == 0 || (err != nil && n == 0) {
		return defaultTitle(id)
	}
	prefix := buffer[:n]

	// A byte order mark would otherwise defeat front-matter fence detection.
	if len(prefix) >= 3 && prefix[0] == 0xEF && prefix[1] == 0xBB && prefix[2] == 0xBF {
		prefix = prefix[3:]
	}
	prefix = normaliseNewlines(prefix)

	fm := parseFrontMatter(prefix, formats)
	if title := fm.Title(); title != "" {
		return title
	}

	// The prefix may end mid-document, which is harmless: the outline builder
	// only needs the headings it can see, and the first one is what matters.
	outline := buildOutline(prefix[fm.BodyOffset:], fm.BodyLine)
	return resolveTitle(id, fm, outline)
}
