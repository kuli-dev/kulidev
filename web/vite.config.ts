import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const API = "http://127.0.0.1:7878";

// Built into internal/web/dist and embedded in the kuli binary.
export default defineConfig({
  plugins: [react()],
  base: "./",
  build: { outDir: "../internal/web/dist", emptyOutDir: true, chunkSizeWarningLimit: 4500 },
  server: {
    // kuli serve refuses cross-origin POSTs; in dev the browser's origin is Vite's,
    // so present the request as coming from the kuli server itself.
    proxy: { "/api": { target: API, configure: (p) => p.on("proxyReq", (req) => req.setHeader("origin", API)) } },
  },
});
