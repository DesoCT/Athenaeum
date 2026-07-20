import { test, expect } from "@playwright/test";

/**
 * Remote images (R3, N7).
 *
 * The Content-Security-Policy previously hard-coded img-src 'self' data:, so
 * every remote image was refused before a request was issued. Asserting on
 * naturalWidth rather than visibility matters here: a CSP-blocked <img> is
 * still "visible" to a locator, which is exactly how this went unnoticed.
 */
const BOOTSTRAP = process.env.ATHENAEUM_URL;

test.describe("Remote images", () => {
  test.skip(!BOOTSTRAP, "ATHENAEUM_URL is required");

  test("a remote image is not blocked by the policy", async ({ page }) => {
    const violations: string[] = [];
    page.on("console", (m) => {
      if (/Content Security Policy|Refused to load/i.test(m.text())) violations.push(m.text());
    });

    // The upstream host is third-party, so the request is stubbed: this test is
    // about the policy permitting the load, not about that server being up.
    await page.route("**/datarob.com/**", (route) =>
      route.fulfill({
        status: 200,
        contentType: "image/png",
        // 1x1 PNG.
        body: Buffer.from(
          "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
          "base64",
        ),
      }),
    );

    await page.goto(BOOTSTRAP!);
    await page.keyboard.press("Control+p");
    await page.getByLabel("Quick open query").fill("remote");
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");

    const image = page.locator('img[data-remote="true"]').first();
    await expect(image).toBeVisible();
    await expect
      .poll(() => image.evaluate((el: HTMLImageElement) => el.naturalWidth))
      .toBeGreaterThan(0);

    expect(violations).toEqual([]);
  });
});
