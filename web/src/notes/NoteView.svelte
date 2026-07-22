<script lang="ts">
  import { createNote, updateNote, deleteNote, NoteConflictError, ApiError } from "../api/client";
  import type { Capabilities, DocumentDetail, DocumentSummary } from "../api/types";
  import type { Note, NoteLink, Visibility } from "./types";
  import Editor from "../editor/Editor.svelte";
  import Preview from "../renderer/Preview.svelte";

  interface Props {
    /** The note to edit, or a blank draft (empty id) to create. */
    note: Note;
    capabilities: Capabilities;
    active?: boolean;
    /** Documents offered as link targets. */
    documents?: DocumentSummary[];
    /** Opens a linked document, optionally at a heading (G4). */
    onopenlink?: (link: NoteLink) => void;
    /** Reports dirty state so the modal can guard a close. */
    ondirty?: (dirty: boolean) => void;
    /** Fires after a successful create or update, so the notes list refreshes. */
    onsaved?: (note: Note) => void;
    /** Removes this note after deletion. */
    onclosed?: () => void;
  }

  let { note, capabilities, active = true, documents = [], onopenlink, ondirty, onsaved, onclosed }: Props = $props();

  const isNew = $derived(note.id === "");

  type Mode = "split" | "source" | "preview";
  let mode = $state<Mode>("split");
  let wrap = $state(true);

  /* svelte-ignore state_referenced_locally */
  let title = $state(note.title);
  /* svelte-ignore state_referenced_locally */
  let body = $state(note.body);
  /* svelte-ignore state_referenced_locally */
  let visibility = $state<Visibility>(note.visibility || "personal");
  /* svelte-ignore state_referenced_locally */
  let links = $state<NoteLink[]>([...(note.links ?? [])]);
  /* svelte-ignore state_referenced_locally */
  let baseVersion = $state(note.version);
  /* svelte-ignore state_referenced_locally */
  let lastLoadedId = $state(note.id);

  // Draft link being added.
  let linkDoc = $state("");
  let linkHeading = $state("");

  type SaveState = { kind: "saved" } | { kind: "dirty" } | { kind: "saving" } | { kind: "failed"; message: string };
  let saveState = $state<SaveState>({ kind: "saved" });

  // Re-seed when a different note (or draft) arrives in the same slot.
  $effect(() => {
    if (note.id !== lastLoadedId) {
      lastLoadedId = note.id;
      title = note.title;
      body = note.body;
      visibility = note.visibility || "personal";
      links = [...(note.links ?? [])];
      baseVersion = note.version;
      saveState = { kind: "saved" };
    }
  });

  const linksChanged = $derived(JSON.stringify(links) !== JSON.stringify(note.links ?? []));
  const dirty = $derived(
    isNew
      ? title.trim() !== "" || body.trim() !== "" || links.length > 0
      : title !== note.title || body !== note.body || linksChanged || saveState.kind === "dirty",
  );
  $effect(() => ondirty?.(dirty));

  // The body renders through the same pipeline as a document, so a note reads
  // exactly like the rest of the workspace (reuse over a second renderer).
  const asDocument = $derived<DocumentDetail>({
    id: `note:${note.id || "new"}`,
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

  function addLink(): void {
    if (!linkDoc) return;
    links = [...links, { document: linkDoc, ...(linkHeading.trim() ? { heading: linkHeading.trim() } : {}) }];
    linkDoc = "";
    linkHeading = "";
  }

  function removeLink(index: number): void {
    links = links.filter((_, i) => i !== index);
  }

  async function save(): Promise<void> {
    if (title.trim() === "") {
      saveState = { kind: "failed", message: "A note needs a title." };
      return;
    }
    saveState = { kind: "saving" };
    try {
      const result = isNew
        ? await createNote({ title: title.trim(), visibility, body, links })
        : await updateNote(note.id, {
            visibility: note.visibility,
            expected_version: baseVersion,
            title: title.trim(),
            body,
            links,
          });
      note = result;
      title = result.title;
      body = result.body;
      visibility = result.visibility;
      links = [...(result.links ?? [])];
      baseVersion = result.version;
      lastLoadedId = result.id;
      saveState = { kind: "saved" };
      onsaved?.(result);
    } catch (err) {
      if (err instanceof NoteConflictError) {
        saveState = { kind: "failed", message: "This note changed elsewhere. Reopen it to see the current version." };
        return;
      }
      saveState = { kind: "failed", message: err instanceof ApiError ? `${err.code}: ${err.message}` : "The note could not be saved." };
    }
  }

  async function remove(): Promise<void> {
    if (isNew) {
      onclosed?.();
      return;
    }
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
      default: return isNew ? "New note" : dirty ? "Unsaved changes" : "Saved";
    }
  });
