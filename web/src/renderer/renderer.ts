import MarkdownIt from "markdown-it";
import type { Options } from "markdown-it";
import type Token from "markdown-it/lib/token.mjs";
import footnotePlugin from "markdown-it-footnote";
import taskListPlugin from "markdown-it-task-lists";
import DOMPurify from "dompurify";

import {
  wikiLinks,
  callouts,
  math,
  mermaidBlocks,
  type RenderEnv,
} from "./plugins";
import type { Heading } from "../api/types";

/** Renderer features, mirroring the workspace `capabilities` payload. */
export interface RenderCapabilities {
  raw_html: boolean;
  wiki_links: boolean;
  footnotes: boolean;
  callouts: boolean;
  math: boolean;
  mermaid: boolean;
}

export interface RenderInput {
  /** Markdown source, front matter already removed. */
  source: string;
  /** 1-based source line at which `source` begins in the original file. */
  sourceStartLine: number;
  /** The backend's authoritative outline (ADR-0003). */
  outline: Heading[];
  capabilities: RenderCapabilities;
}

export interface RenderResult {
  html: string;
  /**
   * Math and Mermaid sources, referenced from the HTML by index.
   *
   * They are kept out of the markup because DOMPurify strips any attribute
   * value containing "-->" as an mXSS precaution, and "-->" is the most common
   * token in Mermaid flowchart syntax.
   */
  mathSources: string[];
  mermaidSources: string[];
  /**
   * Headings the renderer produced but the backend outline does not contain,
   * or vice versa. ADR-0003 requires these to be surfaced rather than silently
   * patched over.
   */
  headingWarnings: string[];
}

/**
 * Elements and attributes permitted after sanitisation (spec 03 section 9).
 *
 * This is an allowlist, not a blocklist: anything not named here is removed.
 * Script, style, iframe, object, embed, form, and event handlers are therefore
 * excluded by construction rather than by enumeration.
 */
const ALLOWED_TAGS = [
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "p",
  "br",
  "hr",
  "div",
  "span",
  "strong",
  "b",
  "em",
  "i",
  "u",
  "s",
  "del",
  "ins",
  "mark",
  "sub",
  "sup",
  "small",
  "abbr",
  "cite",
  "q",
  "dfn",
  "time",
  "var",
  "figure",
  "figcaption",
  "ul",
  "ol",
  "li",
  "blockquote",
  "pre",
  "code",
  "kbd",
  "samp",
  "table",
  "thead",
  "tbody",
  "tfoot",
  "tr",
  "th",
  "td",
  "caption",
  "a",
  "img",
  "dl",
  "dt",
  "dd",
  "section",
  "input",
];

const ALLOWED_ATTR = [
  "href",
  "src",
  "alt",
  "title",
  "class",
  "id",
  "colspan",
  "rowspan",
  "align",
  "start",
  "reversed",
  "type",
  "checked",
  "disabled",
  // Athenaeum-specific placeholders resolved after sanitisation.
  "data-wiki-target",
  "data-math-index",
  "data-mermaid-index",
  "data-callout",
  "data-line",
  "data-remote",
  "data-heading-path",
];

