import { test, expect } from "@playwright/test";

/**
 * Captures the screenshots used in README.md.
 *
 * These are documentation artifacts, not assertions, but each shot still waits
 * for the state it is meant to show — a screenshot of a half-rendered page is
 * worse than none, because it looks authoritative.
 *
 * Run against the repository's own workspace:
 *   ATHENAEUM_URL=<bootstrap URL> npx playwright test e2e/capture.spec.ts
 */
const BOOTSTRAP = process.env.ATHENAEUM_URL;
const SHOTS = "../docs/screenshots";

test.describe("README screenshots", () => {
  test.skip(!BOOTSTRAP, "ATHENAEUM_URL is required");

  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(BOOTSTRAP!);
  });

  async function open(page: import("@playwright/test").Page, query: string) {
    await page.keyboard.press("Control+p");
    const field = page.getByLabel("Quick open query");
    await expect(field).toBeFocused();
    await field.fill(query);
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog")).toHaveCount(0);
  }

  test("map room", async ({ page }) => {
    await expect(page.getByRole("heading", { name: "Athenaeum", level: 1 })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Specification" })).toBeVisible();
    await page.screenshot({ path: `${SHOTS}/map-room.png` });
  });

  test("document with a diagram", async ({ page }) => {
    await open(page, "architecture");
    await expect(
      page.getByRole("heading", { name: /System Architecture/i, level: 1 }),
    ).toBeVisible();

    // Wait for the diagram to actually render, not merely for its placeholder.
    const diagram = page.locator(".mermaid-block").first();
    await expect(diagram.locator("svg")).toBeVisible({ timeout: 20000 });
    await page.getByRole("button", { name: "Preview" }).click();
    await page.waitForTimeout(600);
    await page.screenshot({ path: `${SHOTS}/document.png` });
  });

  test("split editing", async ({ page }) => {
    await open(page, "0003-backend");
    await expect(page.getByLabel("Markdown source")).toBeVisible();
    await expect(page.locator(".preview")).toBeVisible();
    await page.waitForTimeout(800);
    await page.screenshot({ path: `${SHOTS}/split-editing.png` });
  });

  test("search", async ({ page }) => {
    // Cmd/Ctrl+Shift+F is the workspace-search shortcut (spec 04 section 14).
    await page.keyboard.press("Control+Shift+F");

    const field = page.getByLabel("Search query");
    await expect(field).toBeVisible({ timeout: 5000 });
    await field.fill("atomic write");

    // Wait for a result rather than a fixed delay, so the shot is never of an
    // empty list that happens to be mid-query.
    await expect(page.getByRole("listitem").first()).toBeVisible({ timeout: 10000 });
    await page.waitForTimeout(400);
    await page.screenshot({ path: `${SHOTS}/search.png` });
  });
});
