/** Shapes returned by the Athenaeum API (spec 02 section 5). */

export interface Health {
  status: string;
  version: string;
  workspace: string;
  remote: boolean;
  frontend: "embedded" | "missing";
}

/** Renderer and service features the workspace configuration enables. */
export interface Capabilities {
  raw_html: boolean;
  wiki_links: boolean;
  footnotes: boolean;
  callouts: boolean;
  math: boolean;
  mermaid: boolean;
  git: boolean;
  search: boolean;
}

export interface Group {
  id: string;
  title: string;
  count: number;
}

export interface Diagnostic {
  severity: "error" | "warning";
  field: string;
  message: string;
  remedy?: string;
}

export interface WorkspaceInfo {
  name: string;
  root: string;
  document_count: number;
  groups: Group[];
  diagnostics?: Diagnostic[];
  capabilities: Capabilities;
}

/** A document as listed in the tree, without its content. */
export interface DocumentSummary {
  id: string;
  title: string;
  size: number;
  mod_time: string;
  groups?: string[];
  writable: boolean;
  too_large: boolean;
  large_warning: boolean;
}

/**
 * One heading from the backend's authoritative outline (ADR-0003).
 * The renderer adopts `slug` and `path`, matching by `line`.
 */
export interface Heading {
  level: number;
  text: string;
  slug: string;
  path: string[];
  line: number;
}

/** A fully read document. */
export interface DocumentDetail extends DocumentSummary {
  content: string;
  version: string;
  encoding: "utf-8" | "unknown";
  line_ending: "lf" | "crlf" | "mixed";
  has_bom: boolean;
  front_matter?: Record<string, unknown>;
  front_matter_format: "none" | "yaml" | "toml";
  outline: Heading[];
  read_only: boolean;
  warnings?: string[];
}

/** The result of a successful save. */
export interface SaveResult {
  id: string;
  version: string;
  size: number;
  line_ending: string;
}

/** The disk side of a save conflict (R6). */
export interface ConflictInfo {
  current_version: string;
  current_content: string;
}

/** The stable error object every endpoint uses on failure. */
export interface ApiErrorBody {
  code: string;
  message: string;
  details?: Record<string, string>;
}

/**
 * One run of a search snippet.
 *
 * The server splits the snippet into plain and matched runs rather than
 * returning markup, so highlighting never builds HTML from document text
 * (spec 03 section 9).
 */
export interface SearchSegment {
  text: string;
  match?: boolean;
}

/** Where a search result matched. */
export type SearchField = "body" | "heading" | "title" | "path";

/** One ranked search result (R7, spec 04 section 8). */
export interface SearchResult {
  document_id: string;
  title: string;
  groups?: string[];
  /** The enclosing heading chain, from the backend outline (ADR-0003). */
  heading_path?: string[];
  heading_slug?: string;
  /** 1-based source line, or absent when the match was in the path or title. */
  line?: number;
  snippet?: SearchSegment[];
  field: SearchField;
}

/** Index states shown in the status bar and the search panel. */
export type IndexState =
  | "disabled"
  | "unavailable"
  | "building"
  | "rebuilding"
  | "ready";

export interface IndexStatus {
  state: IndexState;
  indexed: number;
  total: number;
  /** Documents queued for indexing. Non-zero means results may be stale. */
  pending: number;
  git_filter: boolean;
  last_built_at?: string;
  last_duration_ms?: number;
  /** A stable code, never a message containing document content. */
  error?: string;
}

export interface SearchResponse {
  results: SearchResult[];
  truncated: boolean;
  status: IndexStatus;
}

/** Filters a search may apply (R7). */
export interface SearchFilters {
  path?: string;
  group?: string;
  git?: "modified" | "untracked" | "clean";
}

/** View modes (spec 04 section 6). */
export type ViewMode = "split" | "source" | "preview";

/** One restorable open tab (R13). */
export interface SessionTab {
  document_id: string;
  mode: ViewMode;
  /** 0..1 fraction of the preview's scroll height, so it survives a resize. */
  preview_scroll: number;
  source_line: number;
}

export interface SessionLayout {
  navigation: boolean;
  context: boolean;
  search: boolean;
}

/**
 * Restorable UI state (R13).
 *
 * Search query history is deliberately absent: R13 permits restoring command
 * history "that contains no sensitive content", and what someone searched their
 * own notes for does not qualify.
 */
export interface SessionState {
  schema_version: number;
  updated_at?: string;
  tabs: SessionTab[];
  active_document?: string;
  recent?: string[];
  layout: SessionLayout;
}

/**
 * One registered workspace as the picker sees it (ADR-0004).
 *
 * An unavailable entry carries its reason and remedy rather than being hidden,
 * so a mistyped path is visible and fixable (R1). `path` is the one place the
 * picker deliberately shows an absolute path: the user needs it to tell two
 * similarly named workspaces apart.
 */
export interface WorkspaceEntry {
  name: string;
  path: string;
  available: boolean;
  code?: string;
  reason?: string;
  remedy?: string;
  /** True for the entry the process currently has open. At most one is. */
  active: boolean;
}

/** The workspace the process has open, or null at the picker. */
export interface ActiveWorkspace {
  name: string;
  path: string;
}

/**
 * The picker's whole view of the registry.
 *
 * This is a launcher, not a mount table: exactly one workspace is ever active,
 * and nothing here loads two at once (ADR-0004, D-006, R1).
 */
export interface WorkspaceRegistry {
  registry_path: string;
  present: boolean;
  active: ActiveWorkspace | null;
  entries: WorkspaceEntry[];
  diagnostics?: Diagnostic[];
}
