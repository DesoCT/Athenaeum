# ADR-0005: Annotation anchoring and repair

- **Status:** Accepted — approved by the owner 2026-07-21
- **Date:** 2026-07-21
- **Affects:** R8, spec 02 sections 3.6, spec 03 section 3, acceptance G1–G3
- **Raised by:** Phase 4 implementation (annotations vertical slice)

## Context

Spec 03 section 3 locks the annotation *format* — one JSON sidecar per document
per visibility, with an anchor carrying both a structural position and a quoted
context. It does not settle four implementation questions that the format leaves
open, and R8's "broken anchors MUST be shown as detached, never deleted"
constrains all of them. This ADR records the answers rather than deciding them
silently in code (spec 07 rules).

## Decisions

### 1. Repair is computed on read; the sidecar is never rewritten to chase edits

When a document changes under an anchor, the anchor's current line range is
recomputed on every read from the stored selector, and the result — `anchored`
or `detached`, plus the current lines — is returned alongside the annotation.
The stored selector (`exact`, `prefix`, `suffix`, `heading_path`,
`source_hash`) is the durable truth and is **not** rewritten just because a
document moved.

Why: writing on read introduces a race (two readers repairing the same file)
and a surprise (a read mutating committable data). Recomputing from the selector
is cheap — an unchanged document is caught by a hash comparison and skips the
search entirely — and idempotent, so there is nothing to persist. A repaired
position that the user wants to make permanent is made permanent the next time
they edit the annotation, not by a side effect of viewing it.

### 2. `text_quote` repair is a W3C TextQuoteSelector, and ambiguity detaches

On a changed document, the exact quote is searched for. A single occurrence, or
a single occurrence whose surrounding text matches the stored `prefix`/`suffix`,
repairs to that location. Zero matches, or a tie that the context cannot break,
becomes **detached** — never moved to a guess. This is the direct implementation
of R8's "if repair is ambiguous, it becomes detached rather than moving
silently" (acceptance G3).

A consequence worth stating: the quote is captured from the *rendered* selection
and searched against the *source*. For plain prose the two are identical and
repair is exact; for text carrying inline Markdown they can differ, in which case
an edited document detaches the anchor rather than mis-repairing it. The
unchanged-document fast path (decision 1) means this only ever affects an anchor
whose document was actually edited.

### 3. Personal and shared sidecars carry independent revisions

Each visibility is a separate file with its own `revision`, and every write
presents the revision it last saw (HTTP 409 on a mismatch, mirroring document
saves). Visibility is fixed at creation in this slice — moving an annotation
between personal and shared is a cross-file operation deferred to a later slice —
so the addressed file is always unambiguous.

### 4. IDs are ULID-shaped, generated without a dependency

Spec 03 section 3 requires globally unique, sortable ids. A ~40-line local
generator (48-bit millisecond timestamp + 80 bits of `crypto/rand`, Crockford
base32) satisfies both without adding a module, keeping to the dependency
discipline in `docs/dependencies.md`.

## Consequences

- Anchoring lives in `internal/annotations/repair.go`, kept free of the
  filesystem and the documents package so it is unit-tested directly.
- The frontend captures the selector in `web/src/annotations/anchor.ts` from the
  rendered article's `data-line` blocks (ADR-0003 makes those authoritative) and
  never reads the filesystem.
- The atomic-write policy is shared with documents via `internal/atomicfs`
  (spec 03 section 8), so sidecars are written with the same crash safety.
