import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { render, type RenderCapabilities } from "./renderer";
import type { Heading } from "../api/types";

/**
 * Acceptance C1 renders the *shipped* dialect fixture rather than a synthetic
 * string, so the example workspace and the renderer cannot drift apart.
 */
const here = dirname(fileURLToPath(import.meta.url));
const FIXTURE = resolve(here, "../../../examples/workspace/docs/design/rendering.md");

const capabilities: RenderCapabilities = {
  raw_html: false,
  wiki_links: true,
  footnotes: true,
  callouts: true,
  math: true,
  mermaid: true,
};

/** stripFrontMatter mirrors what Preview.svelte does before rendering. */
function stripFrontMatter(source: string): { body: string; startLine: number } {
  const lines = source.split("\n");
  if (lines[0]?.trim() !== "---") return { body: source, startLine: 1 };
  for (let i = 1; i < lines.length; i++) {
    if (lines[i].trim() === "---") {
      return { body: lines.slice(i + 1).join("\n"), startLine: i + 2 };
    }
  }
  return { body: source, startLine: 1 };
}

describe("C1 dialect fixture", () => {
  const source = readFileSync(FIXTURE, "utf8");
  const { body, startLine } = stripFrontMatter(source);
  const result = render({ source: body, sourceStartLine: startLine, outline: [], capabilities });

  it("renders the GFM table", () => {
    expect(result.html).toContain("<table");
    expect(result.html).toContain("Callouts");
  });

  it("renders the task list", () => {
    expect(result.html).toContain('type="checkbox"');
    expect(result.html).toContain("Write the fixture");
  });

  it("renders strikethrough and the footnote", () => {
    expect(result.html).toContain("<s>infers</s>");
    expect(result.html).toContain("footnote");
    expect(result.html).toContain("Inference is excluded");
  });

  it("renders the fenced code block with its language", () => {
    expect(result.html).toContain("language-go");
    expect(result.html).toContain("func Render");
  });

  it("renders the wiki link as a target", () => {
    expect(result.html).toContain('data-wiki-target="docs/operations/runbook"');
  });

  it("renders the callout", () => {
    expect(result.html).toContain('data-callout="note"');
  });

  it("extracts the mermaid diagram to the side channel intact", () => {
    expect(result.html).toContain("mermaid-block");
    expect(result.mermaidSources).toHaveLength(1);
    // The arrows are the part DOMPurify would have destroyed in an attribute.
    expect(result.mermaidSources[0]).toContain("Source --> Parser");
  });

  it("extracts both inline and display mathematics", () => {
    expect(result.html).toContain("math-inline");
    expect(result.html).toContain("math-block");
    expect(result.mathSources.some((m) => m.includes("E = mc^2"))).toBe(true);
    expect(result.mathSources.some((m) => m.includes("\\sum"))).toBe(true);
  });

  it("produces no script or event handler anywhere", () => {
    expect(result.html).not.toContain("<script");
    expect(result.html).not.toMatch(/\son\w+=/);
  });

  it("attaches source lines for every block, so the editor can navigate", () => {
    expect(result.html).toMatch(/data-line="\d+"/);
  });
});

describe("C1 outline reconciliation against the fixture", () => {
  const source = readFileSync(FIXTURE, "utf8");
  const { body, startLine } = stripFrontMatter(source);

  /**
   * The Go backend reports nine headings for this fixture, at these lines.
   * If the two parsers ever disagree, reconciliation reports it and this test
   * fails — which is exactly the guarantee ADR-0003 exists to provide.
   */
  const backendOutline: Heading[] = [
    { level: 1, text: "Rendering notes", slug: "rendering-notes", path: ["Rendering notes"], line: 7 },
    { level: 2, text: "Table", slug: "table", path: ["Rendering notes", "Table"], line: 12 },
    { level: 2, text: "Task list", slug: "task-list", path: ["Rendering notes", "Task list"], line: 20 },
    {
      level: 2,
      text: "Strikethrough and footnotes",
      slug: "strikethrough-and-footnotes",
      path: ["Rendering notes", "Strikethrough and footnotes"],
      line: 26,
    },
    { level: 2, text: "Code", slug: "code", path: ["Rendering notes", "Code"], line: 32 },
    { level: 2, text: "Wiki link", slug: "wiki-link", path: ["Rendering notes", "Wiki link"], line: 40 },
    { level: 2, text: "Callout", slug: "callout", path: ["Rendering notes", "Callout"], line: 44 },
    { level: 2, text: "Mermaid", slug: "mermaid", path: ["Rendering notes", "Mermaid"], line: 49 },
    {
      level: 2,
      text: "Mathematics",
      slug: "mathematics",
      path: ["Rendering notes", "Mathematics"],
      line: 56,
    },
  ];

  it("agrees with the Go outline on every heading", () => {
    const result = render({
      source: body,
      sourceStartLine: startLine,
      outline: backendOutline,
      capabilities,
    });
    expect(result.headingWarnings).toEqual([]);
  });

  it("applies every backend slug as an anchor id", () => {
    const result = render({
      source: body,
      sourceStartLine: startLine,
      outline: backendOutline,
      capabilities,
    });
    for (const heading of backendOutline) {
      expect(result.html).toContain(`id="${heading.slug}"`);
    }
  });
});
