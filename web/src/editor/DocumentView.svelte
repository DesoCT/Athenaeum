<script lang="ts">
  import {
    saveDocument,
    recordRecovery,
    discardRecovery,
    storeAsset,
    ConflictError,
    AssetCollisionError,
    ApiError,
  } from "../api/client";
  import type { Capabilities, DocumentDetail } from "../api/types";
  import Editor from "./Editor.svelte";
  import ConflictView from "./ConflictView.svelte";
  import Preview from "../renderer/Preview.svelte";

  interface Props {
    document: DocumentDetail;
    capabilities: Capabilities;
    /** Re-reads the document from the server after an external change. */
    onreload: () => Promise<void>;
    /**
     * Text recovered from an unsaved buffer, seeded once when the user chose
     * to restore it. Null in every other case, so recovery is never applied
     * implicitly (acceptance E3).
     */
    restoredContent?: string | null;
    /**
     * The version now on disk, when the watcher has reported a change the
     * editor has not yet taken in. Drives the E1 and E2 split below.
     */
    diskVersion?: string | null;
    /**
     * A source line to reveal and highlight temporarily, set when the document
     * was opened from a search result (spec 04 section 8).
     */
    highlightLine?: number | null;
    /** Session state to restore for this tab (R13). */
    restoreMode?: Mode | null;
    restoreScroll?: number | null;
    restoreLine?: number | null;
    /** Reports view state so the session can record it (R13). */
    onviewstate?: (state: { mode: Mode; previewScroll: number; sourceLine: number }) => void;
    /** Reports dirty state so the tab strip can show it. */
    ondirty?: (dirty: boolean) => void;
    /**
     * Whether this tab is the visible one.
     *
     * An inactive tab renders nothing but stays mounted, so its unsaved buffer
     * survives a tab switch. Component state lives in the instance, not in the
     * DOM — and losing typed text by switching tabs would be a data-loss bug,
     * which spec 08 lists as a release blocker.
     */
    active?: boolean;
  }

  let {
    document: doc,
    capabilities,
    onreload,
    restoredContent = null,
    diskVersion = null,
    highlightLine = null,
    restoreMode = null,
    restoreScroll = null,
    restoreLine = null,
    onviewstate,
    ondirty,
    active = true,
  }: Props = $props();

  /** View modes (spec 04 section 6). Split is the default. */
  type Mode = "split" | "source" | "preview";
  /* svelte-ignore state_referenced_locally */
  let mode = $state<Mode>(restoreMode ?? "split");
  let wrap = $state(true);

  // Session bookkeeping (R13). These track the live view so the shell can
  // persist it without reaching into this component.
  /* svelte-ignore state_referenced_locally */
  let previewScroll = $state(restoreScroll ?? 0);
  /* svelte-ignore state_referenced_locally */
  let sourceLine = $state(restoreLine ?? 0);
  /* svelte-ignore state_referenced_locally */
  let pendingScroll = $state<number | null>(restoreScroll);

  // The buffer is the user's text. It is never replaced without an explicit
  // action, which is what keeps unsaved work safe (R5 step 5, R6).
  //
  // Capturing only the initial value here is deliberate: re-reading the same
  // document (after a save, say) must not overwrite what the user has typed.
  // The effect below re-seeds the buffer, but only when the document changes.
  /* svelte-ignore state_referenced_locally */
  let buffer = $state(restoredContent ?? doc.content);
  /* svelte-ignore state_referenced_locally */
  let baseVersion = $state(doc.version);
  /* svelte-ignore state_referenced_locally */
  let lastLoadedId = $state(doc.id);

  type SaveState =
    | { kind: "saved" }
    | { kind: "dirty" }
    | { kind: "saving" }
    | { kind: "failed"; message: string }
    | { kind: "conflict"; disk: string; diskVersion: string };

  /* svelte-ignore state_referenced_locally */
  let saveState = $state<SaveState>(
    restoredContent == null ? { kind: "saved" } : { kind: "dirty" },
  );
  let revealLine = $state<number | null>(null);

  /**
   * Unsaved text is mirrored to the recovery store so an abnormal exit cannot
   * lose it (R13, acceptance E3). It is debounced because this runs on every
   * keystroke, and it is never applied automatically on the way back: startup
   * offers the buffer and the user decides.
   */
  const RECOVERY_DEBOUNCE_MS = 800;
  let recoveryTimer: ReturnType<typeof setTimeout> | null = null;

  function scheduleRecovery(content: string): void {
    if (recoveryTimer) clearTimeout(recoveryTimer);
    recoveryTimer = setTimeout(() => {
      void recordRecovery(doc.id, content, baseVersion).catch(() => {
        // Recovery is a safety net, not the save path. A failure here must not
        // interrupt editing; the visible save state remains authoritative.
      });
    }, RECOVERY_DEBOUNCE_MS);
  }

  function cancelRecovery(): void {
    if (recoveryTimer) {
      clearTimeout(recoveryTimer);
      recoveryTimer = null;
    }
  }

  /**
   * The server content the buffer was last seeded from.
   *
   * Dirtiness is `buffer !== lastServerContent`, not `buffer !== doc.content`.
   * Once a reload lands, doc.content has already moved on, so comparing
   * against it would report unsaved changes the user never made.
   */
  /* svelte-ignore state_referenced_locally */
  let lastServerContent = $state(restoredContent ?? doc.content);

  /** seed replaces the buffer with server content and records what it was. */
  function seed(content: string, version: string, dirtyAfter: boolean): void {
    buffer = content;
    lastServerContent = content;
    baseVersion = version;
    saveState = dirtyAfter ? { kind: "dirty" } : { kind: "saved" };
  }

  // Two distinct cases, and conflating them was the bug behind E1.
  //
  // A different document is a full reset. The *same* document arriving with new
  // content is a reload, and the buffer must be re-seeded too — otherwise it
  // keeps the old text, dirty flips true spuriously, and changedOnDisk stays
  // permanently true so the reload effect fires forever.
  $effect(() => {
    if (doc.id !== lastLoadedId) {
      lastLoadedId = doc.id;
      seed(restoredContent ?? doc.content, doc.version, restoredContent != null);
      // A different document adopts its own restored view state, falling back
      // to the split default (spec 04 section 6, R13).
      mode = restoreMode ?? "split";
      previewScroll = restoreScroll ?? 0;
      sourceLine = restoreLine ?? 0;
      pendingScroll = restoreScroll;
      return;
    }

    // Same document, new server content. Adopt it only when there is nothing
    // of the user's to lose; a dirty buffer goes down the conflict path.
    if (doc.version !== baseVersion && buffer === lastServerContent) {
      seed(doc.content, doc.version, false);
    }
  });

  /**
   * Recovered text can arrive after this tab is already open.
   *
   * Session restoration (R13) reopens the document the user was editing, so by
   * the time they accept the recovery offer the component is mounted and the
   * mount-time seed has long since run. Without this, accepting recovery
   * silently did nothing — which acceptance E3 would read as the buffer having
   * been discarded.
   */
  let appliedRestore = $state<string | null>(null);

  $effect(() => {
    if (restoredContent == null || appliedRestore === restoredContent) return;
    appliedRestore = restoredContent;
    buffer = restoredContent;
    // Deliberately not seeded: the recovered text is unsaved by definition, so
    // lastServerContent stays at the disk content and the buffer reads dirty.
    saveState = { kind: "dirty" };
  });

  const dirty = $derived(buffer !== lastServerContent || saveState.kind === "dirty");

  $effect(() => {
    ondirty?.(dirty);
  });

  // Publish view state on every change so the shell can persist it (R13).
  $effect(() => {
    onviewstate?.({ mode, previewScroll, sourceLine });
  });

  /**
   * An external change was reported and the editor has not caught up.
   *
   * A clean editor reloads on its own with a notice; a dirty one must not, so
   * it is flagged and left alone until the user decides (R6, E1 and E2).
   */
  const changedOnDisk = $derived(diskVersion != null && diskVersion !== baseVersion);
  let reloadNotice = $state<string | null>(null);

  $effect(() => {
    if (!changedOnDisk) return;
    if (dirty || saveState.kind === "conflict" || saveState.kind === "saving") return;

    // Clean: adopt the new content and say so, without interrupting anything.
    void onreload().then(() => {
      reloadNotice = "This document changed on disk and was reloaded.";
      setTimeout(() => (reloadNotice = null), 6000);
    });
  });
  const canEdit = $derived(!doc.read_only && doc.writable);

  function onchange(next: string): void {
    buffer = next;
    if (saveState.kind !== "conflict") {
      saveState = next === doc.content ? { kind: "saved" } : { kind: "dirty" };
    }
    if (next === doc.content) {
      // Back to the saved text: there is nothing left to recover.
      cancelRecovery();
      void discardRecovery(doc.id).catch(() => {});
      return;
    }
    scheduleRecovery(next);
  }

  async function save(force = false): Promise<void> {
    if (!canEdit) return;
    saveState = { kind: "saving" };
    try {
      const result = await saveDocument(doc.id, {
        content: buffer,
        version: force ? undefined : baseVersion,
        force,
        lineEnding: doc.line_ending,
        keepBOM: doc.has_bom,
      });
      baseVersion = result.version;
      saveState = { kind: "saved" };
      // The text is on disk, so the recovery copy is no longer needed
      // (spec 03 section 8 step 9).
      cancelRecovery();
      void discardRecovery(doc.id).catch(() => {});
      // Refresh metadata (outline, size) without touching the buffer.
      await onreload();
    } catch (err) {
      if (err instanceof ConflictError) {
        // Neither version is discarded: both are handed to the comparison view.
        saveState = {
          kind: "conflict",
          disk: err.conflict.current_content,
          diskVersion: err.conflict.current_version,
        };
        return;
      }
      saveState = {
        kind: "failed",
        message: err instanceof ApiError ? `${err.code}: ${err.message}` : "The save failed.",
      };
    }
  }

  async function keepLocal(): Promise<void> {
    await save(true);
  }

  async function acceptDisk(): Promise<void> {
    if (saveState.kind !== "conflict") return;
    buffer = saveState.disk;
    baseVersion = saveState.diskVersion;
    saveState = { kind: "saved" };
    // The user explicitly chose the disk version, so their buffer is gone by
    // their decision, not by ours.
    cancelRecovery();
    void discardRecovery(doc.id).catch(() => {});
    await onreload();
  }

  /** Clicking a preview heading moves the source caret to it (R4). */
  function onHeadingClick(event: MouseEvent): void {
    const target = (event.target as HTMLElement)?.closest("[data-line]");
    if (!target) return;
    const line = Number(target.getAttribute("data-line"));
    if (!Number.isFinite(line)) return;

    // Only move the caret when the editor is actually on screen. Someone in
    // preview-only chose that deliberately, and yanking them into split view
    // to reveal a line they cannot see is a worse answer than doing nothing.
    if (mode === "preview") return;

    revealLine = line;
    // Reset so the same heading can be clicked twice.
    queueMicrotask(() => (revealLine = null));
  }

  /**
   * Stores a pasted or dropped image and returns the Markdown to insert (R11).
   *
   * A name collision is never resolved silently: the user is asked, and the
   * server only overwrites when explicitly told to (acceptance I2).
   */
  /**
   * Files dropped anywhere on the document surface are stored, including on the
   * preview and in preview-only mode where no editor exists. Binding the drop
   * to the textarea alone meant most of the target area silently ignored it.
   */
  async function onDocumentDrop(event: DragEvent): Promise<void> {
    const files = Array.from(event.dataTransfer?.files ?? []).filter((f) =>
      f.type.startsWith("image/"),
    );
    if (files.length === 0 || !canEdit) return;
    event.preventDefault();

    for (const file of files) {
      const markdown = await handleFile(file);
      if (!markdown) continue;

      // The editor inserts at the caret when it is visible. Otherwise append,
      // so a drop in preview-only mode still lands somewhere predictable
      // rather than being lost.
      if (mode === "preview") {
        const separator = buffer.endsWith("\n") ? "\n" : "\n\n";
        onchange(buffer + separator + markdown + "\n");
      } else {
        pendingInsert = markdown;
        queueMicrotask(() => (pendingInsert = null));
      }
    }
  }

  function onDocumentDragOver(event: DragEvent): void {
    if (canEdit && event.dataTransfer?.types.includes("Files")) {
      event.preventDefault();
      dragging = true;
    }
  }

  let dragging = $state(false);
  let pendingInsert = $state<string | null>(null);

  async function handleFile(file: File): Promise<string | null> {
    const bytes = new Uint8Array(await file.arrayBuffer());
    const fileName = file.name || "pasted-image.png";

    try {
      const result = await storeAsset({ documentId: doc.id, fileName, bytes });
      return result.markdown;
    } catch (err) {
      if (err instanceof AssetCollisionError) {
        const answer = window.prompt(
          `${err.message}\n\nEnter a different name, or leave "${err.suggestion}" to use that. ` +
            `Type OVERWRITE to replace the existing file.`,
          err.suggestion,
        );
        if (answer === null) return null; // Cancelled: nothing is written.

        try {
          const retry = await storeAsset({
            documentId: doc.id,
            fileName,
            bytes,
            overwrite: answer.trim().toUpperCase() === "OVERWRITE",
            preferredName: answer.trim().toUpperCase() === "OVERWRITE" ? undefined : answer.trim(),
          });
          return retry.markdown;
        } catch (retryErr) {
          assetError =
            retryErr instanceof ApiError ? retryErr.message : "The image could not be stored.";
          return null;
        }
      }
      assetError = err instanceof ApiError ? err.message : "The image could not be stored.";
      return null;
    }
  }

  let assetError = $state<string | null>(null);

  const stateLabel = $derived.by(() => {
    // read_only covers encoding and size limits; a document outside the write
    // boundary is equally uneditable and must say so (spec 04 section 7).
    if (!canEdit) return doc.read_only ? "Read-only" : "Read-only (not writable)";
    switch (saveState.kind) {
      case "saving":
        return "Saving…";
      case "failed":
        return "Save failed";
      case "conflict":
        return "Conflict";
      default:
        if (dirty && changedOnDisk) return "Changed on disk";
        return dirty ? "Unsaved changes" : "Saved";
    }
  });
