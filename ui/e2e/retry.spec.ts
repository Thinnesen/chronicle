import { expect, test } from "@playwright/test";

import { mockAPI } from "./mock";

const runURL = "/ns/team-a/runs/etl-nightly-8k2pq";

test("retries a failed run", async ({ page }) => {
  const calls = await mockAPI(page, { runPhase: "Failed" });
  await page.goto(runURL);
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Retrying etl-nightly-8k2pq");
  expect(calls.RetryWorkflow).toEqual([{ namespace: "team-a", name: "etl-nightly-8k2pq" }]);
});

test("retries a finished run from a node", async ({ page }) => {
  const calls = await mockAPI(page, { runPhase: "Succeeded" });
  await page.goto(`${runURL}?node=n2`);
  const panel = page.getByRole("complementary", { name: "Step validate" });
  await panel.getByRole("tab", { name: "Summary" }).click();
  await panel.getByRole("button", { name: "Retry from this node" }).click();
  await expect(page.getByRole("dialog")).toContainText("The nodes before it keep their results.");
  await page.getByRole("dialog").getByRole("button", { name: "Retry" }).click();
  await expect(page.getByRole("status")).toContainText("Retrying etl-nightly-8k2pq from validate");
  expect(calls.RetryWorkflow).toEqual([{ namespace: "team-a", name: "etl-nightly-8k2pq", node: "n2" }]);
});

test("re-runs a run with a changed parameter", async ({ page }) => {
  const calls = await mockAPI(page, { runPhase: "Succeeded" });
  await page.goto(runURL);
  await page.getByRole("button", { name: "Re-run" }).click();
  const dialog = page.getByRole("dialog", { name: "Re-run etl-nightly-8k2pq" });
  await expect(dialog.getByRole("textbox", { name: "day" })).toHaveValue("2026-10-17");
  await dialog.getByRole("textbox", { name: "rows" }).fill("1000");
  await dialog.getByRole("button", { name: "Start the new run" }).click();
  await expect(page).toHaveURL(/\/runs\/etl-nightly-rr001$/);
  expect(calls.RerunWorkflow).toEqual([{ namespace: "team-a", name: "etl-nightly-8k2pq", parameters: { rows: "1000" } }]);
});

test("a refused re-run tells the user what to do", async ({ page }) => {
  await mockAPI(page, { runPhase: "Failed", deny: ["RerunWorkflow"] });
  await page.goto(runURL);
  await page.getByRole("button", { name: "Re-run" }).click();
  await page.getByRole("button", { name: "Start the new run" }).click();
  await expect(page.getByRole("status")).toContainText("You cannot re-run runs in team-a.");
});

test("a viewer cannot retry", async ({ page }) => {
  await mockAPI(page, { runPhase: "Failed", canSubmit: false });
  await page.goto(`${runURL}?node=n2`);
  await expect(page.getByRole("heading", { name: "etl-nightly-8k2pq" })).toBeVisible();
  await expect(page.getByRole("button", { name: /Retry|Re-run/ })).toHaveCount(0);
});

test("an archived run offers only a re-run", async ({ page }) => {
  await mockAPI(page, { runPhase: "Failed", archived: true });
  await page.goto(runURL);
  await expect(page.getByText("This run is in the archive.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Re-run" })).toBeVisible();
  await expect(page.getByRole("button", { name: /^(Retry|Delete)$/ })).toHaveCount(0);
  await page.goto("/ns/team-a");
  await expect(page.getByRole("row", { name: /backfill-2026-10/ }).getByText("Archived")).toBeVisible();
});
