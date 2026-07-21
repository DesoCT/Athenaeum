<script lang="ts">
  import { updateNote, deleteNote, NoteConflictError, ApiError } from "../api/client";
  import type { Capabilities, DocumentDetail } from "../api/types";
  import type { Note, NoteLink } from "./types";
  import Editor from "../editor/Editor.svelte";
  import Preview from "../renderer/Preview.svelte";

  interface Props {
    note: Note;
    capabilities: Capabilities;
    active?: boolean;
    /** Opens a linked document, optionally at a heading (G4). */
    onopenlink?: (link: NoteLink) => void;
    /** Reports dirty state to the tab strip. */
    ondirty?: (dirty: boolean) => void;
    /** Removes this note's tab after deletion. */
    onclosed?: () => void;
  }

  let { note, capabilities, active = true, onopenlink, ondirty, onclosed }: Props = $props();

  type Mode = "split" | "source" | "preview";
  let mode = $state<Mode>("split");
  let wrap = $state(true);

  /* svelte-ignore state_referenced_locally */
  let title = $state(note.title);
  /* svelte-ignore state_referenced_locally */
  let body = $state(note.body);
  /* svelte-ignore state_referenced_locally */
  let baseVersion = $state(note.version);
  /* svelte-ignore state_referenced_locally */
  let lastLoadedId = $state(note.id);

  type SaveState = { kind: "saved" } | { kind: "dirty" } | { kind: "saving" } | { kind: "failed"; message: string };
  let saveState = $state<SaveState>({ kind: "saved" });

  // Re-seed when a different note arrives in the same component slot.
  $effect(() => {
    if (note.id !== lastLoadedId) {
      lastLoadedId = note.id;
      title = note.title;
      body = note.body;
      baseVersion = note.version;
      saveState = { kind: "saved" };
    }
  });

  const dirty = $derived(title !== note.title || body !== note.body || saveState.kind === "dirty");
  $effect(() => ondirty?.(dirty));

  // The body is rendered through the same pipeline as a document, so a note
  // reads exactly like the rest of the workspace (reuse over a second renderer).
  const asDocument = $derived<DocumentDetail>({
    id: `note:${note.id}`,
    title,
    size: body.length,
    mod_time: note.updated_at,
    writable: true,
    too_large: false,
    large_warning: false,
    content: body,
    version: baseVersion,
    encoding: "utf-8",
    line_ending: "lf",
    has_bom: false,
    front_matter_format: "none",
    outline: [],
    read_only: false,
  });

  async function save(): Promise<void> {
    saveState = { kind: "saving" };
    try {
      const updated = await updateNote(note.id, {
        visibility: note.visibility,
        expected_version: baseVersion,
        title,
        body,
      });
      baseVersion = updated.version;
      // Reflect the saved values back so `dirty` settles to false.
      note = updated;
      title = updated.title;
      body = updated.body;
      saveState = { kind: "saved" };
    } catch (err) {
      if (err instanceof NoteConflictError) {
        saveState = { kind: "failed", message: "This note changed elsewhere. Reopen it to see the current version." };
        return;
      }
      saveState = { kind: "failed", message: err instanceof ApiError ? `${err.code}: ${err.message}` : "The note could not be saved." };
    }
  }

  async function remove(): Promise<void> {
    if (!window.confirm(`Delete note "${note.title}"? This cannot be undone.`)) return;
    try {
      await deleteNote(note.visibility, note.id);
      onclosed?.();
    } catch (err) {
      saveState = { kind: "failed", message: err instanceof ApiError ? `${err.code}: ${err.message}` : "The note could not be deleted." };
    }
  }

  const stateLabel = $derived.by(() => {
    switch (saveState.kind) {
      case "saving": return "Saving…";
      case "failed": return "Save failed";
      default: return dirty ? "Unsaved changes" : "Saved";
    }
  });
</script>

