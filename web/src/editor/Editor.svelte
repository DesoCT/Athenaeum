<script lang="ts">
  /**
   * Plain-text Markdown source editor (R4).
   *
   * A textarea rather than a code-editor component: R4 asks for plain-text
   * editing, line numbers, find, undo/redo, and wrapping, and the browser
   * already provides find, undo, and redo natively on a textarea. WYSIWYG and
   * block editing are excluded by D-011.
   */
  interface Props {
    value: string;
    readOnly: boolean;
    wrap: boolean;
    /** Called on every keystroke so the caller can track dirty state. */
    onchange: (value: string) => void;
    onsave: () => void;
    /**
     * Handles a pasted or dropped file and returns the Markdown to insert at
     * the caret, or null if it was not stored (R11).
     */
    onfile?: (file: File) => Promise<string | null>;
    /** 1-based line to reveal, set when navigating from the preview. */
    revealLine?: number | null;
  }

  let { value, readOnly, wrap, onchange, onsave, onfile, revealLine = null }: Props = $props();

  let textarea: HTMLTextAreaElement | null = $state(null);
  let gutter: HTMLElement | null = $state(null);
  let cursorLine = $state(1);
  let cursorColumn = $state(1);

  const lineCount = $derived(value.split("\n").length);
  const lineNumbers = $derived(Array.from({ length: lineCount }, (_, i) => i + 1));

  function onkeydown(event: KeyboardEvent): void {
    const meta = event.metaKey || event.ctrlKey;

    if (meta && event.key.toLowerCase() === "s") {
      event.preventDefault();
      onsave();
      return;
    }

    // Tab inserts an indent rather than moving focus, which is what a source
    // editor must do. Escape still releases focus for keyboard users, so the
    // textarea is not a focus trap (spec 04 section 15).
    if (event.key === "Tab" && !event.shiftKey && !readOnly) {
      event.preventDefault();
      insert("  ");
    }
  }

  function insert(text: string): void {
    if (!textarea) return;
    const { selectionStart, selectionEnd } = textarea;
    const next = value.slice(0, selectionStart) + text + value.slice(selectionEnd);
    onchange(next);
    // Restore the caret after Svelte updates the bound value.
    queueMicrotask(() => {
      if (!textarea) return;
      textarea.selectionStart = textarea.selectionEnd = selectionStart + text.length;
    });
  }

  /** filesFrom extracts image files from a paste or drop. */
  function filesFrom(transfer: DataTransfer | null): File[] {
    if (!transfer) return [];
    return Array.from(transfer.files).filter((f) => f.type.startsWith("image/"));
  }

  async function handleFiles(files: File[]): Promise<void> {
    if (!onfile || readOnly) return;
    for (const file of files) {
      const markdown = await onfile(file);
      if (markdown) insert(markdown);
    }
  }

  function onpaste(event: ClipboardEvent): void {
    const files = filesFrom(event.clipboardData);
    if (files.length === 0) return; // Ordinary text paste is left alone.
    event.preventDefault();
    void handleFiles(files);
  }

  function ondrop(event: DragEvent): void {
    const files = filesFrom(event.dataTransfer);
    if (files.length === 0) return;
    event.preventDefault();
    void handleFiles(files);
  }

  function updateCursor(): void {
    if (!textarea) return;
    const upto = value.slice(0, textarea.selectionStart);
    const lines = upto.split("\n");
    cursorLine = lines.length;
    cursorColumn = (lines[lines.length - 1]?.length ?? 0) + 1;
  }

  /** Keep the gutter aligned with the textarea while scrolling. */
  function onscroll(): void {
    if (gutter && textarea) gutter.scrollTop = textarea.scrollTop;
  }

  $effect(() => {
    if (revealLine == null || !textarea) return;
    // Place the caret at the start of the requested line and scroll to it.
    const lines = value.split("\n");
    const clamped = Math.min(Math.max(revealLine, 1), lines.length);
    const offset = lines.slice(0, clamped - 1).reduce((sum, l) => sum + l.length + 1, 0);
    textarea.focus();
    textarea.selectionStart = textarea.selectionEnd = offset;
    // Approximate scroll: line height times line index.
    const lineHeight = parseFloat(getComputedStyle(textarea).lineHeight || "20");
    textarea.scrollTop = Math.max(0, (clamped - 3) * lineHeight);
    updateCursor();
  });
</script>

<div class="editor" class:read-only={readOnly}>
  <div class="gutter" bind:this={gutter} aria-hidden="true">
    {#each lineNumbers as n (n)}
      <span class="line-number" class:current={n === cursorLine}>{n}</span>
    {/each}
  </div>

  <textarea
    bind:this={textarea}
    {value}
    {onkeydown}
    {onscroll}
    {onpaste}
    {ondrop}
    ondragover={(e) => {
      if (onfile && !readOnly && e.dataTransfer?.types.includes("Files")) e.preventDefault();
    }}
    oninput={(e) => {
      onchange(e.currentTarget.value);
      updateCursor();
    }}
    onclick={updateCursor}
    onkeyup={updateCursor}
    readonly={readOnly}
    class:wrap
    spellcheck="false"
    autocomplete="off"
    autocapitalize="off"
    aria-label="Markdown source"
    aria-readonly={readOnly}
  ></textarea>
</div>

<div class="cursor-position" aria-live="off">
  Ln {cursorLine}, Col {cursorColumn}
</div>

<style>
  .editor {
    display: grid;
    grid-template-columns: auto 1fr;
    min-height: 0;
    height: 100%;
    background: var(--surface-panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .gutter {
    display: flex;
    flex-direction: column;
    padding: 0.75rem 0.5rem 0.75rem 0.75rem;
    background: var(--surface-table);
    border-right: 1px solid var(--line);
    color: var(--text-muted);
    font-family: var(--font-mono);
    font-size: 0.82rem;
    line-height: 1.55;
    text-align: right;
    overflow: hidden;
    user-select: none;
  }

  .line-number.current {
    color: var(--accent);
  }

  textarea {
    margin: 0;
    padding: 0.75rem 1rem;
    border: 0;
    background: transparent;
    color: var(--text-primary);
    font-family: var(--font-mono);
    font-size: 0.82rem;
    line-height: 1.55;
    resize: none;
    outline: none;
    overflow-y: auto;
    white-space: pre;
    tab-size: 2;
  }

  textarea.wrap {
    white-space: pre-wrap;
    word-break: break-word;
  }

  textarea:focus {
    /* The container shows focus, so the inner outline would double it. */
    outline: none;
  }

  .editor:focus-within {
    border-color: var(--focus);
  }

  .read-only textarea {
    color: var(--text-secondary);
  }

  .cursor-position {
    padding: 0.2rem 0.4rem;
    font-family: var(--font-mono);
    font-size: 0.7rem;
    color: var(--text-muted);
    text-align: right;
  }
</style>
