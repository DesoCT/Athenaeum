<script lang="ts">
  import { getRelationships, ApiError } from "../api/client";
  import type { RelationshipResult, RelationshipRef } from "./types";

  interface Props {
    /** The active document, or null when a note or nothing is open. */
    documentId: string | null;
    /** Bumped by the shell after a workspace switch or a document change. */
    generation: number;
    onopen: (documentId: string) => void;
  }

  let { documentId, generation, onopen }: Props = $props();

  let result = $state<RelationshipResult | null>(null);
  let error = $state<string | null>(null);
  let loading = $state(false);

  const SOURCE_LABEL: Record<string, string> = {
    markdown: "link",
    wiki: "wiki",
    front_matter: "front matter",
    sidecar: "sidecar",
  };

  // Reload whenever the active document changes, or the corpus does.
  $effect(() => {
    const id = documentId;
    void generation;
    if (!id) {
      result = null;
      error = null;
      return;
    }
    loading = true;
    getRelationships(id)
      .then((r) => {
        result = r;
        error = null;
      })
      .catch((err) => {
        error = err instanceof ApiError ? `${err.code}: ${err.message}` : "Relationships could not be loaded.";
        result = null;
      })
      .finally(() => (loading = false));
  });
</script>

<div class="relationships-panel">
  {#if !documentId}
    <p class="pending">Open a document to see its links and backlinks.</p>
  {:else if error}
    <p class="error" role="status">{error}</p>
  {:else if loading && !result}
    <p class="pending">Loading…</p>
  {:else if result}
    {@render section("Outgoing", result.outgoing)}
    {@render section("Backlinks", result.backlinks)}
    {#if result.outgoing.length === 0 && result.backlinks.length === 0}
      <p class="pending">No explicit relationships. Athenaeum never infers links.</p>
    {/if}
  {/if}
</div>

{#snippet section(heading: string, refs: RelationshipRef[])}
  {#if refs.length > 0}
    <section class="rel-section">
      <h3>{heading}</h3>
      <ul>
        {#each refs as ref}
          <li>
            <button type="button" class="rel-item" onclick={() => onopen(ref.document_id)}>
              <span class="rel-title">{ref.title}</span>
              <span class="rel-source {ref.source}">
                {ref.kind ? ref.kind : SOURCE_LABEL[ref.source] ?? ref.source}
              </span>
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/if}
{/snippet}

<style>
  .relationships-panel { display: flex; flex-direction: column; gap: 0.8rem; }
  .rel-section h3 {
    margin: 0 0 0.35rem; font-size: 0.7rem; font-weight: 600; letter-spacing: 0.08em;
    text-transform: uppercase; color: var(--text-secondary);
  }
  .rel-section ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.25rem; }
  .rel-item {
    display: flex; align-items: center; justify-content: space-between; gap: 0.4rem; width: 100%;
    padding: 0.3rem 0.45rem; border: 1px solid transparent; border-radius: var(--radius);
    background: var(--surface-panel); color: var(--text-primary); font: inherit; font-size: 0.8rem;
    text-align: left; cursor: pointer;
  }
  .rel-item:hover { border-color: var(--line-strong); }
  .rel-title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .rel-source {
    flex-shrink: 0; padding: 0.05rem 0.4rem; border-radius: 999px; font-size: 0.6rem;
    text-transform: lowercase; letter-spacing: 0.02em; border: 1px solid var(--line-strong); color: var(--text-muted);
  }
  .rel-source.markdown { color: var(--accent); }
  .rel-source.wiki { color: #3f8a5c; }
  .rel-source.front_matter { color: var(--warn); }
  .rel-source.sidecar { color: #7a4ba8; }
  .pending { margin: 0.2rem 0; color: var(--text-muted); font-size: 0.8rem; line-height: 1.4; }
  .error { margin: 0; color: var(--danger); font-size: 0.78rem; }
</style>
