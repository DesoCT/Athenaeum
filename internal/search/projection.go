package search

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"athenaeum/internal/config"
)

// ProjectionKey fingerprints everything that changes what the index means.
//
// When any of it changes, rows already in the projection can no longer be
// trusted: a narrowed include set leaves indexed documents that must not be
// searchable any more, and switching code-block indexing changes what "matched"
// means. The projection is discarded and rebuilt rather than migrated, which is
// always available to a cache and never to a source of truth (C2, D-014).
func ProjectionKey(cfg *config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "root=%s\n", cfg.AbsRoot)
	fmt.Fprintf(&b, "include=%s\n", strings.Join(cfg.Include, "\x00"))
	fmt.Fprintf(&b, "exclude=%s\n", strings.Join(cfg.Exclude, "\x00"))
	fmt.Fprintf(&b, "front_matter=%s\n", strings.Join(cfg.Documents.FrontMatter, "\x00"))
	fmt.Fprintf(&b, "code_blocks=%t\n", cfg.Search.IndexCodeBlocks)
	fmt.Fprintf(&b, "index_front_matter=%t\n", cfg.Search.IndexFrontMatter)

	// Group membership is a stored filter column, so a changed group definition
	// invalidates the rows just as an include change does.
	for _, group := range cfg.Groups {
		fmt.Fprintf(&b, "group=%s:%s\n", group.ID, strings.Join(group.Patterns, "\x00"))
	}

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16])
}
