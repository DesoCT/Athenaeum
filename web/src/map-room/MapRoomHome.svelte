<script lang="ts">
  import type { DocumentSummary, WorkspaceInfo } from "../api/types";

  interface Props {
    workspace: WorkspaceInfo;
    documents: DocumentSummary[];
    onopen: (id: string) => void;
  }

  let { workspace, documents, onopen }: Props = $props();

  const errors = $derived((workspace.diagnostics ?? []).filter((d) => d.severity === "error"));
  const warnings = $derived((workspace.diagnostics ?? []).filter((d) => d.severity === "warning"));

  function inGroup(id: string): DocumentSummary[] {
    return documents.filter((d) => d.groups?.includes(id));
  }
</script>

<div class="home">
  <section class="card">
    <h1>{workspace.name}</h1>
    <p class="root">{workspace.root}</p>
    <dl class="facts">
      <div>
        <dt>Documents</dt>
        <dd>{workspace.document_count}</dd>
      </div>
      <div>
        <dt>Groups</dt>
        <dd>{workspace.groups.length}</dd>
      </div>
      <div>
        <dt>Git</dt>
        <dd>{workspace.capabilities.git ? "enabled" : "off"}</dd>
      </div>
      <div>
        <dt>Search</dt>
        <dd>{workspace.capabilities.search ? "enabled" : "off"}</dd>
      </div>
    </dl>
  </section>

  {#if errors.length > 0 || warnings.length > 0}
    <section class="card diagnostics" aria-labelledby="diagnostics-heading">
      <h2 id="diagnostics-heading">Configuration</h2>
      {#each [...errors, ...warnings] as d}
        <div class="diagnostic" class:error={d.severity === "error"}>
          <p class="field">{d.field}</p>
          <p class="message">{d.message}</p>
          {#if d.remedy}<p class="remedy">{d.remedy}</p>{/if}
        </div>
      {/each}
    </section>
  {/if}

  {#each workspace.groups as group (group.id)}
    {@const members = inGroup(group.id)}
    {#if members.length > 0}
      <section class="card">
        <h2>{group.title}</h2>
        <ul class="documents">
          {#each members as doc (doc.id)}
            <li>
              <button type="button" onclick={() => onopen(doc.id)}>
                <span class="title">{doc.title}</span>
                <span class="path">{doc.id}</span>
              </button>
            </li>
          {/each}
        </ul>
      </section>
    {/if}
  {/each}

  <section class="card next">
    <h2>Not yet built</h2>
    <p>
      Pinned and recent documents, changed files, and unresolved annotations
      appear here as later phases land. They are listed as absent rather than
      shown as empty, so the Map Room never implies data it does not have.
    </p>
    <ul>
      <li><strong>Phase 2</strong> — editing, atomic saves, conflicts, recovery</li>
      <li><strong>Phase 3</strong> — search index, session restoration</li>
      <li><strong>Phase 4</strong> — annotations, notes, backlinks</li>
      <li><strong>Phase 5</strong> — read-only Git context</li>
    </ul>
  </section>
</div>

<style>
  .home {
    display: flex;
    flex-direction: column;
    gap: var(--gap);
    padding: 2rem;
    max-width: 56rem;
  }

  .card {
    padding: 1.5rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
  }

  h1 {
    margin: 0 0 0.25rem;
    font-size: 1.5rem;
    font-weight: 600;
  }

  h2 {
    margin: 0 0 0.75rem;
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--text-secondary);
  }

  .root {
    margin: 0 0 1.25rem;
    font-family: var(--font-mono);
    font-size: 0.78rem;
    color: var(--text-muted);
  }

  .facts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr));
    gap: 1rem;
    margin: 0;
  }

  .facts div {
    border-left: 2px solid var(--accent);
    padding-left: 0.75rem;
  }

  dt {
    font-size: 0.68rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
  }

  dd {
    margin: 0.15rem 0 0;
    font-family: var(--font-mono);
    font-size: 0.95rem;
  }

  .documents {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .documents button {
    display: flex;
    justify-content: space-between;
    gap: 1rem;
    width: 100%;
    padding: 0.3rem 0.4rem;
    border: 0;
    border-radius: 2px;
    background: none;
    color: var(--text-primary);
    font: inherit;
    font-size: 0.85rem;
    text-align: left;
    cursor: pointer;
  }

  .documents button:hover {
    background: var(--surface-panel);
  }

  .documents .path {
    font-family: var(--font-mono);
    font-size: 0.72rem;
    color: var(--text-muted);
  }

  .diagnostic {
    padding: 0.5rem 0.75rem;
    border-left: 2px solid var(--warn);
    margin-bottom: 0.6rem;
  }

  .diagnostic.error {
    border-left-color: var(--danger);
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
    color: var(--text-primary);
  }

  .remedy {
    margin: 0.15rem 0 0;
    font-size: 0.8rem;
    color: var(--text-muted);
  }

  .next p {
    margin: 0 0 0.75rem;
    color: var(--text-secondary);
    font-size: 0.9rem;
  }

  .next ul {
    margin: 0;
    padding-left: 1.1rem;
    color: var(--text-muted);
    font-size: 0.85rem;
  }
</style>
