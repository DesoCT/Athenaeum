<script lang="ts">
  /**
   * Workspace search (R7, spec 04 section 8).
   *
   * The panel shows a query field, path/group/Git-state filters, a result list
   * carrying document, heading, snippet, and location, and the index status
   * including its rebuilding and stale states.
   */
  import { searchWorkspace, rebuildIndex, ApiError } from "../api/client";
  import type {
    Group,
    IndexStatus,
    SearchFilters,
    SearchResult,
  } from "../api/types";

  interface Props {
    groups: Group[];
    status: IndexStatus | null;
    /** Opens a result at its matched line. */
    onopen: (documentId: string, line?: number) => void;
    /** Asks the shell to refresh the index status after a rebuild. */
    onstatuschange: (status: IndexStatus) => void;
  }

  let { groups, status, onopen, onstatuschange }: Props = $props();

  let query = $state("");
  let path = $state("");
  let group = $state("");
  let git = $state("");
  let results = $state<SearchResult[]>([]);
  let truncated = $state(false);
  let selected = $state(0);
  let searching = $state(false);
  let problem = $state<string | null>(null);
  let field: HTMLInputElement | null = $state(null);

  /**
   * Searching is debounced and the previous request is aborted.
   *
   * Without the abort, a slow query for "d" can land after a fast query for
   * "design" and replace correct results with stale ones.
   */
  const DEBOUNCE_MS = 150;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let inFlight: AbortController | null = null;

  function filters(): SearchFilters {
    const active: SearchFilters = {};
    if (path.trim()) active.path = path.trim();
    if (group) active.group = group;
    if (git) active.git = git as SearchFilters["git"];
    return active;
  }

  async function run(): Promise<void> {
    const text = query.trim();
    if (!text) {
      results = [];
      truncated = false;
      problem = null;
      searching = false;
      return;
    }

    inFlight?.abort();
    const controller = new AbortController();
    inFlight = controller;
    searching = true;

    try {
      const response = await searchWorkspace(text, filters(), 25, controller.signal);
      if (controller.signal.aborted) return;
      results = response.results;
      truncated = response.truncated;
      selected = 0;
      problem = null;
      onstatuschange(response.status);
    } catch (err) {
      if (controller.signal.aborted || (err instanceof DOMException && err.name === "AbortError")) {
        return;
      }
      results = [];
      truncated = false;
      // The server distinguishes "your query has no words" from "the index is
      // unavailable", and the user needs to know which (requirement N6).
      problem = err instanceof ApiError ? err.message : "The search could not be completed.";
    } finally {
      if (inFlight === controller) {
        inFlight = null;
        searching = false;
      }
    }
  }

  function schedule(): void {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => void run(), DEBOUNCE_MS);
  }

  // Re-run whenever the query or any filter changes.
  $effect(() => {
    // Referenced so the effect re-runs on each of them.
    void query;
    void path;
    void group;
    void git;
    schedule();
    return () => {
      if (timer) clearTimeout(timer);
    };
  });

  $effect(() => {
    field?.focus();
  });

  function openResult(result: SearchResult): void {
    onopen(result.document_id, result.line);
  }

  /** Keyboard traversal of the result list (spec 04 section 8, N5). */
  function onkeydown(event: KeyboardEvent): void {
    if (results.length === 0) return;
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        selected = (selected + 1) % results.length;
        break;
      case "ArrowUp":
        event.preventDefault();
        selected = (selected - 1 + results.length) % results.length;
        break;
      case "Enter":
        event.preventDefault();
        if (results[selected]) openResult(results[selected]);
        break;
    }
  }

  async function rebuild(): Promise<void> {
    try {
      onstatuschange(await rebuildIndex());
    } catch {
      // The status poll will report the real state shortly.
    }
  }

  const indexLabel = $derived.by(() => {
    if (!status) return "Index: checking";
    switch (status.state) {
      case "disabled":
        return "Index: disabled for this workspace";
      case "unavailable":
        return `Index: unavailable (${status.error ?? "unknown"})`;
      case "building":
        return `Index: building ${status.indexed} of ${status.total}`;
      case "rebuilding":
        return `Index: rebuilding, ${status.pending} document${status.pending === 1 ? "" : "s"} queued`;
      default:
        return `Index: ready, ${status.indexed} document${status.indexed === 1 ? "" : "s"}`;
    }
  });

  /**
   * Results are stale while documents are queued. Saying so is the honest
   * alternative to showing possibly-incomplete results as if they were
   * complete (constitution C8).
   */
  const stale = $derived(
    status != null && (status.state === "building" || status.state === "rebuilding"),
  );
  const searchable = $derived(
    status == null || (status.state !== "disabled" && status.state !== "unavailable"),
  );

  function location(result: SearchResult): string {
    const heading = result.heading_path?.join(" › ");
    if (heading && result.line) return `${heading} · line ${result.line}`;
    if (heading) return heading;
    if (result.line) return `line ${result.line}`;
    return result.field === "path" ? "path match" : "title match";
  }
</script>

