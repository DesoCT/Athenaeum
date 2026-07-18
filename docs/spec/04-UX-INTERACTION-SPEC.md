# Athenaeum v0.1 UX and Interaction Specification

## 1. Experience goal

Athenaeum should feel like a calm intelligence desk: a place where the workspace can be surveyed, inspected, and acted upon without becoming an IDE clone or a decorative fantasy interface.

Visual metaphor: dark plotting table, warm paper surfaces, restrained coordinate/grid cues, and precise status markings.

## 2. Information architecture

Primary regions:

```text
┌──────────────────────────────────────────────────────────┐
│ Workspace / command bar / status                         │
├──────────────┬──────────────────────────┬────────────────┤
│ Navigation   │ Main document surface    │ Context panel  │
│              │                          │                │
│ Map Room     │ Source / Preview / Split │ Outline        │
│ Tree         │                          │ Annotations    │
│ Search       │                          │ Links          │
│ Notes        │                          │ Git            │
├──────────────┴──────────────────────────┴────────────────┤
│ Save, path, line/column, Git state, index state          │
└──────────────────────────────────────────────────────────┘
```

The context panel MAY collapse. The document surface is always primary.

## 3. Map Room home

When no document is active, show:

- workspace name and root summary;
- pinned documents;
- recent documents;
- configured document groups;
- changed files;
- unresolved annotations;
- indexing or configuration warnings;
- quick-open input.

Do not show chat, prompts, agent suggestions, or generated summaries.

## 4. Navigation

### 4.1 File tree

- mirrors included workspace paths;
- hides excluded paths;
- shows Markdown documents and approved assets;
- indicates modified, untracked, conflicted, and ignored Git states where available;
- supports keyboard expansion and filtering;
- preserves expansion state per workspace.

### 4.2 Quick open

Default shortcut: `Cmd/Ctrl+P`.

Searches path, title, headings, pins, and recent documents. Results must be ranked deterministically and show why each result matched.

### 4.3 Command palette

Default shortcut: `Cmd/Ctrl+Shift+P`.

Commands include:

- open document;
- toggle source/preview/split;
- save;
- search workspace;
- add annotation;
- create note;
- pin/unpin;
- show Git diff;
- reveal backlinks;
- rebuild index;
- open settings diagnostics.

Destructive commands require confirmation and must not be the first ranked result for a vague query.

## 5. Document view

Header displays:

- document title;
- relative path;
- saved/dirty/conflict state;
- Git state;
- annotation count;
- external/remote asset warnings when relevant.

Tabs use full accessible labels and retain unsaved-state indicators.

## 6. Editor and preview

Modes:

- Split: source left, preview right; default.
- Source: full-width editor.
- Preview: full-width rendered document.

Expected interactions:

- clicking a preview heading moves source cursor to the heading;
- selecting source text enables "add comment";
- clicking an annotation scrolls to its anchor;
- preview scroll position and source cursor are preserved independently;
- optional synchronised scrolling may be enabled but is off by default if unreliable.

## 7. Save and conflict states

State labels are explicit:

- `Saved`
- `Unsaved changes`
- `Saving…`
- `Save failed`
- `Changed on disk`
- `Conflict`
- `Read-only`

A conflict opens a dedicated comparison view with:

- local unsaved version;
- current disk version;
- last common version when available;
- actions: keep local, accept disk, save local as new, copy either version.

No automatic merge is required in v0.1.

## 8. Search

Search surface includes:

- query field;
- path/group/Git-state filters;
- result list with document, heading, snippet, and location;
- keyboard traversal;
- index status;
- clear "rebuilding" and "stale" states.

Clicking a result opens the document at the matched line and highlights the match temporarily.

## 9. Annotations

Annotations appear in the context panel and as unobtrusive markers beside source/preview.

Creation flow:

1. select text or heading, or choose document-level annotation;
2. enter Markdown comment;
3. choose Personal or Shared;
4. save.

The visibility choice must be explicit and remembered only as a convenience default, not silently applied without display.

Detached annotations appear in a dedicated section with tools to re-anchor or delete.

## 10. Notes

Notes have a dedicated navigation area and open in the same editor surface. A note visibly identifies itself as Personal or Shared.

Creating a note from a document may prepopulate a link to that document or selection, but must not copy document text without user action.

## 11. Relationships

Context panel shows:

- outgoing links;
- backlinks;
- front-matter relations;
- user-authored sidecar relations.

Each relation is labelled by source type. No relation may be presented as inferred or intelligent in v0.1.

## 12. Git context

Git panel is read-only and includes:

- current file state;
- working-tree diff;
- compact history;
- blame for selected line or visible range.

The UI MUST NOT include stage, commit, discard, checkout, or reset actions.

## 13. Responsive behaviour

v0.1 targets laptop and desktop widths. Below 900 px:

- one side panel is visible at a time;
- split view may collapse to source or preview;
- all functions remain available.

Mobile optimisation is not a release requirement.

## 14. Keyboard baseline

| Action | Shortcut |
|---|---|
| Quick open | `Cmd/Ctrl+P` |
| Command palette | `Cmd/Ctrl+Shift+P` |
| Save | `Cmd/Ctrl+S` |
| Workspace search | `Cmd/Ctrl+Shift+F` |
| Find in document | `Cmd/Ctrl+F` |
| Toggle source/preview | `Cmd/Ctrl+\` |
| Close tab | `Cmd/Ctrl+W` |
| Reopen closed tab | `Cmd/Ctrl+Shift+T` |
| Add annotation | configurable; no browser-conflicting default required |

## 15. Accessibility

- Semantic landmarks and headings are required.
- Every icon-only control has an accessible name and tooltip.
- Focus is never trapped except in an active modal.
- Modals close with Escape when safe.
- Status is conveyed by text/icon as well as colour.
- Reduced-motion preferences are honoured.
