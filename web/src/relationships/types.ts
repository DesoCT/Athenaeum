/** Relationship shapes mirroring the backend (spec 03 section 5, R10). */

export type RelationshipSource = "markdown" | "wiki" | "front_matter" | "sidecar";

export interface RelationshipRef {
  document_id: string;
  title: string;
  source: RelationshipSource;
  kind?: string;
  label?: string;
}

export interface RelationshipResult {
  document_id: string;
  outgoing: RelationshipRef[];
  backlinks: RelationshipRef[];
}
