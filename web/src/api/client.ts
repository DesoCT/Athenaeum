import type {
  ApiErrorBody,
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
  const encoded = id.split("/").map(encodeURIComponent).join("/");
  return request<DocumentDetail>(`/documents/${encoded}`);
}
