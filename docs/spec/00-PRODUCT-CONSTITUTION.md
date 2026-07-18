# Athenaeum Product Constitution

## 1. Product identity

Athenaeum is a lightweight, local-first developer command centre for reading, editing, annotating, navigating, and understanding configured Markdown workspaces.

The primary workspace experience is called the **Map Room**.

Athenaeum is not defined by chat, AI, or any external memory service. Those capabilities may exist later as removable integrations, but they may not become prerequisites for the core product.

## 2. Non-negotiable principles

### C1 — Core completeness

The default Athenaeum build MUST be fully useful with:

- no language model;
- no API key;
- no internet connection;
- no MCP or external knowledge service;
- no server-side database;
- no cloud account;
- no background daemon beyond the running Athenaeum process.

A feature-gated build must never be required to obtain the intended command-centre experience.

### C2 — Files remain authoritative

Markdown documents and their referenced assets remain the source of truth.

Athenaeum MUST NOT silently replace the workspace with a proprietary internal representation. Any index or cache MUST be disposable and reconstructable from authoritative files and explicit sidecars.

### C3 — Explicit authority

Every operation belongs to one authority class:

1. read-only;
2. ephemeral UI state;
3. personal sidecar state;
4. shared sidecar state;
5. direct document write;
6. read-only Git operation;
7. external integration call.

The UI and backend MUST preserve these boundaries. Direct document writes MUST be intentional, atomic, and recoverable.

### C4 — Removable integrations

Optional integrations MUST sit behind narrow interfaces and isolated packages. Removing an integration MUST NOT require changes to workspace loading, document rendering, editing, annotations, notes, search, Git context, or the Map Room.

### C5 — Local-first and offline-first

Local files MUST open, render, edit, search, and annotate while offline. Remote assets may fail gracefully but MUST NOT block local use.

### C6 — Lightweight distribution

The standard release SHOULD be a single Go executable containing the compiled web frontend. It MAY create local cache and configuration directories. It MUST NOT require Node.js, a separate database process, or a browser extension at runtime.

### C7 — Safe defaults

Athenaeum MUST bind to loopback by default. Remote access MUST require an explicit launch option and authentication.

The workspace write boundary MUST default to configured included files. Expansion to additional files beneath the root must be explicit.

### C8 — Reviewable behaviour

The application MUST favour visible state and inspectable files over opaque automation. Hidden mutation, silent conflict resolution, implicit network activity, and unreviewed agent edits are prohibited.

### C9 — Boring core, expressive surface

The backend SHOULD use straightforward, testable components. The UI may be distinctive, but visual novelty must not obscure file identity, save state, provenance, or user authority.

### C10 — No speculative platform

Version 0.1 MUST solve the command-centre workflow. Agents MUST NOT introduce plugin platforms, collaboration systems, semantic embeddings, agent orchestration, or generic extensibility frameworks without an approved amendment.

## 3. Technical constitution

- Core implementation language: **Go**.
- UI: **Svelte + TypeScript**, compiled and embedded in the Go executable.
- Runtime mode: local web application opened in the user's browser.
- First-class release platforms: macOS and Linux.
- Windows: code portability target, not a v0.1 release gate.
- Workspace config: TOML.
- Search index: embedded SQLite FTS, disposable cache only.
- Document renderer: GFM baseline with approved extensions.
- Git: system `git` executable, read-only operations only in v0.1.
- Licence: Apache-2.0.

## 4. Amendment process

A locked decision may change only through an Architecture Decision Record containing:

1. the current rule;
2. the proposed replacement;
3. the reason the current rule is insufficient;
4. affected requirements and tests;
5. migration or compatibility impact;
6. owner approval.

An implementation agent may propose an ADR but MUST NOT silently implement it.

## 5. Definition of constitutional violation

A change violates the constitution when it:

- makes AI or an external service necessary for core use;
- stores authoritative document content only in an internal database;
- writes outside the approved workspace boundary;
- performs hidden or irreversible mutation;
- turns the product into a chat-first interface;
- adds a major subsystem outside the approved v0.1 scope;
- weakens a MUST-level acceptance criterion without an amendment.
