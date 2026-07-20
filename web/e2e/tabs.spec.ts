import { test, expect } from "@playwright/test";
import { writeFileSync } from "node:fs";

/** Tab behaviour (spec 04 section 5). */
const BOOTSTRAP = process.env.ATHENAEUM_URL;
const SCRATCH = process.env.ATHENAEUM_SCRATCH;

test.describe("Tabs", () => {
  test.skip(!BOOTSTRAP || !SCRATCH, "ATHENAEUM_URL and ATHENAEUM_SCRATCH are required");

  test("middle click closes a tab", async ({ page }) => {
    writeFileSync(`${SCRATCH}/docs/one.md`, "# One\n");
    writeFileSync(`${SCRATCH}/docs/two.md`, "# Two\n");
    await page.goto(BOOTSTRAP!);

    async function open(query: string) {
      await page.keyboard.press("Control+p");
      await page.getByLabel("Quick open query").fill(query);
      await expect(page.getByRole("option").first()).toBeVisible();
      await page.keyboard.press("Enter");
      await expect(page.getByRole("dialog")).toHaveCount(0);
    }

    await open("one");
    await open("two");

    const tabs = page.getByRole("tab");
    await expect(tabs).toHaveCount(2);

    // Middle click the first tab. button:1 is the middle button.
    await tabs.first().click({ button: "middle" });

    await expect(tabs).toHaveCount(1);
    await expect(tabs.first()).toContainText("Two");
  });
});
