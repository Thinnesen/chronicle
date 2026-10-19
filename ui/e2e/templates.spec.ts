import { expect, test } from "@playwright/test";

import { mockAPI, openMenu } from "./mock";

test("starts a run from a template with parameters", async ({ page }) => {
  const calls = await mockAPI(page);
  await page.goto("/ns/team-a");
  await openMenu(page);
  await page.getByRole("navigation", { name: "Sections" }).getByRole("link", { name: "Templates" }).click();
  await expect(page.getByText("Managed by Argo CD")).toBeVisible();
  await page.getByRole("link", { name: "deploy-api" }).click();
  await expect(page.getByLabel("The YAML of deploy-api")).toContainText("entrypoint: main");

  await page.getByRole("combobox", { name: "env" }).selectOption("prod");
  await page.getByRole("button", { name: "Start the run" }).click();
  expect(calls.SubmitWorkflow ?? []).toHaveLength(0); // the required version is missing
  await page.getByRole("textbox", { name: /version/ }).fill("1.4.2");
  await page.getByRole("button", { name: "Start the run" }).click();
  await expect(page).toHaveURL(/\/runs\/deploy-api-new01$/);
  expect(calls.SubmitWorkflow).toEqual([
    { namespace: "team-a", template: { name: "deploy-api" }, parameters: { env: "prod", version: "1.4.2" } },
  ]);
});

test("a viewer cannot start a run", async ({ page }) => {
  await mockAPI(page, { canSubmit: false });
  await page.goto("/ns/team-a/templates/deploy-api");
  await expect(page.getByText("Only editors of team-a can start runs.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Start the run" })).toHaveCount(0);
});

test("shows cron schedules and suspends one", async ({ page }) => {
  const calls = await mockAPI(page);
  await page.goto("/ns/team-a/cron");
  await expect(page.getByText("At 03:00 (Europe/Berlin)")).toBeVisible();
  await expect(page.getByText("Next run in 3h 4m")).toBeVisible();
  await page.getByRole("link", { name: "etl-nightly" }).click();
  await page.getByRole("button", { name: "Suspend" }).click();
  await expect(page.getByRole("status")).toContainText("Suspended etl-nightly");
  expect(calls.SuspendCronWorkflow).toEqual([{ namespace: "team-a", name: "etl-nightly" }]);

  await page.goto("/ns/team-a/cron/cleanup");
  await expect(page.getByRole("button", { name: "Resume" })).toBeDisabled();
  await expect(page.getByText("Managed by Flux")).toBeVisible();
});

for (const scheme of ["light", "dark"] as const) {
  test(`renders a template and a cron workflow in ${scheme} mode`, async ({ page }, info) => {
    await page.emulateMedia({ colorScheme: scheme });
    await mockAPI(page);
    await page.goto("/ns/team-a/templates/deploy-api");
    await expect(page.getByRole("button", { name: "Start the run" })).toBeVisible();
    await page.screenshot({ path: `test-results/screens/template-${info.project.name}-${scheme}.png`, fullPage: true });
    await page.goto("/ns/team-a/cron/etl-nightly");
    await expect(page.getByText("Recent runs")).toBeVisible();
    await page.screenshot({ path: `test-results/screens/cron-${info.project.name}-${scheme}.png`, fullPage: true });
  });
}
