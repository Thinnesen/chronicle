import { expect, test } from "@playwright/test";

import { mockAPI } from "./mock";

test("sends a visitor without a session to the login page", async ({ page }) => {
  await mockAPI(page, { loggedIn: false });
  await page.goto("/ns/team-a");
  await expect(page.getByRole("heading", { name: "chronicle" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Log in" })).toHaveAttribute("href", "/auth/login?redirect=%2Fns%2Fteam-a");
});

test("lets the user choose a namespace and shows its runs", async ({ page }) => {
  // The strict CSP must not block any part of the page, for example an inlined font.
  const violations: string[] = [];
  page.on("console", (m) => m.text().includes("Content Security Policy") && violations.push(m.text().slice(0, 120)));
  await mockAPI(page);
  await page.goto("/");
  await page.getByRole("link", { name: "team-a" }).click();
  await expect(page.getByRole("heading", { name: "Runs in team-a" })).toBeVisible();
  await expect(page.getByRole("link", { name: "etl-nightly-8k2pq" })).toBeVisible();
  await expect(page.getByRole("img", { name: "3 of 7 nodes done" })).toBeVisible();
  expect(violations).toEqual([]);
});

test("opens the only namespace at once", async ({ page }) => {
  await mockAPI(page, { namespaces: ["team-a"] });
  await page.goto("/");
  await expect(page).toHaveURL(/\/ns\/team-a$/);
});

test("switches the theme and keeps it", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a");
  await page.getByRole("radio", { name: "Dark" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
});

for (const scheme of ["light", "dark"] as const) {
  test(`renders the runs board in ${scheme} mode`, async ({ page }, info) => {
    await page.emulateMedia({ colorScheme: scheme });
    await mockAPI(page);
    await page.goto("/ns/team-a");
    await expect(page.getByRole("link", { name: "backfill-2026-10" })).toBeVisible();
    await page.screenshot({ path: `test-results/screens/runs-${info.project.name}-${scheme}.png`, fullPage: true });
  });
}
