import { expect, test } from "./fixtures";

import { mockAPI, now } from "./mock";

test("shows the run as a graph and a timeline, and selects a node in both", async ({ page }) => {
  await mockAPI(page);
  await page.goto("/ns/team-a/runs/etl-nightly-8k2pq");
  await expect(page).toHaveTitle("etl-nightly-8k2pq (Running) - chronicle");
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

test("finds runs by name and by phase", async ({ page }, info) => {
  await mockAPI(page);
  await page.goto("/ns/team-a");
  await expect(page.getByRole("row", { name: /deploy-api-x7c1m/ }).getByText("Needs approval")).toBeVisible();
  await expect(page.getByRole("row", { name: /etl-nightly-8k2pq/ }).getByText("ImagePullBackOff")).toBeVisible();
  const notice = page.getByRole("status", { name: "Runs that wait for an approval" });
  await expect(notice).toContainText("deploy-api-x7c1m waits for an approval.");
  await page.getByRole("searchbox", { name: "Search runs" }).fill("model");
  await expect(page).toHaveURL(/q=model/);
  await expect(page.getByRole("link", { name: "train-model-4rq9z" })).toBeVisible();
  await expect(page.getByRole("link", { name: "etl-nightly-8k2pq" })).toBeHidden();

  await page.getByRole("searchbox", { name: "Search runs" }).fill("");
  await page.getByRole("radio", { name: "Succeeded" }).click();
  await expect(page).toHaveURL(/phase=Succeeded/);
  await expect(page.getByRole("link", { name: "backfill-2026-10" })).toBeVisible();
  await expect(page.getByRole("table").getByRole("link", { name: "deploy-api-x7c1m" })).toBeHidden();

  // A phone has no template column.
  if (info.project.name === "desktop") {
    await page.getByRole("radio", { name: "All" }).click();
    await page.getByRole("button", { name: "Show the runs of deploy-api" }).click();
    await expect(page).toHaveURL(/q=deploy-api/);
    await expect(page.getByRole("table").getByRole("link", { name: "train-model-4rq9z" })).toBeHidden();
  }

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

test("shows the live runs when the archive does not answer", async ({ page }) => {
  await mockAPI(page);
  await page.route("**/chronicle.v1.WorkflowService/ListWorkflows", (r) =>
    r.fulfill({ json: { workflows: [{ namespace: "team-a", name: "live-run", uid: "u7", phase: "Running" }], archiveUnavailable: true } }),
  );
  await page.goto("/ns/team-a");
  await expect(page.getByRole("link", { name: "live-run" })).toBeVisible();
  await expect(page.getByText("The archive did not answer. The list shows only the runs in the cluster.")).toBeVisible();
});

test("says that a new run has not started yet", async ({ page }) => {
  await mockAPI(page);
  await page.route("**/chronicle.v1.WorkflowService/GetWorkflow", (r) =>
    r.fulfill({
      json: {
        summary: { namespace: "team-a", name: "fresh-run", uid: "u8" },
        workflow: { metadata: { name: "fresh-run", namespace: "team-a" }, spec: { workflowTemplateRef: { name: "etl-nightly" } } },
      },
    }),
  );
  await page.goto("/ns/team-a/runs/fresh-run");
  await expect(page.getByText("The run has not started yet. The controller starts it in a moment.")).toBeVisible();
  await expect(page.getByText("Select a step to see its details and logs.")).toHaveCount(0);
});

test("says when a scheduled run starts", async ({ page }) => {
  await mockAPI(page);
  const at = new Date(now + 2 * 3600_000 + 60_000).toISOString();
  await page.route("**/chronicle.v1.WorkflowService/GetWorkflow", (r) =>
    r.fulfill({
      json: {
        summary: { namespace: "team-a", name: "later-run", uid: "u8", phase: "Pending", message: `the run starts at ${at}` },
        workflow: { metadata: { name: "later-run", namespace: "team-a" }, spec: { startAt: at }, status: { phase: "Pending" } },
      },
    }),
  );
  await page.goto("/ns/team-a/runs/later-run");
  await expect(page.getByText(/^The run starts at .+, in 2h 1m\.$/)).toBeVisible();
  await expect(page.getByRole("term").filter({ hasText: "Starts" })).toBeVisible();
  await expect(page.getByText(`the run starts at ${at}`)).toHaveCount(0);
});

test("shows the start time of a scheduled run in the list", async ({ page }) => {
  await mockAPI(page);
  const at = new Date(now + 2 * 3600_000).toISOString();
  await page.route("**/chronicle.v1.WorkflowService/ListWorkflows", (r) =>
    r.fulfill({
      json: { workflows: [{ namespace: "team-a", name: "later-run", uid: "u8", phase: "Pending", message: `the run starts at ${at}`, createdAt: new Date(now).toISOString(), startAt: at }] },
    }),
  );
  await page.goto("/ns/team-a");
  const row = page.getByRole("row").filter({ hasText: "later-run" });
  await expect(row.getByText(/^Starts /)).toBeVisible();
  await expect(row.getByText(`the run starts at ${at}`)).toHaveCount(0);
  // The run has not started, so the Started cell is empty.
  await expect(row.locator(".board-time")).toHaveText("–");
});

test("names the parent of a child run in the list", async ({ page }) => {
  await mockAPI(page);
  await page.route("**/chronicle.v1.WorkflowService/ListWorkflows", (r) =>
    r.fulfill({
      json: { workflows: [{ namespace: "team-a", name: "release-q4-0a1b2c3d", uid: "u8", phase: "Running", createdAt: new Date(now).toISOString(), parent: "release-q4" }] },
    }),
  );
  await page.goto("/ns/team-a");
  const row = page.getByRole("row").filter({ hasText: "release-q4-0a1b2c3d" });
  await expect(row.getByRole("link", { name: "release-q4", exact: true })).toHaveAttribute("href", "/ns/team-a/runs/release-q4");
});
