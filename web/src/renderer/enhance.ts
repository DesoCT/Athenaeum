/**
 * Post-render enhancers.
 *
 * Highlighting, mathematics, and Mermaid are applied to already-sanitised DOM,
 * and each module is imported dynamically so its bundle is fetched only when a
 * document actually needs it (spec 02 section 7, constitution C6). All three
 * are served from the embedded frontend, so they work offline (C5, A2).
 */

/**
 * Languages registered for highlighting, kept deliberately small (C6).
 *
 * Each import is written out in full rather than built from a template string.
 * Vite cannot statically analyse `import(`.../${name}`)`, so a templated
 * version produces no chunks at all: every language fails to load, hljs falls
 * back to highlightAuto with nothing registered, and code renders unhighlighted
 * with no error anywhere. An explicit map is verbose but is the only form the
 * bundler can follow.
 */
const LANGUAGE_LOADERS: Record<string, () => Promise<{ default: unknown }>> = {
  bash: () => import("highlight.js/lib/languages/bash"),
  c: () => import("highlight.js/lib/languages/c"),
  cpp: () => import("highlight.js/lib/languages/cpp"),
  csharp: () => import("highlight.js/lib/languages/csharp"),
  css: () => import("highlight.js/lib/languages/css"),
  diff: () => import("highlight.js/lib/languages/diff"),
  dockerfile: () => import("highlight.js/lib/languages/dockerfile"),
  go: () => import("highlight.js/lib/languages/go"),
  graphql: () => import("highlight.js/lib/languages/graphql"),
  ini: () => import("highlight.js/lib/languages/ini"),
  java: () => import("highlight.js/lib/languages/java"),
  javascript: () => import("highlight.js/lib/languages/javascript"),
  json: () => import("highlight.js/lib/languages/json"),
  kotlin: () => import("highlight.js/lib/languages/kotlin"),
  lua: () => import("highlight.js/lib/languages/lua"),
  makefile: () => import("highlight.js/lib/languages/makefile"),
  markdown: () => import("highlight.js/lib/languages/markdown"),
  nix: () => import("highlight.js/lib/languages/nix"),
  perl: () => import("highlight.js/lib/languages/perl"),
  php: () => import("highlight.js/lib/languages/php"),
  python: () => import("highlight.js/lib/languages/python"),
  ruby: () => import("highlight.js/lib/languages/ruby"),
  rust: () => import("highlight.js/lib/languages/rust"),
  scss: () => import("highlight.js/lib/languages/scss"),
  shell: () => import("highlight.js/lib/languages/shell"),
  sql: () => import("highlight.js/lib/languages/sql"),
  swift: () => import("highlight.js/lib/languages/swift"),
  typescript: () => import("highlight.js/lib/languages/typescript"),
  xml: () => import("highlight.js/lib/languages/xml"),
  yaml: () => import("highlight.js/lib/languages/yaml"),
};

let highlighter: typeof import("highlight.js").default | null = null;

async function loadHighlighter() {
  if (highlighter) return highlighter;
  const { default: hljs } = await import("highlight.js/lib/core");

  const failures: string[] = [];
  await Promise.all(
    Object.entries(LANGUAGE_LOADERS).map(async ([name, load]) => {
      try {
        const mod = (await load()) as { default: never };
        hljs.registerLanguage(name, mod.default);
      } catch {
        failures.push(name);
      }
    }),
  );

  // Silence here would hide a total highlighting failure, which is how the
  // templated-import bug went unnoticed.
  if (failures.length > 0) {
    console.warn(`Athenaeum: ${failures.length} highlight language(s) failed to load:`, failures);
  }

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
