import type {
  ActiveWorkspace,
  ApiErrorBody,
  ConflictInfo,
  SaveResult,
  DocumentDetail,
  DocumentSummary,
  Health,
  IndexStatus,
  SearchFilters,
  SearchResponse,
  SessionState,
  WorkspaceInfo,
  WorkspaceRegistry,
} from "./types";
import type {
  Annotation,
  AnnotationList,
  AnnotationStatus,
  CreateAnnotationInput,
} from "../annotations/types";
import type { Note, NoteSummary, NoteLink, CreateNoteInput } from "../notes/types";

const API_PREFIX = "/api/v1";

/**
 * ApiError carries the server's stable error code so the UI can branch on it
 * without parsing prose (requirement N6).
 */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  readonly details?: Record<string, string>;

  constructor(status: number, body: ApiErrorBody) {
    super(body.message);
    this.name = "ApiError";
    this.status = status;
    this.code = body.code;
    this.details = body.details;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`${API_PREFIX}${path}`, {
    ...init,
    // The session cookie is HttpOnly, so it must be sent explicitly.
    credentials: "same-origin",
    headers: { Accept: "application/json", ...init.headers },
  });

  if (!response.ok) {
    throw new ApiError(response.status, await readError(response));
  }
  return (await response.json()) as T;
}

async function readError(response: Response): Promise<ApiErrorBody> {
  try {
    const body = (await response.json()) as { error?: ApiErrorBody };
    if (body.error?.code) {
      return body.error;
    }
  } catch {
    // Fall through to a synthesised error below.
  }
  return {
    code: `HTTP_${response.status}`,
    message: response.statusText || "The request failed.",
  };
}

export function getHealth(): Promise<Health> {
  return request<Health>("/health");
}

export function getWorkspace(): Promise<WorkspaceInfo> {
  return request<WorkspaceInfo>("/workspace");
}

/**
 * listWorkspaces reads the workspace registry (ADR-0004).
 *
 * The registry is re-read on every call because it is hand-edited; a user who
 * adds an entry expects to see it without restarting (C8). A process started
 * without a registry answers 404, which the picker treats as "no registry"
 * rather than an error.
 */
export function listWorkspaces(): Promise<WorkspaceRegistry> {
  return request<WorkspaceRegistry>("/workspaces");
}

/**
 * openWorkspace switches the process to a registered workspace.
 *
 * The previous workspace is fully unloaded first: search, the tree, the write
 * boundary, and every other service are rebuilt for the new root, and at no
 * moment are two roots loaded (ADR-0004). The caller reloads its own state
 * afterwards, because everything it holds belonged to the workspace just left.
 */
export async function openWorkspace(name: string): Promise<ActiveWorkspace | null> {
  const response = await fetch(`${API_PREFIX}/workspaces/open`, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({ name }),
  });
  if (!response.ok) {
    throw new ApiError(response.status, await readError(response));
  }
  const body = (await response.json()) as { active: ActiveWorkspace | null };
  return body.active;
}

/** leaveWorkspace returns the process to the picker (ADR-0004). */
export async function leaveWorkspace(): Promise<void> {
  const response = await fetch(`${API_PREFIX}/workspaces/leave`, {
    method: "POST",
    credentials: "same-origin",
    headers: { Accept: "application/json" },
  });
  if (!response.ok) {
    throw new ApiError(response.status, await readError(response));
  }
}

export async function listDocuments(): Promise<DocumentSummary[]> {
  const body = await request<{ documents: DocumentSummary[] }>("/documents");
  return body.documents;
}

/**
 * getDocument reads one document.
 *
 * The ID is encoded per path segment: it contains slashes that must stay
 * meaningful, but any other character needs escaping. The server rejects
 * traversal on the raw path regardless.
 */
export function getDocument(id: string): Promise<DocumentDetail> {
  return request<DocumentDetail>(`/documents/${encodePath(id)}`);
}

function encodePath(id: string): string {
  return id.split("/").map(encodeURIComponent).join("/");
}

/**
 * ConflictError is raised when the file changed on disk under an unsaved
 * buffer. It carries the disk version so the comparison view needs no second
 * request that could race again (R6).
 */
export class ConflictError extends ApiError {
  readonly conflict: ConflictInfo;

  constructor(status: number, body: ApiErrorBody, conflict: ConflictInfo) {
    super(status, body);
    this.name = "ConflictError";
    this.conflict = conflict;
  }
}

export interface SaveOptions {
  content: string;
  /** The version the editor last observed. Omitted only when forcing. */
  version?: string;
  /** Set only after the user chose "keep my version" in the conflict view. */
  force?: boolean;
  lineEnding?: string;
  keepBOM?: boolean;
}

export interface RecoveryBuffer {
  document_id: string;
  content: string;
  base_version: string;
  updated_at: string;
}

export async function listRecovery(): Promise<RecoveryBuffer[]> {
  const body = await request<{ buffers: RecoveryBuffer[] }>("/recovery");
  return body.buffers ?? [];
}

