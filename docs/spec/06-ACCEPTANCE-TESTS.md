# Athenaeum v0.1 Acceptance Tests

A release candidate is acceptable only when all MUST-level scenarios pass on macOS and Linux.

## A. Build and launch

### A1 — Single-runtime artifact

**Given** a release package  
**When** a user runs `athenaeum open examples/athenaeum.toml`  
**Then** Athenaeum starts without Node.js, npm, SQLite CLI, or another server process.

### A2 — Offline launch

**Given** no internet connection  
**When** Athenaeum opens the fixture workspace  
**Then** local documents, search, notes, annotations, and editing work.

### A3 — Loopback default

**When** Athenaeum starts without remote flags  
**Then** it binds only to loopback and rejects requests without a valid session.

## B. Workspace and security

### B1 — Include/exclude

Fixture files matching includes appear; excluded files do not appear and cannot be opened through crafted API paths.

### B2 — Traversal rejection

Requests containing absolute paths, encoded traversal, symlink escape, or `..` escape return a stable path-security error.

### B3 — Write boundary

An included writable Markdown file saves successfully. A readable but non-writable file opens read-only. A write outside configured authority is rejected.

### B4 — Config diagnostics

Malformed TOML, unknown fields, invalid globs, and escaping writable paths produce actionable validation output and non-zero exit from `athenaeum validate`.

## C. Rendering

### C1 — Dialect fixture

A fixture document containing GFM table, task list, footnote, code block, local image, wiki link, callout, Mermaid, and math renders correctly when enabled.

### C2 — Sanitisation

Script tags, inline handlers, JavaScript URLs, and unsafe SVG payloads do not execute.

### C3 — Raw HTML default

Raw HTML is visibly escaped or omitted when `raw_html=false`.

## D. Editing and saving

### D1 — Split editing

The user edits source, sees preview update, saves, reloads the page, and observes exact persisted content.

### D2 — Atomic failure

A simulated write failure leaves the original file intact and preserves the unsaved buffer plus recovery data.

### D3 — Line endings

Opening and saving a CRLF document preserves CRLF unless the user explicitly changes the format.

### D4 — Read-only encoding

A non-UTF-8 fixture opens read-only with a clear explanation and cannot be overwritten.

## E. External changes and recovery

### E1 — Clean external update

An externally changed clean document reloads automatically and shows a notice.

### E2 — Dirty external update

An externally changed dirty document enters conflict state and preserves local and disk versions.

### E3 — Crash recovery

After terminating the process with an unsaved buffer, restart offers recovery and does not silently apply or discard it.

## F. Search

### F1 — Initial index

The fixture corpus indexes path, title, heading, and body content with correct result locations.

### F2 — Incremental update

After editing or externally changing a document, new text becomes searchable within two seconds under normal load.

### F3 — Rebuild

Deleting the cache and restarting rebuilds the index with no loss of authoritative data.

### F4 — Scale

A generated corpus of 5,000 documents remains navigable and searchable without blocking primary UI interaction.

## G. Annotations and notes

### G1 — Personal annotation isolation

A personal annotation is stored outside the workspace and does not create repository files.

### G2 — Shared annotation portability

A shared annotation is stored under `.athenaeum/shared/`, survives restart, and is readable after copying the workspace to another machine.

### G3 — Anchor repair

After nearby source edits, a quote-anchored annotation repairs to the intended passage. If repair is ambiguous, it becomes detached rather than moving silently.

### G4 — Note links

A note links to a document heading and navigation opens the correct target.

## H. Relationships

### H1 — Backlinks

Markdown, wiki-link, front-matter, and sidecar relationships all appear with correct source labels.

### H2 — No inference

No semantic or similarity relationship is shown for unlinked but textually similar documents.

## I. Assets

### I1 — Paste image

Pasting an image writes under the configured asset directory and inserts a relative Markdown link.

### I2 — Collision

A filename collision prompts for a new name or explicit overwrite; silent overwrite is prohibited.

## J. Git

### J1 — Status and diff

Modified, untracked, and clean states are displayed correctly. Working-tree diff matches `git diff -- <file>`.

### J2 — History and blame

File history and blame are available without changing repository state.

### J3 — No mutation

Automated tests inspect command invocation and prove no Git write command can be called through the adapter.

### J4 — Git unavailable

Without `git` installed, core functionality remains available and the Git panel explains the limitation.

## K. Remote mode

### K1 — Auth required

Remote mode without a token fails startup.

### K2 — Origin and token protection

Unauthenticated and disallowed-origin requests are rejected. Tokens never appear in logs or URLs.

## L. Exclusion enforcement

The release contains no chat UI, no AI calls, no MCP dependency, no embedding model, no Git mutation endpoint, and no collaboration/account subsystem.
