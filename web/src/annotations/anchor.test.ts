import { describe, it, expect } from "vitest";
import { quoteSelector, headingPathFor, blockLine, selectionAnchor } from "./anchor";
import type { Heading } from "../api/types";

describe("quoteSelector", () => {
  const text = "The index is a disposable cache that can be rebuilt.";

  it("captures the exact selection with bounded context", () => {
    const start = text.indexOf("disposable cache");
    const sel = quoteSelector(text, start, start + "disposable cache".length, 8);
    expect(sel.exact).toBe("disposable cache");
    expect(sel.prefix).toBe("ex is a ");
    expect(sel.suffix).toBe(" that ca");
  });

  it("clamps offsets and orders them", () => {
    const sel = quoteSelector("abc", 5, -2);
    expect(sel.exact).toBe("abc");
  });
});

describe("headingPathFor", () => {
  const outline: Heading[] = [
    { level: 1, text: "Title", slug: "title", path: ["Title"], line: 1 },
    { level: 2, text: "Search", slug: "search", path: ["Title", "Search"], line: 10 },
    { level: 2, text: "Later", slug: "later", path: ["Title", "Later"], line: 40 },
  ];

  it("returns the nearest preceding heading path", () => {
    expect(headingPathFor(12, outline)).toEqual(["Title", "Search"]);
    expect(headingPathFor(40, outline)).toEqual(["Title", "Later"]);
    expect(headingPathFor(2, outline)).toEqual(["Title"]);
  });

  it("is undefined before any heading", () => {
    expect(headingPathFor(0, outline)).toBeUndefined();
  });
});

describe("blockLine", () => {
  it("reads data-line from the nearest ancestor element", () => {
    document.body.innerHTML = `<p data-line="7">hello <em>there</em></p>`;
    const em = document.querySelector("em")!;
    expect(blockLine(em.firstChild)).toBe(7);
  });

  it("is null when no ancestor carries a line", () => {
    document.body.innerHTML = `<p>no line</p>`;
    expect(blockLine(document.querySelector("p"))).toBeNull();
  });
});

describe("selectionAnchor", () => {
  it("builds a text anchor from a selection inside the root", () => {
    document.body.innerHTML =
      `<article id="root"><p data-line="2">The index is a disposable cache.</p></article>`;
    const root = document.getElementById("root") as HTMLElement;
    const p = root.querySelector("p")!;
    const textNode = p.firstChild as Text;

    const range = document.createRange();
    const at = textNode.data.indexOf("disposable cache");
    range.setStart(textNode, at);
    range.setEnd(textNode, at + "disposable cache".length);
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);

    const anchor = selectionAnchor(root, []);
    expect(anchor).not.toBeNull();
    expect(anchor!.type).toBe("text_quote");
    expect(anchor!.exact).toBe("disposable cache");
    expect(anchor!.start_line).toBe(2);
    expect(anchor!.prefix).toBe("The index is a ");
  });

  it("returns null when nothing is selected", () => {
    document.body.innerHTML = `<article id="root"><p data-line="1">x</p></article>`;
    window.getSelection()!.removeAllRanges();
    expect(selectionAnchor(document.getElementById("root") as HTMLElement, [])).toBeNull();
  });
});
