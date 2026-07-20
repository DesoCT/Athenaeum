import { test, expect, type Page } from "@playwright/test";

/**
 * Acceptance F4: a generated corpus of 5,000 documents remains navigable and
 * searchable without blocking primary UI interaction.
 *
 * Opt-in, because it needs a server pointed at the generated scale fixture:
 *
 *   make test-scale                      # generates the corpus
 *   ./bin/athenaeum serve <fixture>/athenaeum.toml --port 79xx --no-open
 *   ATHENAEUM_SCALE_URL="<bootstrap URL>" npx playwright test e2e/scale.spec.ts
 */
const SCALE_URL = process.env.ATHENAEUM_SCALE_URL;

/** waitForIndex blocks until the status bar reports a settled index. */
async function waitForIndex(page: Page) {
  await expect(page.getByRole("status").filter({ hasText: /^Index: ready/ }).first()).toBeVisible({
    timeout: 120_000,
  });
}

function searchResults(page: Page) {
  return page.getByRole("listbox", { name: "Search results" }).getByRole("option");
}

test.describe("Scale (F4)", () => {
  test.skip(!SCALE_URL, "ATHENAEUM_SCALE_URL is not set");
  test.setTimeout(180_000);

  test.beforeEach(async ({ page }) => {
    await page.goto(SCALE_URL!);
    // Start from a clean session so the measurements describe a cold open.
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

  test("the workspace opens and reports the full corpus", async ({ page }) => {
    await expect(page.getByRole("heading", { name: "Scale Fixture", level: 1 })).toBeVisible();
    await expect(page.getByText("5000", { exact: true })).toBeVisible();
    await waitForIndex(page);
  });

  test("the file tree is navigable at 5,000 documents", async ({ page }) => {
    const nav = page.getByRole("navigation", { name: "Workspace navigation" });
    await expect(nav).toBeVisible();

    const started = Date.now();
    await nav.getByRole("button", { name: "area-00" }).first().click();
    await expect(nav.getByRole("button", { name: "part-00" }).first()).toBeVisible();
    const elapsed = Date.now() - started;

    // Expanding a directory is a primary interaction; it must feel immediate.
    expect(elapsed).toBeLessThan(2000);
  });

  test("quick open stays responsive", async ({ page }) => {
    const started = Date.now();
    await page.keyboard.press("Control+p");
    const field = page.getByLabel("Quick open query");
    await field.fill("doc-04242");
    const results = page.getByRole("listbox", { name: "Results" }).getByRole("option");
    await expect(results.first()).toBeVisible();
    await expect(results.first()).toContainText("doc-04242.md");
    expect(Date.now() - started).toBeLessThan(5000);
  });

  test("search returns located results across the corpus", async ({ page }) => {
    await waitForIndex(page);
    await page.keyboard.press("Control+Shift+F");
    const field = page.getByLabel("Search query");
    await expect(field).toBeFocused();

    // A token the generator plants in exactly one document, so the assertion is
    // about correctness at scale rather than about result-set size.
    await field.fill("zqx02500");
    const first = searchResults(page).first();
    await expect(first).toBeVisible({ timeout: 30_000 });
    await expect(first).toContainText("doc-02500.md");
    await expect(first).toContainText(/line \d+/);
    await expect(first.locator("mark").first()).toBeVisible();
  });

  test("opening a search result lands on the matched line", async ({ page }) => {
    await waitForIndex(page);
    await page.keyboard.press("Control+Shift+F");
    await page.getByLabel("Search query").fill("zqx01234");

    const first = searchResults(page).first();
    await expect(first).toBeVisible({ timeout: 30_000 });
    await first.click();

    const editor = page.getByLabel("Markdown source");
    await expect(editor).toBeVisible({ timeout: 30_000 });

    const selected = await editor.evaluate((node) => {
      const area = node as HTMLTextAreaElement;
      return area.value.slice(area.selectionStart, area.selectionEnd);
    });
    expect(selected).toContain("zqx01234");
  });

  // The core of F4: indexing must not block what the user is doing.
  test("the interface stays responsive while the index rebuilds (N2)", async ({ page }) => {
    await waitForIndex(page);
    await page.keyboard.press("Control+Shift+F");
    await page.getByRole("button", { name: "Rebuild" }).click();

    // Drive a primary interaction repeatedly while the rebuild runs, and record
    // the worst round trip.
    let worst = 0;
    for (let i = 0; i < 8; i++) {
      const started = Date.now();
      await page.keyboard.press("Control+p");
      const field = page.getByLabel("Quick open query");
      await field.fill(`doc-0${1000 + i}`);
      const results = page.getByRole("listbox", { name: "Results" }).getByRole("option");
      await expect(results.first()).toBeVisible();
      await page.keyboard.press("Escape");
      worst = Math.max(worst, Date.now() - started);
    }

    console.log(`F4/N2: worst quick-open round trip during rebuild: ${worst} ms`);
    expect(worst).toBeLessThan(3000);
  });
});
