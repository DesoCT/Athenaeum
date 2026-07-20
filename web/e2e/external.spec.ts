import { test, expect } from "@playwright/test";
import { writeFileSync } from "node:fs";

/** External change handling (R6, acceptance E1 and E2). */
const BOOTSTRAP = process.env.ATHENAEUM_URL;
const SCRATCH = process.env.ATHENAEUM_SCRATCH;

test.describe("External changes", () => {
  test.skip(!BOOTSTRAP || !SCRATCH, "ATHENAEUM_URL and ATHENAEUM_SCRATCH are required");

  test.beforeEach(async ({ page }) => {
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nOriginal body.\n");
    await page.goto(BOOTSTRAP!);
    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("note");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");
    await expect(page.getByLabel("Markdown source")).toBeVisible();
  });

  // Acceptance E1: a clean editor reloads automatically with a notice.
  test("a clean editor reloads automatically and says so (E1)", async ({ page }) => {
    await expect(page.getByLabel("Markdown source")).toHaveValue("# Note\n\nOriginal body.\n");

    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nChanged by something else.\n");

    // The editor should adopt the new content without being asked.
    await expect(page.getByLabel("Markdown source")).toHaveValue(
      "# Note\n\nChanged by something else.\n",
      { timeout: 10000 },
    );
    // And the reload must be announced, not silent.
    await expect(page.getByText(/changed on disk and was reloaded/i)).toBeVisible();
    await expect(page.locator(".preview")).toContainText("Changed by something else.");
  });

  // Acceptance E2: a dirty editor must not be reloaded from under the user.
  test("a dirty editor is flagged, never reloaded (E2)", async ({ page }) => {
    await page.getByLabel("Markdown source").fill("# Note\n\nMy unsaved work.\n");
    await expect(page.getByRole("status").filter({ hasText: "Unsaved changes" })).toBeVisible();

    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nChanged behind my back.\n");

    // The warning appears, and the buffer is left exactly as typed.
    await expect(page.getByText(/changed on disk while you have unsaved edits/i)).toBeVisible({
      timeout: 10000,
    });
    await expect(page.getByLabel("Markdown source")).toHaveValue("# Note\n\nMy unsaved work.\n");
    await expect(page.getByRole("status").filter({ hasText: "Changed on disk" })).toBeVisible();
  });

  test("a document created externally appears in the tree", async ({ page }) => {
    writeFileSync(`${SCRATCH}/docs/brand-new.md`, "# Brand new\n");

    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("brand-new");
    await expect(page.getByRole("option").first()).toBeVisible({ timeout: 10000 });
  });
});
