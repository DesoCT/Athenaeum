<script lang="ts">
  // A recursive component imports itself in Svelte 5; svelte:self is removed.
  import FileTree from "./FileTree.svelte";
  import type { TreeNode } from "./tree";

  interface Props {
    nodes: TreeNode[];
    activeId: string | null;
    onopen: (id: string) => void;
    depth?: number;
    expanded: Set<string>;
    ontoggle: (path: string) => void;
    /** Right-clicking a directory offers to make a workspace from it. */
    onscaffold?: (dirPath: string) => void;
  }

  let { nodes, activeId, onopen, depth = 0, expanded, ontoggle, onscaffold }: Props = $props();
</script>

<ul class="tree" role={depth === 0 ? "tree" : "group"}>
  {#each nodes as node (node.path)}
    {@const isOpen = expanded.has(node.path)}
    <li role="none">
      {#if node.kind === "directory"}
        <button
          type="button"
          class="row directory"
          style="padding-left: {0.5 + depth * 0.75}rem"
          aria-expanded={isOpen}
          title={onscaffold ? "Right-click to make a workspace from this folder" : undefined}
          onclick={() => ontoggle(node.path)}
          oncontextmenu={(e) => {
            if (!onscaffold) return;
            e.preventDefault();
            onscaffold(node.path);
          }}
        >
          <span class="chevron" class:open={isOpen} aria-hidden="true">›</span>
          <span class="label">{node.name}</span>
        </button>
        {#if isOpen}
          <FileTree
            nodes={node.children}
            {activeId}
            {onopen}
            {expanded}
            {ontoggle}
            {onscaffold}
            depth={depth + 1}
          />
        {/if}
      {:else}
        <button
          type="button"
          class="row document"
          class:active={node.path === activeId}
          style="padding-left: {1.1 + depth * 0.75}rem"
          aria-current={node.path === activeId ? "true" : undefined}
          onclick={() => onopen(node.path)}
          title={node.path}
        >
          <span class="label">{node.name}</span>
          {#if node.document && !node.document.writable}
            <span class="badge" title="Outside the configured write boundary">read-only</span>
          {/if}
          {#if node.document?.too_large}
            <span class="badge warn" title="Above the editable size limit">large</span>
          {/if}
        </button>
      {/if}
    </li>
  {/each}
</ul>

<style>
  .tree {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    width: 100%;
    padding: 0.2rem 0.5rem;
    border: 0;
    background: none;
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.85rem;
    text-align: left;
    cursor: pointer;
  }

  .row:hover {
    background: var(--surface-raised);
    color: var(--text-primary);
  }

  .row.active {
    background: var(--surface-raised);
    color: var(--text-primary);
    box-shadow: inset 2px 0 0 var(--accent);
  }

  .directory {
    color: var(--text-primary);
  }

  .chevron {
    display: inline-block;
    width: 0.7rem;
    transition: transform 0.12s ease;
  }

  .chevron.open {
    transform: rotate(90deg);
  }

  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .badge {
    margin-left: auto;
    padding: 0 0.3rem;
    border: 1px solid var(--line-strong);
    border-radius: 2px;
    color: var(--text-muted);
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .badge.warn {
    border-color: var(--warn);
    color: var(--warn);
  }
</style>