</script>

{#if active}
<div class="note">
  <header class="note-header">
    <div class="identity">
      <input class="note-title" bind:value={title} aria-label="Note title" placeholder="Untitled note" />
      {#if isNew}
        <select class="visibility-select" bind:value={visibility} aria-label="Visibility">
          <option value="personal">Personal</option>
          <option value="shared">Shared</option>
        </select>
      {:else}
        <span class="visibility-badge {note.visibility}">{note.visibility}</span>
      {/if}
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
      <button type="button" class="delete" onclick={remove}>{isNew ? "Discard" : "Delete"}</button>
    </div>
  </header>

  <div class="links-row">
    {#each links as link, i (i)}
      {#if link.document}
        <span class="link-chip">
          <button type="button" class="chip-open" onclick={() => onopenlink?.(link)}>
            → {link.document}{link.heading ? ` › ${link.heading}` : ""}
          </button>
          <button type="button" class="chip-remove" aria-label="Remove link" onclick={() => removeLink(i)}>×</button>
        </span>
      {/if}
    {/each}
    <span class="add-link">
      <select bind:value={linkDoc} aria-label="Link a document">
        <option value="">Link a document…</option>
        {#each documents as d}
          <option value={d.id}>{d.title}</option>
        {/each}
      </select>
      {#if linkDoc}
        <input class="heading-input" bind:value={linkHeading} placeholder="Heading (optional)" aria-label="Link heading" />
      {/if}
      <button type="button" class="add-btn" onclick={addLink} disabled={!linkDoc}>Add</button>
    </span>
  </div>

  {#if saveState.kind === "failed"}
    <aside class="save-failed" role="alert"><p>{(saveState as { message: string }).message}</p></aside>
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
  .note-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; margin-bottom: 0.6rem; }
  .identity { display: flex; align-items: center; gap: 0.6rem; flex: 1; min-width: 0; }
  .note-title {
    flex: 1; min-width: 0; padding: 0.3rem 0.4rem; border: 1px solid transparent; border-radius: var(--radius);
    background: transparent; color: var(--text-primary); font: inherit; font-size: 1.1rem; font-weight: 600;
  }
  .note-title:hover, .note-title:focus { border-color: var(--line-strong); background: var(--surface-panel); outline: none; }
  .visibility-select {
    padding: 0.25rem 0.4rem; border: 1px solid var(--line-strong); border-radius: var(--radius);
    background: var(--surface-panel); color: var(--text-secondary); font: inherit; font-size: 0.75rem;
  }
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
  .links-row { display: flex; flex-wrap: wrap; align-items: center; gap: 0.4rem; margin-bottom: 0.6rem; }
  .link-chip { display: inline-flex; align-items: center; border: 1px solid var(--line-strong); border-radius: 999px; overflow: hidden; }
  .chip-open { padding: 0.2rem 0.5rem; border: 0; background: transparent; color: var(--accent); font: inherit; font-size: 0.72rem; cursor: pointer; }
  .chip-remove { padding: 0.2rem 0.4rem; border: 0; border-left: 1px solid var(--line-strong); background: transparent; color: var(--text-muted); font: inherit; cursor: pointer; }
  .chip-remove:hover { color: var(--danger); }
  .add-link { display: inline-flex; align-items: center; gap: 0.3rem; }
  .add-link select, .heading-input { padding: 0.2rem 0.35rem; border: 1px solid var(--line-strong); border-radius: var(--radius); background: var(--surface-panel); color: var(--text-secondary); font: inherit; font-size: 0.72rem; }
  .add-btn { padding: 0.2rem 0.5rem; border: 1px solid var(--line-strong); border-radius: var(--radius); background: var(--surface-panel); color: var(--text-secondary); font: inherit; font-size: 0.72rem; cursor: pointer; }
  .add-btn:disabled { opacity: 0.45; cursor: default; }
  .save-failed { margin-bottom: 0.6rem; padding: 0.5rem 0.8rem; border: 1px solid var(--danger); border-radius: var(--radius); color: var(--danger); font-size: 0.82rem; }
  .save-failed p { margin: 0; }
  .surface { display: grid; grid-template-columns: 1fr; gap: 1rem; flex: 1; min-height: 0; }
  .surface.split { grid-template-columns: 1fr 1fr; }
  .editor-pane { display: flex; flex-direction: column; min-height: 0; min-width: 0; }
  .preview-pane { min-height: 0; min-width: 0; overflow-y: auto; }
  .wrap-toggle { display: flex; align-items: center; gap: 0.35rem; margin-top: 0.5rem; color: var(--text-muted); font-size: 0.75rem; }
  @media (max-width: 900px) { .surface.split { grid-template-columns: 1fr; } }
</style>
