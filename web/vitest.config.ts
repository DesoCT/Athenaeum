import { defineConfig } from "vitest/config";
import { svelte } from "@sveltejs/vite-plugin-svelte";

export default defineConfig({
  // HMR is off under the test runner already; vite-plugin-svelte 7 removed the
  // explicit `hot` option, so passing it is now an "invalid option" warning.
  plugins: [svelte()],
  test: {
    // The renderer manipulates DOM during sanitisation and heading
    // reconciliation, so tests need a document.
    environment: "jsdom",
    include: ["src/**/*.test.ts"],
    exclude: ["e2e/**"],
  },
});
