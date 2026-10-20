import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "./fixtures";

import { mockAPI } from "./mock";

// The pages must pass the WCAG 2 A and AA rules of axe in both themes (docs/DESIGN.md section 8.1).
const pages = [
  "/ns/team-a",
  "/ns/team-a/runs/etl-nightly-8k2pq?node=n1",
  "/ns/team-a/runs/etl-nightly-8k2pq?tab=timeline",
  "/ns/team-a/runs/etl-nightly-8k2pq?node=n5",
  "/ns/team-a/runs/etl-nightly-8k2pq?node=n6",
  "/ns/team-a/templates",
  "/ns/team-a/templates/deploy-api",
  "/ns/team-a/cron",
  "/ns/team-a/cron/etl-nightly",
  "/ns/team-a/tasks",
];

for (const scheme of ["light", "dark"] as const) {
  test(`has no accessibility violations in ${scheme} mode`, async ({ page }) => {
    // The test checks many pages, which takes longer than one page test, most of all in Firefox under load.
    test.setTimeout(90_000);
    await page.emulateMedia({ colorScheme: scheme });
    // A waiting approval and a step that cannot start add the warning colors to the pages.
    await mockAPI(page, { gate: true, stuck: true });
    for (const path of pages) {
      await page.goto(path);
      await page.getByRole("banner").waitFor();
      await page.waitForLoadState("networkidle");
      const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
      const found = result.violations.map((v) => `${path}: ${v.id} (${v.nodes.length}) ${v.nodes[0]?.target.join(" ")} ${v.nodes[0]?.failureSummary ?? ""}`);
      expect(found).toEqual([]);
    }
  });
}
