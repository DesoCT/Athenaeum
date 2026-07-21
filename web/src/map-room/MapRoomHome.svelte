<script lang="ts">
  import type { DocumentSummary, WorkspaceInfo } from "../api/types";
  import type { AnnotationOverview } from "../annotations/types";
  import type { GitFile } from "../git/types";
  import { getAnnotationOverview, getGitStatus, ApiError } from "../api/client";

  interface Props {
    workspace: WorkspaceInfo;
    documents: DocumentSummary[];
    onopen: (id: string, line?: number) => void;
    /** Recently opened document IDs, most recent first (R2, R13). */
    recent?: string[];
    /** Bumped by the shell to refresh the annotation summary. */
    generation?: number;
  }

  let { workspace, documents, onopen, recent = [], generation = 0 }: Props = $props();

  let overview = $state<AnnotationOverview | null>(null);

  // The pin and unresolved summaries (spec 04 section 3). A failure leaves them
  // absent rather than showing an error on the home screen; they are context,
  // never a prerequisite for the Map Room.
  $effect(() => {
    void generation;
    getAnnotationOverview()
      .then((o) => (overview = o))
      .catch((err) => {
        if (!(err instanceof ApiError)) return;
        overview = null;
      });
  });

  const pins = $derived(overview?.pins ?? []);
  const unresolved = $derived(overview?.unresolved ?? []);

  // Changed files from Git status (spec 04 section 3). Absent when Git is off,
  // never an error on the home.
  let changed = $state<GitFile[]>([]);
  $effect(() => {
    void generation;
    if (!workspace.capabilities.git) {
      changed = [];
      return;
    }
    getGitStatus()
      .then((s) => (changed = s.files.filter((f) => f.state !== "clean")))
      .catch(() => (changed = []));
  });

  function titleOf(id: string): string {
    return documents.find((d) => d.id === id)?.title ?? id;
  }

  function summarise(body: string): string {
    const trimmed = body.trim().replace(/\s+/g, " ");
    return trimmed.length > 80 ? `${trimmed.slice(0, 77)}…` : trimmed;
  }

  // Recents are resolved against the live document list, so an entry for a
  // document that has since been removed or excluded simply does not appear.
  const recentDocuments = $derived(
    recent
      .map((id) => documents.find((d) => d.id === id))
      .filter((d): d is DocumentSummary => d != null)
      .slice(0, 8),
  );

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

  {#if pins.length > 0}
    <section class="card" aria-labelledby="pins-heading">
      <h2 id="pins-heading">Pinned</h2>
      <ul class="documents">
        {#each pins as pin (pin.id)}
          <li>
            <button type="button" onclick={() => onopen(pin.document_id, pin.line)}>
              <span class="title">{pin.body ? summarise(pin.body) : titleOf(pin.document_id)}</span>
              <span class="path">{pin.document_id}</span>
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if unresolved.length > 0}
    <section class="card" aria-labelledby="unresolved-heading">
      <h2 id="unresolved-heading">Unresolved ({unresolved.length})</h2>
      <ul class="documents">
        {#each unresolved as item (item.id)}
          <li>
            <button type="button" onclick={() => onopen(item.document_id, item.line)}>
              <span class="title">{summarise(item.body)}</span>
              <span class="path">{titleOf(item.document_id)}</span>
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if changed.length > 0}
    <section class="card" aria-labelledby="changed-heading">
      <h2 id="changed-heading">Changed</h2>
      <ul class="documents">
        {#each changed as file (file.document_id)}
          <li>
            <button type="button" onclick={() => onopen(file.document_id)}>
              <span class="title">{titleOf(file.document_id)}</span>
              <span class="git-state {file.state}">{file.state}</span>
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if recentDocuments.length > 0}
    <section class="card" aria-labelledby="recent-heading">
      <h2 id="recent-heading">Recent</h2>
      <ul class="documents">
        {#each recentDocuments as doc (doc.id)}
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

  .git-state {
    font-family: var(--font-mono);
    font-size: 0.68rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-muted);
  }

  .git-state.modified {
    color: var(--warn);
  }

  .git-state.untracked {
    color: var(--accent);
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
</style>
