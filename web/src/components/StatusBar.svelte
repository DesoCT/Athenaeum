<script lang="ts">
  import type { DocumentDetail, IndexStatus, WorkspaceInfo } from "../api/types";

  interface Props {
    workspace: WorkspaceInfo | null;
    document: DocumentDetail | null;
    state: "loading" | "ready" | "error";
    /** Search index status (spec 04 sections 2 and 8). */
    index?: IndexStatus | null;
  }

  let { workspace, document: doc, state, index = null }: Props = $props();

  // Status is conveyed by text as well as colour (spec 04 section 15).
  const label = $derived(
    state === "ready" ? "Connected" : state === "loading" ? "Connecting" : "Disconnected",
  );
  const tone = $derived(state === "ready" ? "ok" : state === "loading" ? "pending" : "danger");

  /**
   * The index label states its condition in words. "Rebuilding" and "stale" are
   * named explicitly rather than implied by a spinner, so a user always knows
   * whether search results are complete (spec 04 section 8, constitution C8).
   */
  const indexLabel = $derived.by(() => {
    if (!index) return "Index: checking";
    switch (index.state) {
      case "disabled":
        return "Index: disabled";
      case "unavailable":
        return `Index: unavailable (${index.error ?? "unknown"})`;
      case "building":
        return `Index: building ${index.indexed}/${index.total}`;
      case "rebuilding":
        return `Index: rebuilding (${index.pending} queued, results stale)`;
      default:
        return `Index: ready (${index.indexed})`;
    }
  });

  const indexTone = $derived.by(() => {
    if (!index) return "muted";
    if (index.state === "unavailable") return "danger";
    if (index.state === "building" || index.state === "rebuilding") return "warn";
    if (index.state === "disabled") return "muted";
    return "ok";
  });
</script>

<footer class="status-bar">
  <span class="indicator {tone}">
    <span class="dot" aria-hidden="true"></span>
    {label}
  </span>

  {#if doc}
    <span class="field">{doc.id}</span>
    <span class="field">{doc.outline.length} heading{doc.outline.length === 1 ? "" : "s"}</span>
    <span class="field">{(doc.size / 1024).toFixed(1)} KB</span>
    {#if !doc.writable}
      <span class="field muted">read-only</span>
    {/if}
  {:else if workspace}
    <span class="field">{workspace.document_count} documents</span>
  {/if}

  <span class="spacer"></span>
  <span class="field {indexTone}" role="status">{indexLabel}</span>
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
    font-size: 0.72rem;
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

  .indicator.ok { color: var(--ok); }
  .indicator.pending { color: var(--text-muted); }
  .indicator.danger { color: var(--danger); }

  .spacer {
    flex: 1;
  }

  .muted {
    color: var(--text-muted);
  }

  .field.ok { color: var(--ok); }
  .field.warn { color: var(--warn); }
  .field.danger { color: var(--danger); }
</style>
