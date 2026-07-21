import { test, expect } from "@playwright/test";
import { writeFileSync } from "node:fs";

/**
 * Relationship tests write to a scratch workspace, never the repository
 * (spec 07 section 5). They cover the Links context tab: outgoing links and
 * backlinks with their source labels (H1), and navigation to a related
 * document.
 */
const BOOTSTRAP = process.env.ATHENAEUM_URL;
const SCRATCH = process.env.ATHENAEUM_SCRATCH;

test.describe("Relationships", () => {
  test.skip(!BOOTSTRAP || !SCRATCH, "ATHENAEUM_URL and ATHENAEUM_SCRATCH are required");

  test.beforeEach(async ({ page }) => {
    writeFileSync(`${SCRATCH}/docs/hub.md`, "# Hub\n\nA hub document.\n");
    writeFileSync(`${SCRATCH}/docs/one.md`, "# One\n\nLinks to [the hub](hub.md).\n");
    await page.goto(BOOTSTRAP!);
  });

  async function openDoc(page: import("@playwright/test").Page, query: string) {
    await page.keyboard.press("Control+p");
    const field = page.getByLabel("Quick open query");
    await field.fill(query);
    await expect(page.getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog")).toHaveCount(0);
  }

  test("shows outgoing links and backlinks with source labels, and navigates (H1)", async ({ page }) => {
    await openDoc(page, "one");
    await expect(page.locator(".preview")).toContainText("Links to");

    // The Links tab shows one.md's outgoing markdown link to the hub.
    await page.getByRole("button", { name: "Links", pressed: false }).click();
    const outgoing = page.locator(".rel-section", { hasText: "Outgoing" });
    await expect(outgoing.locator(".rel-item", { hasText: "Hub" })).toBeVisible();
    await expect(outgoing.locator(".rel-source", { hasText: "link" })).toBeVisible();

    // Following it opens the hub, whose Links tab now shows the backlink.
    await outgoing.locator(".rel-item", { hasText: "Hub" }).click();
    await expect(page.locator(".preview")).toContainText("A hub document.");
    await expect(
      page.locator(".rel-section", { hasText: "Backlinks" }).locator(".rel-item", { hasText: "One" }),
    ).toBeVisible();
  });
});
