import { resolvePath, decodeOnce } from "./renderer";
import type { DocumentSummary } from "../api/types";

/**
 * What a click inside the preview should do.
 *
 * Relative Markdown links and wiki links point at documents in the workspace,
 * not at URLs the browser should navigate to. Left alone, a relative href sends
 * the browser to a path the SPA answers with index.html: the address bar
 * changes, the app reloads, and the user loses their place (R3, R10).
 */
export type LinkAction =
  | { kind: "none" }
  | { kind: "external" }
  | { kind: "anchor"; slug: string }
  | { kind: "document"; documentId: string }
  | { kind: "missing"; target: string };

/**
 * resolveLink decides what a clicked element means.
 *
 * documentId is the document being viewed; relative targets resolve against its
 * directory. documents is the workspace listing, used to confirm a target
 * exists and to try the extensions a wiki link omits.
 */
export function resolveLink(
  element: Element,
  documentId: string,
  documents: DocumentSummary[],
): LinkAction {
  const anchor = element.closest("a");
  if (!anchor) return { kind: "none" };

  const baseDir = documentId.includes("/")
    ? documentId.slice(0, documentId.lastIndexOf("/"))
    : "";

  // Wiki links carry a target rather than an href, because resolving one
  // against the workspace is the application's job, not the renderer's.
  const wikiTarget = anchor.getAttribute("data-wiki-target");
  if (wikiTarget) {
    const found = findDocument(wikiTarget, baseDir, documents);
    return found ? { kind: "document", documentId: found } : { kind: "missing", target: wikiTarget };
  }

  const href = anchor.getAttribute("href");
  if (!href) return { kind: "none" };

  // Anything with a scheme is the browser's business; the sanitiser has
  // already restricted which schemes survive.
  if (/^[a-z][a-z0-9+.-]*:/i.test(href)) return { kind: "external" };

  // A bare fragment is in-document navigation, which the browser handles.
  if (href.startsWith("#")) return { kind: "anchor", slug: href.slice(1) };

  // Strip any fragment: a link may point at a heading in another document.
  const [pathPart] = href.split("#", 1);
  if (!pathPart) return { kind: "none" };

  const resolved = resolvePath(baseDir, decodeOnce(pathPart));
  if (!resolved) return { kind: "missing", target: href };

  const found = findDocument(resolved, "", documents);
  return found ? { kind: "document", documentId: found } : { kind: "missing", target: resolved };
}

/**
 * findDocument matches a target against the workspace listing.
 *
 * Wiki links conventionally omit the extension, and may name a document by its
 * bare title rather than its path, so several forms are tried before giving up.
 * Matching against the listing rather than guessing means a link to something
 * outside the workspace is reported as missing instead of opening a 404.
 */
function findDocument(
  target: string,
  baseDir: string,
  documents: DocumentSummary[],
): string | null {
  const candidates = [target];

  const resolvedAgainstBase = baseDir ? resolvePath(baseDir, target) : null;
  if (resolvedAgainstBase) candidates.push(resolvedAgainstBase);

  const withExtensions: string[] = [];
  for (const candidate of candidates) {
    if (!/\.[a-z0-9]+$/i.test(candidate)) {
      withExtensions.push(`${candidate}.md`, `${candidate}.markdown`);
    }
  }
  candidates.push(...withExtensions);

  for (const candidate of candidates) {
    if (documents.some((d) => d.id === candidate)) return candidate;
  }

  // Last resort: a wiki link naming a document by file name or title alone.
  const bare = target.split("/").pop() ?? target;
  const byName = documents.find(
    (d) =>
      d.id.endsWith(`/${bare}.md`) ||
      d.id === `${bare}.md` ||
      d.title.toLowerCase() === bare.toLowerCase(),
  );
  return byName ? byName.id : null;
}
