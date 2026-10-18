import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// In development, the API and the login run on a local chronicle-server.
const api = process.env.CHRONICLE_SERVER ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  build: { sourcemap: false, chunkSizeWarningLimit: 900 },
  server: {
    proxy: {
      "/chronicle.v1.": api,
      "/auth/": api,
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
