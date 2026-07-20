// Package search maintains the disposable SQLite FTS projection (R7, D-014).
//
// The index is a cache, never a source of truth: it is rebuilt from the
// authoritative files whenever it is missing, stale, or of an unknown schema
// version (constitution C2).
package search

// The driver is registered as "sqlite" by this blank import. modernc.org/sqlite
// is a pure-Go translation of SQLite, so the release keeps CGO_ENABLED=0 and
// stays a single cross-compilable static binary (constitution C6, requirement
// N4). FTS5, snippet(), and porter stemming are all verified present under
// that build.
import _ "modernc.org/sqlite"
