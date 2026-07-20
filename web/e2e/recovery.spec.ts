import { test, expect } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";

/**
 * Crash recovery (R13, acceptance E3).
 *
 * The server is restarted between the edit and the check, so this exercises
 * the real durability path rather than in-memory state.
 */
const BOOTSTRAP = process.env.ATHENAEUM_URL;
const RESTART_URL = process.env.ATHENAEUM_RESTART_URL;
const SCRATCH = process.env.ATHENAEUM_SCRATCH;

test.describe("Crash recovery", () => {
  test.skip(!BOOTSTRAP || !SCRATCH, "ATHENAEUM_URL and ATHENAEUM_SCRATCH are required");

  test("an unsaved buffer is offered after a restart, never applied (E3)", async ({ page }) => {
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nOriginal body.\n");

    await page.goto(BOOTSTRAP!);
    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("note");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");

    await page.getByLabel("Markdown source").fill("# Note\n\nWork I never saved.\n");
    await expect(page.getByRole("status").filter({ hasText: "Unsaved changes" })).toBeVisible();

    // Give the debounced recovery write time to land.
    await page.waitForTimeout(1500);

    // The file on disk must be untouched: recovery is not a save.
    expect(readFileSync(`${SCRATCH}/docs/note.md`, "utf8")).toBe("# Note\n\nOriginal body.\n");

    // Simulate an abnormal exit by reloading against a freshly started server.
    await page.goto(RESTART_URL ?? BOOTSTRAP!);

    const prompt = page.getByRole("region", { name: "Unsaved work found" });
    await expect(prompt).toBeVisible();
    await expect(prompt).toContainText("docs/note.md");

    // Still nothing written, and the buffer was not applied on its own.
    expect(readFileSync(`${SCRATCH}/docs/note.md`, "utf8")).toBe("# Note\n\nOriginal body.\n");

    await page.screenshot({ path: "e2e/screenshots/07-recovery.png" });

    // Restoring loads the unsaved text as a dirty buffer, not as a save.
    await prompt.getByRole("button", { name: "Open with these edits" }).click();
    await expect(page.getByLabel("Markdown source")).toHaveValue("# Note\n\nWork I never saved.\n");
    await expect(page.getByRole("status").filter({ hasText: "Unsaved changes" })).toBeVisible();
    expect(readFileSync(`${SCRATCH}/docs/note.md`, "utf8")).toBe("# Note\n\nOriginal body.\n");
  });

  test("saving clears the recovery buffer", async ({ page }) => {
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nOriginal body.\n");

    await page.goto(BOOTSTRAP!);
    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("note");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");

    await page.getByLabel("Markdown source").fill("# Note\n\nSaved properly.\n");
    await page.waitForTimeout(1200);
    await page.keyboard.press("Control+s");
    await expect(page.getByRole("status").filter({ hasText: "Saved" })).toBeVisible();
    await page.waitForTimeout(500);

    // A fresh load must not offer recovery for work that reached disk.
    await page.goto(BOOTSTRAP!);
    await expect(page.getByRole("region", { name: "Unsaved work found" })).toHaveCount(0);
  });
});
