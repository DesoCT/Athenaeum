import { test, expect } from "@playwright/test";

/** Local image loading (R3). */
const BOOTSTRAP = process.env.ATHENAEUM_URL;

test.describe("Local images", () => {
  test.skip(!BOOTSTRAP, "ATHENAEUM_URL is required");

  test("a relative local image actually loads", async ({ page }) => {
    const failed: string[] = [];
    page.on("requestfailed", (r) => failed.push(r.url()));
    page.on("response", (r) => {
      if (r.url().includes("/api/v1/assets/") && !r.ok()) {
        failed.push(`${r.status()} ${r.url()}`);
      }
    });

    await page.goto(BOOTSTRAP!);
    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("images");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");

    const sibling = page.locator('img[data-local-asset="docs/img/swatch.png"]');
    await expect(sibling).toBeVisible();

    // naturalWidth is 0 when a browser failed to decode the image, which is
    // how a broken <img> looks to a test that only checks visibility.
    await expect
      .poll(() => sibling.evaluate((el: HTMLImageElement) => el.naturalWidth))
      .toBeGreaterThan(0);

    // A parent-relative path must resolve too.
    const parent = page.locator('img[data-local-asset="assets-test.png"]');
    await expect
      .poll(() => parent.evaluate((el: HTMLImageElement) => el.naturalWidth))
      .toBeGreaterThan(0);

    expect(failed.filter((f) => f.includes("/api/v1/assets/"))).toEqual([]);
    await page.screenshot({ path: "e2e/screenshots/08-images.png" });
  });
});