/** recordRecovery preserves an unsaved buffer against an abnormal exit (E3). */
export async function recordRecovery(
  documentId: string,
  content: string,
  baseVersion: string,
): Promise<void> {
  await fetch(`${API_PREFIX}/recovery`, {
    method: "PUT",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      document_id: documentId,
      content,
      base_version: baseVersion,
    }),
  });
}

/** discardRecovery removes a buffer. Only an explicit action calls this. */
export async function discardRecovery(documentId: string): Promise<void> {
  const encoded = documentId.split("/").map(encodeURIComponent).join("/");
  await fetch(`${API_PREFIX}/recovery/${encoded}`, {
    method: "DELETE",
    credentials: "same-origin",
  });
}

export interface DocumentChange {
  document_id: string;
  kind: "modified" | "created" | "removed";
  version?: string;
}

/**
 * subscribeToChanges opens the server-sent event stream.
 *
 * The stream only makes the UI live. Correctness never depends on it: a missed
 * event is caught by the version check on the next read or save.
 */
export function subscribeToChanges(
  onChanges: (changes: DocumentChange[]) => void,
): () => void {
  const source = new EventSource(`${API_PREFIX}/events`, { withCredentials: true });

  source.addEventListener("documents", (event) => {
    try {
      onChanges(JSON.parse((event as MessageEvent).data) as DocumentChange[]);
    } catch {
      // A malformed frame is ignored rather than breaking the stream.
    }
  });

  // EventSource reconnects on its own; nothing to do but avoid noise.
  source.onerror = () => {};

  return () => source.close();
}

export interface AssetResult {
  asset_id: string;
  markdown: string;
  relative_path: string;
  size: number;
}

/** AssetCollisionError carries a suggested free name (acceptance I2). */
export class AssetCollisionError extends ApiError {
  readonly suggestion: string;

  constructor(body: ApiErrorBody, suggestion: string) {
    super(409, body);
    this.name = "AssetCollisionError";
    this.suggestion = suggestion;
  }
}

export interface StoreAssetOptions {
  documentId: string;
  fileName: string;
  /** Raw bytes; encoded to base64 for transport. */
  bytes: Uint8Array;
  overwrite?: boolean;
  preferredName?: string;
}

export async function storeAsset(options: StoreAssetOptions): Promise<AssetResult> {
  const response = await fetch(`${API_PREFIX}/assets`, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({
      document_id: options.documentId,
      file_name: options.fileName,
      content: toBase64(options.bytes),
      overwrite: options.overwrite ?? false,
      preferred_name: options.preferredName ?? "",
    }),
  });

  if (response.status === 409) {
    const body = (await response.json()) as { error: ApiErrorBody; suggestion: string };
    throw new AssetCollisionError(body.error, body.suggestion);
  }
  if (!response.ok) {
    throw new ApiError(response.status, await readError(response));
  }
  return (await response.json()) as AssetResult;
}

/** toBase64 encodes bytes in chunks, so a large image cannot blow the stack. */
function toBase64(bytes: Uint8Array): string {
  const CHUNK = 0x8000;
  let binary = "";
  for (let i = 0; i < bytes.length; i += CHUNK) {
    binary += String.fromCharCode(...bytes.subarray(i, i + CHUNK));
  }
  return btoa(binary);
}

/**
 * searchWorkspace runs a workspace search (R7).
 *
 * A malformed query returns a stable 400 code rather than a fault, so the panel
 * can explain the problem instead of showing a failure.
 */
export function searchWorkspace(
  query: string,
  filters: SearchFilters = {},
  limit = 25,
  signal?: AbortSignal,
): Promise<SearchResponse> {
  const params = new URLSearchParams({ q: query, limit: String(limit) });
  if (filters.path) params.set("path", filters.path);
  if (filters.group) params.set("group", filters.group);
  if (filters.git) params.set("git", filters.git);
  return request<SearchResponse>(`/search?${params.toString()}`, { signal });
}

export function getIndexStatus(): Promise<IndexStatus> {
  return request<IndexStatus>("/search/status");
}

/** rebuildIndex re-examines every document (spec 04 section 4.3). */
export async function rebuildIndex(): Promise<IndexStatus> {
  const response = await fetch(`${API_PREFIX}/search/rebuild`, {
    method: "POST",
    credentials: "same-origin",
    headers: { Accept: "application/json" },
  });
  if (!response.ok) {
    throw new ApiError(response.status, await readError(response));
  }
  return (await response.json()) as IndexStatus;
}

/** getSession reads the restorable UI state (R13). */
export function getSession(): Promise<SessionState> {
  return request<SessionState>("/session");
}

/**
 * saveSession records the UI state.
 *
 * Session state is disposable, so a failure is swallowed: losing a layout must
 * never interrupt what the user is doing.
 */
export async function saveSession(state: SessionState): Promise<void> {
  try {
    await fetch(`${API_PREFIX}/session`, {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(state),
    });
  } catch {
    // Deliberately ignored; see the note above.
  }
}

