<script lang="ts">
  import {
    getWorkspace,
    listDocuments,
    getDocument,
    listRecovery,
    discardRecovery,
    subscribeToChanges,
    getIndexStatus,
    getSession,
    saveSession,
    listWorkspaces,
    openWorkspace,
    leaveWorkspace,
    ApiError,
    type RecoveryBuffer,
  } from "./api/client";
  import type { Note, NoteLink } from "./notes/types";
  import type {
    DocumentDetail,
    DocumentSummary,
    IndexStatus,
    SessionLayout,
    SessionTab,
    ViewMode,
    WorkspaceInfo,
    WorkspaceRegistry,
  } from "./api/types";
  import { buildTree } from "./map-room/tree";
  import FileTree from "./map-room/FileTree.svelte";
  import QuickOpen from "./map-room/QuickOpen.svelte";
  import MapRoomHome from "./map-room/MapRoomHome.svelte";
  import WorkspacePicker from "./map-room/WorkspacePicker.svelte";
  import DocumentView from "./editor/DocumentView.svelte";
  import NoteView from "./notes/NoteView.svelte";
  import NotesPanel from "./notes/NotesPanel.svelte";
  import RelationshipsPanel from "./relationships/RelationshipsPanel.svelte";
  import RecoveryPrompt from "./editor/RecoveryPrompt.svelte";
  import Outline from "./components/Outline.svelte";
  import StatusBar from "./components/StatusBar.svelte";
  import TabStrip from "./components/TabStrip.svelte";
  import SearchPanel from "./search/SearchPanel.svelte";

  type Load =
    | { kind: "loading" }
    | { kind: "ready" }
    | { kind: "error"; code: string; message: string };

  /**
   * The top-level screen (ADR-0004).
   *
   * "picker" and "workspace" are mutually exclusive by construction: the picker
   * is shown only when no workspace is open, and a workspace surface is shown
   * only when one is. There is no state in which both are visible, which is the
   * frontend half of the single-root guarantee — two roots are never on screen
   * at the same moment.
   */
  let screen = $state<"loading" | "picker" | "workspace">("loading");

  /** The registry, when this process supports a launcher; null otherwise. */
  let registry = $state<WorkspaceRegistry | null>(null);
  let pickerBusy = $state(false);
  let pickerError = $state<{ code: string; message: string; remedy?: string } | null>(null);

  /**
   * Incremented on every switch. The change stream and the index poller depend
   * on it, so they tear down and reconnect to the new workspace rather than
   * lingering on the one just left.
   */
  let workspaceGeneration = $state(0);

  let load = $state<Load>({ kind: "loading" });
  let workspace = $state<WorkspaceInfo | null>(null);
  let documents = $state<DocumentSummary[]>([]);

  let activeId = $state<string | null>(null);
  let docError = $state<string | null>(null);
  let quickOpenVisible = $state(false);

  /**
   * Open tabs (R13).
   *
   * A tab's document is fetched when it is first activated, so restoring a
   * dozen tabs costs one read rather than a dozen. Once loaded, the tab's
   * DocumentView stays mounted while inactive, which is what preserves an
   * unsaved buffer across a tab switch.
   */
  let openTabs = $state<string[]>([]);
  let loadedDocs = $state<Record<string, DocumentDetail>>({});
  // Open note tabs, keyed by their tab id ("note:<visibility>:<id>"). Notes and
  // documents share one tab list, distinguished by the id prefix, so the tab
  // strip, close, and switch logic serve both (R9).
  let loadedNotes = $state<Record<string, Note>>({});
  /** Which context-panel tab is showing: the outline, notes, or links. */
  let contextTab = $state<"outline" | "notes" | "links">("outline");
  /** Bumped to make the notes panel re-read after a create or delete. */
  let notesReload = $state(0);
  /** Bumped when the corpus changes, so the links panel refreshes backlinks. */
  let relationshipsGen = $state(0);
  let tabView = $state<Record<string, { mode: ViewMode; previewScroll: number; sourceLine: number }>>({});
  let dirtyDocs = $state<Record<string, boolean>>({});
  let recent = $state<string[]>([]);
  let closedTabs = $state<string[]>([]);
  let layout = $state<SessionLayout>({ navigation: true, context: true, search: false });

  /** Set when a document is opened from a search result (spec 04 section 8). */
  let highlightLine = $state<number | null>(null);
  /** Restored view state, applied once when a tab first mounts. */
  let restoring = $state<Record<string, SessionTab>>({});

  let indexStatus = $state<IndexStatus | null>(null);
  let sessionReady = $state(false);
  /**
   * Whether the user has acted since the page loaded.
   *
   * Session restoration is asynchronous; this is what stops a late restore
   * from overwriting a document the user opened while it was still in flight.
   */
  let userActed = $state(false);

  /** Tabs are capped so a session cannot grow without bound. */
  const MAX_TABS = 12;

  const activeDoc = $derived(activeId ? (loadedDocs[activeId] ?? null) : null);

  /**
   * Recording a tab's view state must be idempotent.
   *
   * DocumentView reports its view state from an effect, and an inline callback
   * prop is a new closure on every render — so assigning unconditionally makes
   * report and render feed each other forever. Comparing first turns the second
   * report into a no-op, which is what ends the cycle.
   */
  function recordViewState(
    id: string,
    view: { mode: ViewMode; previewScroll: number; sourceLine: number },
  ): void {
    const previous = tabView[id];
    if (
      previous &&
      previous.mode === view.mode &&
      previous.previewScroll === view.previewScroll &&
      previous.sourceLine === view.sourceLine
    ) {
      return;
    }
    tabView = { ...tabView, [id]: view };
  }

  function recordDirty(id: string, dirty: boolean): void {
    if (dirtyDocs[id] === dirty) return;
    dirtyDocs = { ...dirtyDocs, [id]: dirty };
  }

  const tabDescriptors = $derived(
    openTabs.map((id) =>
      isNoteTab(id)
        ? { documentId: id, title: loadedNotes[id]?.title ?? "Note", dirty: dirtyDocs[id] === true }
        : {
            documentId: id,
            title: loadedDocs[id]?.title ?? documents.find((d) => d.id === id)?.title ?? id,
            dirty: dirtyDocs[id] === true,
          },
    ),
  );

  /** A tab id names a note when it carries the note prefix. */
  function isNoteTab(id: string): boolean {
    return id.startsWith("note:");
  }

  function noteTabId(note: { visibility: string; id: string }): string {
    return `note:${note.visibility}:${note.id}`;
  }

  // Unsaved buffers found at startup. They are offered, never applied (E3).
  let recoveryBuffers = $state<RecoveryBuffer[]>([]);
  let recoveryDismissed = $state(false);
  let restored = $state<{ id: string; content: string } | null>(null);

  // Latest on-disk version per document, as reported by the watcher. The
  // document view decides what to do with it (E1 reloads, E2 flags).
  let diskVersions = $state<Record<string, string>>({});
  let expanded = $state(new SvelteSet<string>());

  // Svelte 5 needs a reactive Set; a plain Set does not trigger updates.
  import { SvelteSet } from "svelte/reactivity";

  const tree = $derived(buildTree(documents));

  /**
   * Unsaved work is always offered when it exists (acceptance E3 is a MUST,
   * while session restoration is a SHOULD — so a restored tab must not suppress
   * the offer).
   *
   * It is shown as a banner above the document surface rather than instead of
   * it: displacing the surface would unmount every open tab and take their
   * unsaved buffers with it, which is precisely the loss the prompt exists to
   * prevent.
   */
  const showRecovery = $derived(recoveryBuffers.length > 0 && !recoveryDismissed);

  /**
   * boot decides between the picker and a workspace surface (ADR-0004).
   *
   * The registry is consulted first. A process launched at the picker — no
   * path, no local athenaeum.toml — reports no active workspace, and the picker
   * is shown. A process that opened a workspace, or one built without a
   * registry at all, goes straight to the workspace surface, exactly as before.
   */
  async function boot(): Promise<void> {
    screen = "loading";
    load = { kind: "loading" };

    try {
      registry = await listWorkspaces();
    } catch {
      // A process without a registry launcher (an embedder, or a test) simply
      // has no picker. It always has a workspace, so fall through to it.
      registry = null;
    }

    if (registry && !registry.active) {
      screen = "picker";
      return;
    }
    await bootWorkspace();
  }

  /** bootWorkspace loads everything scoped to the open workspace. */
  async function bootWorkspace(): Promise<void> {
    screen = "workspace";
    load = { kind: "loading" };
    try {
      const [info, docs] = await Promise.all([getWorkspace(), listDocuments()]);
      workspace = info;
      documents = docs;
      load = { kind: "ready" };

      // Offering recovery must never block the workspace opening.
      try {
        recoveryBuffers = await listRecovery();
      } catch {
        recoveryBuffers = [];
      }
      // Expand the first level so the tree is not a wall of closed folders.
      for (const node of buildTree(docs)) {
        if (node.kind === "directory") expanded.add(node.path);
      }

      await restoreSession();
    } catch (err) {
      load =
        err instanceof ApiError
          ? { kind: "error", code: err.code, message: err.message }
          : {
              kind: "error",
              code: "NETWORK_UNAVAILABLE",
              message: "The Athenaeum process is not reachable from this page.",
            };
    }
  }

  /**
   * resetWorkspaceState discards everything belonging to the workspace being
   * left.
   *
   * This is the frontend half of a total switch. The server has already
   * unloaded the previous workspace's services; leaving its documents, tabs,
   * recents, or unsaved buffers in the UI would let one workspace's content
   * surface while another is open — exactly what ADR-0004 forbids. Every
   * per-workspace store is cleared here.
   */
  function resetWorkspaceState(): void {
    if (sessionTimer) {
      clearTimeout(sessionTimer);
      sessionTimer = null;
    }
    workspace = null;
    documents = [];
    activeId = null;
    docError = null;
    highlightLine = null;
    quickOpenVisible = false;
    openTabs = [];
    loadedDocs = {};
    tabView = {};
    dirtyDocs = {};
    recent = [];
    closedTabs = [];
    restoring = {};
    diskVersions = {};
    recoveryBuffers = [];
    recoveryDismissed = false;
    restored = null;
    indexStatus = null;
    expanded = new SvelteSet<string>();
    layout = { navigation: true, context: true, search: false };
    // Persisting is disabled until the next workspace has restored its own
    // session, so an empty state cannot overwrite a real one.
    sessionReady = false;
    userActed = false;
  }

  /** chooseWorkspace opens a registry entry, unloading the current one first. */
  async function chooseWorkspace(name: string): Promise<void> {
    pickerBusy = true;
    pickerError = null;
    try {
      await openWorkspace(name);
      resetWorkspaceState();
      // Reconnect the live streams to the new workspace.
      workspaceGeneration += 1;
      await bootWorkspace();
    } catch (err) {
      pickerError =
        err instanceof ApiError
          ? { code: err.code, message: err.message, remedy: err.details?.remedy }
          : { code: "NETWORK_UNAVAILABLE", message: "The workspace could not be opened." };
      // A failed open leaves the process where it was — at the picker — so
      // refresh the list in case the failure changed what is available.
      await refreshRegistry();
    } finally {
      pickerBusy = false;
    }
  }

  /** backToPicker leaves the open workspace and returns to the registry. */
  async function backToPicker(): Promise<void> {
    if (!registry) return;
    try {
      await leaveWorkspace();
    } catch {
      // Even a failure to close cleanly should not trap the user in the
      // workspace; the server treats a repeated leave as a no-op.
    }
    resetWorkspaceState();
    workspaceGeneration += 1;
    await refreshRegistry();
    screen = "picker";
  }

  /** refreshRegistry re-reads the hand-edited registry file. */
  async function refreshRegistry(): Promise<void> {
    try {
      registry = await listWorkspaces();
    } catch {
      // Keep the last known registry rather than blanking the picker.
    }
  }

  /**
   * restoreSession reopens the previous session (R13).
   *
   * The server has already dropped any tab naming a document the workspace no
   * longer includes, so nothing here can resurrect an excluded file.
   */
  async function restoreSession(): Promise<void> {
    try {
      const state = await getSession();

      // Restoration is asynchronous, and the user can act before it lands —
      // opening a document during startup is entirely normal. Applying the
      // stored session unconditionally at that point wipes the tab they just
      // opened, so what they are doing now wins and the stored session is
      // simply superseded.
      const superseded = userActed;

      // Recent documents merge either way: it is a history, not a view.
      const merged = [...recent, ...(state.recent ?? [])];
      recent = merged.filter((id, index) => merged.indexOf(id) === index).slice(0, 20);

      if (superseded) return;

      layout = state.layout ?? layout;
      openTabs = state.tabs.map((tab) => tab.document_id).slice(0, MAX_TABS);

      const byId: Record<string, SessionTab> = {};
      for (const tab of state.tabs) byId[tab.document_id] = tab;
      restoring = byId;

      // Only the active document is fetched now; the rest load on activation.
      if (state.active_document && openTabs.includes(state.active_document)) {
        await open(state.active_document, undefined, false);
      }
    } catch {
      // A session that cannot be restored costs the layout, never a document.
    } finally {
      // Persisting is enabled only after restoration, or the first debounced
      // save would overwrite the stored session with the empty initial state.
      sessionReady = true;
    }
  }

  /** open activates a document, adding a tab for it if it has none. */
  async function open(id: string, line?: number, record = true): Promise<void> {
    activeId = id;
    docError = null;
    highlightLine = null;

    if (!openTabs.includes(id)) {
      openTabs = [...openTabs, id].slice(-MAX_TABS);
    }
    if (record) {
      // record is false only for the restore itself, which is not a user act.
      userActed = true;
      recent = [id, ...recent.filter((entry) => entry !== id)].slice(0, 20);
    }

    if (!loadedDocs[id]) {
      try {
        loadedDocs = { ...loadedDocs, [id]: await getDocument(id) };
      } catch (err) {
        openTabs = openTabs.filter((entry) => entry !== id);
        docError =
          err instanceof ApiError
            ? `${err.code}: ${err.message}`
            : "The document could not be read.";
        return;
      }
    }

    if (line != null && line > 0) {
      // Cleared and re-set so opening the same result twice highlights again.
      highlightLine = null;
      queueMicrotask(() => (highlightLine = line));
    }
  }

  /** activateTab switches to a tab, fetching a document tab's content lazily. */
  function activateTab(id: string): void {
    if (isNoteTab(id)) {
      userActed = true;
      activeId = id;
      docError = null;
      return;
    }
    void open(id);
  }

  /** openNoteTab opens a note in the main surface, reusing the tab system. */
  function openNoteTab(note: Note): void {
    userActed = true;
    const id = noteTabId(note);
    loadedNotes = { ...loadedNotes, [id]: note };
    if (!openTabs.includes(id)) {
      openTabs = [...openTabs, id].slice(-MAX_TABS);
    }
    activeId = id;
    docError = null;
  }

  /**
   * openLink follows a note's typed link to a document, landing on the linked
   * heading when there is one (acceptance G4). The heading string is matched
   * against the authoritative outline (ADR-0003), so navigation lands on the
   * real source line rather than a guess.
   */
  async function openLink(link: NoteLink): Promise<void> {
    if (!link.document) return;
    let line: number | undefined;
    if (link.heading) {
      try {
        const doc = loadedDocs[link.document] ?? (await getDocument(link.document));
        const heading = doc.outline.find(
          (h) => h.text === link.heading || h.slug === link.heading || h.path.at(-1) === link.heading,
        );
        if (heading) line = heading.line;
      } catch {
        // A link to a document that cannot be read still opens the document,
        // which surfaces the real error there rather than swallowing it here.
      }
    }
    await open(link.document, line);
  }

  function closeTab(id: string): void {
    userActed = true;
    openTabs = openTabs.filter((entry) => entry !== id);
    // Only documents are reopenable from history; a note reopens from its panel,
    // and reopening a note id through the document path would try to fetch it as
    // a document.
    if (!isNoteTab(id)) {
      closedTabs = [id, ...closedTabs.filter((entry) => entry !== id)].slice(0, 10);
    }

    // Dropping the loaded document releases its buffer. That is safe only
    // because closing is an explicit action and the recovery store already
    // holds anything unsaved (acceptance E3).
    const { [id]: _dropped, ...rest } = loadedDocs;
    loadedDocs = rest;
    const { [id]: _note, ...restNotes } = loadedNotes;
    loadedNotes = restNotes;
    delete dirtyDocs[id];
    delete tabView[id];
    // The restored view state has served its purpose. Keeping it would make a
    // tab reopened later in the same session jump to the scroll position it
    // had in the *previous* one, which reads as the interface losing its place.
    delete restoring[id];

    if (activeId === id) {
      activeId = openTabs[openTabs.length - 1] ?? null;
      docError = null;
    }
  }

  function reopenClosedTab(): void {
    const id = closedTabs[0];
    if (!id) return;
    closedTabs = closedTabs.slice(1);
    void open(id);
  }

  function showSearch(): void {
    userActed = true;
    layout = { ...layout, search: true, navigation: true };
  }

  function showTree(): void {
    userActed = true;
    layout = { ...layout, search: false };
  }

  function onkeydown(event: KeyboardEvent): void {
    const meta = event.metaKey || event.ctrlKey;
    if (!meta) return;
    const key = event.key.toLowerCase();

    // Spec 04 section 14.
    if (key === "p" && !event.shiftKey) {
      event.preventDefault();
      quickOpenVisible = true;
      return;
    }
    if (key === "f" && event.shiftKey) {
      event.preventDefault();
      showSearch();
      return;
    }
    if (key === "w" && !event.shiftKey) {
      event.preventDefault();
      if (activeId) closeTab(activeId);
      return;
    }
    if (key === "t" && event.shiftKey) {
      event.preventDefault();
      reopenClosedTab();
    }
  }

  /**
   * Session state is persisted debounced, and only after restoration, so a
   * transient empty state never overwrites a real one.
   */
  const SESSION_DEBOUNCE_MS = 400;
  let sessionTimer: ReturnType<typeof setTimeout> | null = null;

  function persistSession(): void {
    if (sessionTimer) clearTimeout(sessionTimer);
    sessionTimer = setTimeout(() => {
      void saveSession({
        schema_version: 1,
        // Only documents are restored across sessions; note tabs reopen from the
        // notes panel, and a note id is not a document the restore path can read.
        tabs: openTabs
          .filter((id) => !isNoteTab(id))
          .map((id) => ({
            document_id: id,
            mode: tabView[id]?.mode ?? "split",
            preview_scroll: tabView[id]?.previewScroll ?? 0,
            source_line: tabView[id]?.sourceLine ?? 0,
          })),
        active_document: activeId && !isNoteTab(activeId) ? activeId : undefined,
        recent,
        layout,
      });
    }, SESSION_DEBOUNCE_MS);
  }

  $effect(() => {
    // Referenced so the effect re-runs when any of them changes.
    void openTabs;
    void activeId;
    void recent;
    void layout;
    void tabView;
    if (!sessionReady) return;
    persistSession();
  });

  /**
   * Index status polling.
   *
   * Faster while the projection is catching up, so "rebuilding" and "stale"
   * clear promptly, and slower once it is settled so an idle workspace is not
   * polled needlessly.
   */
  $effect(() => {
    // Re-run when the workspace changes, so the poller follows the switch.
    void workspaceGeneration;
    if (screen !== "workspace") return;

    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const poll = async () => {
      // The next interval is chosen from the value just fetched, not from the
      // reactive store, so this loop never reads state it also writes.
      let busy = false;
      try {
        const status = await getIndexStatus();
        busy = status.state === "building" || status.state === "rebuilding";
        if (!stopped) indexStatus = status;
      } catch {
        // The status bar keeps its last known value rather than flickering.
      }
      if (stopped) return;
      timer = setTimeout(() => void poll(), busy ? 700 : 5000);
    };

    void poll();
    return () => {
      stopped = true;
      if (timer) clearTimeout(timer);
    };
  });

  $effect(() => {
    void boot();
  });

  // Live change notifications. The stream is advisory: losing it costs
  // freshness, never correctness. It is scoped to the open workspace and
  // reconnects on a switch, so it never carries one workspace's changes into
  // another (ADR-0004).
  $effect(() => {
    void workspaceGeneration;
    if (screen !== "workspace") return;

    const unsubscribe = subscribeToChanges((changes) => {
      const next = { ...diskVersions };
      let treeStale = false;

      for (const change of changes) {
        if (change.kind === "removed" || change.kind === "created") treeStale = true;
        if (change.version) next[change.document_id] = change.version;
      }
      diskVersions = next;

      // Any change can alter links or backlinks, so let the links panel refresh.
      if (changes.length > 0) relationshipsGen += 1;

      // A creation or removal changes the tree, so re-list quietly.
      if (treeStale) {
        void listDocuments()
          .then((docs) => (documents = docs))
          .catch(() => {});
      }
    });
    return unsubscribe;
  });
