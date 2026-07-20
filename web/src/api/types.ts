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

/** The stable error object every endpoint uses on failure. */
export interface ApiErrorBody {
  code: string;
  message: string;
  details?: Record<string, string>;
}
