import { test, expect } from "@playwright/test";

/**
 * Visual and behavioural checks against a running Athenaeum process.
 *
 * ATHENAEUM_URL must be the full bootstrap URL, including the launch token,
 * so the browser can obtain a session the way a real user does.
 */
const BOOTSTRAP = process.env.ATHENAEUM_URL;

/**
 * openDocument drives quick open the way a user does.
 *
 * The wait for a visible option is essential, not cosmetic: the result list is
 * derived state, and pressing Enter before it recomputes selects nothing. An
 * earlier version of this file omitted the wait and reported five failures
 * that were entirely the test's fault.
 */
async function openDocument(page: import("@playwright/test").Page, query: string) {
  await page.keyboard.press("Control+p");
  const field = page.getByLabel("Quick open query");
  await expect(field).toBeFocused();
  await field.fill(query);
  await expect(page.getByRole("option").first()).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog")).toHaveCount(0);
}

test.describe("Map Room", () => {
  test.skip(!BOOTSTRAP, "ATHENAEUM_URL is not set");

  test.beforeEach(async ({ page }) => {
    // Fail loudly on any console error: a silently broken renderer would
    // otherwise produce a screenshot that merely looks empty.
    page.on("console", (message) => {
      if (message.type() === "error") {
        throw new Error(`console error: ${message.text()}`);
      }
    });
    page.on("pageerror", (error) => {
      throw new Error(`page error: ${error.message}`);
    });

    // Force the immediate comment form on selection, so the annotation
    // screenshot below can create one without the intermediate button.
    await page.addInitScript(() => {
      try {
        localStorage.setItem("athenaeum.settings.v1", JSON.stringify({ annotateOn: "popover" }));
      } catch {
        /* storage unavailable */
      }
    });

    await page.goto(BOOTSTRAP!);

    // These checks describe a first launch, and since R13 the Map Room reopens
    // whatever was last left open. The empty session is stated as a
    // precondition rather than assumed, so a leftover tab from any earlier run
    // cannot be mistaken for a rendering fault.
    await page.evaluate(async () => {
      await fetch("/api/v1/session", {
        method: "PUT",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          schema_version: 1,
          tabs: [],
          recent: [],
          layout: { navigation: true, context: true, search: false },
        }),
      });
    });
    await page.reload();
  });

  test("home shows the workspace and its groups", async ({ page }) => {
    await expect(page.getByRole("heading", { name: "Athenaeum", level: 1 })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Specification" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Decisions" })).toBeVisible();

    // Titles must be real document titles, not file names. Regression guard
    // for the bug that screenshotting the Map Room revealed.
    await expect(page.getByText("Athenaeum Product Constitution")).toBeVisible();
    await expect(page.getByText("00-PRODUCT-CONSTITUTION", { exact: true })).toHaveCount(0);

    await page.screenshot({ path: "e2e/screenshots/01-map-room-home.png", fullPage: false });
  });

  test("file tree lists the specification documents", async ({ page }) => {
    const nav = page.getByRole("navigation", { name: "Workspace navigation" });
    await expect(nav).toBeVisible();
    await expect(nav.getByText("docs")).toBeVisible();
  });

  test("opens a document and renders it", async ({ page }) => {
    await page.keyboard.press("Control+p");
    const query = page.getByLabel("Quick open query");
    await expect(query).toBeFocused();

    await page.screenshot({ path: "e2e/screenshots/02-quick-open.png" });

    await query.fill("architecture");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");

    // The architecture document contains a Mermaid diagram, which is the
    // feature most likely to be silently broken.
    await expect(
      page.getByRole("heading", { name: /System Architecture/i, level: 1 }),
    ).toBeVisible();
    await page.waitForTimeout(2500); // allow lazy Mermaid and highlighting

    await page.screenshot({ path: "e2e/screenshots/03-document.png", fullPage: false });
  });

  test("renders a Mermaid diagram as SVG, not an empty box", async ({ page }) => {
    await openDocument(page, "architecture");

    const diagram = page.locator(".mermaid-block").first();
    await expect(diagram).toBeVisible();

    // The regression that motivated the side-channel fix: a stripped source
    // leaves the placeholder present but empty.
    await expect(diagram.locator("svg")).toBeVisible({ timeout: 15000 });
    const box = await diagram.boundingBox();
    expect(box?.height ?? 0).toBeGreaterThan(40);

    await diagram.screenshot({ path: "e2e/screenshots/04-mermaid.png" });
  });

  test("highlights fenced code", async ({ page }) => {
    await openDocument(page, "architecture");

    const code = page.locator("pre code").first();
    await expect(code).toBeVisible();
    // highlight.js wraps tokens in spans; plain text would have none.
    await expect(code.locator("span").first()).toBeVisible({ timeout: 10000 });
  });

  test("outline panel reflects the backend heading slugs", async ({ page }) => {
    await openDocument(page, "architecture");

    const outline = page.getByRole("navigation", { name: "Document outline" });
    await expect(outline).toBeVisible();
    await expect(outline.getByRole("button").first()).toBeVisible();
  });

  test("reports no outline mismatch on the specification pack", async ({ page }) => {
    await openDocument(page, "architecture");
    await expect(
      page.getByRole("heading", { name: /System Architecture/i, level: 1 }),
    ).toBeVisible();

    // ADR-0003: any disagreement between the Go outline and the rendered
    // document surfaces as this panel. It must not appear.
    await expect(page.getByText("Outline mismatch")).toHaveCount(0);
  });

  test("narrow viewport keeps the document readable", async ({ page }) => {
    await page.setViewportSize({ width: 800, height: 900 });
    await page.screenshot({ path: "e2e/screenshots/05-narrow.png" });
    await expect(page.getByRole("main")).toBeVisible();
  });

  test("a comment anchored in the margin", async ({ page }) => {
    await openDocument(page, "architecture");
    // Preview mode gives the annotation margin its full width, so the card
    // sits cleanly beside the text rather than over a narrow split pane.
    await page
      .getByRole("group", { name: "View mode" })
      .getByRole("button", { name: "Preview", exact: true })
      .click();
    // Select a paragraph in the preview and comment on it.
    await page.locator(".preview p").first().click({ clickCount: 3 });
    const column = page.locator(".annotation-column");
    await column.getByPlaceholder("Add a comment…").fill("Worth clarifying the cache authority here.");
    await column.locator(".draft-card").getByRole("button", { name: "Save" }).click();
    await expect(column.locator(".ann-card").filter({ hasText: "cache authority" })).toBeVisible();
    await page.screenshot({ path: "e2e/screenshots/10-annotation.png", fullPage: false });
  });

  test("the read-only Git panel", async ({ page }) => {
    // The Git panel reports the working-tree diff of the open document, so this
    // capture targets a doc with an uncommitted edit (made on disk by the
    // screenshot runner) shown in full-width preview.
    await openDocument(page, "architecture");
    await page
      .getByRole("group", { name: "View mode" })
      .getByRole("button", { name: "Preview", exact: true })
      .click();
    await page.getByRole("button", { name: "Git", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Working-tree diff" })).toBeVisible();
    await page.screenshot({ path: "e2e/screenshots/11-git.png", fullPage: false });
  });

  test("the context panel popped out", async ({ page }) => {
    await openDocument(page, "architecture");
    await page.getByRole("button", { name: "Git", exact: true }).click();
    await page.getByRole("button", { name: /Pop out/ }).click();
    await expect(page.getByRole("dialog", { name: "Context panel" })).toBeVisible();
    await page.screenshot({ path: "e2e/screenshots/12-context-popout.png", fullPage: false });
  });

  test("the settings menu", async ({ page }) => {
    await page.getByRole("button", { name: "Settings" }).click();
    await expect(page.getByText("Interface size")).toBeVisible();
    await page.screenshot({ path: "e2e/screenshots/13-settings.png", fullPage: false });
  });

  test("split editing: source beside live preview", async ({ page }) => {
    await openDocument(page, "architecture");
    const modes = page.getByRole("group", { name: "View mode" });
    await modes.getByRole("button", { name: "Split", exact: true }).click();
    // Both panes present: the source editor (a plain textarea) and the preview.
    await expect(page.locator(".editor-pane textarea")).toBeVisible();
    await expect(page.locator(".preview")).toBeVisible();
    await page.waitForTimeout(2500); // lazy Mermaid and highlighting in the preview
    await page.screenshot({ path: "e2e/screenshots/20-split-editing.png", fullPage: false });
  });

  test("a rendered document in preview", async ({ page }) => {
    await openDocument(page, "architecture");
    await page
      .getByRole("group", { name: "View mode" })
      .getByRole("button", { name: "Preview", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: /System Architecture/i, level: 1 }),
    ).toBeVisible();
    await expect(page.locator(".mermaid-block svg").first()).toBeVisible({ timeout: 15000 });
    await page.waitForTimeout(1500); // settle highlighting
    await page.screenshot({ path: "e2e/screenshots/21-document.png", fullPage: false });
  });

  test("workspace search with results", async ({ page }) => {
    await page.getByRole("button", { name: "Search", exact: true }).click();
    const field = page.getByLabel("Search query");
    await expect(field).toBeVisible();
    await field.fill("workspace");
    // Wait for the lexical index to return at least one hit.
    await expect(page.getByRole("listbox", { name: "Search results" }).getByRole("option").first()).toBeVisible();
    await page.screenshot({ path: "e2e/screenshots/22-search.png", fullPage: false });
  });
});
