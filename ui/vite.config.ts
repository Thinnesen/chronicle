import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// In development, the API and the login run on a local chronicle-server.
const api = process.env.CHRONICLE_SERVER ?? "http://localhost:8080";

// The same Content Security Policy as the Go server (internal/server/static.go), so that tests find violations.
const csp =
  "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; " +
  "connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'";

export default defineConfig({
  preview: { headers: { "Content-Security-Policy": csp } },
  plugins: [react()],
  // No inline assets: the CSP allows fonts and images only from the server itself.
  build: { sourcemap: false, chunkSizeWarningLimit: 900, assetsInlineLimit: 0 },
  server: {
    proxy: {
      "/chronicle.v1.": api,
      "/auth/": api,
      "/artifacts/": api,
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
