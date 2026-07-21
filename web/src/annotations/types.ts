/** Annotation shapes mirroring the backend sidecar model (spec 03 section 3). */

export type Visibility = "personal" | "shared";
export type AnnotationKind = "comment" | "pin";
export type AnnotationStatus = "open" | "resolved";
export type AnchorType = "text_quote" | "heading" | "document";
export type AnchorState = "anchored" | "detached";

/**
 * An anchor stores both a structural position and a quoted context so it can be
 * repaired after edits (R8). `state`, `start_line`, and `end_line` are computed
 * by the backend on every read and reflect the current document, while the
 * selector fields (`exact`, `prefix`, `suffix`, `heading_path`) are durable.
 */
export interface Anchor {
  type: AnchorType;
  heading_path?: string[];
  start_line?: number;
  end_line?: number;
  exact?: string;
  prefix?: string;
  suffix?: string;
  source_hash?: string;
  state?: AnchorState;
}

export interface Annotation {
  id: string;
  kind: AnnotationKind;
  visibility: Visibility;
  status: AnnotationStatus;
  body: string;
  created_at: string;
  updated_at: string;
  anchor: Anchor;
}

/** The merged list for one document, with each sidecar's revision (R8). */
export interface AnnotationList {
  document_id: string;
  personal_revision: number;
  shared_revision: number;
  annotations: Annotation[];
}

/** The selector captured from a selection, before the server stamps an id. */
export interface AnchorInput {
  type: AnchorType;
  heading_path?: string[];
  start_line?: number;
  end_line?: number;
  exact?: string;
  prefix?: string;
  suffix?: string;
}

/** A lightweight annotation pointer for the Map Room home (spec 04 section 3). */
export interface AnnotationRef {
  id: string;
  document_id: string;
  visibility: Visibility;
  kind: AnnotationKind;
  status: AnnotationStatus;
  body: string;
  line?: number;
}

/** Workspace-wide annotation summary: pins and unresolved comments. */
export interface AnnotationOverview {
  pins: AnnotationRef[];
  unresolved: AnnotationRef[];
}

export interface CreateAnnotationInput {
  document_id: string;
  kind: AnnotationKind;
  visibility: Visibility;
  status?: AnnotationStatus;
  body: string;
  anchor: AnchorInput;
  expected_revision: number;
}
