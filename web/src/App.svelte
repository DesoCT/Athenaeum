<script lang="ts">
  import { getHealth, ApiError } from "./api/client";
  import type { Health } from "./api/types";
  import MapRoom from "./map-room/MapRoom.svelte";
  import StatusBar from "./components/StatusBar.svelte";

  type Connection =
    | { kind: "loading" }
    | { kind: "ready"; health: Health }
    | { kind: "error"; code: string; message: string };

  let connection = $state<Connection>({ kind: "loading" });

  async function load(): Promise<void> {
    connection = { kind: "loading" };
    try {
      connection = { kind: "ready", health: await getHealth() };
    } catch (err) {
      if (err instanceof ApiError) {
        connection = { kind: "error", code: err.code, message: err.message };
      } else {
        connection = {
          kind: "error",
          code: "NETWORK_UNAVAILABLE",
          message: "The Athenaeum process is not reachable from this page.",
        };
      }
    }
  }

  $effect(() => {
    void load();
  });

  const workspaceName = $derived(
    connection.kind === "ready" ? connection.health.workspace : "—",
  );
</script>

<div class="shell">
  <header class="command-bar">
    <div class="identity">
      <span class="product">Athenaeum</span>
      <span class="separator" aria-hidden="true">/</span>
      <span class="workspace">{workspaceName}</span>
    </div>
    {#if connection.kind === "ready" && connection.health.remote}
      <p class="remote-warning" role="status">
        Remote mode — this workspace is reachable beyond this machine.
      </p>
    {/if}
  </header>

  <div class="body">
    <nav class="panel navigation" aria-label="Workspace navigation">
      <h2>Navigation</h2>
      <p class="pending">
        The file tree, search, and notes arrive with the workspace loader in
        Phase&nbsp;1.
      </p>
    </nav>

    <main class="document-surface" aria-label="Document surface">
      <MapRoom {connection} onretry={load} />
    </main>

    <aside class="panel context" aria-label="Context panel">
      <h2>Context</h2>
      <p class="pending">
        Outline, annotations, links, and Git context arrive in Phases&nbsp;4
        and&nbsp;5.
      </p>
    </aside>
  </div>

  <StatusBar {connection} />
</div>

<style>
  .shell {
    display: grid;
    grid-template-rows: auto 1fr auto;
    height: 100%;
  }

  .command-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--gap);
    padding: 0.6rem 1rem;
    border-bottom: 1px solid var(--line);
    background: var(--surface-panel);
  }

  .identity {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
  }

  .product {
    font-weight: 600;
    letter-spacing: 0.04em;
    color: var(--accent);
  }

  .separator {
    color: var(--text-muted);
  }

  .workspace {
    font-family: var(--font-mono);
    color: var(--text-primary);
  }

  .remote-warning {
    margin: 0;
    padding: 0.2rem 0.6rem;
    border: 1px solid var(--warn);
    border-radius: var(--radius);
    color: var(--warn);
    font-size: 0.85rem;
  }

  .body {
    display: grid;
    grid-template-columns: 16rem 1fr 18rem;
    min-height: 0;
  }

  .panel {
    padding: 1rem;
    background: var(--surface-panel);
    overflow-y: auto;
  }

  .navigation {
    border-right: 1px solid var(--line);
  }

  .context {
    border-left: 1px solid var(--line);
  }

  .panel h2 {
    margin: 0 0 0.5rem;
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--text-secondary);
  }

  .pending {
    margin: 0;
    color: var(--text-muted);
    font-size: 0.85rem;
  }

  .document-surface {
    min-width: 0;
    overflow-y: auto;
    /* Restrained coordinate cues on the plotting table (spec 04 section 1). */
    background-color: var(--surface-table);
    background-image:
      linear-gradient(var(--line) 1px, transparent 1px),
      linear-gradient(90deg, var(--line) 1px, transparent 1px);
    background-size: 48px 48px;
    background-position: -1px -1px;
  }

  /* Below 900px one side panel is visible at a time (spec 04 section 13). */
  @media (max-width: 900px) {
    .body {
      grid-template-columns: 1fr;
    }

    .navigation,
    .context {
      display: none;
    }
  }
</style>
