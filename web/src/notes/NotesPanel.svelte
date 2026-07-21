<script lang="ts">
  import { listNotes, createNote, getNote, ApiError } from "../api/client";
  import type { DocumentSummary } from "../api/types";
  import type { Note, NoteSummary, NoteLink, Visibility } from "./types";

  interface Props {
    /** Documents offered as link targets when creating a note (R9). */
    documents: DocumentSummary[];
    /** Bumped by the shell to force a reload after a workspace switch. */
    generation: number;
    /** The open note tab, so the list can mark it active. */
    activeId?: string | null;
    onopen: (note: Note) => void;
    onopenlink: (link: NoteLink) => void;
  }

  let { documents, generation, activeId = null, onopen, onopenlink }: Props = $props();

  let notes = $state<NoteSummary[]>([]);
  let error = $state<string | null>(null);
  let creating = $state(false);

  // Draft for a new note.
  let draftOpen = $state(false);
  let draftTitle = $state("");
  let draftVisibility = $state<Visibility>("personal");
  let draftDocument = $state("");
  let draftHeading = $state("");

  async function reload(): Promise<void> {
    try {
      notes = await listNotes();
      error = null;
    } catch (err) {
      if (err instanceof ApiError && err.status === 503) {
        notes = [];
        return;
      }
      error = err instanceof ApiError ? `${err.code}: ${err.message}` : "Notes could not be loaded.";
    }
  }

  $effect(() => {
    void generation;
    void reload();
  });

  async function openNote(summary: NoteSummary): Promise<void> {
    try {
      onopen(await getNote(summary.visibility, summary.id));
    } catch (err) {
      error = err instanceof ApiError ? `${err.code}: ${err.message}` : "The note could not be opened.";
    }
  }

  async function create(): Promise<void> {
    if (draftTitle.trim() === "") {
      error = "A note needs a title.";
      return;
    }
    creating = true;
    error = null;
    const links: NoteLink[] = [];
    if (draftDocument) {
      links.push({ document: draftDocument, ...(draftHeading ? { heading: draftHeading } : {}) });
    }
    try {
      const note = await createNote({ title: draftTitle.trim(), visibility: draftVisibility, body: "", links });
      draftOpen = false;
      draftTitle = "";
      draftDocument = "";
      draftHeading = "";
      await reload();
      onopen(note);
    } catch (err) {
      error = err instanceof ApiError ? `${err.code}: ${err.message}` : "The note could not be created.";
    } finally {
      creating = false;
    }
  }
</script>

<div class="notes-panel">
  <div class="panel-actions">
    <button type="button" class="new-note" onclick={() => (draftOpen = !draftOpen)}>
      {draftOpen ? "Cancel" : "New note"}
    </button>
  </div>

  {#if draftOpen}
    <form class="draft" onsubmit={(e) => { e.preventDefault(); void create(); }}>
      <input class="field" bind:value={draftTitle} placeholder="Note title" aria-label="New note title" />
      <select class="field" bind:value={draftVisibility} aria-label="Visibility">
        <option value="personal">Personal (private)</option>
        <option value="shared">Shared (committable)</option>
      </select>
      <select class="field" bind:value={draftDocument} aria-label="Link to document (optional)">
        <option value="">Link to a document… (optional)</option>
        {#each documents as doc}
          <option value={doc.id}>{doc.title}</option>
        {/each}
      </select>
      {#if draftDocument}
        <input class="field" bind:value={draftHeading} placeholder="Heading (optional)" aria-label="Link heading" />
      {/if}
      <button type="submit" class="create" disabled={creating}>{creating ? "Creating…" : "Create note"}</button>
    </form>
  {/if}

  {#if error}
    <p class="error" role="status">{error}</p>
  {/if}

  {#if notes.length === 0}
    <p class="empty">No notes yet. Create one to capture context that lives beside the workspace.</p>
  {:else}
    <ul class="note-list">
      {#each notes as note (note.visibility + note.id)}
        <li>
          <button type="button" class="note-item" class:active={activeId === note.id} onclick={() => void openNote(note)}>
            <span class="note-item-title">{note.title}</span>
            <span class="note-item-badge {note.visibility}">{note.visibility}</span>
          </button>
          {#if note.links && note.links.length > 0}
            <div class="note-item-links">
              {#each note.links as link}
                {#if link.document}
                  <button type="button" class="mini-link" onclick={() => onopenlink(link)}>
                    → {link.heading ? link.heading : link.document}
                  </button>
                {/if}
              {/each}
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .notes-panel { display: flex; flex-direction: column; gap: 0.6rem; }
  .panel-actions { display: flex; }
  .new-note, .create { padding: 0.3rem 0.7rem; border: 1px solid var(--line-strong); border-radius: var(--radius); background: var(--surface-raised); color: var(--text-primary); font: inherit; font-size: 0.78rem; cursor: pointer; }
  .draft { display: flex; flex-direction: column; gap: 0.4rem; padding: 0.5rem; border: 1px solid var(--line-strong); border-radius: var(--radius); background: var(--surface-panel); }
  .field { padding: 0.3rem; border: 1px solid var(--line-strong); border-radius: var(--radius); background: var(--surface-raised); color: var(--text-primary); font: inherit; font-size: 0.78rem; }
  .error { margin: 0; color: var(--danger); font-size: 0.78rem; }
  .empty { margin: 0.4rem 0; color: var(--text-muted); font-size: 0.8rem; line-height: 1.4; }
  .note-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.3rem; }
  .note-item { display: flex; align-items: center; justify-content: space-between; gap: 0.4rem; width: 100%; padding: 0.35rem 0.5rem; border: 1px solid transparent; border-radius: var(--radius); background: var(--surface-panel); color: var(--text-primary); font: inherit; font-size: 0.82rem; text-align: left; cursor: pointer; }
  .note-item:hover { border-color: var(--line-strong); }
  .note-item.active { border-color: var(--accent); }
  .note-item-title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .note-item-badge { flex-shrink: 0; padding: 0.05rem 0.35rem; border-radius: 999px; font-size: 0.6rem; text-transform: uppercase; letter-spacing: 0.04em; border: 1px solid var(--line-strong); color: var(--text-muted); }
  .note-item-badge.shared { color: var(--accent); }
  .note-item-links { display: flex; flex-wrap: wrap; gap: 0.3rem; margin: 0.2rem 0 0 0.5rem; }
  .mini-link { padding: 0.1rem 0.4rem; border: 1px solid var(--line-strong); border-radius: 999px; background: transparent; color: var(--accent); font: inherit; font-size: 0.68rem; cursor: pointer; }
</style>
