import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Built into internal/web/dist and embedded in the kuli binary.
export default defineConfig({
  plugins: [react()],
  base: "./",
  build: { outDir: "../internal/web/dist", emptyOutDir: true, chunkSizeWarningLimit: 800 },
  server: { proxy: { "/api": "http://127.0.0.1:7878" } },
});
