import type { DocumentSummary } from "../api/types";

/** A node in the workspace file tree (spec 04 section 4.1). */
export interface TreeNode {
  name: string;
  /** Full path for a directory, or the document ID for a file. */
  path: string;
  kind: "directory" | "document";
  children: TreeNode[];
  document?: DocumentSummary;
}

/**
 * buildTree groups document IDs into a directory hierarchy.
 *
 * Directories are listed before documents, and each level is sorted
 * case-insensitively so the tree is stable and predictable.
 */
export function buildTree(documents: DocumentSummary[]): TreeNode[] {
  const root: TreeNode = { name: "", path: "", kind: "directory", children: [] };

  for (const doc of documents) {
    const segments = doc.id.split("/");
    let cursor = root;

    for (let i = 0; i < segments.length - 1; i++) {
      const name = segments[i];
      const path = segments.slice(0, i + 1).join("/");
      let next = cursor.children.find((c) => c.kind === "directory" && c.name === name);
      if (!next) {
        next = { name, path, kind: "directory", children: [] };
        cursor.children.push(next);
      }
      cursor = next;
    }

    cursor.children.push({
      name: segments[segments.length - 1],
      path: doc.id,
      kind: "document",
      children: [],
      document: doc,
    });
  }

  sortTree(root);
  return root.children;
}

function sortTree(node: TreeNode): void {
  node.children.sort((a, b) => {
    if (a.kind !== b.kind) return a.kind === "directory" ? -1 : 1;
    return a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
  });
  for (const child of node.children) sortTree(child);
}

/** A quick-open result, carrying why it matched (spec 04 section 4.2). */
export interface QuickOpenResult {
  document: DocumentSummary;
  score: number;
  /** Human-readable reason, shown beside the result. */
  reason: string;
  /** Indices of matched characters in the display string, for highlighting. */
  matched: number[];
}

/**
 * quickOpen ranks documents against a query.
 *
 * Ranking is deterministic: ties break on document ID, so the same query always
 * produces the same order (spec 04 section 4.2). Matching is a subsequence test
 * over the ID and the title, which is what users expect from a fuzzy finder.
 */
export function quickOpen(
  documents: DocumentSummary[],
  query: string,
  limit = 50,
): QuickOpenResult[] {
  const trimmed = query.trim();
  if (!trimmed) {
    return documents
      .slice()
      .sort((a, b) => a.id.localeCompare(b.id))
      .slice(0, limit)
      .map((document) => ({ document, score: 0, reason: "All documents", matched: [] }));
  }

  const needle = trimmed.toLowerCase();
  const results: QuickOpenResult[] = [];

  for (const document of documents) {
    const idMatch = subsequence(document.id.toLowerCase(), needle);
    const titleMatch = subsequence(document.title.toLowerCase(), needle);

    // Prefer a path match, because the path is what the result list displays.
    if (idMatch) {
      results.push({
        document,
        score: idMatch.score + fileNameBonus(document.id, needle),
        reason: reasonFor(document, needle, "path"),
        matched: idMatch.indices,
      });
      continue;
    }
    if (titleMatch) {
      results.push({
        document,
        score: titleMatch.score,
        reason: reasonFor(document, needle, "title"),
        matched: [],
      });
    }
  }

  results.sort((a, b) => {
    if (b.score !== a.score) return b.score - a.score;
    return a.document.id.localeCompare(b.document.id);
  });
  return results.slice(0, limit);
}

function reasonFor(document: DocumentSummary, needle: string, kind: "path" | "title"): string {
  const base = document.id.split("/").pop() ?? document.id;
  if (base.toLowerCase().includes(needle)) return "File name";
  if (kind === "path") return "Path";
  return "Title";
}

/** fileNameBonus rewards a match concentrated in the file name. */
function fileNameBonus(id: string, needle: string): number {
  const base = (id.split("/").pop() ?? "").toLowerCase();
  if (base === needle) return 200;
  if (base.startsWith(needle)) return 120;
  if (base.includes(needle)) return 80;
  return 0;
}

interface SubsequenceMatch {
  score: number;
  indices: number[];
}

/**
 * subsequence tests whether needle appears in haystack in order, scoring
 * contiguous runs and word-boundary starts more highly.
 */
function subsequence(haystack: string, needle: string): SubsequenceMatch | null {
  if (needle.length === 0) return { score: 0, indices: [] };
  if (needle.length > haystack.length) return null;

  const indices: number[] = [];
  let score = 0;
  let hayIndex = 0;
  let previousIndex = -1;

  for (const char of needle) {
    const found = haystack.indexOf(char, hayIndex);
    if (found < 0) return null;

    // Contiguous characters are worth far more than scattered ones.
    if (found === previousIndex + 1) score += 10;
    // A match at a word or path boundary is a strong signal.
    if (found === 0 || "/-_. ".includes(haystack[found - 1])) score += 15;

    score += 1;
    indices.push(found);
    previousIndex = found;
    hayIndex = found + 1;
  }

  // Shorter haystacks are better matches for the same needle.
  score += Math.max(0, 30 - haystack.length);
  return { score, indices };
}
