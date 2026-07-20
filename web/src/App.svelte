<script lang="ts">
  import { getWorkspace, listDocuments, getDocument, ApiError } from "./api/client";
  import type { DocumentDetail, DocumentSummary, WorkspaceInfo } from "./api/types";
  import { buildTree } from "./map-room/tree";
  import FileTree from "./map-room/FileTree.svelte";
  import QuickOpen from "./map-room/QuickOpen.svelte";
  import MapRoomHome from "./map-room/MapRoomHome.svelte";
  import Preview from "./renderer/Preview.svelte";
  import Outline from "./components/Outline.svelte";
  import StatusBar from "./components/StatusBar.svelte";

  type Load =
    | { kind: "loading" }
    | { kind: "ready" }
    | { kind: "error"; code: string; message: string };

  let load = $state<Load>({ kind: "loading" });
  let workspace = $state<WorkspaceInfo | null>(null);
  let documents = $state<DocumentSummary[]>([]);

  let activeId = $state<string | null>(null);
  let activeDoc = $state<DocumentDetail | null>(null);
  let docError = $state<string | null>(null);
  let quickOpenVisible = $state(false);
  let expanded = $state(new SvelteSet<string>());

  // Svelte 5 needs a reactive Set; a plain Set does not trigger updates.
  import { SvelteSet } from "svelte/reactivity";

  const tree = $derived(buildTree(documents));

  async function boot(): Promise<void> {
    load = { kind: "loading" };
    try {
      const [info, docs] = await Promise.all([getWorkspace(), listDocuments()]);
      workspace = info;
      documents = docs;
      load = { kind: "ready" };
      // Expand the first level so the tree is not a wall of closed folders.
      for (const node of buildTree(docs)) {
        if (node.kind === "directory") expanded.add(node.path);
      }
    } catch (err) {
      load =
        err instanceof ApiError
          ? { kind: "error", code: err.code, message: err.message }
          : {
              kind: "error",
              code: "NETWORK_UNAVAILABLE",
              message: "The Athenaeum process is not reachable from this page.",
            };
    }
  }

  async function open(id: string): Promise<void> {
    activeId = id;
    docError = null;
    try {
      activeDoc = await getDocument(id);
    } catch (err) {
      activeDoc = null;
      docError =
        err instanceof ApiError ? `${err.code}: ${err.message}` : "The document could not be read.";
    }
  }

  function closeDocument(): void {
    activeId = null;
    activeDoc = null;
    docError = null;
  }

  function onkeydown(event: KeyboardEvent): void {
    const meta = event.metaKey || event.ctrlKey;
    // Quick open (spec 04 section 14).
    if (meta && event.key.toLowerCase() === "p" && !event.shiftKey) {
      event.preventDefault();
      quickOpenVisible = true;
    }
    if (meta && event.key.toLowerCase() === "w") {
      event.preventDefault();
      closeDocument();
    }
  }

  $effect(() => {
    void boot();
  });
</script>

<svelte:window {onkeydown} />