</script>

<svelte:window {onkeydown} />

{#if screen === "picker" && registry}
  <div class="shell picker-shell">
    <header class="command-bar">
      <div class="identity">
        <span class="product">Athenaeum</span>
        <span class="separator" aria-hidden="true">/</span>
        <span class="workspace muted">select a workspace</span>
      </div>
    </header>
    <div class="picker-body">
      <WorkspacePicker
        {registry}
        busy={pickerBusy}
        error={pickerError}
        onopen={(name) => void chooseWorkspace(name)}
        onreload={() => void refreshRegistry()}
      />
    </div>
  </div>
{:else}
<div class="shell">
  <header class="command-bar">
    <div class="identity">
      <span class="product">Athenaeum</span>
      <span class="separator" aria-hidden="true">/</span>
      <span class="workspace">{workspace?.name ?? "—"}</span>
    </div>

    <div class="bar-actions">
      {#if registry}
        <!-- The way back to the picker (ADR-0004). Present only when a registry
             launcher exists; a process launched with a bare path has nowhere to
             return to. -->
        <button type="button" class="quick-open-trigger" onclick={() => void backToPicker()}>
          Workspaces
        </button>
      {/if}
      <button type="button" class="quick-open-trigger" onclick={() => (quickOpenVisible = true)}>
        Quick open <kbd>⌘P</kbd>
      </button>
      <button type="button" class="quick-open-trigger" onclick={showSearch}>
        Search <kbd>⌘⇧F</kbd>
      </button>
    </div>
  </header>

  <div class="body">
    <nav class="panel navigation" aria-label="Workspace navigation">
      <div class="nav-switch" role="group" aria-label="Navigation view">
        <button
          type="button"
          class:active={!layout.search}
          aria-pressed={!layout.search}
          onclick={showTree}
        >
          Documents
        </button>
        <button
          type="button"
          class:active={layout.search}
          aria-pressed={layout.search}
          onclick={showSearch}
        >
          Search
        </button>
      </div>

      {#if layout.search}
        <SearchPanel
          groups={workspace?.groups ?? []}
          status={indexStatus}
          onopen={(id, line) => void open(id, line)}
          onstatuschange={(status) => (indexStatus = status)}
        />
      {:else if load.kind === "ready"}
        {#if documents.length === 0}
          <p class="pending">No documents match the configured include patterns.</p>
        {:else}
          <FileTree
            nodes={tree}
            {activeId}
            onopen={(id) => void open(id)}
            {expanded}
            ontoggle={(path) => {
              if (expanded.has(path)) expanded.delete(path);
              else expanded.add(path);
            }}
          />
        {/if}
      {:else}
        <p class="pending">Loading…</p>
      {/if}
    </nav>

    <main class="document-surface" aria-label="Document surface">
      <TabStrip
        tabs={tabDescriptors}
        {activeId}
        onselect={activateTab}
        onclose={closeTab}
      />
      {#if load.kind === "error"}
        <section class="card error">
          <h1>Workspace unavailable</h1>
          <p class="code">{load.code}</p>
          <p>{load.message}</p>
          <button type="button" onclick={boot}>Retry</button>
        </section>
      {:else if workspace}
        <!--
          Transient surfaces are siblings of the tab list, never alternatives to
          it. Putting them in one mutually exclusive chain destroyed every open
          DocumentView whenever the active document was momentarily absent —
          which happens on every tab switch, while the next document loads — and
          took the unsaved buffers with it.
        -->
        {#if docError}
          <section class="card error">
            <h1>Document unavailable</h1>
            <p class="code">{docError}</p>
            <button type="button" onclick={() => (docError = null)}>Dismiss</button>
          </section>
        {/if}

        {#if showRecovery}
          <RecoveryPrompt
            buffers={recoveryBuffers}
            onRestore={async (buffer) => {
              restored = { id: buffer.document_id, content: buffer.content };
              recoveryDismissed = true;
              await open(buffer.document_id);
            }}
            onDiscard={async (buffer) => {
              await discardRecovery(buffer.document_id);
              recoveryBuffers = recoveryBuffers.filter(
                (b) => b.document_id !== buffer.document_id,
              );
            }}
            onDismiss={() => (recoveryDismissed = true)}
          />
        {/if}

        <!-- Every loaded tab stays mounted; only the active one renders, so an
             unsaved buffer survives a tab switch. -->
        {#each openTabs as id (id)}
          {#if isNoteTab(id) && loadedNotes[id]}
            <NoteView
              note={loadedNotes[id]}
              capabilities={workspace.capabilities}
              active={id === activeId && !docError}
              onopenlink={(link) => void openLink(link)}
              ondirty={(dirty) => recordDirty(id, dirty)}
              onclosed={() => {
                closeTab(id);
                notesReload += 1;
              }}
            />
          {:else if loadedDocs[id]}
            <DocumentView
              document={loadedDocs[id]}
              capabilities={workspace.capabilities}
              active={id === activeId && !docError}
              restoredContent={restored?.id === id ? restored.content : null}
              diskVersion={diskVersions[id] ?? null}
              highlightLine={id === activeId ? highlightLine : null}
              restoreMode={restoring[id]?.mode ?? null}
              restoreScroll={restoring[id]?.preview_scroll ?? null}
              restoreLine={restoring[id]?.source_line ?? null}
              onviewstate={(view) => recordViewState(id, view)}
              ondirty={(dirty) => recordDirty(id, dirty)}
              onreload={async () => {
                loadedDocs = { ...loadedDocs, [id]: await getDocument(id) };
              }}
            />
          {/if}
        {/each}

        {#if openTabs.length === 0 && !docError}
          <MapRoomHome {workspace} {documents} {recent} onopen={(id) => void open(id)} />
        {/if}
      {/if}
    </main>

    {#if layout.context}
      <aside class="panel context" aria-label="Context panel">
        <div class="nav-switch" role="group" aria-label="Context view">
          <button
            type="button"
            class:active={contextTab === "outline"}
            aria-pressed={contextTab === "outline"}
            onclick={() => (contextTab = "outline")}
          >
            Outline
          </button>
          <button
            type="button"
            class:active={contextTab === "notes"}
            aria-pressed={contextTab === "notes"}
            onclick={() => (contextTab = "notes")}
          >
            Notes
          </button>
          <button
            type="button"
            class:active={contextTab === "links"}
            aria-pressed={contextTab === "links"}
            onclick={() => (contextTab = "links")}
          >
            Links
          </button>
        </div>

        {#if contextTab === "outline"}
          {#if activeDoc}
            <Outline outline={activeDoc.outline} />
          {:else}
            <p class="pending">Open a document to see its outline.</p>
          {/if}
        {:else if contextTab === "notes"}
          <NotesPanel
            {documents}
            generation={workspaceGeneration + notesReload}
            activeId={activeId && isNoteTab(activeId) ? activeId.split(":").slice(2).join(":") : null}
            onopen={openNoteTab}
            onopenlink={(link) => void openLink(link)}
          />
        {:else}
          <RelationshipsPanel
            documentId={activeId && !isNoteTab(activeId) ? activeId : null}
            generation={workspaceGeneration + relationshipsGen}
            onopen={(id) => void open(id)}
          />
        {/if}
      </aside>
    {/if}
  </div>

  <StatusBar {workspace} document={activeDoc} state={load.kind} index={indexStatus} />
</div>
{/if}

{#if quickOpenVisible}
  <QuickOpen
    {documents}
    onopen={(id) => void open(id)}
    onclose={() => (quickOpenVisible = false)}
  />
{/if}

<style>
  .shell {
    display: grid;
    grid-template-rows: auto 1fr auto;
    height: 100%;
  }

  .command-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--gap);
    padding: 0.6rem 1rem;
    border-bottom: 1px solid var(--line);
    background: var(--surface-panel);
  }

  .identity {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
  }

  .product {
    font-weight: 600;
    letter-spacing: 0.04em;
    color: var(--accent);
  }

  .separator {
    color: var(--text-muted);
  }

  .workspace {
    font-family: var(--font-mono);
  }

  .bar-actions {
    display: flex;
    gap: 0.4rem;
  }

  .nav-switch {
    display: flex;
    margin: 0 0.5rem 0.6rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .nav-switch button {
    flex: 1;
    padding: 0.25rem 0.4rem;
    border: 0;
    border-right: 1px solid var(--line-strong);
    border-radius: 0;
    background: var(--surface-panel);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.7rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    cursor: pointer;
  }

  .nav-switch button:last-child {
    border-right: 0;
  }

  .nav-switch button.active {
    background: var(--surface-raised);
    color: var(--accent);
  }

  .quick-open-trigger {
    padding: 0.25rem 0.7rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font: inherit;
    font-size: 0.8rem;
    cursor: pointer;
  }

  .quick-open-trigger:hover {
    border-color: var(--focus);
    color: var(--text-primary);
  }

  kbd {
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: var(--text-muted);
  }

  .body {
    display: grid;
    grid-template-columns: 17rem 1fr 16rem;
    min-height: 0;
  }

  .panel {
    padding: 1rem 0.5rem;
    background: var(--surface-panel);
    overflow-y: auto;
  }

  .navigation {
    display: flex;
    flex-direction: column;
    min-height: 0;
    border-right: 1px solid var(--line);
  }

  .context {
    border-left: 1px solid var(--line);
    padding: 1rem;
  }

  .pending {
    margin: 0 0.5rem;
    color: var(--text-muted);
    font-size: 0.85rem;
  }

  .document-surface {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    overflow-y: auto;
    background-color: var(--surface-table);
    background-image:
      linear-gradient(var(--line) 1px, transparent 1px),
      linear-gradient(90deg, var(--line) 1px, transparent 1px);
    background-size: 48px 48px;
    background-position: -1px -1px;
  }


  

  

  

  

  

  

  

  

  

  .card {
    max-width: 40rem;
    margin: 2rem;
    padding: 1.5rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
  }

  .card.error {
    border-color: var(--danger);
  }

  .card h1 {
    margin: 0 0 0.5rem;
    font-size: 1.3rem;
  }

  .code {
    font-family: var(--font-mono);
    font-size: 0.8rem;
    color: var(--danger);
  }

  button {
    padding: 0.4rem 0.9rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-panel);
    color: var(--text-primary);
    font: inherit;
    cursor: pointer;
  }

  @media (max-width: 900px) {
    .body {
      grid-template-columns: 1fr;
    }

    .navigation,
    .context {
      display: none;
    }
  }
</style>
