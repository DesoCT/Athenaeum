<script lang="ts">
  import { render } from "./renderer";
  import { enhance } from "./enhance";
  import type { Capabilities, DocumentDetail } from "../api/types";

  interface Props {
    document: DocumentDetail;
    capabilities: Capabilities;
  }

  let { document: doc, capabilities }: Props = $props();

  let container: HTMLElement | null = $state(null);
  let headingWarnings = $state<string[]>([]);

  /**
   * The backend reports front matter separately, but `content` still contains
   * it. Strip the same block so rendered line numbers line up with the
   * backend outline (ADR-0003).
   */
  const body = $derived.by(() => {
    if (doc.front_matter_format === "none") {
      return { source: doc.content, startLine: 1 };
    }
    const fence = doc.front_matter_format === "yaml" ? "---" : "+++";
    const lines = doc.content.split("\n");
    if (lines[0]?.trim() !== fence) return { source: doc.content, startLine: 1 };

    for (let i = 1; i < lines.length; i++) {
      if (lines[i].trim() === fence) {
        return { source: lines.slice(i + 1).join("\n"), startLine: i + 2 };
      }
    }
    return { source: doc.content, startLine: 1 };
  });

  const rendered = $derived(
    render({
      source: body.source,
      sourceStartLine: body.startLine,
      outline: doc.outline,
      capabilities,
    }),
  );

  $effect(() => {
    headingWarnings = rendered.headingWarnings;
  });

  $effect(() => {
    const root = container;
    if (!root) return;
    // Reading `rendered` here ties this effect to re-renders.
    const result = rendered;

    void enhance(root, {
      math: capabilities.math,
      mermaid: capabilities.mermaid,
      mathSources: result.mathSources,
      mermaidSources: result.mermaidSources,
      dark: true,
    });
  });
</script>

{#if headingWarnings.length > 0}
  <!-- ADR-0003: a disagreement between the backend outline and the rendered
       document is surfaced, never silently patched over. -->
  <aside class="heading-warnings" role="status">
    <h2>Outline mismatch</h2>
    <p>
      Some headings could not be matched to the document outline, so their
      anchors may be unreliable.
    </p>
    <ul>
      {#each headingWarnings as warning}
        <li>{warning}</li>
      {/each}
    </ul>
  </aside>
{/if}

<article bind:this={container} class="preview">
  <!-- The HTML is sanitised by renderer.ts before it reaches this point. -->
  {@html rendered.html}
</article>

<style>
  .heading-warnings {
    margin: 0 0 1rem;
    padding: 0.75rem 1rem;
    border: 1px solid var(--warn);
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--warn) 8%, transparent);
    font-size: 0.85rem;
  }

  .heading-warnings h2 {
    margin: 0 0 0.25rem;
    font-size: 0.75rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--warn);
  }

  .heading-warnings p {
    margin: 0 0 0.4rem;
    color: var(--text-secondary);
  }

  .heading-warnings ul {
    margin: 0;
    padding-left: 1.1rem;
    color: var(--text-muted);
  }

  /* Warm paper surface for the document itself (spec 04 section 1). */
  .preview {
    padding: 2.5rem 3rem;
    background: var(--surface-paper);
    color: var(--ink);
    border-radius: var(--radius);
    line-height: 1.65;
  }

  .preview :global(h1),
  .preview :global(h2),
  .preview :global(h3),
  .preview :global(h4) {
    margin: 1.6em 0 0.5em;
    line-height: 1.25;
    scroll-margin-top: 1rem;
  }

  .preview :global(h1:first-child) {
    margin-top: 0;
  }

  .preview :global(h1) {
    padding-bottom: 0.2em;
    border-bottom: 1px solid rgb(0 0 0 / 12%);
  }

  .preview :global(a) {
    color: #7a4b16;
    text-decoration: underline;
    text-underline-offset: 2px;
  }

  /* Remote assets carry a visible indicator (R3, N7). */
  .preview :global(a[data-remote="true"])::after {
    content: " ↗";
    color: #8a6a3a;
  }

  .preview :global(img) {
    max-width: 100%;
    height: auto;
  }

  .preview :global(img[data-remote="true"]) {
    outline: 1px dashed #b08a4a;
    outline-offset: 3px;
  }

  .preview :global(code) {
    padding: 0.12em 0.35em;
    border-radius: 3px;
    background: rgb(0 0 0 / 6%);
    font-family: var(--font-mono);
    font-size: 0.88em;
  }

  .preview :global(pre) {
    padding: 0.9rem 1.1rem;
    border-radius: var(--radius);
    background: #1b1e24;
    color: #e8e4dc;
    overflow-x: auto;
  }

  .preview :global(pre code) {
    padding: 0;
    background: none;
    color: inherit;
  }

  .preview :global(table) {
    border-collapse: collapse;
    width: 100%;
    margin: 1em 0;
  }

  .preview :global(th),
  .preview :global(td) {
    padding: 0.4rem 0.6rem;
    border: 1px solid rgb(0 0 0 / 15%);
    text-align: left;
  }

  .preview :global(th) {
    background: rgb(0 0 0 / 5%);
  }

  .preview :global(blockquote) {
    margin: 1em 0;
    padding: 0.1rem 1rem;
    border-left: 3px solid rgb(0 0 0 / 20%);
    color: #4a453d;
  }

  /* Callouts (R3). */
  .preview :global(blockquote[data-callout]) {
    border-left-width: 4px;
    border-radius: 0 var(--radius) var(--radius) 0;
    background: rgb(0 0 0 / 4%);
  }

  .preview :global(.callout-title) {
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-size: 0.75rem;
  }

  .preview :global(blockquote[data-callout="note"]) { border-left-color: #3f6ea8; }
  .preview :global(blockquote[data-callout="tip"]) { border-left-color: #3f8a5c; }
  .preview :global(blockquote[data-callout="important"]) { border-left-color: #7a4ba8; }
  .preview :global(blockquote[data-callout="warning"]),
  .preview :global(blockquote[data-callout="caution"]) { border-left-color: #b0781f; }
  .preview :global(blockquote[data-callout="danger"]) { border-left-color: #b04a3f; }

  .preview :global(.wiki-link) {
    border-bottom: 1px dotted currentColor;
    text-decoration: none;
  }

  .preview :global(.mermaid-block) {
    display: flex;
    justify-content: center;
    margin: 1.2em 0;
  }

  .preview :global(.mermaid-error) {
    display: block;
    padding: 0.8rem;
    border: 1px solid var(--danger);
    border-radius: var(--radius);
    background: rgb(0 0 0 / 5%);
    font-family: var(--font-mono);
    font-size: 0.8rem;
    white-space: pre-wrap;
  }

  .preview :global(.mermaid-error-message) {
    margin: 0 0 0.5rem;
    color: var(--danger);
    font-weight: 600;
  }

  .preview :global(.math-error) {
    color: var(--danger);
    font-family: var(--font-mono);
  }

  .preview :global(input[type="checkbox"]) {
    margin-right: 0.4em;
  }

  .preview :global(ul.contains-task-list) {
    padding-left: 1.2rem;
    list-style: none;
  }
</style>
