<script lang="ts">
  import { quickOpen } from "./tree";
  import type { DocumentSummary } from "../api/types";

  interface Props {
    documents: DocumentSummary[];
    onopen: (id: string) => void;
    onclose: () => void;
  }

  let { documents, onopen, onclose }: Props = $props();

  let query = $state("");
  let selected = $state(0);
  let input: HTMLInputElement | null = $state(null);

  const results = $derived(quickOpen(documents, query, 40));

  $effect(() => {
    // Reset the cursor whenever the result set changes shape.
    if (selected >= results.length) selected = 0;
  });

  $effect(() => {
    input?.focus();
  });

  function onkeydown(event: KeyboardEvent): void {
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        selected = results.length === 0 ? 0 : (selected + 1) % results.length;
        break;
      case "ArrowUp":
        event.preventDefault();
        selected = results.length === 0 ? 0 : (selected - 1 + results.length) % results.length;
        break;
      case "Enter":
        event.preventDefault();
        if (results[selected]) {
          onopen(results[selected].document.id);
          onclose();
        }
        break;
      case "Escape":
        event.preventDefault();
        onclose();
        break;
    }
  }
</script>

<!-- A modal dialog: Escape closes it, and focus stays inside while open
     (spec 04 section 15). -->
<div
  class="backdrop"
  role="presentation"
  onclick={(e) => {
    if (e.target === e.currentTarget) onclose();
  }}
>
  <div class="panel" role="dialog" aria-modal="true" aria-label="Quick open">
    <input
      bind:this={input}
      bind:value={query}
      {onkeydown}
      type="text"
      class="query"
      placeholder="Open a document by path, name, or title…"
      aria-label="Quick open query"
      aria-controls="quick-open-results"
      autocomplete="off"
      spellcheck="false"
    />

    <ul id="quick-open-results" class="results" role="listbox" aria-label="Results">
      {#each results as result, index (result.document.id)}
        <li role="none">
          <button
            type="button"
            role="option"
            aria-selected={index === selected}
            class="result"
            class:selected={index === selected}
            onclick={() => {
              onopen(result.document.id);
              onclose();
            }}
            onmouseenter={() => (selected = index)}
          >
            <span class="title">{result.document.title}</span>
            <span class="path">{result.document.id}</span>
            <!-- Spec 04 section 4.2: show why each result matched. -->
            <span class="reason">{result.reason}</span>
          </button>
        </li>
      {:else}
        <li class="empty">No document matches “{query}”.</li>
      {/each}
    </ul>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    display: flex;
    justify-content: center;
    align-items: flex-start;
    padding-top: 12vh;
    background: rgb(0 0 0 / 55%);
    z-index: 50;
  }

  .panel {
    width: min(42rem, 92vw);
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    box-shadow: 0 16px 48px rgb(0 0 0 / 45%);
    overflow: hidden;
  }

  .query {
    width: 100%;
    padding: 0.8rem 1rem;
    border: 0;
    border-bottom: 1px solid var(--line);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: 1rem;
  }

  .query:focus {
    outline: none;
    border-bottom-color: var(--focus);
  }

  .results {
    max-height: 50vh;
    margin: 0;
    padding: 0;
    list-style: none;
    overflow-y: auto;
  }

  .result {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: baseline;
    gap: 0.6rem;
    width: 100%;
    padding: 0.45rem 1rem;
    border: 0;
    background: none;
    color: var(--text-secondary);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .result.selected {
    background: var(--surface-raised);
    box-shadow: inset 2px 0 0 var(--accent);
  }

  .title {
    color: var(--text-primary);
    font-size: 0.9rem;
  }

  .path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: var(--text-muted);
  }

  .reason {
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
  }

  .empty {
    padding: 1rem;
    color: var(--text-muted);
    font-size: 0.85rem;
  }
</style>
