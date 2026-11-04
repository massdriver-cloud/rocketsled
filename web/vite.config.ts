import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const backend = process.env.ROCKETSLED_BACKEND ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true, sourcemap: false },
  server: {
    port: 5173,
    proxy: {
      "/api": backend,
      "/auth": backend,
      "/hooks": backend,
      "/ci": backend,
    },
  },
});
