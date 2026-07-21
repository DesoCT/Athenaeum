import { test, expect } from "@playwright/test";
import { writeFileSync, rmSync } from "node:fs";

/**
 * Map Room home tests write to a scratch workspace, never the repository
 * (spec 07 section 5). They cover the pin and unresolved-annotation summaries
 * on the home screen (spec 04 section 3).
 */
const BOOTSTRAP = process.env.ATHENAEUM_URL;
const SCRATCH = process.env.ATHENAEUM_SCRATCH;

test.describe("Map Room home", () => {
  test.skip(!BOOTSTRAP || !SCRATCH, "ATHENAEUM_URL and ATHENAEUM_SCRATCH are required");

  test.beforeEach(async ({ page }) => {
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nThe index is a disposable cache.\n");
    rmSync(`${SCRATCH}/.athenaeum`, { recursive: true, force: true });
    await page.goto(BOOTSTRAP!);
  });

  test("shows an unresolved comment on the home summary and opens it", async ({ page }) => {
    // Open the doc, select a phrase, add a shared comment.
    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("note");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");
    await page.locator(".preview p", { hasText: "disposable cache" }).click({ clickCount: 3 });

    const column = page.locator(".annotation-column");
    await column.getByPlaceholder("Add a comment…").fill("Please review this");
    await column.locator(".draft-card select").selectOption("shared");
    await column.locator(".draft-card").getByRole("button", { name: "Save" }).click();
    await expect(column.locator(".ann-card").filter({ hasText: "Please review this" })).toBeVisible();

    // Close the tab to return to the Map Room home.
    await page.locator(".tab", { hasText: "Note" }).getByRole("button").click();

    // The unresolved comment appears in the home summary.
    const unresolved = page.locator(".card", { hasText: "Unresolved" });
    await expect(unresolved).toBeVisible();
    await expect(unresolved).toContainText("Please review this");

    // Following it reopens the document.
    await unresolved.getByText("Please review this").click();
    await expect(page.locator(".preview")).toContainText("disposable cache");
  });
});
