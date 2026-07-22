<script lang="ts">
  import NoteView from "./NoteView.svelte";
  import type { Note, NoteLink } from "./types";
  import type { Capabilities, DocumentSummary } from "../api/types";

  interface Props {
    note: Note;
    capabilities: Capabilities;
    /** Documents offered as link targets when editing the note. */
    documents?: DocumentSummary[];
    /** Close the modal (guarded when there are unsaved edits). */
    onclose: () => void;
    /** Follow a link in the note to a document, closing the modal first. */
    onopenlink: (link: NoteLink) => void;
    /** Bumped after a create, update, or delete so the notes list refreshes. */
    onchanged?: () => void;
  }

  let { note, capabilities, documents = [], onclose, onopenlink, onchanged }: Props = $props();

  let dirty = $state(false);

  // Closing unmounts the editor, so unsaved note text would be lost; confirm
  // first, matching how the document editor protects a dirty buffer.
  function guardedClose(): void {
    if (dirty && !window.confirm("This note has unsaved changes. Discard them?")) return;
    onclose();
  }
</script>

<div class="note-overlay">
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="backdrop" onclick={guardedClose}></div>

  <div class="note-dialog" role="dialog" aria-modal="true" aria-label={`Note: ${note.title}`}>
    <button type="button" class="close" aria-label="Close note" onclick={guardedClose}>×</button>
    <NoteView
      {note}
      {capabilities}
      {documents}
      active={true}
      ondirty={(d) => (dirty = d)}
      onsaved={() => onchanged?.()}
      onopenlink={(link) => {
        onclose();
        onopenlink(link);
      }}
      onclosed={() => {
        onchanged?.();
        onclose();
      }}
    />
  </div>
</div>

<svelte:window onkeydown={(e) => e.key === "Escape" && guardedClose()} />

<style>
  .note-overlay {
    position: fixed;
    inset: 0;
    z-index: 40;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 2.5rem;
  }

  .backdrop {
    position: absolute;
    inset: 0;
    background: rgb(0 0 0 / 45%);
  }

  .note-dialog {
    position: relative;
    display: flex;
    flex-direction: column;
    width: min(64rem, 100%);
    height: min(46rem, 100%);
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-base, var(--surface-panel));
    box-shadow: 0 12px 48px rgb(0 0 0 / 45%);
    overflow: hidden;
  }

  .close {
    position: absolute;
    top: 0.5rem;
    right: 0.6rem;
    z-index: 1;
    padding: 0.15rem 0.5rem;
    border: 0;
    background: none;
    color: var(--text-muted);
    font-size: 1.3rem;
    line-height: 1;
    cursor: pointer;
  }

  .close:hover {
    color: var(--text-primary);
  }
</style>
