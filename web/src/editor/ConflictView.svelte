<script lang="ts">
  /**
   * Conflict comparison (R6, spec 04 section 7).
   *
   * Both versions are always shown and neither is ever discarded implicitly.
   * There is no automatic merge in v0.1, and no action here is destructive
   * without the user choosing it by name.
   */
  interface Props {
    documentId: string;
    /** The unsaved editor buffer. */
    local: string;
    /** The current content on disk. */
    disk: string;
    onKeepLocal: () => void;
    onAcceptDisk: () => void;
    onDismiss: () => void;
  }

  let { documentId, local, disk, onKeepLocal, onAcceptDisk, onDismiss }: Props = $props();

  let copied = $state<"local" | "disk" | null>(null);

  const localLines = $derived(local.split("\n"));
  const diskLines = $derived(disk.split("\n"));

  /** differing marks lines that are not identical, as a reading aid only. */
  function differs(index: number, side: string[], other: string[]): boolean {
    return side[index] !== other[index];
  }

  async function copy(which: "local" | "disk"): Promise<void> {
    try {
      await navigator.clipboard.writeText(which === "local" ? local : disk);
      copied = which;
      setTimeout(() => (copied = null), 2000);
    } catch {
      copied = null;
    }
  }
</script>

<section class="conflict" aria-labelledby="conflict-heading">
  <header>
    <h2 id="conflict-heading">Conflict</h2>
    <p>
      <code>{documentId}</code> changed on disk while you had unsaved edits.
      Both versions are preserved below. Nothing has been written.
    </p>
  </header>

  <div class="panes">
    <article class="pane" aria-labelledby="local-heading">
      <div class="pane-header">
        <h3 id="local-heading">Your unsaved version</h3>
        <button type="button" onclick={() => copy("local")}>
          {copied === "local" ? "Copied" : "Copy"}
        </button>
      </div>
      <pre>{#each localLines as line, i}<span
            class="line"
            class:changed={differs(i, localLines, diskLines)}>{line || " "}</span>{/each}</pre>
    </article>

    <article class="pane" aria-labelledby="disk-heading">
      <div class="pane-header">
        <h3 id="disk-heading">Current version on disk</h3>
        <button type="button" onclick={() => copy("disk")}>
          {copied === "disk" ? "Copied" : "Copy"}
        </button>
      </div>
      <pre>{#each diskLines as line, i}<span
            class="line"
            class:changed={differs(i, diskLines, localLines)}>{line || " "}</span>{/each}</pre>
    </article>
  </div>

  <footer class="actions">
    <button type="button" class="primary" onclick={onKeepLocal}>
      Keep my version
      <span class="hint">overwrites the file on disk</span>
    </button>
    <button type="button" onclick={onAcceptDisk}>
      Use the disk version
      <span class="hint">discards your unsaved edits</span>
    </button>
    <button type="button" class="quiet" onclick={onDismiss}>
      Decide later
      <span class="hint">keeps both, saves nothing</span>
    </button>
  </footer>
</section>

<style>
  .conflict {
    margin: 1.5rem;
    border: 1px solid var(--danger);
    border-radius: var(--radius);
    background: var(--surface-raised);
  }

  header {
    padding: 1rem 1.25rem;
    border-bottom: 1px solid var(--line);
  }

  h2 {
    margin: 0 0 0.3rem;
    font-size: 0.78rem;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--danger);
  }

  header p {
    margin: 0;
    color: var(--text-secondary);
    font-size: 0.9rem;
  }

  code {
    font-family: var(--font-mono);
    color: var(--text-primary);
  }

  .panes {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1px;
    background: var(--line);
  }

  .pane {
    background: var(--surface-panel);
    min-width: 0;
  }

  .pane-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    padding: 0.5rem 0.75rem;
    border-bottom: 1px solid var(--line);
  }

  h3 {
    margin: 0;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-secondary);
  }

  pre {
    margin: 0;
    padding: 0.75rem;
    max-height: 24rem;
    overflow: auto;
    font-family: var(--font-mono);
    font-size: 0.76rem;
    line-height: 1.5;
  }

  .line {
    display: block;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .line.changed {
    background: color-mix(in srgb, var(--warn) 16%, transparent);
    box-shadow: inset 2px 0 0 var(--warn);
    padding-left: 0.4rem;
    margin-left: -0.4rem;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.6rem;
    padding: 1rem 1.25rem;
    border-top: 1px solid var(--line);
  }

  button {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.1rem;
    padding: 0.45rem 0.9rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-primary);
    font: inherit;
    font-size: 0.85rem;
    cursor: pointer;
  }

  button:hover {
    border-color: var(--focus);
  }

  .pane-header button {
    flex-direction: row;
    padding: 0.15rem 0.5rem;
    font-size: 0.72rem;
    color: var(--text-secondary);
  }

  .primary {
    border-color: var(--accent);
  }

  .quiet {
    margin-left: auto;
  }

  .hint {
    font-size: 0.7rem;
    color: var(--text-muted);
  }
</style>
