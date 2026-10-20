import { expect, test } from "./fixtures";

import { mockAPI, openMenu } from "./mock";

test("sends a visitor without a session to the login page", async ({ page }) => {
  await mockAPI(page, { loggedIn: false });
  await page.goto("/ns/team-a");
  await expect(page.getByRole("heading", { name: "chronicle" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Log in" })).toHaveAttribute("href", "/auth/login?redirect=%2Fns%2Fteam-a");
});

test("names Microsoft on the login button for Entra ID", async ({ page }) => {
  await mockAPI(page, { loggedIn: false, provider: "microsoft" });
  await page.goto("/login");
  await expect(page.getByRole("link", { name: "Log in with Microsoft" })).toBeVisible();
});

test("lets the user choose a namespace and shows its runs", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/");
  await page.getByRole("link", { name: "team-a" }).click();
  await expect(page.getByRole("heading", { name: "Runs", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "etl-nightly-8k2pq" })).toBeVisible();
  await expect(page.getByRole("img", { name: "3 of 7 nodes done" })).toBeVisible();
  await expect(page).toHaveTitle("Runs in team-a - chronicle");
});

test("opens the only namespace at once", async ({ page }) => {
  await mockAPI(page, { namespaces: ["team-a"] });
  await page.goto("/");
  await expect(page).toHaveURL(/\/ns\/team-a$/);
});

test("switches the theme and keeps it", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a");
  await openMenu(page);
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

test("finds a run with the command palette", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a");
  await expect(page.getByRole("link", { name: "backfill-2026-10" })).toBeVisible();
  await page.keyboard.press("Control+k");
  await page.getByRole("combobox").fill("train");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/runs\/train-model-4rq9z$/);
});

test("goes to the login page when the session ends on an open page", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a");
  await expect(page.getByRole("link", { name: "etl-nightly-8k2pq" })).toBeVisible();
  // The session ends, for example after the idle timeout: the server answers each call with unauthenticated.
  await page.route("**/auth/me", (r) => r.fulfill({ status: 401, body: "log in first" }));
  await page.route("**/chronicle.v1.**", (r) => r.fulfill({ status: 401, json: { code: "unauthenticated", message: "the session ended" } }));
  await expect(page).toHaveURL(/\/login\?back=%2Fns%2Fteam-a/, { timeout: 15_000 });
});
