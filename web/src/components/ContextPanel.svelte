<script lang="ts">
  import Outline from "./Outline.svelte";
  import NotesPanel from "../notes/NotesPanel.svelte";
  import RelationshipsPanel from "../relationships/RelationshipsPanel.svelte";
  import GitPanel from "../git/GitPanel.svelte";
  import type { DocumentDetail } from "../api/types";
  import type { Note, NoteLink } from "../notes/types";

  type Tab = "outline" | "notes" | "links" | "git";
  type Dock = "right" | "bottom";

  interface Props {
    gitEnabled: boolean;
    tab: Tab;
    ontab: (tab: Tab) => void;
    /** The active document (its outline drives the Outline tab). */
    activeDoc: DocumentDetail | null;
    notesGeneration: number;
    linksGeneration: number;
    activeId: string | null;
    noteActiveId: string | null;
    onopen: (id: string) => void;
    onopennote: (note: Note) => void;
    onnewnote: () => void;
    onopenlink: (link: NoteLink) => void;
    /** Placement controls. */
    dock: Dock;
    popped: boolean;
    ondock: (dock: Dock) => void;
    onpopout: () => void;
    onclose: () => void;
    /** Hide the panel entirely (docked view). */
    onhide: () => void;
  }

  let {
    gitEnabled,
    tab,
    ontab,
    activeDoc,
    notesGeneration,
    linksGeneration,
    activeId,
    noteActiveId,
    onopen,
    onopennote,
    onnewnote,
    onopenlink,
    dock,
    popped,
    ondock,
    onpopout,
    onclose,
    onhide,
  }: Props = $props();

  const tabs: { value: Tab; label: string; show: boolean }[] = $derived([
    { value: "outline", label: "Outline", show: true },
    { value: "notes", label: "Notes", show: true },
    { value: "links", label: "Links", show: true },
    { value: "git", label: "Git", show: gitEnabled },
  ]);
</script>

<div class="ctx">
  <div class="ctx-toolbar">
    {#if popped}
      <button type="button" class="tool" title="Close" aria-label="Close" onclick={onclose}>Close ×</button>
    {:else}
      <button
        type="button"
        class="tool"
        class:active={dock === "right"}
        title="Dock to the right"
        onclick={() => ondock("right")}
      >
        Side
      </button>
      <button
        type="button"
        class="tool"
        class:active={dock === "bottom"}
        title="Dock to the bottom"
        onclick={() => ondock("bottom")}
      >
        Bottom
      </button>
      <button type="button" class="tool" title="Pop out into a window" onclick={onpopout}>Pop out ⤢</button>
      <button type="button" class="tool" title="Hide the context sidebar (⌘⇧B)" aria-label="Hide the context sidebar" onclick={onhide}>Hide ×</button>
    {/if}
  </div>

  <div class="nav-switch" role="group" aria-label="Context view">
    {#each tabs as t}
      {#if t.show}
        <button type="button" class:active={tab === t.value} aria-pressed={tab === t.value} onclick={() => ontab(t.value)}>
          {t.label}
        </button>
      {/if}
    {/each}
  </div>

  <div class="ctx-body">
    {#if tab === "outline"}
      {#if activeDoc}
        <Outline outline={activeDoc.outline} />
      {:else}
        <p class="pending">Open a document to see its outline.</p>
      {/if}
    {:else if tab === "notes"}
      <NotesPanel
        generation={notesGeneration}
        activeId={noteActiveId}
        onopen={onopennote}
        onnew={onnewnote}
        {onopenlink}
      />
    {:else if tab === "links"}
      <RelationshipsPanel documentId={activeId} generation={linksGeneration} {onopen} />
    {:else}
      <GitPanel documentId={activeId} generation={linksGeneration} />
    {/if}
  </div>
</div>

<style>
  .ctx {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  .ctx-toolbar {
    display: flex;
    justify-content: flex-end;
    gap: 0.25rem;
    margin-bottom: 0.4rem;
  }

  .tool {
    padding: 0.1rem 0.4rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-muted);
    font: inherit;
    font-size: 0.68rem;
    cursor: pointer;
  }

  .tool:hover {
    color: var(--text-primary);
    border-color: var(--focus);
  }

  .tool.active {
    color: var(--accent);
    border-color: var(--accent);
  }

  .nav-switch {
    display: flex;
    margin-bottom: 0.6rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .nav-switch button {
    flex: 1;
    padding: 0.25rem 0.4rem;
    border: 0;
    border-right: 1px solid var(--line-strong);
    background: var(--surface-panel);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.7rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    cursor: pointer;
  }

  .nav-switch button:last-child {
    border-right: 0;
  }

  .nav-switch button.active {
    background: var(--surface-raised);
    color: var(--accent);
  }

  .ctx-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .pending {
    margin: 0 0.5rem;
    color: var(--text-muted);
    font-size: 0.85rem;
  }
</style>
