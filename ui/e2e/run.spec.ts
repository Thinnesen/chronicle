import { expect, test } from "./fixtures";

import { mockAPI } from "./mock";

test("shows the run as a graph and a timeline, and selects a node in both", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq");
  await expect(page.getByRole("heading", { name: "etl-nightly-8k2pq" })).toBeVisible();
  await expect(page.getByRole("list", { name: "Parameters" })).toContainText("day2026-10-17");
  const graph = page.locator(".run-graph");
  await expect(graph.getByText("fetch", { exact: true })).toBeVisible();
  await expect(graph.locator(".track.is-lit")).toHaveCount(2);

  await graph.getByText("validate", { exact: true }).click();
  await expect(page).toHaveURL(/node=n2/);
  const panel = page.getByRole("complementary", { name: "Step validate" });
  await expect(panel.getByText("1204 rows")).toBeVisible();

  await page.keyboard.press("Escape");
  await expect(page).not.toHaveURL(/node=/);
  await graph.locator(".react-flow__node").filter({ hasText: "validate" }).focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/node=n2/);

  await page.getByRole("tab", { name: "Timeline" }).click();
  await expect(page).toHaveURL(/tab=timeline/);
  await expect(page.getByRole("listitem").filter({ has: page.getByRole("button", { name: /validate, Succeeded/ }) })).toHaveClass(/is-selected/);
  await page.getByRole("button", { name: /fetch, Succeeded/ }).click();
  await expect(page.getByRole("complementary", { name: "Step fetch" })).toBeVisible();
});

test("finds runs by name and by phase", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a");
  await expect(page.getByRole("row", { name: /deploy-api-x7c1m/ }).getByText("Needs approval")).toBeVisible();
  await page.getByRole("searchbox", { name: "Search runs" }).fill("model");
  await expect(page).toHaveURL(/q=model/);
  await expect(page.getByRole("link", { name: "train-model-4rq9z" })).toBeVisible();
  await expect(page.getByRole("link", { name: "etl-nightly-8k2pq" })).toBeHidden();

  await page.getByRole("searchbox", { name: "Search runs" }).fill("");
  await page.getByRole("radio", { name: "Succeeded" }).click();
  await expect(page).toHaveURL(/phase=Succeeded/);
  await expect(page.getByRole("link", { name: "backfill-2026-10" })).toBeVisible();
  await expect(page.getByRole("link", { name: "deploy-api-x7c1m" })).toBeHidden();

  await page.getByRole("searchbox", { name: "Search runs" }).fill("nothing-like-this");
  await expect(page.getByText("No run matches the filter.")).toBeVisible();
});

test("keeps the run on the page when a refresh fails", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq");
  await expect(page.getByRole("heading", { name: "etl-nightly-8k2pq" })).toBeVisible();
  await page.route("**/chronicle.v1.WorkflowService/GetWorkflow", (r) => r.fulfill({ status: 503, json: { code: "unavailable", message: "the server restarts" } }));
  await expect(page.getByText("Could not refresh: ")).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole("heading", { name: "etl-nightly-8k2pq" })).toBeVisible();
});

test("starts a run from YAML", async ({ page }) => {
  const calls = await mockAPI(page);
  await page.goto("/ns/team-a");
  await page.getByRole("button", { name: "New run" }).click();
  const dialog = page.getByRole("dialog", { name: "New run in team-a" });
  await dialog.getByRole("textbox", { name: "Workflow" }).fill("kind: [broken");
  await dialog.getByRole("button", { name: "Start the run" }).click();
  await expect(dialog.getByRole("alert")).toContainText("The YAML is not valid");
  // YAML 1.2: "yes" and "n" stay strings.
  await dialog.getByRole("textbox", { name: "Workflow" }).fill("kind: Workflow\nspec:\n  arguments:\n    parameters: [{name: n, value: yes}]\n");
  await dialog.getByRole("button", { name: "Start the run" }).click();
  await expect(page).toHaveURL(/\/runs\/deploy-api-new01$/);
  expect(calls.SubmitWorkflow).toEqual([{ namespace: "team-a", workflow: { kind: "Workflow", spec: { arguments: { parameters: [{ name: "n", value: "yes" }] } } } }]);
});

test("loads older runs on request", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a");
  await expect(page.getByRole("link", { name: "backfill-2026-10" })).toBeVisible();
  await page.getByRole("button", { name: "Show more runs" }).click();
  await expect(page.getByRole("link", { name: "etl-nightly-old01" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Show more runs" })).toBeHidden();
});

for (const scheme of ["light", "dark"] as const) {
  test(`renders a finished run in ${scheme} mode`, async ({ page }, info) => {
    await page.emulateMedia({ colorScheme: scheme });
    await mockAPI(page, { runPhase: "Succeeded" });
    await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n3");
    await expect(page.locator(".run-graph").getByText("report", { exact: true })).toBeVisible();
    await page.screenshot({ path: `test-results/screens/run-${info.project.name}-${scheme}.png`, fullPage: true });
  });
}
