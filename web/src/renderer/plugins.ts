import type MarkdownIt from "markdown-it";
import type { Options, PluginSimple } from "markdown-it";
import type Token from "markdown-it/lib/token.mjs";
import type Renderer from "markdown-it/lib/renderer.mjs";
import type StateInline from "markdown-it/lib/rules_inline/state_inline.mjs";
import type StateBlock from "markdown-it/lib/rules_block/state_block.mjs";

/**
 * Wiki links: [[target]] or [[target|label]] (R3, enabled by
 * documents.wiki_links).
 *
 * The target is emitted as a data attribute rather than a resolved href,
 * because resolution against the workspace is the application's job, not the
 * renderer's. The sanitiser allowlist permits `data-wiki-target`.
 */
export const wikiLinks: PluginSimple = (md: MarkdownIt) => {
  md.inline.ruler.before(
    "link",
    "wiki_link",
    (state: StateInline, silent: boolean) => {
      const start = state.pos;
      if (state.src.charCodeAt(start) !== 0x5b) return false; // [
      if (state.src.charCodeAt(start + 1) !== 0x5b) return false;

      const end = state.src.indexOf("]]", start + 2);
      if (end < 0) return false;

      const inner = state.src.slice(start + 2, end);
      // A newline means this was never a wiki link, just stray brackets.
      if (inner.includes("\n") || inner.length === 0) return false;

      if (!silent) {
        const pipe = inner.indexOf("|");
        const target = (pipe >= 0 ? inner.slice(0, pipe) : inner).trim();
        const label = (pipe >= 0 ? inner.slice(pipe + 1) : inner).trim();
        if (!target) return false;

        const open = state.push("wiki_link_open", "a", 1);
        open.attrSet("class", "wiki-link");
        open.attrSet("data-wiki-target", target);

        const text = state.push("text", "", 0);
        text.content = label || target;

        state.push("wiki_link_close", "a", -1);
      }

      state.pos = end + 2;
      return true;
    },
  );
};

/** Callout kinds recognised in `> [!NOTE]` blockquotes (R3). */
const CALLOUT_KINDS = new Set([
  "note",
  "tip",
  "important",
  "warning",
  "caution",
  "info",
  "danger",
  "example",
  "quote",
]);

/**
 * Callouts: a blockquote whose first line is `[!NOTE]`, optionally followed by
 * a title (enabled by documents.callouts).
 */
export const callouts: PluginSimple = (md: MarkdownIt) => {
  md.core.ruler.after("block", "callouts", (state) => {
    const tokens = state.tokens;

    for (let i = 0; i < tokens.length; i++) {
      if (tokens[i].type !== "blockquote_open") continue;

      // The first inline token inside the quote carries the marker.
      const inline = tokens[i + 2];
      if (!inline || inline.type !== "inline") continue;

      const match = /^\[!(\w+)\]\s*(.*)$/.exec(inline.content);
      if (!match) continue;

      const kind = match[1].toLowerCase();
      if (!CALLOUT_KINDS.has(kind)) continue;

      const title = match[2].trim();

      tokens[i].attrSet("class", `callout callout-${kind}`);
      tokens[i].attrSet("data-callout", kind);

      // Replace the marker line with a heading-like title element.
      inline.content = title || kind.charAt(0).toUpperCase() + kind.slice(1);
      inline.children = null;
      state.md.inline.parse(
        inline.content,
        state.md,
        state.env,
        (inline.children = []),
      );

      const paragraphOpen = tokens[i + 1];
      if (paragraphOpen && paragraphOpen.type === "paragraph_open") {
        paragraphOpen.attrSet("class", "callout-title");
      }
    }
    return true;
  });
};

/**
 * Math: `$inline$` and `$$display$$` (enabled by documents.math).
 *
 * The expression is emitted verbatim into a placeholder element. KaTeX renders
 * it later, on the client, so the renderer stays synchronous and the KaTeX
 * bundle is only fetched when a document actually contains mathematics.
 */
