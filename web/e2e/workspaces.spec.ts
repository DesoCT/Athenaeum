import { test, expect } from "@playwright/test";

/** Workspace registry and switching (R1, R2, ADR-0004). */
const BOOTSTRAP = process.env.ATHENAEUM_URL;

test.describe("Workspace switching", () => {
  test.skip(!BOOTSTRAP, "ATHENAEUM_URL is required (start with --pick and a registry)");

  test("picks a workspace, then drills back out and picks the other", async ({ page }) => {
    await page.goto(BOOTSTRAP!);

    // Workspace selection is server-side and one process serves one workspace,
    // so a previous test may have left it inside a workspace. Return to the
    // picker first, making this test independent of run order.
    await page.evaluate(() =>
      fetch("/api/v1/workspaces/leave", { method: "POST", credentials: "same-origin" }),
    );
    await page.reload();

    // The picker lists both registered workspaces.
    await expect(page.getByRole("heading", { name: "Choose a workspace" })).toBeVisible();
    await expect(page.getByText("Alpha WS")).toBeVisible();
    await expect(page.getByText("Beta WS")).toBeVisible();

    // Open Alpha and confirm its document is reachable.
    await page.getByRole("button", { name: /Alpha WS/ }).click();
    await expect(page.getByRole("heading", { name: "Choose a workspace" })).toHaveCount(0);
    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("alpha");
    await expect(page.getByRole("option").first()).toBeVisible();
    await expect(page.getByRole("option").first()).toContainText("alpha.md");
    await page.keyboard.press("Escape");

    await page.screenshot({ path: "e2e/screenshots/09-picker-opened.png" });

    // Drill back out to the picker.
    await page.getByRole("button", { name: "Workspaces" }).click();
    await expect(page.getByRole("heading", { name: "Choose a workspace" })).toBeVisible();
    await expect(page.getByText("Beta WS")).toBeVisible();

    // Open Beta and confirm ITS document is reachable, and Alpha's is not.
    await page.getByRole("button", { name: /Beta WS/ }).click();
    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("beta");
    await expect(page.getByRole("option").first()).toContainText("beta.md");

    await page.getByLabel("Quick open query").fill("alpha");
    // Alpha's document must not appear in Beta's workspace.
    await expect(page.getByRole("option").filter({ hasText: "alpha.md" })).toHaveCount(0);
  });
});
