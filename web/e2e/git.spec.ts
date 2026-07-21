import { test, expect } from "@playwright/test";
import { writeFileSync } from "node:fs";

/**
 * Git panel tests write to a scratch workspace, never the repository (spec 07
 * section 5). The Git panel only appears when the workspace is a Git repository
 * and `git` is on PATH; when it is not, the tab is absent and these tests skip,
 * matching acceptance J4 (core functionality does not depend on Git).
 */
const BOOTSTRAP = process.env.ATHENAEUM_URL;
const SCRATCH = process.env.ATHENAEUM_SCRATCH;

test.describe("Git panel", () => {
  test.skip(!BOOTSTRAP || !SCRATCH, "ATHENAEUM_URL and ATHENAEUM_SCRATCH are required");

  test("shows working-tree diff and history for a tracked, edited document (J1, J2)", async ({ page }) => {
    await page.goto(BOOTSTRAP!);

    // The Git tab is present only for a repository workspace (J4). Skip cleanly
    // otherwise rather than failing on a workspace Athenaeum cannot give Git for.
    const gitTab = page.getByRole("button", { name: "Git", exact: true });
    test.skip((await gitTab.count()) === 0, "workspace is not a Git repository");

    // Edit a document so there is an uncommitted change to diff.
    writeFileSync(`${SCRATCH}/docs/note.md`, "# Note\n\nAn edit for the diff.\n");

    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("note");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");

    await gitTab.click();

    // The diff and history sections render for the open document.
    await expect(page.locator(".git-section", { hasText: "Working-tree diff" })).toBeVisible();
    await expect(page.locator(".git-section", { hasText: "History" })).toBeVisible();
  });
});
