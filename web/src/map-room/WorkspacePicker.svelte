<script lang="ts">
  import type { WorkspaceRegistry, WorkspaceEntry } from "../api/types";

  interface Props {
    registry: WorkspaceRegistry;
    /** Set while a switch is in flight, so the surface can disable itself. */
    busy?: boolean;
    /** A failed open, shown against the whole picker rather than one entry. */
    error?: { code: string; message: string; remedy?: string } | null;
    onopen: (name: string) => void;
    onreload: () => void;
  }

  let { registry, busy = false, error = null, onopen, onreload }: Props = $props();

  // Available entries first, so the common case is at the top; unavailable ones
  // stay visible below with their reason rather than being hidden (R1).
  const ordered = $derived(
    [...registry.entries].sort((a, b) => Number(b.available) - Number(a.available)),
  );

  const availableCount = $derived(registry.entries.filter((e) => e.available).length);

  function activate(entry: WorkspaceEntry): void {
    if (busy || !entry.available) return;
    onopen(entry.name);
  }
</script>

<div class="picker">
  <section class="intro">
    <h1>Choose a workspace</h1>
    <p class="lede">
      A session opens exactly one workspace. Opening one is the same as launching
      Athenaeum with its path — one root, one index, one write boundary.
    </p>
    <p class="registry-path">
      Registry: <code>{registry.registry_path}</code>
      <button type="button" class="link" onclick={onreload} disabled={busy}>Reload</button>
    </p>
  </section>

  {#if error}
    <div class="alert" role="alert">
      <p class="code">{error.code}</p>
      <p class="message">{error.message}</p>
      {#if error.remedy}<p class="remedy">{error.remedy}</p>{/if}
    </div>
  {/if}

  {#if !registry.present}
    <section class="empty card">
      <h2>No registry yet</h2>
      <p>
        Create <code>{registry.registry_path}</code> to list the workspaces you
        move between. Athenaeum only reads this file; you edit it by hand.
      </p>
      <pre>{`[[workspace]]
name = "Athenaeum"
path = "~/dev/athenaeum"`}</pre>
    </section>
  {:else if registry.entries.length === 0}
    <section class="empty card">
      <h2>No workspaces registered</h2>
      <p>
        Add a <code>[[workspace]]</code> table with a name and a path to
        <code>{registry.registry_path}</code>.
      </p>
    </section>
  {:else}
    <ul class="entries">
      {#each ordered as entry (entry.name + entry.path)}
        <li>
          <button
            type="button"
            class="entry"
            class:unavailable={!entry.available}
            class:active={entry.active}
            disabled={busy || !entry.available}
            aria-disabled={!entry.available}
            onclick={() => activate(entry)}
          >
            <span class="head">
              <span class="name">{entry.name}</span>
              {#if entry.active}<span class="badge">open</span>{/if}
              {#if !entry.available}<span class="badge warn">{entry.code}</span>{/if}
            </span>
            <span class="path">{entry.path}</span>
            {#if !entry.available && entry.reason}
              <span class="reason">{entry.reason}</span>
              {#if entry.remedy}<span class="remedy">{entry.remedy}</span>{/if}
            {/if}
          </button>
        </li>
      {/each}
    </ul>

    {#if availableCount === 0}
      <p class="none-available">
        None of the registered workspaces can be opened right now. Each is shown
        above with the reason it is unavailable.
      </p>
    {/if}
  {/if}

  {#if registry.diagnostics && registry.diagnostics.length > 0}
    <section class="card diagnostics" aria-labelledby="reg-diag">
      <h2 id="reg-diag">Registry warnings</h2>
      {#each registry.diagnostics as d}
        <div class="diagnostic">
          <p class="field">{d.field}</p>
          <p class="message">{d.message}</p>
          {#if d.remedy}<p class="remedy">{d.remedy}</p>{/if}
        </div>
      {/each}
    </section>
  {/if}
</div>

<style>
  .picker {
    display: flex;
    flex-direction: column;
    gap: var(--gap);
    padding: 2.5rem;
    max-width: 48rem;
    margin: 0 auto;
  }

  h1 {
    margin: 0 0 0.4rem;
    font-size: 1.6rem;
    font-weight: 600;
  }

  .lede {
    margin: 0 0 0.75rem;
    color: var(--text-secondary);
    font-size: 0.95rem;
    max-width: 36rem;
  }

  .registry-path {
    margin: 0;
    font-size: 0.8rem;
    color: var(--text-muted);
  }

  .registry-path code {
    font-family: var(--font-mono);
  }

  .link {
    margin-left: 0.5rem;
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font: inherit;
    font-size: 0.8rem;
    cursor: pointer;
    text-decoration: underline;
  }

  .link:disabled {
    color: var(--text-muted);
    cursor: default;
  }

  .entries {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .entry {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    width: 100%;
    padding: 0.9rem 1rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .entry:hover:not(:disabled) {
    border-color: var(--focus);
  }

  .entry:disabled {
    cursor: default;
  }

  .entry.unavailable {
    background: var(--surface-panel);
    opacity: 0.85;
  }

  .entry.active {
    border-color: var(--accent);
  }

  .head {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .name {
    font-size: 1rem;
    font-weight: 600;
  }

  .badge {
    padding: 0.05rem 0.4rem;
    border-radius: 999px;
    background: var(--accent);
    color: var(--surface-table);
    font-size: 0.62rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .badge.warn {
    background: none;
    border: 1px solid var(--warn);
    color: var(--warn);
    font-family: var(--font-mono);
    letter-spacing: 0;
    text-transform: none;
  }

  .path {
    font-family: var(--font-mono);
    font-size: 0.74rem;
    color: var(--text-muted);
  }

  .reason {
    margin-top: 0.3rem;
    font-size: 0.82rem;
    color: var(--text-secondary);
  }

  .remedy {
    font-size: 0.8rem;
    color: var(--text-muted);
  }

  .card {
    padding: 1.5rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
  }

  .empty h2,
  .diagnostics h2 {
    margin: 0 0 0.5rem;
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--text-secondary);
  }

  .empty p {
    margin: 0 0 0.75rem;
    color: var(--text-secondary);
    font-size: 0.9rem;
  }

  .empty pre {
    margin: 0;
    padding: 0.75rem 1rem;
    border-radius: var(--radius);
    background: var(--surface-panel);
    font-family: var(--font-mono);
    font-size: 0.8rem;
    overflow-x: auto;
  }

  code {
    font-family: var(--font-mono);
  }

  .alert {
    padding: 0.75rem 1rem;
    border: 1px solid var(--danger);
    border-radius: var(--radius);
    background: var(--surface-raised);
  }

  .alert .code {
    margin: 0;
    font-family: var(--font-mono);
    font-size: 0.78rem;
    color: var(--danger);
  }

  .alert .message {
    margin: 0.2rem 0 0;
    font-size: 0.9rem;
  }

  .none-available {
    color: var(--text-muted);
    font-size: 0.85rem;
  }

  .diagnostic {
    padding: 0.4rem 0.7rem;
    border-left: 2px solid var(--warn);
    margin-bottom: 0.5rem;
  }

  .field {
    margin: 0;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: var(--accent);
  }

  .message {
    margin: 0.15rem 0 0;
    font-size: 0.85rem;
  }
</style>
