import { describe, it, expect } from "vitest";
import { render, type RenderCapabilities } from "./renderer";
import type { Heading } from "../api/types";

const allEnabled: RenderCapabilities = {
  raw_html: false,
  wiki_links: true,
  footnotes: true,
  callouts: true,
  math: true,
  mermaid: true,
};

function renderMd(source: string, caps: Partial<RenderCapabilities> = {}, outline: Heading[] = []) {
  return render({
    source,
    sourceStartLine: 1,
    outline,
    capabilities: { ...allEnabled, ...caps },
  });
}

// ---------------------------------------------------------------------------
// Acceptance C2 — sanitisation
// ---------------------------------------------------------------------------

describe("C2 sanitisation", () => {
  it("removes script tags", () => {
    const { html } = renderMd("<script>window.pwned = true</script>\n\nText.\n", { raw_html: true });
    expect(html).not.toContain("<script");
    expect(html).not.toContain("window.pwned");
  });

  it("removes inline event handlers", () => {
    const { html } = renderMd(`<img src="x" onerror="window.pwned=1">`, { raw_html: true });
    expect(html).not.toContain("onerror");
    expect(html.toLowerCase()).not.toContain("pwned");
  });

  /** hrefs returns every href actually present in the rendered output. */
  function hrefs(html: string): string[] {
    const el = document.createElement("div");
    el.innerHTML = html;
    return Array.from(el.querySelectorAll("a")).map((a) => a.getAttribute("href") ?? "");
  }

  it("never produces a javascript: href", () => {
    const { html } = renderMd("[click me](javascript:window.pwned=1)");
    expect(hrefs(html).some((h) => /javascript:/i.test(h))).toBe(false);
  });

  it("never produces a javascript: href written with entities", () => {
    const { html } = renderMd("[click](java&#115;cript:alert(1))");
    expect(hrefs(html).some((h) => /javascript:/i.test(h))).toBe(false);
  });

  it("never produces a data:text/html href", () => {
    const { html } = renderMd("[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)");
    expect(hrefs(html).some((h) => /^data:text\/html/i.test(h))).toBe(false);
  });

  it("permits data: image URLs, which are inert", () => {
    const png = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUg==";
    const { html } = renderMd(`![dot](${png})`);
    expect(html).toContain("data:image/png");
  });

  it("removes unsafe SVG payloads", () => {
    const { html } = renderMd(
      `<svg><script>alert(1)</script></svg>\n\n<svg onload="alert(1)"></svg>`,
      { raw_html: true },
    );
    expect(html).not.toContain("<svg");
    expect(html).not.toContain("onload");
    expect(html).not.toContain("alert");
  });

  it("removes iframes, objects, embeds and forms", () => {
    const { html } = renderMd(
      `<iframe src="http://evil.example"></iframe>
<object data="x.swf"></object>
<embed src="x.swf">
<form action="http://evil.example"><input name="a"></form>`,
      { raw_html: true },
    );
    for (const tag of ["<iframe", "<object", "<embed", "<form"]) {
      expect(html).not.toContain(tag);
    }
  });

  it("removes style tags and blocks formaction", () => {
    const { html } = renderMd(
      `<style>body{display:none}</style><button formaction="javascript:alert(1)">x</button>`,
      { raw_html: true },
    );
    expect(html).not.toContain("<style");
    expect(html).not.toContain("formaction");
  });

  it("neutralises a javascript URL hidden by whitespace and case", () => {
    const { html } = renderMd("[x](\tJaVaScRiPt:alert(1))");
    expect(hrefs(html).some((h) => /javascript:/i.test(h))).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Acceptance C3 — raw HTML default
// ---------------------------------------------------------------------------

describe("C3 raw HTML default", () => {
  it("escapes raw HTML when raw_html is false", () => {
    const { html } = renderMd("<b>bold</b> and <em>em</em>\n", { raw_html: false });
    expect(html).not.toContain("<b>");
    expect(html).toContain("&lt;b&gt;");
  });

  it("still renders Markdown emphasis when raw HTML is off", () => {
    const { html } = renderMd("**bold** and *em*\n", { raw_html: false });
    expect(html).toContain("<strong>bold</strong>");
    expect(html).toContain("<em>em</em>");
  });

  it("permits safe HTML when raw_html is explicitly enabled", () => {
    const { html } = renderMd("<b>bold</b>\n", { raw_html: true });
    expect(html).toContain("<b>bold</b>");
  });

  it("sanitises even when raw HTML is enabled", () => {
    const { html } = renderMd("<b onclick='steal()'>bold</b>\n", { raw_html: true });
    expect(html).toContain("<b>");
    expect(html).not.toContain("onclick");
  });
});

// ---------------------------------------------------------------------------
// Acceptance C1 — dialect
// ---------------------------------------------------------------------------

describe("C1 dialect", () => {
  it("renders GFM tables", () => {
    const { html } = renderMd("| A | B |\n| - | - |\n| 1 | 2 |\n");
    expect(html).toContain("<table");
    expect(html).toContain("<th>A</th>");
    expect(html).toContain("<td>1</td>");
  });

  it("renders task lists as disabled checkboxes", () => {
    const { html } = renderMd("- [x] done\n- [ ] todo\n");
    expect(html).toContain("<input");
    expect(html).toContain("checked");
    expect(html).toContain("disabled");
  });

  it("renders strikethrough", () => {
    const { html } = renderMd("~~gone~~\n");
    expect(html).toContain("<s>gone</s>");
  });

  it("renders footnotes when enabled", () => {
    const { html } = renderMd("Text.[^a]\n\n[^a]: The note.\n", { footnotes: true });
    expect(html).toContain("footnote");
    expect(html).toContain("The note.");
  });

  it("renders fenced code with a language class", () => {
    const { html } = renderMd("```go\nfunc main() {}\n```\n");
    expect(html).toContain("language-go");
    expect(html).toContain("func main()");
  });

  it("renders wiki links as targets, not resolved hrefs", () => {
    const { html } = renderMd("See [[docs/design]] and [[docs/ops|Operations]].\n", { wiki_links: true });
    expect(html).toContain('data-wiki-target="docs/design"');
    expect(html).toContain('data-wiki-target="docs/ops"');
    expect(html).toContain("Operations");
  });

  it("leaves wiki links as text when disabled", () => {
    const { html } = renderMd("See [[docs/design]].\n", { wiki_links: false });
    expect(html).not.toContain("data-wiki-target");
    expect(html).toContain("[[docs/design]]");
  });

  it("renders callouts when enabled", () => {
    const { html } = renderMd("> [!NOTE]\n> Body text.\n", { callouts: true });
    expect(html).toContain("callout-note");
    expect(html).toContain('data-callout="note"');
  });

  it("leaves an unknown callout kind as an ordinary blockquote", () => {
    const { html } = renderMd("> [!NONSENSE]\n> Body.\n", { callouts: true });
    expect(html).not.toContain("data-callout");
    expect(html).toContain("<blockquote");
  });

  it("emits mermaid placeholders rather than rendering inline", () => {
    const result = renderMd("```mermaid\nflowchart LR\n  A --> B\n```\n");
    expect(result.html).toContain("mermaid-block");
    expect(result.html).toContain("data-mermaid-index");
    expect(result.mermaidSources).toHaveLength(1);
    expect(result.mermaidSources[0]).toContain("flowchart LR");
  });

  // Regression: DOMPurify strips any attribute value containing "-->" as an
  // mXSS precaution, and "-->" is the commonest token in Mermaid flowcharts.
  // Carrying the source in an attribute silently emptied every diagram.
  it("preserves arrow syntax in mermaid source", () => {
    const result = renderMd("```mermaid\nflowchart LR\n  A --> B --> C\n```\n");
    expect(result.mermaidSources[0]).toContain("A --> B --> C");
  });

  it("preserves math expressions containing comment-like sequences", () => {
    const result = renderMd("$$\na --> b\n$$\n", { math: true });
    expect(result.mathSources[0]).toContain("-->");
  });

  it("emits math placeholders for inline and display expressions", () => {
    const result = renderMd("Inline $E = mc^2$ here.\n\n$$\n\\sum_{i=1}^{n} x_i\n$$\n", { math: true });
    expect(result.html).toContain("math-inline");
    expect(result.html).toContain("math-block");
    expect(result.mathSources).toContain("E = mc^2");
  });

  it("leaves dollar signs alone when math is disabled", () => {
    const { html } = renderMd("It costs $5 and $10.\n", { math: false });
    expect(html).not.toContain("math-inline");
    expect(html).toContain("$5");
  });

  it("marks remote images and links, and hardens their rel", () => {
    const { html } = renderMd("![x](https://example.com/a.png)\n\n[y](https://example.com)\n");
    expect(html).toContain('data-remote="true"');
    expect(html).toContain("noopener");
    expect(html).toContain('referrerpolicy="no-referrer"');
  });

  it("does not mark relative assets as remote", () => {
    const { html } = renderMd("![x](assets/a.png)\n");
    expect(html).not.toContain("data-remote");
  });
});

// ---------------------------------------------------------------------------
// ADR-0003 — backend-authoritative heading identity
// ---------------------------------------------------------------------------

describe("ADR-0003 heading reconciliation", () => {
  const outline: Heading[] = [
    { level: 1, text: "Title", slug: "title", path: ["Title"], line: 1 },
    { level: 2, text: "Section", slug: "section", path: ["Title", "Section"], line: 3 },
  ];

  it("adopts backend slugs as heading ids", () => {
    const { html, headingWarnings } = renderMd("# Title\n\n## Section\n", {}, outline);
    expect(html).toContain('id="title"');
    expect(html).toContain('id="section"');
    expect(headingWarnings).toHaveLength(0);
  });

  it("carries the backend heading path for annotation anchoring", () => {
    const { html } = renderMd("# Title\n\n## Section\n", {}, outline);
    expect(html).toContain('data-heading-path="Title > Section"');
  });

  it("matches by source line, so one mismatch does not shift the rest", () => {
    // The backend reports a heading at line 5 that the renderer puts at line 3.
    const shifted: Heading[] = [
      { level: 1, text: "Title", slug: "title", path: ["Title"], line: 1 },
      { level: 2, text: "Section", slug: "section", path: ["Title", "Section"], line: 5 },
    ];
    const { html, headingWarnings } = renderMd("# Title\n\n## Section\n", {}, shifted);

    // The first heading still matches correctly.
    expect(html).toContain('id="title"');
    // The mismatch is reported, not silently applied to the wrong heading.
    expect(headingWarnings.length).toBeGreaterThan(0);
    expect(html).not.toContain('id="section"');
  });

  it("warns when the renderer produces a heading the outline lacks", () => {
    const { headingWarnings } = renderMd("# Title\n\n## Surprise\n", {}, [outline[0]]);
    expect(headingWarnings.some((w) => w.includes("outline does not contain"))).toBe(true);
  });

  it("warns when the outline lists a heading the renderer did not produce", () => {
    const { headingWarnings } = renderMd("# Title\n", {}, outline);
    expect(headingWarnings.some((w) => w.includes("did not render"))).toBe(true);
  });

  it("does not treat a hash inside a fenced block as a heading", () => {
    // The backend outline contains only the real heading; if the renderer
    // invented one from the code block, reconciliation would warn.
    const { headingWarnings } = renderMd("# Title\n\n```bash\n# a comment\n```\n", {}, [outline[0]]);
    expect(headingWarnings).toHaveLength(0);
  });
});
