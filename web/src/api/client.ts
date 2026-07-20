import type {
  ApiErrorBody,
  ConflictInfo,
  SaveResult,
  DocumentDetail,
  DocumentSummary,
  Health,
  WorkspaceInfo,
} from "./types";

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
