import { test, expect } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";

/**
 * Editing tests write to a scratch workspace, never the repository
 * (spec 07 section 5). ATHENAEUM_SCRATCH is the path of the writable fixture.
 */
const BOOTSTRAP = process.env.ATHENAEUM_URL;
const SCRATCH = process.env.ATHENAEUM_SCRATCH;

test.describe("Editing", () => {
  test.skip(!BOOTSTRAP || !SCRATCH, "ATHENAEUM_URL and ATHENAEUM_SCRATCH are required");

  test.beforeEach(async ({ page }) => {
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nOriginal body.\n");
    await page.goto(BOOTSTRAP!);
  });

  async function openNote(page: import("@playwright/test").Page) {
    await page.keyboard.press("Control+p");
    const field = page.getByLabel("Quick open query");
    await expect(field).toBeFocused();
    await field.fill("note");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByLabel("Markdown source")).toBeVisible();
  }

  test("edits source, sees preview update, saves, and persists (D1)", async ({ page }) => {
    await openNote(page);

    const editor = page.getByLabel("Markdown source");
    await expect(page.getByRole("status").filter({ hasText: "Saved" })).toBeVisible();

    await editor.fill("# Note\n\nEdited body.\n");
    await expect(page.getByRole("status").filter({ hasText: "Unsaved changes" })).toBeVisible();

    // The preview reflects the buffer, not the file.
    await expect(page.locator(".preview")).toContainText("Edited body.");

    await page.keyboard.press("Control+s");
    await expect(page.getByRole("status").filter({ hasText: "Saved" })).toBeVisible();

    expect(readFileSync(`${SCRATCH}/docs/note.md`, "utf8")).toBe("# Note\n\nEdited body.\n");

    // Reload and confirm the persisted content is what comes back.
    await page.reload();
    await openNote(page);
    await expect(page.getByLabel("Markdown source")).toHaveValue("# Note\n\nEdited body.\n");
  });

  test("an external change under an unsaved buffer becomes a conflict (E2)", async ({ page }) => {
    await openNote(page);

    await page.getByLabel("Markdown source").fill("# Note\n\nMy local edit.\n");
    await expect(page.getByRole("status").filter({ hasText: "Unsaved changes" })).toBeVisible();

    // Something else rewrites the file.
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nChanged on disk.\n");

    await page.keyboard.press("Control+s");

    // Both versions must be visible and nothing written.
    const conflict = page.getByRole("region", { name: "Conflict" });
    await expect(conflict).toBeVisible();
    await expect(conflict).toContainText("My local edit.");
    await expect(conflict).toContainText("Changed on disk.");
    expect(readFileSync(`${SCRATCH}/docs/note.md`, "utf8")).toBe("# Note\n\nChanged on disk.\n");
  });

  test("keeping the local version resolves the conflict", async ({ page }) => {
    await openNote(page);
    await page.getByLabel("Markdown source").fill("# Note\n\nMine wins.\n");
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nDisk.\n");
    await page.keyboard.press("Control+s");

    await page.getByRole("button", { name: /Keep my version/ }).click();
    await expect(page.getByRole("status").filter({ hasText: "Saved" })).toBeVisible();
    expect(readFileSync(`${SCRATCH}/docs/note.md`, "utf8")).toBe("# Note\n\nMine wins.\n");
  });

  test("accepting the disk version discards the buffer only when chosen", async ({ page }) => {
    await openNote(page);
    await page.getByLabel("Markdown source").fill("# Note\n\nMine.\n");
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nDisk wins.\n");
    await page.keyboard.press("Control+s");

    await page.getByRole("button", { name: /Use the disk version/ }).click();
    await expect(page.getByLabel("Markdown source")).toHaveValue("# Note\n\nDisk wins.\n");
    expect(readFileSync(`${SCRATCH}/docs/note.md`, "utf8")).toBe("# Note\n\nDisk wins.\n");
  });

  test("read-only documents offer no save control (B3)", async ({ page }) => {
    await page.keyboard.press("Control+p");
    const field = page.getByLabel("Quick open query");
    await field.fill("readonly");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");

    // Anchored: the explanation banner also mentions "read-only", and a bare
    // substring filter matches both it and the status chip.
    await expect(
      page.getByRole("status").filter({ hasText: /^Read-only( \(not writable\))?$/ }),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: /^Save/ })).toHaveCount(0);
    await expect(page.getByLabel("Markdown source")).toHaveAttribute("readonly", "");
  });

  test("split, source, and preview modes all work", async ({ page }) => {
    await openNote(page);

    await expect(page.getByLabel("Markdown source")).toBeVisible();
    await expect(page.locator(".preview")).toBeVisible();

    await page.getByRole("button", { name: "Source" }).click();
    await expect(page.locator(".preview")).toHaveCount(0);

    await page.getByRole("button", { name: "Preview" }).click();
    await expect(page.getByLabel("Markdown source")).toHaveCount(0);
    await expect(page.locator(".preview")).toBeVisible();

    await page.getByRole("button", { name: "Split" }).click();
    await page.screenshot({ path: "e2e/screenshots/06-split-editing.png" });
  });
});

test.describe("Preview mode", () => {
  test.skip(!BOOTSTRAP || !SCRATCH, "ATHENAEUM_URL and ATHENAEUM_SCRATCH are required");

  test.beforeEach(async ({ page }) => {
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\n## Section\n\nBody.\n");
    await page.goto(BOOTSTRAP!);
  });

  // Regression: clicking in the preview switched the view back to split, which
  // yanked the user out of a mode they had chosen deliberately.
  test("clicking in preview-only stays in preview-only", async ({ page }) => {
    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("note");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");

    await page.getByRole("button", { name: "Preview" }).click();
    await expect(page.getByLabel("Markdown source")).toHaveCount(0);

    // Click a heading, which is what previously forced split view.
    await page.getByRole("heading", { name: "Section" }).click();

    await expect(page.getByLabel("Markdown source")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Preview" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  test("clicking a heading in split view still moves the caret", async ({ page }) => {
    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("note");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");

    await expect(page.getByLabel("Markdown source")).toBeVisible();
    await page.getByRole("heading", { name: "Section" }).click();
    await expect(page.getByLabel("Markdown source")).toBeFocused();
  });
});
