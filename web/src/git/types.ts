/** Git shapes mirroring the backend read-only adapter (R12, spec 02 section 3.10). */

export type GitState = "modified" | "untracked" | "clean";

export interface GitFile {
  document_id: string;
  state: GitState;
}

export interface GitStatus {
  available: boolean;
  files: GitFile[];
}

export interface GitCommit {
  hash: string;
  short_hash: string;
  author: string;
  date: string;
  subject: string;
}

export interface GitHistory {
  available: boolean;
  commits: GitCommit[];
}

export interface GitBlameLine {
  line: number;
  hash: string;
  author: string;
  date: string;
  content: string;
}

export interface GitBlame {
  available: boolean;
  lines: GitBlameLine[];
}

export interface GitDiff {
  available: boolean;
  diff: string;
}
