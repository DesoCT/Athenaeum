<script lang="ts">
  import {
    listAnnotations,
    createAnnotation,
    updateAnnotation,
    deleteAnnotation,
    AnnotationConflictError,
    ApiError,
  } from "../api/client";
  import { render } from "../renderer/renderer";
  import { selectionAnchor } from "./anchor";
  import type { Annotation, AnnotationList, AnchorInput, Visibility, AnnotationKind } from "./types";
  import type { Capabilities, Heading } from "../api/types";

  interface Props {
    documentId: string;
    /** The rendered article, handed over by Preview's onrendered (R8). */
    root: HTMLElement | null;
    /** The authoritative outline, for the heading path of a new anchor (ADR-0003). */
    outline: Heading[];
    capabilities: Capabilities;
    /** Whether personal annotation storage is available this session. */
  }

  let { documentId, root, outline, capabilities }: Props = $props();

  let list = $state<AnnotationList | null>(null);
  let notice = $state<string | null>(null);

  // A pending anchor is a live selection the user may turn into an annotation.
  let draftAnchor = $state<AnchorInput | null>(null);
  let draftTop = $state(0);
  let draftBody = $state("");
  let draftVisibility = $state<Visibility>("personal");
  let draftKind = $state<AnnotationKind>("comment");
  let saving = $state(false);

  // Card vertical positions, keyed by annotation id, so the margin column lines
  // up with the text it refers to (Hypothesis/Docs style).
  let tops = $state<Record<string, number>>({});
  let hovered = $state<string | null>(null);
  let cardEls: Record<string, HTMLElement> = {};

  const anchored = $derived((list?.annotations ?? []).filter((a) => a.anchor.state !== "detached"));
  const detached = $derived((list?.annotations ?? []).filter((a) => a.anchor.state === "detached"));

  async function reload(): Promise<void> {
    try {
      list = await listAnnotations(documentId);
    } catch (err) {
      if (err instanceof ApiError && err.status === 503) {
        // No annotation storage in this session: the panel simply stays empty.
        list = null;
        return;
      }
      notice = err instanceof ApiError ? `${err.code}: ${err.message}` : "Annotations could not be loaded.";
    }
  }

  // Reload whenever the document changes.
  $effect(() => {
    void documentId;
    draftAnchor = null;
    void reload();
  });

  // Observe selections in the rendered article. mouseup is enough: a selection
  // is complete when the pointer is released.
  $effect(() => {
    const el = root;
    if (!el) return;
    const onMouseUp = () => {
      const anchor = selectionAnchor(el, outline);
      if (!anchor) {
        draftAnchor = null;
        return;
      }
      draftAnchor = anchor;
      draftBody = "";
      draftTop = anchorTop(anchor.start_line ?? 1);
    };
    el.addEventListener("mouseup", onMouseUp);
    return () => el.removeEventListener("mouseup", onMouseUp);
  });

  /** blockFor finds the rendered block that begins at or before a source line. */
  function blockFor(line: number): HTMLElement | null {
    if (!root) return null;
    let best: HTMLElement | null = null;
    let bestLine = 0;
    for (const el of root.querySelectorAll<HTMLElement>("[data-line]")) {
      const value = Number(el.getAttribute("data-line"));
      if (!Number.isFinite(value) || value > line) continue;
      if (value >= bestLine) {
        best = el;
        bestLine = value;
      }
    }
    return best;
  }

  /** anchorTop is a block's top within the scrolling pane's content. */
  function anchorTop(line: number): number {
    const pane = root?.parentElement;
    const block = blockFor(line);
    if (!pane || !block) return 0;
    const paneRect = pane.getBoundingClientRect();
    const rect = block.getBoundingClientRect();
    return rect.top - paneRect.top + pane.scrollTop;
  }

  // Recompute card positions and in-text highlights whenever the list or the
  // rendered geometry changes. A simple downward stack keeps close anchors from
  // overlapping.
  $effect(() => {
    const items = anchored;
    void root;
    // Wait for the cards to be in the DOM so their heights are measurable.
    requestAnimationFrame(() => {
      markHighlights(items);
      const next: Record<string, number> = {};
      let floor = 0;
      for (const ann of items) {
        const desired = anchorTop(ann.anchor.start_line ?? 1);
        const top = Math.max(desired, floor);
        next[ann.id] = top;
        const height = cardEls[ann.id]?.offsetHeight ?? 72;
        floor = top + height + 8;
      }
      tops = next;
    });
  });

  /** markHighlights tags the anchored blocks so the text shows it is annotated. */
  function markHighlights(items: Annotation[]): void {
    if (!root) return;
    for (const el of root.querySelectorAll("[data-annotated]")) {
      el.removeAttribute("data-annotated");
    }
    for (const ann of items) {
      const block = blockFor(ann.anchor.start_line ?? 1);
      block?.setAttribute("data-annotated", ann.id);
    }
  }

  function revisionFor(visibility: Visibility): number {
    if (!list) return 0;
    return visibility === "shared" ? list.shared_revision : list.personal_revision;
  }

  async function saveDraft(): Promise<void> {
    if (!draftAnchor) return;
    if (draftKind === "comment" && draftBody.trim() === "") {
      notice = "A comment needs a body.";
      return;
    }
    saving = true;
    notice = null;
    try {
      await createAnnotation({
        document_id: documentId,
        kind: draftKind,
        visibility: draftVisibility,
        body: draftBody,
        anchor: draftAnchor,
        expected_revision: revisionFor(draftVisibility),
      });
      draftAnchor = null;
      draftBody = "";
      window.getSelection()?.removeAllRanges();
      await reload();
    } catch (err) {
      await handleWriteError(err, "The annotation could not be saved.");
    } finally {
      saving = false;
    }
  }

  async function toggleStatus(ann: Annotation): Promise<void> {
    try {
      await updateAnnotation(ann.id, {
        document_id: documentId,
        visibility: ann.visibility,
        expected_revision: revisionFor(ann.visibility),
        status: ann.status === "open" ? "resolved" : "open",
      });
      await reload();
    } catch (err) {
      await handleWriteError(err, "The annotation could not be updated.");
    }
  }

  async function remove(ann: Annotation): Promise<void> {
    try {
      await deleteAnnotation(ann.id, {
        document_id: documentId,
        visibility: ann.visibility,
        expected_revision: revisionFor(ann.visibility),
      });
      await reload();
    } catch (err) {
      await handleWriteError(err, "The annotation could not be deleted.");
    }
  }

  async function handleWriteError(err: unknown, fallback: string): Promise<void> {
    if (err instanceof AnnotationConflictError) {
      // The sidecar moved under us; re-read so the next action uses the current
      // revision, and tell the user nothing was lost.
      notice = "These annotations changed elsewhere and were refreshed. Please try again.";
      await reload();
      return;
    }
    notice = err instanceof ApiError ? `${err.code}: ${err.message}` : fallback;
  }

  function scrollTo(ann: Annotation): void {
    const block = blockFor(ann.anchor.start_line ?? 1);
    if (!block) return;
    block.classList.add("search-hit");
    block.scrollIntoView({ block: "center", behavior: "auto" });
    setTimeout(() => block.classList.remove("search-hit"), 1500);
  }

  function bodyHtml(markdown: string): string {
    return render({
      source: markdown,
      documentId,
      sourceStartLine: 1,
      outline: [],
      capabilities,
    }).html;
  }