<section class="search" aria-label="Workspace search">
  <div class="query-row">
    <input
      bind:this={field}
      bind:value={query}
      {onkeydown}
      type="search"
      class="query"
      placeholder="Search this workspace…"
      aria-label="Search query"
      aria-controls="search-results"
      autocomplete="off"
      spellcheck="false"
      disabled={!searchable}
    />
  </div>

  <div class="filters">
    <label>
      <span>Path</span>
      <input
        bind:value={path}
        type="text"
        placeholder="docs/"
        aria-label="Filter by path"
        autocomplete="off"
      />
    </label>

    <label>
      <span>Group</span>
      <select bind:value={group} aria-label="Filter by document group">
        <option value="">Any group</option>
        {#each groups as g (g.id)}
          <option value={g.id}>{g.title}</option>
        {/each}
      </select>
    </label>

    <label>
      <span>Git</span>
      <select
        bind:value={git}
        aria-label="Filter by Git state"
        disabled={!status?.git_filter}
        title={status?.git_filter
          ? "Filter by Git state"
          : "Git state is not available for this workspace"}
      >
        <option value="">Any state</option>
        <option value="modified">Modified</option>
        <option value="untracked">Untracked</option>
        <option value="clean">Clean</option>
      </select>
    </label>
  </div>

  <!-- Index state is text as well as colour (spec 04 section 15). -->
  <p class="index-status" class:stale role="status">
    <span>{indexLabel}</span>
    {#if stale}
      <span class="stale-note">Results may be incomplete while indexing.</span>
    {/if}
    <button type="button" class="rebuild" onclick={rebuild} disabled={!searchable}>
      Rebuild
    </button>
  </p>

  {#if problem}
    <p class="problem" role="alert">{problem}</p>
  {/if}

  <ul id="search-results" class="results" role="listbox" aria-label="Search results">
    {#each results as result, index (result.document_id + ":" + (result.line ?? 0))}
      <li role="none">
        <button
          type="button"
          role="option"
          aria-selected={index === selected}
          class="result"
          class:selected={index === selected}
          onclick={() => openResult(result)}
          onmouseenter={() => (selected = index)}
        >
          <span class="result-head">
            <span class="doc-title">{result.title}</span>
            <span class="matched-in">{result.field}</span>
          </span>
          <span class="doc-path">{result.document_id}</span>
          <span class="location">{location(result)}</span>
          {#if result.snippet && result.snippet.length > 0}
            <span class="snippet">
              <!-- Segments are data, never markup: the server never sends HTML
                   and this never builds any (spec 03 section 9). -->
              {#each result.snippet as segment}
                {#if segment.match}<mark>{segment.text}</mark>{:else}{segment.text}{/if}
              {/each}
            </span>
          {/if}
        </button>
      </li>
    {:else}
      {#if query.trim() && !searching && !problem}
        <li class="empty">No document matches that query.</li>
      {:else if !query.trim()}
        <li class="empty">Enter a query to search titles, headings, paths, and text.</li>
      {/if}
    {/each}
  </ul>

  {#if truncated}
    <p class="truncated">More documents matched than are shown. Add a word to narrow the search.</p>
  {/if}
</section>

<style>
  .search {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    min-height: 0;
  }

  .query-row {
    padding: 0 0.5rem;
  }

  .query {
    width: 100%;
    padding: 0.45rem 0.6rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: 0.85rem;
  }

  .query:focus {
    outline: 2px solid var(--focus);
    outline-offset: 1px;
  }

  .filters {
    display: grid;
    grid-template-columns: 1fr;
    gap: 0.35rem;
    padding: 0 0.5rem;
  }

  .filters label {
    display: grid;
    grid-template-columns: 3.2rem 1fr;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.7rem;
    color: var(--text-muted);
  }

  .filters input,
  .filters select {
    padding: 0.2rem 0.35rem;
    border: 1px solid var(--line);
    border-radius: 2px;
    background: var(--surface-raised);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.72rem;
  }

  .filters select:disabled,
  .query:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .index-status {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.4rem;
    margin: 0;
    padding: 0.3rem 0.5rem;
    border-top: 1px solid var(--line);
    border-bottom: 1px solid var(--line);
    font-family: var(--font-mono);
    font-size: 0.68rem;
    color: var(--text-muted);
  }

  .index-status.stale {
    color: var(--warn);
  }

  .stale-note {
    flex-basis: 100%;
  }

  .rebuild {
    margin-left: auto;
    padding: 0.1rem 0.4rem;
    border: 1px solid var(--line-strong);
    border-radius: 2px;
    background: var(--surface-raised);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.65rem;
    cursor: pointer;
  }

  .problem {
    margin: 0;
    padding: 0.4rem 0.5rem;
    color: var(--danger);
    font-size: 0.75rem;
  }

  .results {
    margin: 0;
    padding: 0;
    list-style: none;
    overflow-y: auto;
  }

  .result {
    display: grid;
    gap: 0.1rem;
    width: 100%;
    padding: 0.4rem 0.5rem;
    border: 0;
    border-bottom: 1px solid var(--line);
    background: none;
    color: var(--text-secondary);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .result.selected {
    background: var(--surface-raised);
    box-shadow: inset 2px 0 0 var(--accent);
  }

  .result:focus-visible {
    outline: 2px solid var(--focus);
    outline-offset: -2px;
  }

  .result-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.4rem;
  }

  .doc-title {
    color: var(--text-primary);
    font-size: 0.82rem;
  }

  .matched-in {
    font-size: 0.6rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
  }

  .doc-path,
  .location {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono);
    font-size: 0.66rem;
    color: var(--text-muted);
  }

  .snippet {
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
    margin-top: 0.15rem;
    font-size: 0.72rem;
    line-height: 1.4;
    color: var(--text-secondary);
    word-break: break-word;
  }

  .snippet mark {
    background: color-mix(in srgb, var(--accent) 35%, transparent);
    color: var(--text-primary);
    border-radius: 2px;
  }

  .empty,
  .truncated {
    padding: 0.6rem 0.5rem;
    color: var(--text-muted);
    font-size: 0.75rem;
  }

  .truncated {
    margin: 0;
    border-top: 1px solid var(--line);
  }
</style>