export async function saveDocument(id: string, options: SaveOptions): Promise<SaveResult> {
  const response = await fetch(`${API_PREFIX}/documents/${encodePath(id)}`, {
    method: "PUT",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({
      content: options.content,
      version: options.version ?? "",
      force: options.force ?? false,
      line_ending: options.lineEnding ?? "",
      keep_bom: options.keepBOM ?? false,
    }),
  });

  if (response.status === 409) {
    const body = (await response.json()) as { error: ApiErrorBody; conflict: ConflictInfo };
    throw new ConflictError(409, body.error, body.conflict);
  }
  if (!response.ok) {
    throw new ApiError(response.status, await readError(response));
  }
  return (await response.json()) as SaveResult;
}

/**
 * AnnotationConflictError is raised when a sidecar changed since it was last
 * read (R8). It carries the current revision and annotations so the caller can
 * reconcile without a second request that could race again, mirroring the
 * document conflict path.
 */
export class AnnotationConflictError extends ApiError {
  readonly visibility: string;
  readonly currentRevision: number;
  readonly current: Annotation[];

  constructor(body: ApiErrorBody, conflict: AnnotationConflict) {
    super(409, body);
    this.name = "AnnotationConflictError";
    this.visibility = conflict.visibility;
    this.currentRevision = conflict.current_revision;
    this.current = conflict.current;
  }
}

interface AnnotationConflict {
  visibility: string;
  current_revision: number;
  current: Annotation[];
}

interface AnnotationResult {
  annotation: Annotation;
  revision: number;
}

/** listAnnotations reads the merged personal and shared annotations (R8). */
export function listAnnotations(documentId: string): Promise<AnnotationList> {
  return request<AnnotationList>(`/annotations?document=${encodePath(documentId)}`);
}

export async function createAnnotation(input: CreateAnnotationInput): Promise<AnnotationResult> {
  return annotationWrite("POST", "/annotations", input);
}

export async function updateAnnotation(
  id: string,
  patch: {
    document_id: string;
    visibility: string;
    expected_revision: number;
    body?: string;
    status?: AnnotationStatus;
  },
): Promise<AnnotationResult> {
  return annotationWrite("PATCH", `/annotations/${encodeURIComponent(id)}`, patch);
}

export async function deleteAnnotation(
  id: string,
  patch: { document_id: string; visibility: string; expected_revision: number },
): Promise<{ revision: number }> {
  return annotationWrite("DELETE", `/annotations/${encodeURIComponent(id)}`, patch);
}

/** annotationWrite performs a mutating annotation request, surfacing conflicts. */
async function annotationWrite<T>(method: string, path: string, body: unknown): Promise<T> {
  const response = await fetch(`${API_PREFIX}${path}`, {
    method,
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(body),
  });
  if (response.status === 409) {
    const parsed = (await response.json()) as { error: ApiErrorBody; conflict: AnnotationConflict };
    throw new AnnotationConflictError(parsed.error, parsed.conflict);
  }
  if (!response.ok) {
    throw new ApiError(response.status, await readError(response));
  }
  return (await response.json()) as T;
}

/**
 * NoteConflictError is raised when a note changed on disk since it was last
 * read (R9), carrying the current note so the editor can reconcile.
 */
export class NoteConflictError extends ApiError {
  readonly current: Note | null;
  constructor(body: ApiErrorBody, current: Note | null) {
    super(409, body);
    this.name = "NoteConflictError";
    this.current = current;
  }
}

export async function listNotes(): Promise<NoteSummary[]> {
  const body = await request<{ notes: NoteSummary[] }>("/notes");
  return body.notes ?? [];
}

export function getNote(visibility: string, id: string): Promise<Note> {
  return request<Note>(`/notes/${encodeURIComponent(id)}?visibility=${encodeURIComponent(visibility)}`);
}

export async function createNote(input: CreateNoteInput): Promise<Note> {
  const response = await fetch(`${API_PREFIX}/notes`, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(input),
  });
  if (!response.ok) throw new ApiError(response.status, await readError(response));
  return (await response.json()) as Note;
}

export async function updateNote(
  id: string,
  patch: {
    visibility: string;
    expected_version: string;
    title?: string;
    body?: string;
    links?: NoteLink[];
  },
): Promise<Note> {
  const response = await fetch(`${API_PREFIX}/notes/${encodeURIComponent(id)}`, {
    method: "PUT",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(patch),
  });
  if (response.status === 409) {
    const parsed = (await response.json()) as { error: ApiErrorBody; conflict: Note | null };
    throw new NoteConflictError(parsed.error, parsed.conflict);
  }
  if (!response.ok) throw new ApiError(response.status, await readError(response));
  return (await response.json()) as Note;
}

export async function deleteNote(visibility: string, id: string): Promise<void> {
  const response = await fetch(
    `${API_PREFIX}/notes/${encodeURIComponent(id)}?visibility=${encodeURIComponent(visibility)}`,
    { method: "DELETE", credentials: "same-origin", headers: { Accept: "application/json" } },
  );
  if (!response.ok) throw new ApiError(response.status, await readError(response));
}
