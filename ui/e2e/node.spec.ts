import { expect, test } from "./fixtures";

import { mockAPI } from "./mock";

test("shows the live log of a node and filters it", async ({ page }, info) => {
  await mockAPI(page);
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n1");
  const panel = page.getByRole("complementary", { name: "Step fetch" });
  await expect(panel.getByRole("tab", { name: "Logs" })).toHaveAttribute("aria-selected", "true");
  const log = panel.getByLabel("Log", { exact: true });
  await expect(log.getByText("starting n1")).toBeVisible();
  await expect(log.getByText("\u001b")).toHaveCount(0);
  await expect(page.locator(".run-graph").getByText("validate", { exact: true })).toBeVisible();
  await page.screenshot({ path: `test-results/screens/log-${info.project.name}.png`, fullPage: true });
  await panel.getByRole("searchbox", { name: "Filter the log" }).fill("epoch");
  await expect(log.getByText("epoch 1/3 loss=0.41")).toBeVisible();
  await expect(log.getByText("loaded 1204 rows")).toBeHidden();

  const saved = page.waitForEvent("download");
  await panel.getByRole("button", { name: "Download" }).click();
  const file = await saved;
  expect(file.suggestedFilename()).toBe("etl-nightly-8k2pq-n1.log");

  await panel.getByRole("tab", { name: "Summary" }).click();
  await expect(panel.getByText("1204 rows")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(panel).toBeHidden();
  await expect(page).not.toHaveURL(/node=/);
});

test("lists the attempts of a retried node", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n3");
  const panel = page.getByRole("complementary", { name: "Step train" });
  await panel.getByRole("tab", { name: "Attempts (2)" }).click();
  await expect(panel.getByText("OOMKilled (exit code 137)")).toBeVisible();
  await panel.getByRole("button", { name: /Attempt 1/ }).click();
  await expect(page).toHaveURL(/node=n3a/);
  await expect(page.getByRole("complementary", { name: "Step train(0)" })).toBeVisible();
});

test("shows the archived log and the artifacts of a node", async ({ page }, info) => {
  await mockAPI(page, { archived: true, runPhase: "Succeeded" });
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n1");
  const panel = page.getByRole("complementary", { name: "Step fetch" });
  await expect(panel.getByText("This log comes from the log archive.")).toBeVisible();
  await expect(panel.getByLabel("Log", { exact: true }).getByText("loaded 1204 rows")).toBeVisible();

  await panel.getByRole("tab", { name: "Summary" }).click();
  await page.screenshot({ path: `test-results/screens/artifacts-${info.project.name}.png`, fullPage: true });
  await expect(panel.getByRole("link", { name: "rows.tgz" })).toHaveAttribute("href", "/artifacts/team-a/etl-nightly-8k2pq/n1/rows");
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n2");
  await page.getByRole("complementary", { name: "Step validate" }).getByRole("tab", { name: "Summary" }).click();
  await expect(page.getByText("/tmp/rows")).toBeVisible();
});

test("connects the log of a running node again after a broken stream", async ({ page }) => {
  await mockAPI(page);
  // The first stream breaks, as during a server rollout. Routes added later win.
  let calls = 0;
  await page.route("**/chronicle.v1.LogService/StreamLogs", (r) => (calls++ === 0 ? r.fulfill({ status: 503, body: "" }) : r.fallback()));
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n3b");
  const panel = page.getByRole("complementary", { name: "Step train(1)" });
  await expect(panel.getByLabel("Log", { exact: true }).getByText("starting n3b")).toBeVisible();
  expect(calls).toBeGreaterThan(1);
});

test("shows the Kubernetes events of a node", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n3b");
  const panel = page.getByRole("complementary", { name: "Step train(1)" });
  await panel.getByRole("tab", { name: "Events" }).click();
  await expect(panel.getByText("BackOff")).toBeVisible();
  await expect(panel.getByText("4 times")).toBeVisible();
  await expect(panel.getByLabel("Warning")).toBeVisible();
});

test("a step that cannot start opens on its events and explains the empty log", async ({ page }) => {
  await mockAPI(page, { stuck: true });
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq?node=n6");
  const panel = page.getByRole("complementary", { name: "Step publish" });
  await expect(panel.getByRole("tab", { name: "Events" })).toHaveAttribute("aria-selected", "true");
  await panel.getByRole("tab", { name: "Logs" }).click();
  await expect(panel).toContainText("The step has not started yet.");
  await expect(panel.getByRole("alert")).toHaveCount(0);
});
