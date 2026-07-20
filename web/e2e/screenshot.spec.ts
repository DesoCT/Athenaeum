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

    await page.goto(BOOTSTRAP!);
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
});
