import { defineConfig } from "vitest/config";
import { svelte } from "@sveltejs/vite-plugin-svelte";

export default defineConfig({
  plugins: [svelte({ hot: false })],
  test: {
    // The renderer manipulates DOM during sanitisation and heading
    // reconciliation, so tests need a document.
    environment: "jsdom",
    include: ["src/**/*.test.ts"],
  },
});
