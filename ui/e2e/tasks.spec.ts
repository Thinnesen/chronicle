import { expect, test } from "./fixtures";

import { mockAPI, openMenu } from "./mock";

test("lists the tasks of the queue and filters them by state", async ({ page }, info) => {
  await mockAPI(page);
  await page.goto("/ns/team-a");
  await openMenu(page);
  await page.getByRole("navigation", { name: "Sections" }).getByRole("link", { name: "Tasks" }).click();
  await expect(page.getByRole("heading", { name: "Tasks", exact: true })).toBeVisible();
  await expect(page.getByText("score-model")).toBeVisible();
  await expect(page.getByRole("link", { name: "etl-nightly-8k2pq.notify" })).toHaveAttribute("href", "/ns/team-a/runs/etl-nightly-8k2pq");
  await page.screenshot({ path: `test-results/screens/tasks-${info.project.name}.png`, fullPage: true });

  await page.getByRole("radio", { name: "Failed" }).click();
  await expect(page).toHaveURL(/state=failed/);
  await expect(page.getByText("the worker sent no heartbeat for 30s")).toBeVisible();
  await expect(page.getByText("score-model")).toBeHidden();
  await expect(page.getByText("the workflow was stopped")).toBeHidden();

  await page.getByRole("radio", { name: "Cancelled" }).click();
  await expect(page).toHaveURL(/state=cancelled/);
  await expect(page.getByText("the workflow was stopped")).toBeVisible();
  await expect(page.getByText("the worker sent no heartbeat for 30s")).toBeHidden();
});
