# ADR-0006: Note storage and identity

- **Status:** Accepted — approved by the owner 2026-07-21
- **Date:** 2026-07-21
- **Affects:** R9, spec 02 section 3.7, spec 03 section 4, acceptance G4
- **Raised by:** Phase 4 implementation (notes vertical slice)

## Context

Spec 03 section 4 gives the note *format* — a Markdown file with a small YAML
front matter (id, title, visibility, timestamps, links) — through an example,
`.athenaeum/shared/notes/design-review.md`. It leaves the filename scheme, the
front-matter dialect, and the concurrency model unstated. This ADR records those
choices rather than deciding them silently (spec 07 rules).

## Decisions

### 1. A note's filename is its id, not a title slug

The example filename `design-review.md` reads as a title slug, but notes are
stored as `<id>.md`, where the id is a ULID (shared with annotations via
`internal/ulid`). The title lives in the front matter and is what the UI shows.

Why: a title-slug filename churns on every rename, collides between two notes of
the same title, and turns "rename a note" into "move a file", which reads as a
delete-plus-create in Git history. An id filename is stable, collision-free, and
makes the title a first-class editable field rather than a filesystem artefact.
The ugliness of the filename is rarely seen, because notes are addressed by
title in the UI.

### 2. Note front matter is YAML only

Documents accept YAML or TOML front matter because they are user-authored files
Athenaeum only reads. A note is a file Athenaeum *writes*, so it emits one
dialect — YAML, matching the spec example — and a note without a terminated
front-matter block is treated as corruption to report, not body to render.

### 3. Concurrency is a content fingerprint, not a revision counter

Unlike an annotation sidecar (a single JSON file with a monotonic `revision`), a
note is an individual file. Its optimistic-concurrency token is a SHA-256
fingerprint of the file bytes — the same mechanism documents already use — which
a write must present and which a stale write fails on (HTTP 409). Visibility is
fixed at creation, as with annotations, so the addressed file is unambiguous.

### 4. Notes and documents share one tab model in the frontend

An open note is a tab in the same strip as documents, distinguished by a
`note:<visibility>:<id>` id prefix, so tab selection, closing, and the
single-active-surface guarantee serve both without a parallel system. Note tabs
are deliberately excluded from session restoration (R13 restores documents and
layout); a note reopens from the notes panel instead.

## Consequences

- Storage lives in `internal/notes`, reusing `internal/atomicfs` (spec 03
  section 8) and `internal/ulid`, mirroring the annotation slice.
- The editing surface reuses the document `Editor` and `Preview` components
  rather than a second editor, so a note renders exactly like the rest of the
  workspace. The data-loss-critical `DocumentView` is left untouched.
