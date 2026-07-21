/** Note shapes mirroring the backend model (spec 03 section 4, R9). */

import type { Visibility } from "../annotations/types";
export type { Visibility } from "../annotations/types";

/** One typed link from a note (R9). */
export interface NoteLink {
  document?: string;
  heading?: string;
  note?: string;
  annotation?: string;
}

export interface Note {
  id: string;
  title: string;
  visibility: Visibility;
  created_at: string;
  updated_at: string;
  links?: NoteLink[];
  body: string;
  /** Fingerprint for optimistic concurrency; required on update. */
  version: string;
}

/** A note without its body, for the list. */
export interface NoteSummary {
  id: string;
  title: string;
  visibility: Visibility;
  updated_at: string;
  links?: NoteLink[];
}

export interface CreateNoteInput {
  title: string;
  visibility: Visibility;
  body: string;
  links?: NoteLink[];
}
