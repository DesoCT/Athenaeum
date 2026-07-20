import { defineConfig, devices } from "@playwright/test";

// Browser tests run against an already-running Athenaeum process, driven by
// ATHENAEUM_URL (the bootstrap URL including its launch token).
export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: {
    ...devices["Desktop Chrome"],
    viewport: { width: 1440, height: 900 },
    ignoreHTTPSErrors: true,
  },
});
