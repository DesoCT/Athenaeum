<script lang="ts">
  import type { Health } from "../api/types";

  type Connection =
    | { kind: "loading" }
    | { kind: "ready"; health: Health }
    | { kind: "error"; code: string; message: string };

  interface Props {
    connection: Connection;
    onretry: () => void;
  }

  let { connection, onretry }: Props = $props();
</script>

<div class="map-room">
  {#if connection.kind === "loading"}
    <p class="status" role="status">Connecting to the Athenaeum process…</p>
  {:else if connection.kind === "error"}
    <section class="card error" aria-labelledby="error-heading">
      <h1 id="error-heading">Workspace unavailable</h1>
      <p class="code">{connection.code}</p>
      <p>{connection.message}</p>
      <button type="button" onclick={onretry}>Retry</button>
    </section>
  {:else}
    <section class="card" aria-labelledby="workspace-heading">
      <h1 id="workspace-heading">{connection.health.workspace}</h1>
      <p class="lede">
        The Map Room is the workspace surface. Phase&nbsp;0 establishes the
        runtime: a single executable, an embedded frontend, and an
        authenticated loopback session.
      </p>

      <dl class="facts">
        <div>
          <dt>Version</dt>
          <dd>{connection.health.version}</dd>
        </div>
        <div>
          <dt>Session</dt>
          <dd>Authenticated</dd>
        </div>
        <div>
          <dt>Binding</dt>
          <dd>{connection.health.remote ? "Remote" : "Loopback"}</dd>
        </div>
        <div>
          <dt>Frontend</dt>
          <dd>{connection.health.frontend}</dd>
        </div>
      </dl>
    </section>

    <section class="card next" aria-labelledby="next-heading">
      <h2 id="next-heading">Not yet built</h2>
      <p>
        Pinned and recent documents, document groups, changed files, and
        unresolved annotations appear here once the workspace loader lands. They
        are listed as absent rather than shown as empty so the Map Room never
        implies data it does not have.
      </p>
      <ul>
        <li><strong>Phase 1</strong> — workspace loading, file tree, quick open, rendering</li>
        <li><strong>Phase 2</strong> — editing, atomic saves, conflicts, recovery</li>
        <li><strong>Phase 3</strong> — search index and session restoration</li>
      </ul>
    </section>
  {/if}
</div>

<style>
  .map-room {
    display: flex;
    flex-direction: column;
    gap: var(--gap);
    padding: 2rem;
    max-width: 56rem;
  }

  .status {
    color: var(--text-secondary);
  }

  .card {
    padding: 1.5rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
  }

  .card.error {
    border-color: var(--danger);
  }

  h1 {
    margin: 0 0 0.5rem;
    font-size: 1.5rem;
    font-weight: 600;
  }

  h2 {
    margin: 0 0 0.5rem;
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--text-secondary);
  }

  .lede {
    margin: 0 0 1.25rem;
    color: var(--text-secondary);
  }

  .code {
    margin: 0 0 0.5rem;
    font-family: var(--font-mono);
    font-size: 0.85rem;
    color: var(--danger);
  }

  .facts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(9rem, 1fr));
    gap: 1rem;
    margin: 0;
  }

  .facts div {
    border-left: 2px solid var(--accent);
    padding-left: 0.75rem;
  }

  dt {
    font-size: 0.7rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
  }

  dd {
    margin: 0.15rem 0 0;
    font-family: var(--font-mono);
    font-size: 0.95rem;
  }

  .next p {
    margin: 0 0 0.75rem;
    color: var(--text-secondary);
  }

  .next ul {
    margin: 0;
    padding-left: 1.1rem;
    color: var(--text-muted);
  }

  .next li {
    margin-bottom: 0.25rem;
  }

  button {
    margin-top: 0.75rem;
    padding: 0.4rem 0.9rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-primary);
    font: inherit;
    cursor: pointer;
  }

  button:hover {
    border-color: var(--focus);
  }
</style>
