# Production dependencies

Spec 07 section 7 requires each production dependency to record its reason,
alternatives, licence, and runtime impact. No dependency here introduces
telemetry, network activity, or CGO.

## Go

### github.com/BurntSushi/toml — MIT

- **Reason:** the workspace configuration format is TOML (constitution section 3).
- **Alternatives:** `pelletier/go-toml/v2` is comparable and slightly faster.
  BurntSushi exposes `MetaData.IsDefined` and `Undecoded`, which the validator
  needs to reject unknown fields and to distinguish "absent" from "zero"
  (spec 05 section 6).
- **Impact:** pure Go, no subprocess, no network.

### github.com/bmatcuk/doublestar/v4 — MIT

- **Reason:** include and exclude patterns use `**` (spec 05 section 2), which
  the standard library's `path/filepath.Match` does not support.
- **Alternatives:** `gobwas/glob` compiles faster but has no filesystem-aware
  walking; hand-rolling `**` semantics is a correctness trap that would need the
  same test surface as the library.
- **Impact:** pure Go, no subprocess, no network.

### github.com/fsnotify/fsnotify — BSD-3-Clause

- **Reason:** the watcher subscribes to filesystem changes (spec 02 section 3.4).
- **Alternatives:** polling is portable but cannot meet the two-second
  searchability target (R7) at the 5,000-file scale (N3) without wasteful
  scanning.
- **Impact:** pure Go, uses platform syscalls (inotify, kqueue). The watcher is
  advisory by design — correctness also uses metadata and content hashes — so a
  watcher failure degrades latency, not correctness.

### github.com/yuin/goldmark — MIT

- **Reason:** the backend is authoritative for heading identity (ADR-0003), and
  that requires a real GFM parser. Regex heading extraction misidentifies `#`
  inside fenced and indented code blocks, which is precisely the edge case
  ADR-0003 exists to prevent.
- **Alternatives:** `gomarkdown/markdown` is less actively maintained;
  `russross/blackfriday` is in maintenance mode and is not GFM-accurate.
  Goldmark is the parser behind Hugo and is CommonMark-compliant with GFM
  extensions.
- **Impact:** pure Go, no subprocess, no network. Used for structure extraction
  only in v0.1 — rendering to HTML remains a frontend concern (spec 02 section 7).

### gopkg.in/yaml.v3 — MIT and Apache-2.0

- **Reason:** YAML front matter (R3, spec 03 section 4) and the note file format,
  which is Markdown with a YAML front-matter block (R9, spec 03 section 4).
- **Alternatives:** `goccy/go-yaml` is faster and has better error messages but
  a larger surface; yaml.v3 is the de facto standard and adequate for
  front-matter-sized documents.
- **Impact:** pure Go, no subprocess, no network.

### modernc.org/sqlite — BSD-3-Clause

- **Reason:** the disposable search projection needs embedded SQLite with FTS5
  (R7, D-014, spec 02 section 3.5).
- **Alternatives:** `mattn/go-sqlite3` is the usual choice and is faster, but it
  requires CGO. That would end single-command cross-compilation and make the
  macOS and Linux release artifacts depend on a C toolchain per target —
  directly against constitution C6 and requirement N4.
- **Verified before adoption**, because the whole phase rests on it: under
  `CGO_ENABLED=0` this driver provides FTS5 virtual tables, `MATCH` queries,
  `snippet()` with highlight delimiters, and porter stemming. The release
  binary remains statically linked with no dynamic dependencies, and the size
  cost is under 1 MB because the frontend bundle already dominates.
- **Impact:** pure Go, no subprocess, no network. Slower than the CGO driver
  under heavy write load, which is acceptable: the index is a cache rebuilt in
  the background, and correctness never depends on it.

### golang.org/x/sys — BSD-3-Clause

- **Reason:** the path-security check must reject device files, sockets, and
  named pipes before a write (spec 03 section 6), which needs `Stat` mode bits
  the standard library does not expose portably; `internal/security` imports
  `x/sys/unix` for that. It is also a transitive dependency of fsnotify.
- **Impact:** pure Go, platform syscalls only, no subprocess or network.

## Frontend

Frontend dependencies are build-time only; the release binary embeds compiled
output and requires no Node.js runtime (constitution C6, requirement N4).

### svelte, typescript, vite, @sveltejs/vite-plugin-svelte — MIT

- **Reason:** the UI stack is fixed by constitution section 3 and D-004.
- **Impact:** build time only.

### svelte-check, vitest — MIT

- **Reason:** type checking and unit tests.
- **Impact:** build and test time only.
