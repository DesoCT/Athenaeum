/**
 * Anchor capture: turn a selection in the rendered preview into a durable
 * selector the backend can repair (R8, spec 03 section 3).
 *
 * The rendered document carries `data-line` on every block element, offset to
 * absolute source lines by the renderer (ADR-0003), so a selection maps back to
 * source lines without the frontend ever reading the filesystem. The quote
 * itself is the selected text; the backend searches the source for it and, when
 * it moved, disambiguates with the surrounding context captured here.
 */

import type { AnchorInput } from "./types";
import type { Heading } from "../api/types";

/** How much context to keep on each side of a quote for disambiguation. */
export const CONTEXT = 32;

/**
 * quoteSelector builds the exact/prefix/suffix triple from a block of plain
 * text and the selection offsets within it. Pure and framework-free, so it is
 * unit-tested directly.
 */
export function quoteSelector(
  text: string,
  start: number,
  end: number,
  context = CONTEXT,
): { exact: string; prefix: string; suffix: string } {
  const lo = Math.max(0, Math.min(start, end));
  const hi = Math.min(text.length, Math.max(start, end));
  return {
    exact: text.slice(lo, hi),
    prefix: text.slice(Math.max(0, lo - context), lo),
    suffix: text.slice(hi, Math.min(text.length, hi + context)),
  };
}

/**
 * headingPathFor returns the heading chain enclosing a source line: the path of
 * the last heading that begins at or before it. It gives a text anchor a region
 * hint and is the whole selector for a heading anchor.
 */
export function headingPathFor(line: number, outline: Heading[]): string[] | undefined {
  let best: Heading | undefined;
  for (const heading of outline) {
    if (heading.line <= line && (!best || heading.line >= best.line)) {
      best = heading;
    }
  }
  return best?.path;
}

/** blockLine reads the source line of the nearest ancestor carrying data-line. */
export function blockLine(node: Node | null): number | null {
  let el: Node | null = node;
  while (el && el.nodeType !== Node.ELEMENT_NODE) {
    el = el.parentNode;
  }
  const start = el as Element | null;
  const carrier = start?.closest?.("[data-line]") ?? null;
  if (!carrier) return null;
  const value = Number(carrier.getAttribute("data-line"));
  return Number.isFinite(value) ? value : null;
}

/**
 * selectionAnchor turns the current window selection inside root into a text
 * anchor, or returns null when there is nothing usable selected. It stays thin
 * over the pure helpers above so the hard logic is the tested part.
 */
export function selectionAnchor(root: HTMLElement, outline: Heading[]): AnchorInput | null {
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed || selection.rangeCount === 0) return null;

  const range = selection.getRangeAt(0);
  if (!root.contains(range.commonAncestorContainer)) return null;

  const exact = selection.toString().trim();
  if (!exact) return null;

  const startLine = blockLine(range.startContainer);
  const endLine = blockLine(range.endContainer) ?? startLine;
  if (startLine == null) return null;

  // Context comes from the plain text of the block that holds the selection
  // start, located by the same exact string so the offsets line up.
  const block = nearestBlock(range.startContainer);
  const blockText = block?.textContent ?? exact;
  const at = blockText.indexOf(exact);
  const { prefix, suffix } =
    at >= 0
      ? quoteSelector(blockText, at, at + exact.length)
      : { prefix: "", suffix: "" };

  return {
    type: "text_quote",
    exact,
    prefix,
    suffix,
    start_line: startLine,
    end_line: endLine ?? startLine,
    heading_path: headingPathFor(startLine, outline),
  };
}

function nearestBlock(node: Node | null): Element | null {
  let el: Node | null = node;
  while (el && el.nodeType !== Node.ELEMENT_NODE) {
    el = el.parentNode;
  }
  return (el as Element | null)?.closest?.("[data-line]") ?? null;
}