{#if active}
<div class="note">
  <header class="note-header">
    <div class="identity">
      <input class="note-title" bind:value={title} aria-label="Note title" placeholder="Untitled note" />
      <span class="visibility-badge {note.visibility}">{note.visibility}</span>
    </div>
    <div class="controls">
      <div class="modes" role="group" aria-label="View mode">
        {#each ["split", "source", "preview"] as const as m}
          <button type="button" class:active={mode === m} aria-pressed={mode === m} onclick={() => (mode = m)}>
            {m[0].toUpperCase() + m.slice(1)}
          </button>
        {/each}
      </div>
      <span class="state" class:dirty={dirty} class:danger={saveState.kind === "failed"} role="status">{stateLabel}</span>
      <button type="button" class="save" onclick={save} disabled={!dirty || saveState.kind === "saving"}>Save <kbd>⌘S</kbd></button>
      <button type="button" class="delete" onclick={remove}>Delete</button>
    </div>
  </header>

  {#if note.links && note.links.length > 0}
    <div class="links" aria-label="Linked targets">
      {#each note.links as link}
        {#if link.document}
          <button type="button" class="link-chip" onclick={() => onopenlink?.(link)}>
            → {link.document}{link.heading ? ` › ${link.heading}` : ""}
          </button>
        {/if}
      {/each}
    </div>
  {/if}

  {#if saveState.kind === "failed"}
    <aside class="save-failed" role="alert"><p>{stateLabel === "Save failed" ? (saveState as { message: string }).message : ""}</p></aside>
  {/if}

  <div class="surface" class:split={mode === "split"}>
    {#if mode !== "preview"}
      <div class="editor-pane">
        <Editor value={body} readOnly={false} {wrap} onchange={(next) => (body = next)} onsave={save} />
      </div>
    {/if}
    {#if mode !== "source"}
      <div class="preview-pane">
        <Preview document={asDocument} {capabilities} />
      </div>
    {/if}
  </div>

  <label class="wrap-toggle">
    <input type="checkbox" bind:checked={wrap} /> Wrap long lines
  </label>
</div>
{/if}

<style>
  .note { display: flex; flex-direction: column; height: 100%; min-height: 0; padding: 1rem 1.25rem; }
  .note-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; margin-bottom: 0.75rem; }
  .identity { display: flex; align-items: center; gap: 0.6rem; flex: 1; min-width: 0; }
  .note-title {
    flex: 1; min-width: 0; padding: 0.3rem 0.4rem; border: 1px solid transparent; border-radius: var(--radius);
    background: transparent; color: var(--text-primary); font: inherit; font-size: 1.1rem; font-weight: 600;
  }
  .note-title:hover, .note-title:focus { border-color: var(--line-strong); background: var(--surface-panel); outline: none; }
  .visibility-badge {
    padding: 0.1rem 0.45rem; border-radius: 999px; font-size: 0.62rem; text-transform: uppercase; letter-spacing: 0.04em;
    border: 1px solid var(--line-strong); color: var(--text-secondary);
  }
  .visibility-badge.shared { color: var(--accent); }
  .controls { display: flex; align-items: center; gap: 0.6rem; flex-shrink: 0; }
  .modes { display: flex; border: 1px solid var(--line-strong); border-radius: var(--radius); overflow: hidden; }
  .modes button { padding: 0.25rem 0.6rem; border: 0; border-right: 1px solid var(--line-strong); background: var(--surface-panel); color: var(--text-secondary); font: inherit; font-size: 0.75rem; cursor: pointer; }
  .modes button:last-child { border-right: 0; }
  .modes button.active { background: var(--surface-raised); color: var(--accent); }
  .state { padding: 0.15rem 0.5rem; border: 1px solid var(--ok); border-radius: 2px; color: var(--ok); font-family: var(--font-mono); font-size: 0.7rem; white-space: nowrap; }
  .state.dirty { border-color: var(--warn); color: var(--warn); }
  .state.danger { border-color: var(--danger); color: var(--danger); }
  .save, .delete { padding: 0.3rem 0.7rem; border: 1px solid var(--line-strong); border-radius: var(--radius); background: var(--surface-raised); color: var(--text-primary); font: inherit; font-size: 0.78rem; cursor: pointer; }
  .delete { color: var(--danger); }
  .save:disabled { opacity: 0.45; cursor: default; }
  kbd { font-family: var(--font-mono); font-size: 0.68rem; color: var(--text-muted); }
  .links { display: flex; flex-wrap: wrap; gap: 0.4rem; margin-bottom: 0.6rem; }
  .link-chip { padding: 0.2rem 0.55rem; border: 1px solid var(--line-strong); border-radius: 999px; background: var(--surface-panel); color: var(--accent); font: inherit; font-size: 0.72rem; cursor: pointer; }
  .save-failed { margin-bottom: 0.75rem; padding: 0.6rem 0.9rem; border: 1px solid var(--danger); border-radius: var(--radius); color: var(--danger); font-size: 0.85rem; }
  .save-failed p { margin: 0; }
  .surface { display: grid; grid-template-columns: 1fr; gap: 1rem; flex: 1; min-height: 0; }
  .surface.split { grid-template-columns: 1fr 1fr; }
  .editor-pane { display: flex; flex-direction: column; min-height: 0; min-width: 0; }
  .preview-pane { min-height: 0; min-width: 0; overflow-y: auto; }
  .wrap-toggle { display: flex; align-items: center; gap: 0.35rem; margin-top: 0.5rem; color: var(--text-muted); font-size: 0.75rem; }
  @media (max-width: 900px) { .surface.split { grid-template-columns: 1fr; } }
</style>
