# Athenaeum v0.1 Product Requirements

## 1. Goal

Deliver a dependable local command centre in which a developer can open a configured Markdown workspace, navigate its documents, render them richly, edit them safely, search the corpus, attach personal or shared context, inspect explicit relationships, and understand Git state.

## 2. Primary user

A technically capable individual working with Markdown-heavy projects containing tens to several thousand documents. The user values portability, keyboard access, inspectability, and safe interaction with files.

## 3. Core jobs

1. Open a workspace from `athenaeum.toml`.
2. Find a document quickly by title, path, heading, or content.
3. Read rich Markdown without losing source-file context.
4. Edit Markdown with source and preview visible together.
5. See whether a file is saved, changed externally, or modified in Git.
6. Add comments, notes, pins, and explicit relationships without polluting document content.
7. Reopen the workspace with state and annotations intact.

## 4. Functional requirements

### R1 — Workspace loading

Athenaeum MUST load one primary workspace root with include and exclude globs.

The product model reserves future support for additional mounts, but v0.1 MUST NOT implement unrelated multi-root workspaces.

It MUST validate:

- the workspace root;
- include and exclude patterns;
- duplicate or overlapping entries;
- paths escaping the root;
- unreadable files;
- malformed configuration.

Configuration errors MUST identify the field and remediation.

### R2 — Workspace discovery

The Map Room MUST provide:

- hierarchical file tree;
- recently opened documents;
- pinned documents;
- configurable document groups;
- quick-open command;
- workspace-wide search;
- visible Git-state summaries.

A graph visualisation is not required in v0.1.

### R3 — Markdown rendering

The renderer MUST support:

- GitHub Flavoured Markdown;
- YAML and TOML front matter;
- tables;
- task lists;
- strikethrough;
- footnotes;
- fenced code blocks with syntax highlighting;
- local images;
- remote images with a visible remote indicator;
- relative document links;
- wiki links when enabled;
- callouts when enabled;
- Mermaid diagrams when enabled;
- inline and display mathematics when enabled.

Rendering MUST be sanitised. Embedded raw HTML is disabled by default and may be enabled only by explicit workspace configuration.

### R4 — Source editing

The editor MUST provide:

- plain-text Markdown editing;
- source and preview split view;
- source-only and preview-only modes;
- line numbers;
- find within document;
- undo and redo;
- keyboard save;
- visible dirty state;
- configurable line wrapping;
- navigation from rendered headings to source position.

WYSIWYG and block editing are explicitly excluded.

### R5 — Saving

Explicit save is the default.

Optional delayed autosave MAY be enabled per user. All writes MUST:

1. preserve the original file permissions where practical;
2. write to a temporary file in the same filesystem;
3. flush and atomically replace the target;
4. retain a recoverable pre-write copy until success is confirmed;
5. surface failure without discarding the editor buffer.

### R6 — External changes

If an open document changes externally:

- a clean editor MUST reload automatically and show a non-blocking notice;
- a dirty editor MUST enter conflict state;
- Athenaeum MUST preserve both the local buffer and current disk content;
- the user MUST be able to compare, keep local, accept disk, or save local as a new file.

Athenaeum MUST NOT silently overwrite either version.

### R7 — Search

Athenaeum MUST index configured Markdown files and expose:

- filename and path search;
- title and heading search;
- full-text search;
- result snippets;
- matched-term highlighting;
- filters for path, document group, and Git state.

The index MUST be disposable and reconstructable. Changes SHOULD become searchable within two seconds for ordinary documents.

### R8 — Annotations

Athenaeum MUST support:

- comments anchored to a text selection;
- comments anchored to a heading;
- document-level comments;
- bookmarks/pins;
- resolved/unresolved state;
- personal or shared visibility per annotation.

Anchors MUST store both structural position and quoted text context so they can be repaired after edits. Broken anchors MUST be shown as detached, never deleted silently.

### R9 — Notes

Athenaeum MUST support free-standing workspace notes in Markdown.

Notes may be personal or shared and MAY link to:

- documents;
- headings;
- annotations;
- other notes.

### R10 — Explicit relationships

Athenaeum MUST recognise:

- ordinary Markdown links;
- wiki links when enabled;
- configured front-matter relationships;
- user-created sidecar relationships.

It MUST show backlinks and outgoing links. It MUST NOT infer semantic relationships in v0.1.

### R11 — Assets

The user MUST be able to:

- render relative local assets;
- paste or drag an image into an editable document;
- choose or accept the configured asset destination;
- insert a relative Markdown link;
- detect a filename collision before writing.

The default managed-asset directory is `<workspace>/assets/`.

### R12 — Git context

When the workspace is inside a Git repository and `git.enabled=true`, Athenaeum MUST provide read-only:

- repository status;
- per-file state;
- working-tree diff for the open document;
- file history;
- line blame.

Athenaeum MUST NOT stage, commit, push, pull, reset, checkout, rebase, or mutate repository state in v0.1.

### R13 — Session restoration

Athenaeum SHOULD restore:

- open tabs;
- active document;
- pane layout;
- scroll positions;
- source/preview mode;
- recent documents;
- command history that contains no sensitive content.

Unsaved buffers MUST be recoverable after an abnormal exit.

### R14 — Local and remote runtime

Default mode:

- bind to `127.0.0.1` or `::1` only;
- generate a random local session secret;
- open the default browser unless `--no-open` is supplied.

Remote mode:

- requires `--remote`;
- requires an explicit bind address;
- requires token authentication;
- MUST reject startup without authentication configured;
- MUST disable arbitrary origin access.

### R15 — External knowledge boundary

Athenaeum v0.1 MUST contain no functional memory, chat, AI, MCP, or external knowledge integration.

The architecture MAY reserve an interface boundary, but it MUST NOT encode assumptions about a provider's tools, transport, data model, or permissions. Integration design begins only after owner-supplied documentation is approved.

## 5. Non-functional requirements

### N1 — Startup

For a warm cache and ordinary workspace, the local server SHOULD become ready within two seconds on a contemporary developer laptop.

### N2 — Responsiveness

UI interactions unrelated to indexing MUST remain responsive while indexing or Git commands run.

### N3 — Scale target

v0.1 MUST support at least:

- 5,000 Markdown files;
- 2 GB total included source content;
- individual files up to 10 MB, with a visible large-file warning above 2 MB.

### N4 — Portability

The release build MUST not require Node.js, npm, SQLite CLI, or a separately installed web server. Read-only Git features may require the system `git` executable.

### N5 — Accessibility

All primary actions MUST be keyboard-accessible. Focus MUST be visible. The UI SHOULD meet WCAG 2.2 AA contrast requirements.

### N6 — Observability

Errors MUST include a stable code, human-readable explanation, and relevant path or operation. Debug logs MUST avoid document content unless a user explicitly enables content logging.

### N7 — Privacy

Athenaeum MUST make no network request during normal local operation except to fetch user-requested remote assets. Remote assets MUST be clearly distinguishable from local assets.

## 6. Explicit exclusions

The following are not part of v0.1:

- chat or conversational UI;
- Flashbulb integration;
- MCP client or server;
- embeddings or semantic search;
- automatic summarisation;
- agent-authored edits;
- WYSIWYG editing;
- collaborative editing;
- accounts or cloud sync;
- mobile application;
- plugin marketplace;
- arbitrary executable extensions;
- Git write operations;
- document graph visualisation;
- PDF or office-document editing;
- multi-root workspaces.
