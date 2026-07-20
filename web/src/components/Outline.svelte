<script lang="ts">
  import type { Heading } from "../api/types";

  interface Props {
    outline: Heading[];
  }

  let { outline }: Props = $props();

  /** Scroll to a heading using the backend-supplied slug (ADR-0003). */
  function go(slug: string): void {
    document.getElementById(slug)?.scrollIntoView({ behavior: "smooth", block: "start" });
  }
</script>

{#if outline.length === 0}
  <p class="empty">This document has no headings.</p>
{:else}
  <nav aria-label="Document outline">
    <ul>
      {#each outline as heading (heading.slug)}
        <li style="padding-left: {(heading.level - 1) * 0.7}rem">
          <button type="button" onclick={() => go(heading.slug)} title={heading.path.join(" › ")}>
            {heading.text}
          </button>
        </li>
      {/each}
    </ul>
  </nav>
{/if}

<style>
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  button {
    display: block;
    width: 100%;
    padding: 0.18rem 0.3rem;
    border: 0;
    background: none;
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.8rem;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: pointer;
  }

  button:hover {
    color: var(--text-primary);
    background: var(--surface-raised);
  }

  .empty {
    margin: 0;
    color: var(--text-muted);
    font-size: 0.85rem;
  }
</style>
