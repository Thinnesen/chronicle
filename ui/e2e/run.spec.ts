import { expect, test } from "@playwright/test";

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

  await page.getByRole("tab", { name: "Timeline" }).click();
  await expect(page).toHaveURL(/tab=timeline/);
  await expect(page.getByRole("listitem").filter({ has: page.getByRole("button", { name: /validate, Succeeded/ }) })).toHaveClass(/is-selected/);
  await page.getByRole("button", { name: /fetch, Succeeded/ }).click();
  await expect(page.getByRole("complementary", { name: "Step fetch" })).toBeVisible();
});

test("finds runs by name and by phase", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a");
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
