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

test("finds a template and a schedule with the command palette", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a");
  await page.keyboard.press("Control+k");
  await page.getByRole("combobox").fill("deploy-api");
  await page.getByRole("option", { name: /deploy-api/ }).filter({ hasText: "Template" }).click();
  await expect(page).toHaveURL(/\/templates\/deploy-api$/);
  await page.keyboard.press("Control+k");
  await page.getByRole("combobox").fill("cleanup");
  await page.getByRole("option", { name: /cleanup/ }).click();
  await expect(page).toHaveURL(/\/cron\/cleanup$/);
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

test("explains a page or a run that does not exist", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/no/such/page");
  await expect(page.getByRole("heading", { name: "This page does not exist" })).toBeVisible();
  await page.getByRole("link", { name: "Go to the start page" }).click();
  await expect(page).toHaveURL(/\/ns\/team-a|\/$/);

  await page.route("**/chronicle.v1.WorkflowService/GetWorkflow", (r) =>
    r.fulfill({ status: 404, json: { code: "not_found", message: "the run does not exist, not even in the archive" } }),
  );
  await page.goto("/ns/team-a/runs/gone-run");
  await expect(page.getByText("the run does not exist, not even in the archive")).toBeVisible();
  await expect(page.getByText("[not_found]")).toHaveCount(0);
  await page.getByRole("link", { name: "Show the runs of team-a" }).click();
  await expect(page).toHaveURL(/\/ns\/team-a$/);
});
