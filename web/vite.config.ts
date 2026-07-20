import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// The Go process serves the compiled output from web/dist via go:embed, so the
// build must emit relative, self-contained assets under that directory.
export default defineConfig({
  plugins: [svelte()],
  resolve: {
    // Mermaid bundles its own KaTeX for maths in diagram labels. Without
    // deduplication the release embeds two ~260 kB copies (constitution C6).
    dedupe: ["katex"],
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // Source maps are development aids. Embedding them would roughly triple
    // the size of the single executable, so they are opt-in via
    // ATHENAEUM_SOURCEMAPS=1 rather than shipped by default.
    sourcemap: process.env.ATHENAEUM_SOURCEMAPS === "1",
    // Mermaid's per-diagram chunks legitimately exceed the default warning
    // threshold, and each is lazy-loaded, so the warning is noise here.
    chunkSizeWarningLimit: 800,
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