</script>

{#if active}
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="document"
  class:dragging
  ondrop={(e) => {
    dragging = false;
    void onDocumentDrop(e);
  }}
  ondragover={onDocumentDragOver}
  ondragleave={() => (dragging = false)}
>
  <header class="document-header">
    <div class="identity">
      <p class="doc-title">{doc.title}</p>
      <p class="path">{doc.id}</p>
    </div>

    <div class="controls">
      <div class="modes" role="group" aria-label="View mode">
        {#each ["split", "source", "preview"] as const as m}
          <button
            type="button"
            class:active={mode === m}
            aria-pressed={mode === m}
            onclick={() => (mode = m)}
          >
            {m[0].toUpperCase() + m.slice(1)}
          </button>
        {/each}
      </div>

      <span
        class="state"
        class:dirty={dirty && saveState.kind !== "conflict"}
        class:danger={saveState.kind === "failed" || saveState.kind === "conflict"}
        class:muted={!canEdit}
        role="status"
      >
        {stateLabel}
      </span>

      {#if canEdit}
        <button
          type="button"
          class="save"
          onclick={() => save()}
          disabled={!dirty || saveState.kind === "saving"}
        >
          Save <kbd>⌘S</kbd>
        </button>
      {/if}
    </div>
  </header>

  {#if !canEdit && !doc.read_only}
    <aside class="notice" role="status">
      <p>
        <code>{doc.id}</code> is outside this workspace's write boundary, so it
        opens read-only. Add a matching pattern to <code>security.writable</code>
        in <code>athenaeum.toml</code> to edit it.
      </p>
    </aside>
  {/if}

  {#if doc.warnings && doc.warnings.length > 0}
    <aside class="doc-warnings" role="status">
      {#each doc.warnings as warning}<p>{warning}</p>{/each}
    </aside>
  {/if}

  <div class="surface" class:split={mode === "split"}>
    {#if mode !== "preview"}
      <div class="editor-pane">
        <Editor
          value={buffer}
          readOnly={!canEdit}
          {wrap}
          {onchange}
          onsave={() => save()}
          onfile={handleFile}
          {pendingInsert}
          {revealLine}
          {highlightLine}
          online={(line) => (sourceLine = line)}
        />
      </div>
    {/if}

    {#if mode !== "source"}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <div class="preview-pane" onclick={onHeadingClick}>
        <Preview
          document={{ ...doc, content: buffer }}
          {capabilities}
          {highlightLine}
          restoreScroll={pendingScroll}
          onscrollfraction={(fraction) => {
            previewScroll = fraction;
            // The restore has served its purpose once the user scrolls.
            pendingScroll = null;
          }}
        />
      </div>
    {/if}
  </div>

  {#if reloadNotice}
    <aside class="notice" role="status">
      <p>{reloadNotice}</p>
    </aside>
  {/if}

  {#if dirty && changedOnDisk && saveState.kind !== "conflict"}
    <aside class="doc-warnings" role="status">
      <p>
        This document changed on disk while you have unsaved edits. Nothing has
        been overwritten. Saving will show both versions so you can choose.
      </p>
    </aside>
  {/if}

  {#if assetError}
    <aside class="save-failed" role="alert">
      <p>{assetError}</p>
      <button type="button" class="dismiss" onclick={() => (assetError = null)}>Dismiss</button>
    </aside>
  {/if}

  {#if saveState.kind === "failed"}
    <aside class="save-failed" role="alert">
      <p>{saveState.message}</p>
      <p class="reassurance">Your text is still here and nothing was written to disk.</p>
    </aside>
  {/if}

  {#if saveState.kind === "conflict"}
    <ConflictView
      documentId={doc.id}
      local={buffer}
      disk={saveState.disk}
      onKeepLocal={keepLocal}
      onAcceptDisk={acceptDisk}
      onDismiss={() => (saveState = { kind: "dirty" })}
    />
  {/if}

  <label class="wrap-toggle">
    <input type="checkbox" bind:checked={wrap} />
    Wrap long lines
  </label>
</div>
{/if}

<style>
  .document.dragging {
    outline: 2px dashed var(--accent);
    outline-offset: -6px;
  }

  .document {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    padding: 1rem 1.25rem;
  }

  .document-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 1rem;
    margin-bottom: 0.75rem;
  }

  .doc-title {
    margin: 0;
    font-size: 1.1rem;
    font-weight: 600;
    color: var(--text-primary);
  }

  .path {
    margin: 0.1rem 0 0;
    font-family: var(--font-mono);
    font-size: 0.72rem;
    color: var(--text-muted);
  }

  .controls {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    flex-shrink: 0;
  }

  .modes {
    display: flex;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .modes button {
    padding: 0.25rem 0.6rem;
    border: 0;
    border-right: 1px solid var(--line-strong);
    background: var(--surface-panel);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.75rem;
    cursor: pointer;
  }

  .modes button:last-child {
    border-right: 0;
  }

  .modes button.active {
    background: var(--surface-raised);
    color: var(--accent);
  }

  .state {
    padding: 0.15rem 0.5rem;
    border: 1px solid var(--ok);
    border-radius: 2px;
    color: var(--ok);
    font-family: var(--font-mono);
    font-size: 0.7rem;
    white-space: nowrap;
  }

  .state.dirty {
    border-color: var(--warn);
    color: var(--warn);
  }

  .state.danger {
    border-color: var(--danger);
    color: var(--danger);
  }

  .state.muted {
    border-color: var(--line-strong);
    color: var(--text-muted);
  }

  .save {
    padding: 0.3rem 0.7rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: 0.78rem;
    cursor: pointer;
  }

  .save:disabled {
    opacity: 0.45;
    cursor: default;
  }

  kbd {
    font-family: var(--font-mono);
    font-size: 0.68rem;
    color: var(--text-muted);
  }

  .surface {
    display: grid;
    grid-template-columns: 1fr;
    gap: 1rem;
    flex: 1;
    min-height: 0;
  }

  .surface.split {
    grid-template-columns: 1fr 1fr;
  }

  .editor-pane {
    display: flex;
    flex-direction: column;
    min-height: 0;
    min-width: 0;
  }

  .preview-pane {
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
  }

  .doc-warnings,
  .save-failed {
    margin-bottom: 0.75rem;
    padding: 0.6rem 0.9rem;
    border: 1px solid var(--warn);
    border-radius: var(--radius);
    font-size: 0.85rem;
    color: var(--warn);
  }

  .save-failed {
    border-color: var(--danger);
    color: var(--danger);
  }

  .doc-warnings p,
  .save-failed p {
    margin: 0;
  }

  .notice {
    margin-bottom: 0.75rem;
    padding: 0.5rem 0.9rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    color: var(--text-secondary);
    font-size: 0.85rem;
  }

  .notice p {
    margin: 0;
  }

  .dismiss {
    margin-top: 0.4rem;
    padding: 0.2rem 0.6rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.75rem;
    cursor: pointer;
  }

  .reassurance {
    margin-top: 0.25rem !important;
    color: var(--text-secondary);
  }

  .wrap-toggle {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    margin-top: 0.5rem;
    color: var(--text-muted);
    font-size: 0.75rem;
  }

  @media (max-width: 900px) {
    .surface.split {
      grid-template-columns: 1fr;
    }
  }
</style>
