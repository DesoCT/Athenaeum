<script lang="ts">
  import type { Scaffold } from "./scaffold";

  interface Props {
    scaffold: Scaffold;
    /** The registry file path, when this process has a registry. */
    registryPath?: string | null;
    onclose: () => void;
  }

  let { scaffold, registryPath = null, onclose }: Props = $props();

  let copied = $state<string | null>(null);

  async function copy(what: string, text: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      copied = what;
      setTimeout(() => (copied = null), 1500);
    } catch {
      copied = null;
    }
  }
</script>

<div class="overlay">
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="backdrop" onclick={onclose}></div>

  <div class="dialog" role="dialog" aria-modal="true" aria-label="Make a workspace">
    <button type="button" class="close" aria-label="Close" onclick={onclose}>×</button>

    <h2>Make a workspace from <code>{scaffold.name}</code></h2>
    <p class="intro">
      Athenaeum only ever reads the workspace registry, so it does not write these
      files for you (ADR-0004). Save the two snippets below and the workspace will
      appear in the <strong>Workspaces</strong> menu.
    </p>

    {#if !scaffold.hasMarkdown}
      <p class="warn">No Markdown was found under this folder, so the workspace may be empty.</p>
    {/if}

    <section>
      <div class="step-head">
        <h3>1. Save this as the workspace config</h3>
        <button type="button" class="copy" onclick={() => copy("config", scaffold.configToml)}>
          {copied === "config" ? "Copied" : "Copy"}
        </button>
      </div>
      <p class="path"><code>{scaffold.configPath}</code></p>
      <pre>{scaffold.configToml}</pre>
    </section>

    <section>
      <div class="step-head">
        <h3>2. Add this to your workspace registry</h3>
        <button type="button" class="copy" onclick={() => copy("registry", scaffold.registryEntry)}>
          {copied === "registry" ? "Copied" : "Copy"}
        </button>
      </div>
      <p class="path">
        <code>{registryPath ?? "<user-config>/athenaeum/workspaces.toml"}</code>
      </p>
      <pre>{scaffold.registryEntry}</pre>
    </section>

    <p class="hint">
      Tip: the <code>athenaeum-config.sh</code> script does both steps for you from
      a terminal — see the README.
    </p>
  </div>
</div>

<svelte:window onkeydown={(e) => e.key === "Escape" && onclose()} />

<style>
  .overlay {
    position: fixed;
    inset: 0;
    z-index: 40;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 2.5rem;
  }
  .backdrop {
    position: absolute;
    inset: 0;
    background: rgb(0 0 0 / 45%);
  }
  .dialog {
    position: relative;
    width: min(46rem, 100%);
    max-height: 100%;
    overflow-y: auto;
    padding: 1.5rem 1.75rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
    box-shadow: 0 12px 48px rgb(0 0 0 / 45%);
  }
  .close {
    position: absolute;
    top: 0.5rem;
    right: 0.7rem;
    border: 0;
    background: none;
    color: var(--text-muted);
    font-size: 1.3rem;
    line-height: 1;
    cursor: pointer;
  }
  .close:hover {
    color: var(--text-primary);
  }
  h2 {
    margin: 0 0 0.5rem;
    font-size: 1.15rem;
  }
  h2 code {
    color: var(--accent);
  }
  .intro {
    margin: 0 0 1rem;
    color: var(--text-secondary);
    font-size: 0.88rem;
    line-height: 1.5;
  }
  .warn {
    margin: 0 0 1rem;
    padding: 0.5rem 0.75rem;
    border: 1px solid var(--warn);
    border-radius: var(--radius);
    color: var(--warn);
    font-size: 0.85rem;
  }
  section {
    margin-bottom: 1.25rem;
  }
  .step-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  h3 {
    margin: 0 0 0.35rem;
    font-size: 0.9rem;
  }
  .path {
    margin: 0 0 0.4rem;
    font-size: 0.78rem;
    color: var(--text-muted);
  }
  .path code,
  .hint code {
    font-family: var(--font-mono);
    font-size: 0.75rem;
  }
  pre {
    margin: 0;
    padding: 0.75rem 0.9rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    font-family: var(--font-mono);
    font-size: 0.72rem;
    line-height: 1.45;
    overflow-x: auto;
    max-height: 22rem;
  }
  .copy {
    flex-shrink: 0;
    padding: 0.2rem 0.7rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.75rem;
    cursor: pointer;
  }
  .copy:hover {
    color: var(--text-primary);
    border-color: var(--focus);
  }
  .hint {
    margin: 0;
    color: var(--text-muted);
    font-size: 0.8rem;
  }
</style>
