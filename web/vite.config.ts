import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// The Go process serves the compiled output from web/dist via go:embed, so the
// build must emit relative, self-contained assets under that directory.
export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: true,
  },
  server: {
    port: 5173,
    strictPort: true,
    // In development the Svelte dev server owns the origin and forwards API
    // calls to the Go process. Launch Athenaeum with
    // ATHENAEUM_DEV_ORIGIN=http://localhost:5173 so the origin policy accepts
    // mutating requests from this origin (spec 02 section 8).
    proxy: {
      "/api": { target: "http://127.0.0.1:7777", changeOrigin: false },
      "/bootstrap": { target: "http://127.0.0.1:7777", changeOrigin: false },
    },
  },
});