</script>

<div class="annotation-column" aria-label="Annotations">
  {#if notice}
    <div class="ann-notice" role="status">
      {notice}
      <button type="button" onclick={() => (notice = null)} aria-label="Dismiss">×</button>
    </div>
  {/if}

  {#if detached.length > 0}
    <div class="detached-group">
      <p class="group-label">Detached ({detached.length})</p>
      {#each detached as ann (ann.id)}
        {@render card(ann, true)}
      {/each}
    </div>
  {/if}

  {#each anchored as ann (ann.id)}
    <div
      class="card-slot"
      style="top: {tops[ann.id] ?? 0}px"
      bind:this={cardEls[ann.id]}
      role="button"
      tabindex="0"
      onmouseenter={() => (hovered = ann.id)}
      onmouseleave={() => (hovered = null)}
      onclick={() => scrollTo(ann)}
      onkeydown={(e) => e.key === "Enter" && scrollTo(ann)}
    >
      {@render card(ann, false)}
    </div>
  {/each}

  {#if draftAnchor}
    <div class="card-slot draft" style="top: {draftTop}px">
      <div class="ann-card draft-card">
        <div class="draft-kind" role="group" aria-label="Annotation kind">
          <button type="button" class:active={draftKind === "comment"} onclick={() => (draftKind = "comment")}>Comment</button>
          <button type="button" class:active={draftKind === "pin"} onclick={() => (draftKind = "pin")}>Pin</button>
        </div>
        {#if draftKind === "comment"}
          <!-- svelte-ignore a11y_autofocus -->
          <textarea
            class="draft-body"
            bind:value={draftBody}
            placeholder="Add a comment…"
            rows="3"
            autofocus
          ></textarea>
        {/if}
        <blockquote class="draft-quote">{draftAnchor.exact}</blockquote>
        <label class="draft-visibility">
          <select bind:value={draftVisibility}>
            <option value="personal">Personal (private)</option>
            <option value="shared">Shared (committable)</option>
          </select>
        </label>
        <div class="draft-actions">
          <button type="button" class="ghost" onclick={() => (draftAnchor = null)}>Cancel</button>
          <button type="button" class="primary" onclick={saveDraft} disabled={saving}>
            {saving ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>

{#snippet card(ann: Annotation, isDetached: boolean)}
  <article
    class="ann-card"
    class:resolved={ann.status === "resolved"}
    class:detached={isDetached}
    class:active={hovered === ann.id}
  >
    <header class="ann-head">
      <span class="badge {ann.visibility}">{ann.visibility}</span>
      {#if ann.kind === "pin"}<span class="badge pin">pin</span>{/if}
      {#if ann.status === "resolved"}<span class="badge done">resolved</span>{/if}
      {#if isDetached}<span class="badge broken">detached</span>{/if}
    </header>
    {#if ann.body}
      <!-- Body is sanitised Markdown, raw HTML off (spec 03 section 3). -->
      <div class="ann-body">{@html bodyHtml(ann.body)}</div>
    {/if}
    <footer class="ann-actions">
      <button type="button" onclick={(e) => { e.stopPropagation(); void toggleStatus(ann); }}>
        {ann.status === "open" ? "Resolve" : "Reopen"}
      </button>
      <button type="button" class="danger" onclick={(e) => { e.stopPropagation(); void remove(ann); }}>
        Delete
      </button>
    </footer>
  </article>
{/snippet}

<style>
  .annotation-column {
    position: absolute;
    top: 0;
    right: 0;
    width: 17rem;
    padding: 0 0.5rem;
    pointer-events: none;
  }

  .detached-group {
    position: relative;
    margin-bottom: 1rem;
    pointer-events: auto;
  }

  .group-label {
    margin: 0 0 0.3rem;
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--danger);
  }

  .card-slot {
    position: absolute;
    right: 0.5rem;
    width: 16rem;
    pointer-events: auto;
    transition: top 120ms ease-out;
  }

  .ann-card {
    padding: 0.55rem 0.65rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
    font-size: 0.8rem;
    box-shadow: 0 1px 3px rgb(0 0 0 / 20%);
  }

  .ann-card.active {
    border-color: var(--accent);
  }

  .ann-card.resolved {
    opacity: 0.7;
  }

  .ann-card.detached {
    border-style: dashed;
    border-color: var(--danger);
  }

  .ann-head {
    display: flex;
    gap: 0.3rem;
    flex-wrap: wrap;
    margin-bottom: 0.35rem;
  }

  .badge {
    padding: 0.05rem 0.35rem;
    border-radius: 999px;
    font-size: 0.62rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    background: var(--surface-panel);
    color: var(--text-secondary);
    border: 1px solid var(--line-strong);
  }

  .badge.shared { color: var(--accent); }
  .badge.personal { color: var(--text-muted); }
  .badge.done { color: var(--ok); }
  .badge.broken { color: var(--danger); }
  .badge.pin { color: var(--warn); }

  .ann-body {
    color: var(--text-primary);
    line-height: 1.4;
    word-break: break-word;
  }

  .ann-body :global(p) { margin: 0 0 0.4rem; }
  .ann-body :global(p:last-child) { margin-bottom: 0; }

  .ann-actions {
    display: flex;
    gap: 0.4rem;
    margin-top: 0.4rem;
  }

  .ann-actions button,
  .draft-actions button {
    padding: 0.15rem 0.5rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.72rem;
    cursor: pointer;
  }

  .ann-actions .danger,
  .draft-actions .primary {
    color: var(--text-primary);
  }

  .draft-card {
    border-color: var(--accent);
  }

  .draft-kind {
    display: flex;
    gap: 0.3rem;
    margin-bottom: 0.4rem;
  }

  .draft-kind button {
    flex: 1;
    padding: 0.15rem 0.3rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.7rem;
    cursor: pointer;
  }

  .draft-kind button.active {
    color: var(--accent);
    border-color: var(--accent);
  }

  .draft-body {
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-primary);
    font: inherit;
    font-size: 0.78rem;
    padding: 0.35rem;
    margin-bottom: 0.4rem;
  }

  .draft-quote {
    margin: 0 0 0.4rem;
    padding: 0.2rem 0.5rem;
    border-left: 3px solid var(--accent);
    color: var(--text-muted);
    font-size: 0.72rem;
    max-height: 3.2rem;
    overflow: hidden;
  }

  .draft-visibility {
    display: block;
    margin-bottom: 0.4rem;
  }

  .draft-visibility select {
    width: 100%;
    padding: 0.2rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.72rem;
  }

  .draft-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.4rem;
  }

  .ann-notice {
    position: relative;
    pointer-events: auto;
    margin-bottom: 0.6rem;
    padding: 0.4rem 1.4rem 0.4rem 0.5rem;
    border: 1px solid var(--warn);
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--warn) 10%, transparent);
    font-size: 0.72rem;
    color: var(--warn);
  }

  .ann-notice button {
    position: absolute;
    top: 0.15rem;
    right: 0.3rem;
    border: 0;
    background: none;
    color: inherit;
    cursor: pointer;
    font-size: 0.9rem;
  }
</style>
