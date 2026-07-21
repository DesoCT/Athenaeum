# ADR-0009: Data-format versioning and the newer-version guard

- **Status:** Accepted — approved by the owner 2026-07-21
- **Date:** 2026-07-21
- **Affects:** spec 03 sections 3–5, spec 08 release blockers, acceptance I-series
- **Raised by:** Phase 6 hardening (migration and version checks)

## Context

Spec 03 requires schema migrations to be "explicit and tested", and spec 08
lists "any data-loss scenario" as a release blocker. Athenaeum writes several
persistent formats, and a build must never silently downgrade a file written by
a newer build — reading it as the current schema and writing it back would drop
whatever the newer version added. This ADR sets one policy and records how each
format follows it.

## Policy

For an **authoritative** format (one whose loss is real data loss), a file whose
`schema_version` is greater than this build understands is **refused, never
rewritten**: reads that feed a mutation fail, and the file is left byte-for-byte
untouched. A **disposable** format may instead discard an unrecognised file,
because rebuilding it costs nothing.

## How each format follows it

- **Annotation sidecars** (authoritative, spec 03 §3): carry `schema_version`.
  A newer version is refused on the mutation path (`SchemaError`), and the list
  view skips it rather than displaying a guess — so a newer sidecar is preserved
  intact even though this build cannot show it. This is the guard added in this
  phase, with a test that asserts the file is unchanged after a refused write.
- **Notes** (authoritative, spec 03 §4): Markdown with YAML front matter and no
  schema field, matching the spec example. A note that cannot be parsed is
  reported as corrupt and never overwritten blind; there is no version to
  downgrade. A future format change will add an explicit field then.
- **Relationship sidecar** (spec 03 §5): read-only and hand-authored (ADR-0007);
  Athenaeum never writes it, so there is nothing to downgrade.
- **Session state** (disposable, spec 03 §2.3): a mismatched `schema_version` is
  discarded — losing a layout is acceptable, and the alternative of migrating
  UI state is not worth the risk.
- **Search index** (disposable, D-014): keyed by a projection key; an
  incompatible index is rebuilt, never trusted.
- **Workspace config** (spec 05): an unsupported `schema_version` fails the load
  loudly rather than running against assumptions it does not meet.

## Consequences

- The only authoritative format Athenaeum both writes and versions —
  annotations — now cannot be downgraded, closing the one place this class of
  data loss could occur.
- New persistent formats must declare which side of the policy they are on when
  they are introduced.
