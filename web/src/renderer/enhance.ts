/**
 * Post-render enhancers.
 *
 * Highlighting, mathematics, and Mermaid are applied to already-sanitised DOM,
 * and each module is imported dynamically so its bundle is fetched only when a
 * document actually needs it (spec 02 section 7, constitution C6). All three
 * are served from the embedded frontend, so they work offline (C5, A2).
 */

/** Languages registered for highlighting. Kept deliberately small (C6). */
const LANGUAGES = [
  "bash", "c", "cpp", "csharp", "css", "diff", "dockerfile", "go", "graphql",
  "html", "ini", "java", "javascript", "json", "kotlin", "lua", "makefile",
  "markdown", "nix", "perl", "php", "python", "ruby", "rust", "scss", "shell",
  "sql", "swift", "toml", "typescript", "xml", "yaml",
];

let highlighter: typeof import("highlight.js").default | null = null;

async function loadHighlighter() {
  if (highlighter) return highlighter;
  const { default: hljs } = await import("highlight.js/lib/core");
  await Promise.all(
    LANGUAGES.map(async (name) => {
      try {
        const mod = await import(`highlight.js/lib/languages/${name}`);
        hljs.registerLanguage(name, mod.default);
      } catch {
        // A language that cannot be loaded simply stays unhighlighted.
      }
    }),
  );
  highlighter = hljs;
  return hljs;
}

/** highlightCode applies syntax highlighting to fenced code blocks. */
export async function highlightCode(root: HTMLElement): Promise<void> {
  const blocks = Array.from(root.querySelectorAll("pre > code"));
  if (blocks.length === 0) return;

  const hljs = await loadHighlighter();

  for (const block of blocks) {
    if (!(block instanceof HTMLElement)) continue;
    if (block.dataset.highlighted === "true") continue;

    const language = Array.from(block.classList)
      .find((c) => c.startsWith("language-"))
      ?.slice("language-".length);

    // The block's text is used, never its HTML, so highlighting cannot
    // reintroduce markup that sanitisation removed.
    const source = block.textContent ?? "";
    try {
      const result =
        language && hljs.getLanguage(language)
          ? hljs.highlight(source, { language, ignoreIllegals: true })
          : hljs.highlightAuto(source);
      block.innerHTML = result.value;
      block.dataset.highlighted = "true";
    } catch {
      // Leave the plain text in place on any failure.
    }
  }
}

/** renderMath replaces math placeholders with KaTeX output. */
export async function renderMath(root: HTMLElement, sources: string[]): Promise<void> {
  const nodes = Array.from(root.querySelectorAll("[data-math-index]"));
  if (nodes.length === 0) return;

  const [{ default: katex }] = await Promise.all([
    import("katex"),
    import("katex/dist/katex.min.css"),
  ]);

  for (const node of nodes) {
    if (!(node instanceof HTMLElement)) continue;
    const expression = sources[Number(node.dataset.mathIndex)] ?? "";
    const display = node.classList.contains("math-block");
    try {
      katex.render(expression, node, {
        displayMode: display,
        throwOnError: false,
        // KaTeX's own output is trusted, but `trust: false` still refuses
        // \href and \includegraphics, which could otherwise reach the DOM.
        trust: false,
        strict: "ignore",
      });
    } catch (err) {
      node.textContent = expression;
      node.classList.add("math-error");
      node.title = err instanceof Error ? err.message : "This expression could not be rendered.";
    }
  }
}

let mermaidReady: Promise<typeof import("mermaid").default> | null = null;

async function loadMermaid(dark: boolean) {
  if (!mermaidReady) {
    mermaidReady = import("mermaid").then(({ default: mermaid }) => {
      mermaid.initialize({
        startOnLoad: false,
        // Spec 03 section 9: Mermaid input must execute in a restricted
        // rendering mode. "strict" disables HTML labels and script directives
        // inside diagram source.
        securityLevel: "strict",
        theme: dark ? "dark" : "default",
        fontFamily: "inherit",
      });
      return mermaid;
    });
  }
  return mermaidReady;
}

/** renderMermaid replaces diagram placeholders with rendered SVG. */
export async function renderMermaid(
  root: HTMLElement,
  sources: string[],
  dark = true,
): Promise<void> {
  const nodes = Array.from(root.querySelectorAll("[data-mermaid-index]"));
  if (nodes.length === 0) return;

  const mermaid = await loadMermaid(dark);

  for (const [index, node] of nodes.entries()) {
    if (!(node instanceof HTMLElement)) continue;
    if (node.dataset.rendered === "true") continue;

    const source = sources[Number(node.dataset.mermaidIndex)] ?? "";
    const id = `mermaid-${Date.now()}-${index}`;
    try {
      const { svg } = await mermaid.render(id, source);
      node.innerHTML = svg;
      node.dataset.rendered = "true";
    } catch (err) {
      // A malformed diagram shows its source and the reason, never a blank box.
      node.classList.add("mermaid-error");
      node.textContent = source;
      const message = document.createElement("p");
      message.className = "mermaid-error-message";
      message.textContent =
        err instanceof Error ? `Diagram error: ${err.message}` : "This diagram could not be rendered.";
      node.prepend(message);
      node.dataset.rendered = "true";
    }
  }
}

/** enhance runs every applicable post-render pass. */
export async function enhance(
  root: HTMLElement,
  options: {
    math: boolean;
    mermaid: boolean;
    mathSources: string[];
    mermaidSources: string[];
    dark?: boolean;
  },
): Promise<void> {
  const work: Promise<void>[] = [highlightCode(root)];
  if (options.math) work.push(renderMath(root, options.mathSources));
  if (options.mermaid) work.push(renderMermaid(root, options.mermaidSources, options.dark ?? true));
  await Promise.all(work);
}
