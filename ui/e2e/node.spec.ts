import { expect, test } from "@playwright/test";

import { mockAPI } from "./mock";

test("shows the live log of a node and filters it", async ({ page }, info) => {
  await mockAPI(page);
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n1");
  const panel = page.getByRole("complementary", { name: "Node fetch" });
  await expect(panel.getByRole("tab", { name: "Logs" })).toHaveAttribute("aria-selected", "true");
  const log = panel.getByLabel("Log", { exact: true });
  await expect(log.getByText("starting n1")).toBeVisible();
  await expect(log.getByText("\u001b")).toHaveCount(0);
  await expect(page.locator(".run-graph").getByText("validate", { exact: true })).toBeVisible();
  await page.screenshot({ path: `test-results/screens/log-${info.project.name}.png`, fullPage: true });
  await panel.getByRole("searchbox", { name: "Filter the log" }).fill("epoch");
  await expect(log.getByText("epoch 1/3 loss=0.41")).toBeVisible();
  await expect(log.getByText("loaded 1204 rows")).toBeHidden();

  await panel.getByRole("tab", { name: "Details" }).click();
  await expect(panel.getByText("1204 rows")).toBeVisible();
});

test("lists the attempts of a retried node", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n3");
  const panel = page.getByRole("complementary", { name: "Node train" });
  await panel.getByRole("tab", { name: "Attempts (2)" }).click();
  await expect(panel.getByText("OOMKilled (exit code 137)")).toBeVisible();
  await panel.getByRole("button", { name: /Attempt 1/ }).click();
  await expect(page).toHaveURL(/node=n3a/);
  await expect(page.getByRole("complementary", { name: "Node train(0)" })).toBeVisible();
});

test("shows the archived log and the artifacts of a node", async ({ page }, info) => {
  await mockAPI(page, { archived: true, runPhase: "Succeeded" });
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n1");
  const panel = page.getByRole("complementary", { name: "Node fetch" });
  await expect(panel.getByText("This log comes from the log archive.")).toBeVisible();
  await expect(panel.getByLabel("Log", { exact: true }).getByText("loaded 1204 rows")).toBeVisible();

  await panel.getByRole("tab", { name: "Details" }).click();
  await page.screenshot({ path: `test-results/screens/artifacts-${info.project.name}.png`, fullPage: true });
  await expect(panel.getByRole("link", { name: "rows.tgz" })).toHaveAttribute("href", "/artifacts/team-a/etl-nightly-8k2pq/n1/rows");
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n2");
  await page.getByRole("complementary", { name: "Node validate" }).getByRole("tab", { name: "Details" }).click();
  await expect(page.getByText("/tmp/rows")).toBeVisible();
});
