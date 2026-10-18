import { expect, test } from "@playwright/test";

import { mockAPI } from "./mock";

const runURL = "/ns/team-a/runs/etl-nightly-8k2pq";

test("an editor suspends and stops a run", async ({ page }) => {
  const calls = await mockAPI(page);
  await page.goto(runURL);
  await page.getByRole("button", { name: "Suspend" }).click();
  await expect(page.getByRole("status")).toContainText("Suspended etl-nightly-8k2pq");
  expect(calls.SuspendWorkflow).toEqual([{ namespace: "team-a", name: "etl-nightly-8k2pq" }]);

  await page.getByRole("button", { name: "Stop", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Stop etl-nightly-8k2pq?" });
  await expect(dialog).toContainText("The exit handler still runs.");
  await dialog.getByRole("button", { name: "Stop the run" }).click();
  await expect(page.getByRole("status")).toContainText("Stopping etl-nightly-8k2pq");
  expect(calls.StopWorkflow).toHaveLength(1);
});

test("a refused action tells the user what to do", async ({ page }) => {
  await mockAPI(page, { deny: ["TerminateWorkflow"] });
  await page.goto(runURL);
  await page.getByRole("button", { name: "Terminate" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Terminate the run" }).click();
  await expect(page.getByRole("status")).toContainText("You cannot terminate runs in team-a. Ask your platform team for the editor role.");
});

test("a viewer sees no controls", async ({ page }) => {
  await mockAPI(page, { canSubmit: false, gate: true });
  await page.goto(`${runURL}?node=n5`);
  await expect(page.getByRole("heading", { name: "etl-nightly-8k2pq" })).toBeVisible();
  await expect(page.getByRole("group", { name: "Run actions" })).toHaveCount(0);
  await expect(page.getByText("This node waits for a decision by an editor of team-a.")).toBeVisible();
});

test("an editor approves a node with a parameter", async ({ page }) => {
  const calls = await mockAPI(page, { gate: true });
  await page.goto(`${runURL}?node=n5`);
  const form = page.getByRole("complementary", { name: "Node gate" });
  await expect(form.getByRole("textbox", { name: "env" })).toHaveValue("staging");
  await form.getByRole("textbox", { name: "env" }).fill("prod");
  await form.getByRole("textbox", { name: "Comment for the run history" }).fill("checked");
  await form.getByRole("button", { name: "Approve" }).click();
  await expect(page.getByRole("status")).toContainText("Approved gate");
  expect(calls.ApproveNode).toEqual([{ namespace: "team-a", name: "etl-nightly-8k2pq", node: "n5", approve: true, parameters: { env: "prod" }, message: "checked" }]);
});

test("an editor deletes a finished run", async ({ page }) => {
  const calls = await mockAPI(page, { runPhase: "Succeeded" });
  await page.goto(runURL);
  await expect(page.getByRole("button", { name: "Suspend" })).toHaveCount(0);
  await page.getByRole("button", { name: "Delete" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Delete the run" }).click();
  await expect(page).toHaveURL(/\/ns\/team-a$/);
  expect(calls.DeleteWorkflow).toHaveLength(1);
});
