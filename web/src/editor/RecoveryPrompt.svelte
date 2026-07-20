<script lang="ts">
  import type { RecoveryBuffer } from "../api/client";

  /**
   * Offers unsaved buffers found at startup (R13, acceptance E3).
   *
   * Nothing here applies or discards a buffer on its own. Every outcome is a
   * named action the user takes, and "Not now" leaves everything exactly as it
   * was so the offer returns next time.
   */
  interface Props {
    buffers: RecoveryBuffer[];
    onRestore: (buffer: RecoveryBuffer) => void;
    onDiscard: (buffer: RecoveryBuffer) => void;
    onDismiss: () => void;
  }

  let { buffers, onRestore, onDiscard, onDismiss }: Props = $props();

  let confirming = $state<string | null>(null);

  function when(iso: string): string {
    if (!iso) return "unknown time";
    const date = new Date(iso);
    return Number.isNaN(date.getTime()) ? "unknown time" : date.toLocaleString();
  }
</script>

<section class="recovery" aria-labelledby="recovery-heading">
  <header>
    <h2 id="recovery-heading">Unsaved work found</h2>
    <p>
      Athenaeum closed with unsaved edits. They are still here. Nothing has been
      written to your files and nothing will be until you choose.
    </p>
  </header>

  <ul>
    {#each buffers as buffer (buffer.document_id)}
      <li>
        <div class="meta">
          <code>{buffer.document_id}</code>
          <span class="time">last edited {when(buffer.updated_at)}</span>
        </div>

        <div class="row-actions">
          <button type="button" class="primary" onclick={() => onRestore(buffer)}>
            Open with these edits
          </button>

          {#if confirming === buffer.document_id}
            <span class="confirm">
              Discard permanently?
              <button type="button" class="danger" onclick={() => onDiscard(buffer)}>Yes, discard</button>
              <button type="button" onclick={() => (confirming = null)}>Cancel</button>
            </span>
          {:else}
            <button type="button" onclick={() => (confirming = buffer.document_id)}>
              Discard…
            </button>
          {/if}
        </div>
      </li>
    {/each}
  </ul>

  <footer>
    <button type="button" class="quiet" onclick={onDismiss}>
      Not now — ask again next time
    </button>
  </footer>
</section>

<style>
  .recovery {
    margin: 1.5rem;
    border: 1px solid var(--warn);
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
    color: var(--warn);
  }

  header p {
    margin: 0;
    color: var(--text-secondary);
    font-size: 0.9rem;
    max-width: 44rem;
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.75rem 1.25rem;
    border-bottom: 1px solid var(--line);
  }

  .meta {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 0;
  }

  code {
    font-family: var(--font-mono);
    font-size: 0.85rem;
    color: var(--text-primary);
  }

  .time {
    font-size: 0.72rem;
    color: var(--text-muted);
  }

  .row-actions,
  .confirm {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  .confirm {
    font-size: 0.78rem;
    color: var(--danger);
  }

  button {
    padding: 0.3rem 0.7rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-primary);
    font: inherit;
    font-size: 0.8rem;
    cursor: pointer;
  }

  button:hover {
    border-color: var(--focus);
  }

  .primary {
    border-color: var(--accent);
  }

  .danger {
    border-color: var(--danger);
    color: var(--danger);
  }

  footer {
    padding: 0.75rem 1.25rem;
  }

  .quiet {
    border-color: transparent;
    color: var(--text-secondary);
  }
</style>
