<script lang="ts">
  import type { Health } from "../api/types";

  type Connection =
    | { kind: "loading" }
    | { kind: "ready"; health: Health }
    | { kind: "error"; code: string; message: string };

  interface Props {
    connection: Connection;
  }

  let { connection }: Props = $props();

  // Status is conveyed by text as well as colour (spec 04 section 15).
  const label = $derived(
    connection.kind === "ready"
      ? "Connected"
      : connection.kind === "loading"
        ? "Connecting"
        : "Disconnected",
  );

  const tone = $derived(
    connection.kind === "ready"
      ? "ok"
      : connection.kind === "loading"
        ? "pending"
        : "danger",
  );
</script>

<footer class="status-bar">
  <span class="indicator {tone}">
    <span class="dot" aria-hidden="true"></span>
    {label}
  </span>

  {#if connection.kind === "ready"}
    <span class="field">athenaeum {connection.health.version}</span>
    <span class="field">
      {connection.health.remote ? "remote" : "loopback"}
    </span>
  {:else if connection.kind === "error"}
    <span class="field">{connection.code}</span>
  {/if}

  <span class="spacer"></span>
  <span class="field muted">Index: not built (Phase 3)</span>
  <span class="field muted">Git: not built (Phase 5)</span>
</footer>

<style>
  .status-bar {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0.35rem 1rem;
    border-top: 1px solid var(--line);
    background: var(--surface-panel);
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: var(--text-secondary);
  }

  .indicator {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
  }

  .dot {
    width: 0.5rem;
    height: 0.5rem;
    border-radius: 50%;
    background: currentColor;
  }

  .indicator.ok {
    color: var(--ok);
  }

  .indicator.pending {
    color: var(--text-muted);
  }

  .indicator.danger {
    color: var(--danger);
  }

  .spacer {
    flex: 1;
  }

  .muted {
    color: var(--text-muted);
  }
</style>
