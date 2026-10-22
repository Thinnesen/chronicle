import { defineConfig, devices } from "@playwright/test";

// The tests run against the built UI with a mocked API (see e2e/mock.ts).
export default defineConfig({
  testDir: "e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  globalTimeout: 10 * 60_000,
  retries: process.env.CI ? 1 : 0,
  // A loaded runner can need more than the default 5 s for a page to update, for example with many workers or a
  // slower browser.
  expect: { timeout: 10_000 },
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: { baseURL: "http://127.0.0.1:4173", trace: "retain-on-failure" },
  // All tests run on the desktop. On the phone, only the tests tagged @phone run: they cover the layout of the phone
  // (the menu drawer, the card list, the node panel), the screenshots and accessibility. The logic is the same.
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"] } },
    { name: "phone", use: { ...devices["Pixel 7"] }, grep: /@phone/ },
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