export const math: PluginSimple = (md: MarkdownIt) => {
  md.inline.ruler.before(
    "escape",
    "math_inline",
    (state: StateInline, silent: boolean) => {
      const start = state.pos;
      if (state.src.charCodeAt(start) !== 0x24) return false; // $
      // `$$` at an inline position is display math, handled by the block rule.
      if (state.src.charCodeAt(start + 1) === 0x24) return false;

      let end = start + 1;
      while (end < state.src.length) {
        if (
          state.src.charCodeAt(end) === 0x24 &&
          state.src.charCodeAt(end - 1) !== 0x5c
        )
          break;
        end++;
      }
      if (end >= state.src.length) return false;

      const content = state.src.slice(start + 1, end);
      if (!content.trim() || content.includes("\n")) return false;

      if (!silent) {
        const token = state.push("math_inline", "span", 0);
        token.content = content;
      }
      state.pos = end + 1;
      return true;
    },
  );

  md.block.ruler.before(
    "fence",
    "math_block",
    (
      state: StateBlock,
      startLine: number,
      endLine: number,
      silent: boolean,
    ) => {
      const start = state.bMarks[startLine] + state.tShift[startLine];
      const max = state.eMarks[startLine];
      if (start + 2 > max) return false;
      if (state.src.slice(start, start + 2) !== "$$") return false;

      let line = startLine;
      let found = false;
      while (++line < endLine) {
        const from = state.bMarks[line] + state.tShift[line];
        const to = state.eMarks[line];
        if (state.src.slice(from, to).trim() === "$$") {
          found = true;
          break;
        }
      }
      if (!found) return false;
      if (silent) return true;

      const content = state
        .getLines(startLine + 1, line, state.blkIndent, false)
        .trim();
      const token = state.push("math_block", "div", 0);
      token.content = content;
      token.map = [startLine, line + 1];
      token.block = true;

      state.line = line + 1;
      return true;
    },
  );

  // The expression is carried in a side channel keyed by index, never in an
  // attribute. DOMPurify's mXSS protection strips any attribute value
  // containing "-->" or "<!--", which would silently empty legitimate content.
  md.renderer.rules.math_inline = (
    tokens: Token[],
    idx: number,
    _o: unknown,
    env: RenderEnv,
  ) =>
    `<span class="math-inline" data-math-index="${pushSource(env, "math", tokens[idx].content)}"></span>`;

  md.renderer.rules.math_block = (
    tokens: Token[],
    idx: number,
    _o: unknown,
    env: RenderEnv,
  ) =>
    `<div class="math-block" data-math-index="${pushSource(env, "math", tokens[idx].content)}"></div>`;
};

/**
 * Mermaid: a fenced block tagged `mermaid` (enabled by documents.mermaid).
 *
 * As with math, the source is preserved in a placeholder and rendered on the
 * client so the Mermaid bundle loads only when a diagram is actually present.
 */
export const mermaidBlocks: PluginSimple = (md: MarkdownIt) => {
  const defaultFence =
    md.renderer.rules.fence ??
    ((
      tokens: Token[],
      idx: number,
      options: Options,
      _env: unknown,
      self: Renderer,
    ) => self.renderToken(tokens, idx, options));

  md.renderer.rules.fence = (
    tokens: Token[],
    idx: number,
    options: Options,
    env: unknown,
    self: Renderer,
  ) => {
    const token = tokens[idx];
    const info = token.info.trim().split(/\s+/)[0]?.toLowerCase();
    if (info === "mermaid") {
      // Mermaid source almost always contains "-->", which DOMPurify strips
      // from attribute values as an mXSS precaution. Carry it in the side
      // channel instead so diagrams survive sanitisation.
      return `<div class="mermaid-block" data-mermaid-index="${pushSource(env as RenderEnv, "mermaid", token.content)}"></div>`;
    }
    return defaultFence(tokens, idx, options, env, self);
  };
};

/**
 * RenderEnv carries block sources that must survive sanitisation intact.
 *
 * Sources are referenced from the HTML by numeric index, because a bare number
 * cannot be mistaken for markup and so is never stripped.
 */
export interface RenderEnv {
  mathSources?: string[];
  mermaidSources?: string[];
}

/** pushSource records a block source and returns its index. */
export function pushSource(
  env: RenderEnv,
  kind: "math" | "mermaid",
  source: string,
): number {
  const key = kind === "math" ? "mathSources" : "mermaidSources";
  const list = (env[key] ??= []);
  list.push(source);
  return list.length - 1;
}
