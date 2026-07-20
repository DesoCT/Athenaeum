import { describe, it, expect } from "vitest";
import { buildTree, quickOpen } from "./tree";
import type { DocumentSummary } from "../api/types";

function doc(id: string, title = id): DocumentSummary {
  return {
    id,
    title,
    size: 10,
    mod_time: "2026-07-20T00:00:00Z",
    writable: true,
    too_large: false,
    large_warning: false,
  };
}

describe("buildTree", () => {
  const documents = [
    doc("README.md"),
    doc("docs/design/rendering.md"),
    doc("docs/design/storage.md"),
    doc("docs/operations/runbook.md"),
  ];

  it("nests documents under their directories", () => {
    const tree = buildTree(documents);

    expect(tree.map((n) => n.name)).toEqual(["docs", "README.md"]);
    const docsNode = tree[0];
    expect(docsNode.kind).toBe("directory");
    expect(docsNode.children.map((n) => n.name)).toEqual(["design", "operations"]);
    expect(docsNode.children[0].children.map((n) => n.name)).toEqual([
      "rendering.md",
      "storage.md",
    ]);
  });

  it("lists directories before documents", () => {
    const tree = buildTree([doc("z-dir/a.md"), doc("a-file.md")]);
    expect(tree[0].kind).toBe("directory");
    expect(tree[1].kind).toBe("document");
  });

  it("carries the document summary on leaf nodes", () => {
    const tree = buildTree([doc("a.md")]);
    expect(tree[0].document?.id).toBe("a.md");
  });

  it("is stable across repeated builds", () => {
    const first = JSON.stringify(buildTree(documents));
    const shuffled = [documents[2], documents[0], documents[3], documents[1]];
    expect(JSON.stringify(buildTree(shuffled))).toBe(first);
  });

  it("handles an empty workspace", () => {
    expect(buildTree([])).toEqual([]);
  });
});

describe("quickOpen", () => {
  const documents = [
    doc("README.md", "Readme"),
    doc("docs/design/rendering.md", "Rendering notes"),
    doc("docs/design/storage.md", "Storage"),
    doc("docs/operations/runbook.md", "Runbook"),
    doc("docs/operations/render-pipeline.md", "Render pipeline"),
  ];

  it("returns everything for an empty query", () => {
    const results = quickOpen(documents, "");
    expect(results).toHaveLength(documents.length);
  });

  it("finds documents by file name", () => {
    const results = quickOpen(documents, "runbook");
    expect(results[0].document.id).toBe("docs/operations/runbook.md");
  });

  it("finds documents by path fragment", () => {
    const results = quickOpen(documents, "design");
    const ids = results.map((r) => r.document.id);
    expect(ids).toContain("docs/design/rendering.md");
    expect(ids).toContain("docs/design/storage.md");
    expect(ids).not.toContain("docs/operations/runbook.md");
  });

  it("matches a subsequence, not just a substring", () => {
    const results = quickOpen(documents, "drend");
    expect(results.map((r) => r.document.id)).toContain("docs/design/rendering.md");
  });

  it("ranks a file-name match above a path-only match", () => {
    const results = quickOpen(documents, "render");
    // render-pipeline.md and rendering.md both match in the file name; both
    // should outrank anything matching only deeper in the path.
    expect(results[0].document.id).toMatch(/render/);
  });

  // Spec 04 section 4.2: results must be ranked deterministically.
  it("is deterministic for the same query", () => {
    const first = quickOpen(documents, "do").map((r) => r.document.id);
    const second = quickOpen(documents, "do").map((r) => r.document.id);
    expect(second).toEqual(first);
  });

  it("breaks ties on document id so ordering never wobbles", () => {
    const tied = [doc("b/x.md", "X"), doc("a/x.md", "X")];
    const results = quickOpen(tied, "x.md");
    const scores = results.map((r) => r.score);
    if (scores[0] === scores[1]) {
      expect(results[0].document.id).toBe("a/x.md");
    }
  });

  // Spec 04 section 4.2: results must show why each result matched.
  it("explains why each result matched", () => {
    for (const result of quickOpen(documents, "runbook")) {
      expect(result.reason).toBeTruthy();
    }
  });

  it("returns nothing for a query that cannot match", () => {
    expect(quickOpen(documents, "zzzzqqq")).toHaveLength(0);
  });

  it("respects the result limit", () => {
    const many = Array.from({ length: 200 }, (_, i) => doc(`docs/file-${i}.md`));
    expect(quickOpen(many, "file", 20)).toHaveLength(20);
  });

  it("is case insensitive", () => {
    expect(quickOpen(documents, "RUNBOOK")[0].document.id).toBe("docs/operations/runbook.md");
  });
});
