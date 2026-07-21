# ADR-0007: Relationships as a disposable projection

- **Status:** Accepted — approved by the owner 2026-07-21
- **Date:** 2026-07-21
- **Affects:** R10, spec 02 section 3.8, spec 03 section 5, acceptance H1 and H2
- **Raised by:** Phase 4 implementation (relationships/backlinks vertical slice)

## Context

R10 requires Athenaeum to recognise four kinds of explicit relationship —
Markdown links, wiki links, configured front-matter fields, and user-authored
sidecar entries — and to show outgoing links and backlinks, without inferring
any relationship (H2). Spec 02 section 3.8 calls backlinks "a projection". The
spec leaves open how that projection is built and maintained, and how much of
the sidecar-relationship lifecycle v0.1 implements.

## Decisions

### 1. Backlinks are a rebuilt projection, never authoritative state

The relationship service scans the corpus into a forward edge list and answers
both outgoing links (edges from a document) and backlinks (edges to it) from it.
The projection is disposable, exactly like the search index (D-014): it is
rebuilt lazily on the first query after a change, and the watcher only marks it
stale rather than forcing an immediate rebuild. Losing it costs nothing; nothing
downstream treats it as a source of truth.

Why lazy rather than incremental: a correct full rebuild is simple and, like the
search build, fast enough at the v0.1 scale ceiling. Incremental maintenance is
an optimisation that can come later without changing the interface. The one cost
recorded honestly: a rebuild reads every document, so the first `GET
/relationships/{id}` after an edit to a large corpus pays a scan.

### 2. Sidecar relationships are read-only in v0.1

`.athenaeum/shared/relationships.json` is read and merged into the projection
with the `sidecar` source label, but there is no API to create or edit it — it
is hand-authored, the same posture as the workspace registry (ADR-0004). R10
requires Athenaeum to *recognise* user-created sidecar relationships, which
reading satisfies; an editing UI is a later slice. A malformed file yields no
sidecar edges rather than an error, so one bad entry cannot hide the real ones.

### 3. Resolution mirrors the frontend, and only explicit links count

Link targets resolve to document ids by the same rules the renderer uses
(`web/src/renderer/links.ts`): relative path, then common extensions, then a
bare file name or title. Only links that resolve to a document in the workspace
become edges; external URLs and bare fragments are dropped. No text-similarity
or co-occurrence signal is ever computed, so H2 ("no inference") holds by
construction rather than by a filter that could regress.

### 4. Relationship front-matter fields are configured, and the field is the kind

`[relationships.front_matter] fields = [...]` (spec 05 section 2) names which
front-matter keys are relationships; the key itself is the relationship kind
(e.g. `implements`). This adds a `Relationships` config struct and removes the
temporary accept-list that previously tolerated the key without a field.

## Consequences

- The engine lives in `internal/relationships`, reusing `documents` for reading
  and `workspace` for enumeration, and follows the watcher like `search`.
- The frontend surfaces it as a **Links** tab in the right context panel, beside
  Outline and Notes, each entry carrying its source label (H1).
