import { test, expect } from "@playwright/test";
import { existsSync, writeFileSync, rmSync } from "node:fs";

/**
 * Annotation tests write to a scratch workspace, never the repository
 * (spec 07 section 5). They cover the annotations slice: creating an anchored
 * comment, its shared sidecar landing under .athenaeum/shared (G2), and
 * survival across a reload.
 */
const BOOTSTRAP = process.env.ATHENAEUM_URL;
const SCRATCH = process.env.ATHENAEUM_SCRATCH;

test.describe("Annotations", () => {
  test.skip(!BOOTSTRAP || !SCRATCH, "ATHENAEUM_URL and ATHENAEUM_SCRATCH are required");

  test.beforeEach(async ({ page }) => {
    // A fresh document and no prior shared sidecar, so each run starts clean.
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nThe index is a disposable cache.\n");
    rmSync(`${SCRATCH}/.athenaeum`, { recursive: true, force: true });
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
    await expect(page.locator(".preview")).toContainText("disposable cache");
  }

  async function selectPhrase(page: import("@playwright/test").Page) {
    // Triple-click selects the paragraph, which the layer turns into an anchor.
    await page.locator(".preview p", { hasText: "disposable cache" }).click({ clickCount: 3 });
  }

  test("creates a shared annotation that persists and lands under .athenaeum/shared (G2)", async ({ page }) => {
    await openNote(page);
    await selectPhrase(page);

    const column = page.locator(".annotation-column");
    await expect(column.locator(".draft-card")).toBeVisible();

    await column.getByPlaceholder("Add a comment…").fill("Is the cache authoritative?");
    await column.locator(".draft-card select").selectOption("shared");
    await column.locator(".draft-card").getByRole("button", { name: "Save" }).click();

    // The card appears in the margin with its body and a shared badge.
    const card = column.locator(".ann-card").filter({ hasText: "Is the cache authoritative?" });
    await expect(card).toBeVisible();
    await expect(card.locator(".badge.shared")).toBeVisible();

    // The shared sidecar is committable and lives under the workspace (G2).
    expect(existsSync(`${SCRATCH}/.athenaeum/shared/annotations/docs/note.md.json`)).toBe(true);

    // It survives a reload, read back from disk.
    await page.reload();
    await openNote(page);
    await expect(
      page.locator(".annotation-column .ann-card").filter({ hasText: "Is the cache authoritative?" }),
    ).toBeVisible();
  });

  test("resolves and deletes an annotation", async ({ page }) => {
    await openNote(page);
    await selectPhrase(page);

    const column = page.locator(".annotation-column");
    await column.getByPlaceholder("Add a comment…").fill("Temporary note");
    await column.locator(".draft-card").getByRole("button", { name: "Save" }).click();

    const card = column.locator(".ann-card").filter({ hasText: "Temporary note" });
    await expect(card).toBeVisible();

    await card.getByRole("button", { name: "Resolve" }).click();
    await expect(card.locator(".badge.done")).toBeVisible();

    await card.getByRole("button", { name: "Delete" }).click();
    await expect(column.locator(".ann-card").filter({ hasText: "Temporary note" })).toHaveCount(0);
  });
});
