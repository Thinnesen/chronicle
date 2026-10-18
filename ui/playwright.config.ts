import { defineConfig, devices } from "@playwright/test";

// The tests run against the built UI with a mocked API (see e2e/mock.ts).
export default defineConfig({
  testDir: "e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  globalTimeout: 10 * 60_000,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: { baseURL: "http://127.0.0.1:4173", trace: "retain-on-failure" },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"] } },
    { name: "phone", use: { ...devices["Pixel 7"] } },
  ],
  // The shim starts vite directly. Through "pnpm exec", the stop signal did not reach vite, and CI waited for 10 minutes.
  webServer: {
    command: "./node_modules/.bin/vite preview --host 127.0.0.1 --port 4173 --strictPort",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
    gracefulShutdown: { signal: "SIGTERM", timeout: 2000 },
  },
});