/** URI schemes permitted in href and src. */
const SAFE_URI = /^(?:https?:|mailto:|tel:|#|\/|\.\/|\.\.\/|[^:]*$)/i;

let purifierConfigured = false;

/**
 * configurePurifier installs hooks that DOMPurify's allowlist alone cannot
 * express. It runs once per process.
 */
function configurePurifier(): void {
  if (purifierConfigured) return;
  purifierConfigured = true;

  DOMPurify.addHook("afterSanitizeAttributes", (node) => {
    if (!(node instanceof Element)) return;

    // Reject any URI scheme outside the safe set. DOMPurify blocks javascript:
    // already; this also catches data: URIs in href, which can carry HTML.
    for (const attr of ["href", "src"]) {
      const value = node.getAttribute(attr);
      if (value === null) continue;

      const trimmed = value.trim();
      const isDataImage =
        attr === "src" &&
        /^data:image\/(png|jpe?g|gif|webp|avif);base64,/i.test(trimmed);
      if (!isDataImage && !SAFE_URI.test(trimmed)) {
        node.removeAttribute(attr);
        continue;
      }

      // Mark remote assets so the UI can show the indicator R3 requires, and
      // stop referrers leaking the local workspace.
      if (/^https?:/i.test(trimmed)) {
        node.setAttribute("data-remote", "true");
        if (node.tagName === "A") {
          node.setAttribute("rel", "noopener noreferrer nofollow");
          node.setAttribute("target", "_blank");
        }
        if (node.tagName === "IMG") {
          node.setAttribute("referrerpolicy", "no-referrer");
          node.setAttribute("loading", "lazy");
        }
      }
    }

    // Task-list checkboxes are the only inputs Markdown can produce; force
    // them read-only so a preview cannot be mistaken for an editing surface.
    if (node.tagName === "INPUT") {
      if (node.getAttribute("type") !== "checkbox") {
        node.remove();
        return;
      }
      node.setAttribute("disabled", "disabled");
    }
  });
}

/** buildMarkdownIt configures the parser for a workspace's enabled dialect. */
function buildMarkdownIt(capabilities: RenderCapabilities): MarkdownIt {
  const md = new MarkdownIt({
    // GFM baseline: tables, strikethrough, autolinks, and fenced code are all
    // enabled by markdown-it's "default" preset plus linkify.
    html: capabilities.raw_html,
    linkify: true,
    breaks: false,
    typographer: false,
  });

  md.use(taskListPlugin, { enabled: false, label: false });
  md.use(mermaidBlocks);

  if (capabilities.footnotes) md.use(footnotePlugin);
  if (capabilities.wiki_links) md.use(wikiLinks);
  if (capabilities.callouts) md.use(callouts);
  if (capabilities.math) md.use(math);

  return md;
}

/**
 * attachSourceLines records the source line of every block-level token, so a
 * rendered heading can be matched to the backend outline by line and so the
 * preview can scroll to a source position.
 */
function attachSourceLines(md: MarkdownIt, sourceStartLine: number): void {
  const rule = md.renderer.renderToken.bind(md.renderer);
  md.renderer.renderToken = (
    tokens: Token[],
    idx: number,
    options: Options,
  ) => {
    const token = tokens[idx];
    if (token.map && token.nesting !== -1) {
      token.attrSet("data-line", String(token.map[0] + sourceStartLine));
    }
    return rule(tokens, idx, options);
  };
}

/**
 * render converts Markdown to sanitised HTML.
 *
 * Heading identity comes from the backend outline (ADR-0003): rendered
 * headings adopt the backend's slug, matched by source line. A mismatch is
 * reported rather than silently resolved.
 */
export function render(input: RenderInput): RenderResult {
  configurePurifier();

  const md = buildMarkdownIt(input.capabilities);
  attachSourceLines(md, input.sourceStartLine);

  const env: RenderEnv = {};
  const rawHtml = md.render(input.source, env);

  const sanitised = DOMPurify.sanitize(rawHtml, {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    // Never allow these even if a tag slips through the allowlist.
    FORBID_TAGS: [
      "script",
      "style",
      "iframe",
      "object",
      "embed",
      "form",
      "svg",
      "math",
      "template",
    ],
    FORBID_ATTR: ["srcset", "formaction", "xlink:href", "ping"],
    ALLOW_DATA_ATTR: false,
    RETURN_TRUSTED_TYPE: false,
  });

  const reconciled = reconcileHeadings(sanitised, input.outline);
  return {
    ...reconciled,
    mathSources: env.mathSources ?? [],
    mermaidSources: env.mermaidSources ?? [],
  };
}

/**
 * reconcileHeadings applies the backend's slugs to the rendered headings.
 *
 * Matching is by source line rather than ordinal position, so a single
 * disagreement between the two parsers stays contained to the heading that
 * actually differs instead of shifting every heading after it (ADR-0003).
 */
function reconcileHeadings(
  html: string,
  outline: Heading[],
): { html: string; headingWarnings: string[] } {
  const warnings: string[] = [];

  const container = document.createElement("div");
  container.innerHTML = html;

  const rendered = Array.from(container.querySelectorAll("h1,h2,h3,h4,h5,h6"));
  const byLine = new Map<number, Heading>();
  for (const heading of outline) byLine.set(heading.line, heading);

  const matched = new Set<number>();

  for (const element of rendered) {
    const lineAttr = element.getAttribute("data-line");
    const line = lineAttr ? Number(lineAttr) : NaN;
    const backing = Number.isFinite(line) ? byLine.get(line) : undefined;

    if (!backing) {
      warnings.push(
        `The preview rendered a heading at line ${lineAttr ?? "?"} ("${element.textContent?.trim() ?? ""}") ` +
          `that the document outline does not contain. Its anchor may be unreliable.`,
      );
      continue;
    }

    element.id = backing.slug;
    element.setAttribute("data-heading-path", backing.path.join(" > "));
    matched.add(backing.line);
  }

  for (const heading of outline) {
    if (!matched.has(heading.line)) {
      warnings.push(
        `The document outline lists a heading at line ${heading.line} ("${heading.text}") ` +
          `that the preview did not render.`,
      );
    }
  }

  return { html: container.innerHTML, headingWarnings: warnings };
}
