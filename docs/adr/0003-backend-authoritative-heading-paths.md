# ADR-0003: The backend is authoritative for heading identity

- **Status:** Accepted — approved by the owner 2026-07-20
- **Date:** 2026-07-20
- **Affects:** R3, R7, R8, spec 02 sections 3.3/3.5/3.6, spec 03 section 3, acceptance G3
- **Raised by:** Phase 0 review

## Problem

Athenaeum parses Markdown in two places by design:

- the **Go backend** needs headings and front matter for the search projection
  (spec 02 section 3.5) and for the `heading_path` in annotation anchors
  (spec 03 section 3);
- the **frontend** renders the document (spec 02 section 7 lists Markdown
  rendering components as a frontend dependency class).

Nothing in the pack says which one is correct when they disagree. Two GFM
parsers in different languages will disagree at the edges: setext headings,
`#` inside fenced or indented code, headings inside HTML blocks, trailing
`#` runs, and duplicate heading text requiring slug disambiguation.

The consequence is not cosmetic. An annotation anchored to
`["System architecture", "Search"]` is resolved against backend heading paths
but *displayed* against frontend headings. If the two disagree, the annotation
silently attaches to the wrong passage — a data-integrity failure that presents
as a rendering quirk, and one that acceptance G3 would not catch because G3
tests repair after edits, not cross-parser agreement.

## Decision

The **backend is the single source of truth for heading identity.**

1. `internal/documents` produces an authoritative outline for every document:
   heading level, text, source line, stable slug, and full heading path.
2. The outline is served as document metadata.
3. The frontend renders Markdown to HTML as before, but **does not invent
   heading IDs**. It matches each rendered heading to the backend outline **by
   source line**, and adopts the backend's slug and heading path.
4. A rendered heading with no backend match, or a backend heading the renderer
   did not produce, is a **detectable inconsistency**. It is surfaced as a
   document warning rather than silently patched over.

Matching is by source line rather than by ordinal position, so a single
disagreement cannot shift every subsequent heading — it stays contained to the
heading that actually differs.

## Alternatives considered

**Frontend authoritative.** Rejected: the search index and annotation sidecars
are written by the backend, so heading identity would depend on a browser
having rendered the document first. Documents that were never opened would have
no heading identity at all.

**Render Markdown to HTML in Go and ship sanitised HTML.** This removes the
second parser entirely and is architecturally cleaner for exactly this problem.
It was not adopted because it is a wider deviation from spec 02 section 7 than
the problem requires, it moves syntax highlighting and sanitisation into the Go
build, and Mermaid and math must still render client-side regardless — so it
would not actually achieve a single rendering path. Revisit if frontend/backend
heading disagreement proves common in practice.

**Accept the divergence.** Rejected: it converts a silent wrong-anchor bug into
an accepted behaviour, which constitution C8 forbids.

## Consequences

- `internal/documents` gains a heading parser, and it is the only place heading
  slugs are computed.
- Document metadata carries the outline, so quick-open and the context panel
  outline both consume backend data rather than re-deriving it.
- The frontend renderer needs source position information from its Markdown
  parser. This constrains the renderer choice: it must expose source line
  numbers per node.
- A cross-parser agreement test belongs in the Phase 1 test set: render the
  fixture corpus and assert every rendered heading matches the backend outline.

## Note on scope

This ADR governs heading *identity*, not heading *appearance*. The frontend
remains responsible for how a heading is rendered and styled.
