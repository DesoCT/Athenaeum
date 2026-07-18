# Athenaeum v0.1 Delivery Plan

## Phase 0 — Repository foundation

Deliverables:

- Go module and command entrypoint;
- Svelte/TypeScript frontend;
- embedded release build;
- development workflow;
- lint, unit-test, integration-test, and browser-test commands;
- Apache-2.0 licence;
- fixture workspace;
- CI for macOS and Linux.

Exit gate: A1-A3 pass with a static embedded frontend.

## Phase 1 — Workspace and read-only Map Room

Deliverables:

- TOML loading and validation;
- include/exclude enumeration;
- safe document IDs;
- file tree, dashboard, quick open;
- document read API;
- GFM rendering and sanitisation;
- watcher notifications;
- safe-mode launch.

Exit gate: B1-B4 and C1-C3 pass; no editing yet.

## Phase 2 — Editing, saves, conflicts, recovery

Deliverables:

- source editor;
- split preview;
- explicit save;
- atomic write pipeline;
- stale-version protection;
- external-change conflict UI;
- crash-recovery buffers;
- asset paste/drop.

Exit gate: D1-D4, E1-E3, and I1-I2 pass.

## Phase 3 — Search and navigation depth

Deliverables:

- SQLite FTS projection;
- incremental indexing;
- search UI and filters;
- heading navigation;
- session restoration;
- scale fixture and performance measurements.

Exit gate: F1-F4 pass and N1-N3 have measured evidence.

## Phase 4 — Annotations, notes, relationships

Deliverables:

- personal/shared sidecars;
- quote/heading/document anchors;
- detached-anchor workflow;
- Markdown notes;
- backlinks and relation sources;
- pins and unresolved summaries on Map Room home.

Exit gate: G1-G4 and H1-H2 pass.

## Phase 5 — Read-only Git context

Deliverables:

- repository detection;
- status;
- per-file diff;
- history;
- blame;
- missing-Git fallback;
- allow-list command tests.

Exit gate: J1-J4 pass.

## Phase 6 — Remote mode and release hardening

Deliverables:

- explicit remote mode;
- token authentication;
- origin controls;
- security review;
- packaging for macOS and Linux;
- migration/version checks;
- end-to-end acceptance run;
- user documentation.

Exit gate: K1-K2 and all prior acceptance tests pass on both release platforms.

## Parallelisation guidance

Safe parallel tracks after Phase 1 contracts stabilise:

- editor/preview UI;
- atomic-write and recovery backend;
- search projection;
- annotation schema and anchor algorithms;
- Git adapter;
- visual system and accessibility.

Do not parallelise ownership of the same API schema or persistent data format without one designated integrator.

## Release blockers

- any data-loss scenario;
- path escape or unauthorised write;
- unsanitised executable content;
- hidden network request;
- index treated as authoritative;
- external change silently overwritten;
- remote mode without authentication;
- introduction of an excluded system;
- failing acceptance test on macOS or Linux.
