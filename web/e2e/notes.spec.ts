import { test, expect } from "@playwright/test";
import { existsSync, writeFileSync, rmSync, readdirSync } from "node:fs";

/**
 * Note tests write to a scratch workspace, never the repository (spec 07
 * section 5). They cover the notes slice: creating a free-standing note in the
 * right context panel, its shared file landing under .athenaeum/shared/notes,
 * and a note link opening the linked document heading (G4).
 */
const BOOTSTRAP = process.env.ATHENAEUM_URL;
const SCRATCH = process.env.ATHENAEUM_SCRATCH;

test.describe("Notes", () => {
  test.skip(!BOOTSTRAP || !SCRATCH, "ATHENAEUM_URL and ATHENAEUM_SCRATCH are required");

  test.beforeEach(async ({ page }) => {
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\n## Search\n\nThe index is disposable.\n");
    rmSync(`${SCRATCH}/.athenaeum`, { recursive: true, force: true });
    await page.goto(BOOTSTRAP!);
  });

  async function openNotesTab(page: import("@playwright/test").Page) {
    await page.getByRole("button", { name: "Notes", pressed: false }).click();
  }

  test("creates a shared note under .athenaeum/shared/notes and opens it", async ({ page }) => {
    await openNotesTab(page);
    await page.getByRole("button", { name: "New note" }).click();

    await page.getByLabel("New note title").fill("Design review");
    await page.getByLabel("Visibility").selectOption("shared");
    await page.getByRole("button", { name: "Create note" }).click();

    // The note opens in the main surface as its own tab.
    await expect(page.getByLabel("Note title")).toHaveValue("Design review");

    // The shared note file is committable and under the workspace.
    const dir = `${SCRATCH}/.athenaeum/shared/notes`;
    expect(existsSync(dir)).toBe(true);
    expect(readdirSync(dir).some((f) => f.endsWith(".md"))).toBe(true);

    // It appears in the notes list.
    await expect(page.locator(".note-item-title", { hasText: "Design review" })).toBeVisible();
  });

  test("a note link opens the linked document heading (G4)", async ({ page }) => {
    await openNotesTab(page);
    await page.getByRole("button", { name: "New note" }).click();

    await page.getByLabel("New note title").fill("Points at search");
    await page.getByLabel("Link to document (optional)").selectOption({ label: "Note" });
    await page.getByLabel("Link heading").fill("Search");
    await page.getByRole("button", { name: "Create note" }).click();

    // Follow the link chip in the open note.
    await page.locator(".link-chip", { hasText: "Search" }).click();

    // The linked document opens and the matched heading is revealed.
    await expect(page.locator(".preview")).toContainText("The index is disposable.");
    await expect(page.locator(".search-hit")).toBeVisible();
  });
});
