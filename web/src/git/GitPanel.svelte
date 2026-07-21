<script lang="ts">
  import { getGitStatus, getGitDiff, getGitHistory, getGitBlame, ApiError } from "../api/client";
  import type { GitState, GitCommit, GitBlameLine } from "./types";

  interface Props {
    /** The active document, or null when a note or nothing is open. */
    documentId: string | null;
    /** Bumped by the shell on a workspace switch or a file change. */
    generation: number;
  }

  let { documentId, generation }: Props = $props();

  let available = $state(true);
  let fileState = $state<GitState | null>(null);
  let diff = $state<string>("");
  let commits = $state<GitCommit[]>([]);
  let blame = $state<GitBlameLine[]>([]);
  let showBlame = $state(false);
  let error = $state<string | null>(null);

  // Reload status, diff, and history whenever the document or corpus changes.
  // Blame is fetched only on demand: it is the largest payload and rarely the
  // first thing wanted.
  $effect(() => {
    const id = documentId;
    void generation;
    showBlame = false;
    blame = [];
    if (!id) {
      fileState = null;
      diff = "";
      commits = [];
      return;
    }
    void load(id);
  });

  async function load(id: string): Promise<void> {
    try {
      const [status, d, history] = await Promise.all([
        getGitStatus(),
        getGitDiff(id),
        getGitHistory(id),
      ]);
      available = status.available && d.available && history.available;
      fileState = status.files.find((f) => f.document_id === id)?.state ?? (available ? "clean" : null);
      diff = d.diff;
      commits = history.commits;
      error = null;
    } catch (err) {
      error = err instanceof ApiError ? `${err.code}: ${err.message}` : "Git information could not be loaded.";
    }
  }

  async function loadBlame(): Promise<void> {
    if (!documentId) return;
    showBlame = true;
    try {
      const result = await getGitBlame(documentId);
      blame = result.lines;
    } catch {
      blame = [];
    }
  }

  // Split a unified diff into lines so each can be coloured by its first
  // character without ever building HTML from repository content.
  const diffLines = $derived(diff ? diff.replace(/\n$/, "").split("\n") : []);

  function diffClass(line: string): string {
    if (line.startsWith("+") && !line.startsWith("+++")) return "add";
    if (line.startsWith("-") && !line.startsWith("---")) return "del";
    if (line.startsWith("@@")) return "hunk";
    if (line.startsWith("diff ") || line.startsWith("index ") || line.startsWith("+++") || line.startsWith("---"))
      return "meta";
    return "";
  }

  function shortDate(iso: string): string {
    return iso.length >= 10 ? iso.slice(0, 10) : iso;
  }
</script>

<div class="git-panel">
  {#if !documentId}
    <p class="pending">Open a document to see its Git history.</p>
  {:else if !available}
    <p class="pending">
      Git is not available for this workspace. Everything else works unchanged; this panel
      needs a Git repository and the <code>git</code> command on PATH.
    </p>
  {:else if error}
    <p class="error" role="status">{error}</p>
  {:else}
    {#if fileState}
      <div class="state-row">
        <span class="state-badge {fileState}">{fileState}</span>
      </div>
    {/if}

    <section class="git-section">
      <h3>Working-tree diff</h3>
      {#if diffLines.length === 0}
        <p class="pending">No uncommitted changes.</p>
      {:else}
        <pre class="diff">{#each diffLines as line}<span class="line {diffClass(line)}">{line}
</span>{/each}</pre>
      {/if}
    </section>

    <section class="git-section">
      <h3>History</h3>
      {#if commits.length === 0}
        <p class="pending">No commits touch this file.</p>
      {:else}
        <ul class="commits">
          {#each commits as commit (commit.hash)}
            <li>
              <span class="subject">{commit.subject}</span>
              <span class="meta-row">
                <code>{commit.short_hash}</code>
                <span>{commit.author}</span>
                <span>{shortDate(commit.date)}</span>
              </span>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="git-section">
      {#if !showBlame}
        <button type="button" class="blame-toggle" onclick={loadBlame}>Show blame</button>
      {:else}
        <h3>Blame</h3>
        <div class="blame">
          {#each blame as bl (bl.line)}
            <div class="blame-line">
              <code class="blame-hash">{bl.hash.slice(0, 7)}</code>
              <span class="blame-num">{bl.line}</span>
              <span class="blame-content">{bl.content}</span>
            </div>
          {/each}
        </div>
      {/if}
    </section>
  {/if}
</div>

<style>
  .git-panel { display: flex; flex-direction: column; gap: 0.8rem; }
  .git-section h3 {
    margin: 0 0 0.35rem; font-size: 0.7rem; font-weight: 600; letter-spacing: 0.08em;
    text-transform: uppercase; color: var(--text-secondary);
  }
  .state-row { display: flex; }
  .state-badge {
    padding: 0.1rem 0.5rem; border-radius: 999px; font-size: 0.66rem; text-transform: uppercase;
    letter-spacing: 0.04em; border: 1px solid var(--line-strong); color: var(--text-muted);
  }
  .state-badge.modified { color: var(--warn); border-color: var(--warn); }
  .state-badge.untracked { color: var(--accent); border-color: var(--accent); }
  .state-badge.clean { color: var(--ok); border-color: var(--ok); }
  .diff {
    margin: 0; padding: 0.4rem; border: 1px solid var(--line-strong); border-radius: var(--radius);
    background: var(--surface-panel); font-family: var(--font-mono); font-size: 0.68rem;
    line-height: 1.35; overflow-x: auto; max-height: 18rem; white-space: pre;
  }
  .diff .line { display: block; }
  .diff .add { color: #3f8a5c; }
  .diff .del { color: var(--danger); }
  .diff .hunk { color: var(--accent); }
  .diff .meta { color: var(--text-muted); }
  .commits { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.4rem; }
  .commits li { display: flex; flex-direction: column; gap: 0.15rem; }
  .subject { font-size: 0.8rem; color: var(--text-primary); }
  .meta-row { display: flex; gap: 0.5rem; font-size: 0.68rem; color: var(--text-muted); }
  .meta-row code { color: var(--accent); }
  .blame-toggle {
    padding: 0.25rem 0.6rem; border: 1px solid var(--line-strong); border-radius: var(--radius);
    background: var(--surface-raised); color: var(--text-secondary); font: inherit; font-size: 0.75rem; cursor: pointer;
  }
  .blame { display: flex; flex-direction: column; max-height: 18rem; overflow-y: auto; font-family: var(--font-mono); font-size: 0.68rem; }
  .blame-line { display: flex; gap: 0.5rem; padding: 0.05rem 0; }
  .blame-hash { color: var(--accent); flex-shrink: 0; }
  .blame-num { color: var(--text-muted); width: 2rem; text-align: right; flex-shrink: 0; }
  .blame-content { white-space: pre; overflow: hidden; text-overflow: ellipsis; }
  .pending { margin: 0.2rem 0; color: var(--text-muted); font-size: 0.8rem; line-height: 1.4; }
  .pending code { font-family: var(--font-mono); font-size: 0.75rem; }
  .error { margin: 0; color: var(--danger); font-size: 0.78rem; }
</style>
