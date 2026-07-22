<script lang="ts">
  /**
   * Open document tabs (spec 04 section 5, R13).
   *
   * Tabs use full accessible labels and keep their unsaved-state indicator, so
   * a dirty buffer is never hidden behind a truncated title.
   */
  interface Tab {
    documentId: string;
    title: string;
    dirty: boolean;
  }

  interface Props {
    tabs: Tab[];
    activeId: string | null;
    onselect: (documentId: string) => void;
    onclose: (documentId: string) => void;
    /** Moves the dragged tab to the position of the tab it was dropped on. */
    onreorder?: (fromId: string, toId: string) => void;
  }

  let { tabs, activeId, onselect, onclose, onreorder }: Props = $props();

  // Drag-to-reorder state. `dragging` is the id being moved; `over` is the tab
  // currently under the pointer, for a drop-target highlight.
  let dragging = $state<string | null>(null);
  let over = $state<string | null>(null);

  function onDrop(toId: string): void {
    if (dragging && dragging !== toId) onreorder?.(dragging, toId);
    dragging = null;
    over = null;
  }

  function onkeydown(event: KeyboardEvent, index: number): void {
    if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
    event.preventDefault();
    const step = event.key === "ArrowRight" ? 1 : -1;
    const next = tabs[(index + step + tabs.length) % tabs.length];
    if (next) onselect(next.documentId);
  }
</script>

{#if tabs.length > 0}
  <div class="tab-strip" role="tablist" aria-label="Open documents">
    {#each tabs as tab, index (tab.documentId)}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="tab"
        class:active={tab.documentId === activeId}
        class:dragging={dragging === tab.documentId}
        class:over={over === tab.documentId && dragging !== tab.documentId}
        draggable={onreorder != null}
        ondragstart={(e) => {
          dragging = tab.documentId;
          e.dataTransfer?.setData("text/plain", tab.documentId);
          if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
        }}
        ondragover={(e) => {
          if (!dragging) return;
          e.preventDefault();
          over = tab.documentId;
        }}
        ondragleave={() => {
          if (over === tab.documentId) over = null;
        }}
        ondrop={(e) => {
          e.preventDefault();
          onDrop(tab.documentId);
        }}
        ondragend={() => {
          dragging = null;
          over = null;
        }}
      >
        <button
          type="button"
          role="tab"
          aria-selected={tab.documentId === activeId}
          class="label"
          title={tab.documentId}
          onclick={() => onselect(tab.documentId)}
          onauxclick={(e) => {
            // Middle click closes, matching every editor and browser.
            if (e.button === 1) {
              e.preventDefault();
              onclose(tab.documentId);
            }
          }}
          onkeydown={(event) => onkeydown(event, index)}
        >
          {#if tab.dirty}
            <span class="dirty-dot" aria-hidden="true">●</span>
          {/if}
          <span class="title">{tab.title}</span>
          {#if tab.dirty}
            <!-- Stated as text too, not conveyed by the dot alone (N5). -->
            <span class="visually-hidden">unsaved changes</span>
          {/if}
        </button>
        <button
          type="button"
          class="close"
          aria-label={`Close ${tab.title}`}
          onclick={() => onclose(tab.documentId)}
        >
          ×
        </button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .tab-strip {
    display: flex;
    align-items: stretch;
    gap: 1px;
    overflow-x: auto;
    border-bottom: 1px solid var(--line);
    background: var(--surface-panel);
  }

  .tab {
    display: flex;
    align-items: center;
    flex-shrink: 0;
    max-width: 16rem;
    border-right: 1px solid var(--line);
    background: var(--surface-panel);
  }

  .tab.active {
    background: var(--surface-raised);
    box-shadow: inset 0 2px 0 var(--accent);
  }

  .tab[draggable="true"] {
    cursor: grab;
  }

  .tab.dragging {
    opacity: 0.4;
  }

  /* A drop-target marker on the left edge of the tab being hovered. */
  .tab.over {
    box-shadow: inset 2px 0 0 var(--accent);
  }

  .label {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    min-width: 0;
    padding: 0.35rem 0.3rem 0.35rem 0.7rem;
    border: 0;
    background: none;
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.78rem;
    cursor: pointer;
  }

  .tab.active .label {
    color: var(--text-primary);
  }

  .title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .dirty-dot {
    color: var(--warn);
    font-size: 0.6rem;
  }

  .close {
    padding: 0.2rem 0.5rem 0.2rem 0.2rem;
    border: 0;
    background: none;
    color: var(--text-muted);
    font: inherit;
    font-size: 0.9rem;
    line-height: 1;
    cursor: pointer;
  }

  .close:hover {
    color: var(--danger);
  }

  .label:focus-visible,
  .close:focus-visible {
    outline: 2px solid var(--focus);
    outline-offset: -2px;
  }

  .visually-hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
