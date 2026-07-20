<script lang="ts">
  import type { DocumentDetail, WorkspaceInfo } from "../api/types";

  interface Props {
    workspace: WorkspaceInfo | null;
    document: DocumentDetail | null;
    state: "loading" | "ready" | "error";
  }

  let { workspace, document: doc, state }: Props = $props();

  // Status is conveyed by text as well as colour (spec 04 section 15).
  const label = $derived(
    state === "ready" ? "Connected" : state === "loading" ? "Connecting" : "Disconnected",
  );
  const tone = $derived(state === "ready" ? "ok" : state === "loading" ? "pending" : "danger");
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
</style>