<div class="shell">
  <header class="command-bar">
    <div class="identity">
      <span class="product">Athenaeum</span>
      <span class="separator" aria-hidden="true">/</span>
      <span class="workspace">{workspace?.name ?? "—"}</span>
    </div>

    <button type="button" class="quick-open-trigger" onclick={() => (quickOpenVisible = true)}>
      Quick open <kbd>⌘P</kbd>
    </button>
  </header>

  <div class="body">
    <nav class="panel navigation" aria-label="Workspace navigation">
      <h2>Documents</h2>
      {#if load.kind === "ready"}
        {#if documents.length === 0}
          <p class="pending">No documents match the configured include patterns.</p>
        {:else}
          <FileTree
            nodes={tree}
            {activeId}
            onopen={open}
            {expanded}
            ontoggle={(path) => {
              if (expanded.has(path)) expanded.delete(path);
              else expanded.add(path);
            }}
          />
        {/if}
      {:else}
        <p class="pending">Loading…</p>
      {/if}
    </nav>

    <main class="document-surface" aria-label="Document surface">
      {#if load.kind === "error"}
        <section class="card error">
          <h1>Workspace unavailable</h1>
          <p class="code">{load.code}</p>
          <p>{load.message}</p>
          <button type="button" onclick={boot}>Retry</button>
        </section>
      {:else if docError}
        <section class="card error">
          <h1>Document unavailable</h1>
          <p class="code">{docError}</p>
          <button type="button" onclick={closeDocument}>Back to the Map Room</button>
        </section>
      {:else if activeDoc && workspace}
        <div class="document">
          <header class="document-header">
            <div>
              <h1>{activeDoc.title}</h1>
              <p class="path">{activeDoc.id}</p>
            </div>
            <div class="states">
              <span class="state" class:readonly={activeDoc.read_only}>
                {activeDoc.read_only ? "Read-only" : "Saved"}
              </span>
              <span class="state muted">{activeDoc.line_ending.toUpperCase()}</span>
              {#if activeDoc.encoding !== "utf-8"}
                <span class="state warn">{activeDoc.encoding}</span>
              {/if}
            </div>
          </header>

          {#if activeDoc.warnings && activeDoc.warnings.length > 0}
            <aside class="doc-warnings" role="status">
              {#each activeDoc.warnings as warning}
                <p>{warning}</p>
              {/each}
            </aside>
          {/if}

          <Preview document={activeDoc} capabilities={workspace.capabilities} />
        </div>
      {:else if workspace}
        <MapRoomHome {workspace} {documents} onopen={open} />
      {/if}
    </main>

    <aside class="panel context" aria-label="Context panel">
      <h2>Outline</h2>
      {#if activeDoc}
        <Outline outline={activeDoc.outline} />
      {:else}
        <p class="pending">Open a document to see its outline.</p>
      {/if}
    </aside>
  </div>

  <StatusBar {workspace} document={activeDoc} state={load.kind} />
</div>

{#if quickOpenVisible}
  <QuickOpen {documents} onopen={open} onclose={() => (quickOpenVisible = false)} />
{/if}

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
  }

  .quick-open-trigger {
    padding: 0.25rem 0.7rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.8rem;
    cursor: pointer;
  }

  .quick-open-trigger:hover {
    border-color: var(--focus);
    color: var(--text-primary);
  }

  kbd {
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: var(--text-muted);
  }

  .body {
    display: grid;
    grid-template-columns: 17rem 1fr 16rem;
    min-height: 0;
  }

  .panel {
    padding: 1rem 0.5rem;
    background: var(--surface-panel);
    overflow-y: auto;
  }

  .navigation {
    border-right: 1px solid var(--line);
  }

  .context {
    border-left: 1px solid var(--line);
    padding: 1rem;
  }

  .panel h2 {
    margin: 0 0 0.5rem 0.5rem;
    font-size: 0.7rem;
    font-weight: 600;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--text-secondary);
  }

  .pending {
    margin: 0 0.5rem;
    color: var(--text-muted);
    font-size: 0.85rem;
  }

  .document-surface {
    min-width: 0;
    overflow-y: auto;
    background-color: var(--surface-table);
    background-image:
      linear-gradient(var(--line) 1px, transparent 1px),
      linear-gradient(90deg, var(--line) 1px, transparent 1px);
    background-size: 48px 48px;
    background-position: -1px -1px;
  }

  .document {
    max-width: 58rem;
    margin: 0 auto;
    padding: 1.5rem;
  }

  .document-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 1rem;
    margin-bottom: 1rem;
  }

  .document-header h1 {
    margin: 0;
    font-size: 1.15rem;
    font-weight: 600;
  }

  .path {
    margin: 0.15rem 0 0;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: var(--text-muted);
  }

  .states {
    display: flex;
    gap: 0.4rem;
    flex-shrink: 0;
  }

  .state {
    padding: 0.1rem 0.45rem;
    border: 1px solid var(--ok);
    border-radius: 2px;
    color: var(--ok);
    font-family: var(--font-mono);
    font-size: 0.7rem;
  }

  .state.readonly,
  .state.muted {
    border-color: var(--line-strong);
    color: var(--text-muted);
  }

  .state.warn {
    border-color: var(--warn);
    color: var(--warn);
  }

  .doc-warnings {
    margin-bottom: 1rem;
    padding: 0.6rem 0.9rem;
    border: 1px solid var(--warn);
    border-radius: var(--radius);
    font-size: 0.85rem;
    color: var(--warn);
  }

  .doc-warnings p {
    margin: 0;
  }

  .card {
    max-width: 40rem;
    margin: 2rem;
    padding: 1.5rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
  }

  .card.error {
    border-color: var(--danger);
  }

  .card h1 {
    margin: 0 0 0.5rem;
    font-size: 1.3rem;
  }

  .code {
    font-family: var(--font-mono);
    font-size: 0.8rem;
    color: var(--danger);
  }

  button {
    padding: 0.4rem 0.9rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-primary);
    font: inherit;
    cursor: pointer;
  }

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
