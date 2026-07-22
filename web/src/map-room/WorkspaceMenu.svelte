<script lang="ts">
  import type { WorkspaceRegistry, WorkspaceEntry } from "../api/types";

  interface Props {
    registry: WorkspaceRegistry;
    busy?: boolean;
    /** Switch to a registered workspace by name (a direct switch, not a leave). */
    onchoose: (name: string) => void;
    /** Open the full picker screen, for the rare case someone wants it. */
    onpicker: () => void;
    /** Re-read the hand-edited registry when the menu opens (C8). */
    onreload: () => void;
  }

  let { registry, busy = false, onchoose, onpicker, onreload }: Props = $props();

  let open = $state(false);

  const activeName = $derived(registry.active?.name ?? null);
  // Available entries first; the active one is marked, not hidden.
  const ordered = $derived(
    [...registry.entries].sort((a, b) => Number(b.available) - Number(a.available)),
  );

  function toggle(): void {
    open = !open;
    if (open) onreload();
  }

  function choose(entry: WorkspaceEntry): void {
    if (busy || !entry.available) return;
    open = false;
    // Selecting the workspace already open is a no-op; switching to another
    // swaps to it directly, without ever landing on an empty picker.
    if (entry.active) return;
    onchoose(entry.name);
  }
</script>

<div class="ws-menu">
  <button
    type="button"
    class="trigger"
    aria-haspopup="menu"
    aria-expanded={open}
    disabled={busy}
    onclick={toggle}
  >
    {activeName ?? "Workspaces"}
    <span class="caret" aria-hidden="true">▾</span>
  </button>

  {#if open}
    <!-- A backdrop so a click anywhere closes the menu. -->
    <button
      type="button"
      class="backdrop"
      aria-label="Close workspace menu"
      onclick={() => (open = false)}
    ></button>

    <div class="menu" role="menu">
      <p class="menu-label">Switch workspace</p>
      {#if ordered.length === 0}
        <p class="empty">No workspaces registered.</p>
      {:else}
        {#each ordered as entry (entry.name)}
          <button
            type="button"
            role="menuitem"
            class="item"
            class:active={entry.active}
            disabled={!entry.available || busy}
            title={entry.available ? entry.path : (entry.reason ?? entry.path)}
            onclick={() => choose(entry)}
          >
            <span class="name">{entry.name}</span>
            {#if entry.active}
              <span class="tag current">current</span>
            {:else if !entry.available}
              <span class="tag off">unavailable</span>
            {/if}
          </button>
        {/each}
      {/if}

      <div class="divider"></div>
      <button
        type="button"
        role="menuitem"
        class="item picker"
        onclick={() => {
          open = false;
          onpicker();
        }}
      >
        Open the workspace picker…
      </button>
    </div>
  {/if}
</div>

<svelte:window onkeydown={(e) => e.key === "Escape" && (open = false)} />

<style>
  .ws-menu {
    position: relative;
  }

  .trigger {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.25rem 0.7rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.8rem;
    cursor: pointer;
  }

  .trigger:hover {
    border-color: var(--focus);
    color: var(--text-primary);
  }

  .caret {
    font-size: 0.6rem;
    color: var(--text-muted);
  }

  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 20;
    border: 0;
    background: none;
    cursor: default;
  }

  .menu {
    position: absolute;
    z-index: 21;
    top: calc(100% + 0.35rem);
    right: 0;
    min-width: 15rem;
    max-height: 70vh;
    overflow-y: auto;
    padding: 0.35rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
    box-shadow: 0 6px 24px rgb(0 0 0 / 35%);
  }

  .menu-label {
    margin: 0.2rem 0.4rem 0.4rem;
    font-size: 0.66rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
  }

  .item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    width: 100%;
    padding: 0.4rem 0.5rem;
    border: 0;
    border-radius: var(--radius);
    background: none;
    color: var(--text-primary);
    font: inherit;
    font-size: 0.82rem;
    text-align: left;
    cursor: pointer;
  }

  .item:hover:not(:disabled) {
    background: var(--surface-panel);
  }

  .item:disabled {
    cursor: default;
  }

  .item.active {
    color: var(--accent);
  }

  .item.active:disabled {
    opacity: 1;
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tag {
    flex-shrink: 0;
    padding: 0.05rem 0.35rem;
    border-radius: 999px;
    font-size: 0.6rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    border: 1px solid var(--line-strong);
  }

  .tag.current {
    color: var(--accent);
    border-color: var(--accent);
  }

  .tag.off {
    color: var(--text-muted);
  }

  .divider {
    height: 1px;
    margin: 0.35rem 0;
    background: var(--line);
  }

  .item.picker {
    color: var(--text-secondary);
    font-size: 0.78rem;
  }

  .empty {
    margin: 0.2rem 0.4rem;
    color: var(--text-muted);
    font-size: 0.8rem;
  }
</style>
