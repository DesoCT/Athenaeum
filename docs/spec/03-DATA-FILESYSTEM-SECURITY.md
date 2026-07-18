# Athenaeum Data, Filesystem, and Security Rules

## 1. Data classes

| Class | Authority | Storage | Git expectation |
|---|---|---|---|
| Workspace documents | Authoritative | Workspace | User-controlled |
| Managed assets | Authoritative | Workspace `assets/` by default | User-controlled |
| Shared annotations | Authoritative sidecar | Workspace `.athenaeum/shared/` | Intended to be committable |
| Shared notes | Authoritative sidecar | Workspace `.athenaeum/shared/notes/` | Intended to be committable |
| Personal annotations | Authoritative sidecar | User data directory | Never placed in repository |
| Personal notes | Authoritative sidecar | User data directory | Never placed in repository |
| Search index | Disposable projection | User cache directory | Never committed |
| Session state | Disposable/recoverable | User state directory | Never committed |
| Recovery buffers | Temporary safety data | User state directory | Never committed |

## 2. Directory layout

### 2.1 Shared workspace data

```text
workspace/
├── athenaeum.toml
├── assets/
└── .athenaeum/
    └── shared/
        ├── annotations/
        │   └── docs/
        │       └── architecture.md.json
        ├── notes/
        │   └── design-review.md
        └── relationships.json
```

### 2.2 Personal data

Use operating-system conventions:

```text
<user-data>/athenaeum/workspaces/<workspace-key>/
├── annotations/
├── notes/
└── workspace.json
```

`workspace-key` is a stable hash of the canonical root path plus an optional configured workspace UUID. The raw root path MUST NOT be exposed in filenames.

### 2.3 Cache and state

```text
<user-cache>/athenaeum/<workspace-key>/search.sqlite
<user-state>/athenaeum/<workspace-key>/session.json
<user-state>/athenaeum/<workspace-key>/recovery/
```

## 3. Annotation format

One JSON document per annotated source document.

```json
{
  "schema_version": 1,
  "document_id": "docs/architecture.md",
  "revision": 7,
  "annotations": [
    {
      "id": "01J...",
      "kind": "comment",
      "visibility": "shared",
      "status": "open",
      "body": "Clarify whether the cache is authoritative.",
      "created_at": "2026-07-18T12:00:00Z",
      "updated_at": "2026-07-18T12:00:00Z",
      "anchor": {
        "type": "text_quote",
        "heading_path": ["System architecture", "Search"],
        "start_line": 92,
        "end_line": 92,
        "exact": "The index is disposable.",
        "prefix": "Search rule: ",
        "suffix": " It can be rebuilt.",
        "source_hash": "sha256:..."
      }
    }
  ]
}
```

Requirements:

- IDs MUST be globally unique and sortable.
- Timestamps MUST use UTC RFC 3339.
- Unknown fields MUST be preserved on read/write where practical.
- Schema migrations MUST be explicit and tested.
- Annotation body is plain Markdown with raw HTML disabled.

## 4. Notes format

Notes are Markdown files with front matter:

```markdown
---
id: 01J...
title: Design review
visibility: shared
created_at: 2026-07-18T12:00:00Z
updated_at: 2026-07-18T12:00:00Z
links:
  - document: docs/architecture.md
    heading: Search
---

Notes here.
```

## 5. Relationship format

Sidecar relationships use a single versioned JSON file:

```json
{
  "schema_version": 1,
  "revision": 3,
  "relationships": [
    {
      "id": "01J...",
      "from": "docs/design.md",
      "to": "docs/architecture.md",
      "kind": "implements",
      "label": "Design implements architecture constraints"
    }
  ]
}
```

Relationship kinds are free-form slugs in v0.1 but MUST be rendered as user-authored, not inferred.

## 6. Path security

Every file operation MUST:

1. canonicalise the workspace root;
2. clean and normalise the requested relative path;
3. reject absolute paths from API callers;
4. resolve symlinks before a write decision;
5. verify the resolved target is inside an approved write root;
6. reject traversal such as `..` escaping the root;
7. reject device files, sockets, and named pipes;
8. enforce request and file-size limits.

Symlinked files that resolve outside the workspace MAY be read only when explicitly included and `security.allow_external_reads=true`. They MUST remain read-only in v0.1.

## 7. Write boundary

Default writable targets:

- included Markdown documents;
- configured managed-asset directory;
- shared `.athenaeum/shared/` directory;
- personal user-data directory.

Writing another file beneath the root requires the user to expand the permission in configuration. UI prompts alone do not grant durable authority.

## 8. Atomic document writes

Required algorithm:

1. verify expected source version;
2. create a same-directory temporary file;
3. copy relevant mode bits;
4. write complete content;
5. flush file;
6. optionally fsync parent directory where supported;
7. rename atomically over target;
8. verify new fingerprint;
9. remove recovery copy only after success.

If atomic replace is unavailable, the operation MUST fail rather than degrade silently.

## 9. Content security

- Rendered Markdown MUST be sanitised.
- Raw HTML is off by default.
- Script tags, inline event handlers, JavaScript URLs, and unsafe SVG content MUST be blocked.
- Mermaid input MUST execute in a restricted rendering mode.
- Remote assets MUST not receive local authentication headers.
- Local files MUST be served through identifier-based API routes, not unrestricted `file://` URLs.
- Content Security Policy SHOULD prohibit arbitrary script execution and restrict connections to self by default.

## 10. Local authentication

The server MUST create a high-entropy session secret at startup. The browser receives it through the one-time launch URL or an equivalent bootstrap mechanism, then uses secure same-site session state.

API requests that mutate state MUST require both authenticated session state and same-origin/CSRF protection.

## 11. Remote mode

Remote mode is opt-in and MUST require:

- explicit bind address;
- configured token sourced from environment or protected file;
- origin allow-list;
- no automatic browser bootstrap containing the token;
- no token in logs;
- secure-cookie mode when TLS is terminated locally;
- a visible persistent remote-mode warning.

Athenaeum does not provide TLS termination in v0.1. Documentation may show operation behind Tailscale or a trusted reverse proxy, but remote mode remains an advanced feature.

## 12. Logging

Default logs may include:

- operation type;
- duration;
- document ID/path relative to workspace;
- stable error code;
- byte counts;
- index progress.

Default logs MUST NOT include:

- document body;
- annotation body;
- note body;
- authentication tokens;
- full absolute paths unless debug path logging is explicitly enabled.
