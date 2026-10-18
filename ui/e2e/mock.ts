import type { Page } from "@playwright/test";

// The fake API. Connect uses JSON over POST, so each RPC is one route.
export const now = Date.parse("2026-10-18T14:00:00Z");

const ts = (offsetSeconds: number) => new Date(now + offsetSeconds * 1000).toISOString();

export const runs = [
  { namespace: "team-a", name: "etl-nightly-8k2pq", uid: "u1", phase: "Running", createdAt: ts(-300), startedAt: ts(-290), template: "etl-nightly", nodesDone: 3, nodesTotal: 7 },
  { namespace: "team-a", name: "deploy-api-x7c1m", uid: "u2", phase: "Suspended", message: "Waiting for an approval", createdAt: ts(-900), startedAt: ts(-890), nodesDone: 2, nodesTotal: 4 },
  { namespace: "team-a", name: "train-model-4rq9z", uid: "u3", phase: "Failed", message: 'task "evaluate" failed', createdAt: ts(-4000), startedAt: ts(-3990), finishedAt: ts(-1200), nodesDone: 5, nodesTotal: 6 },
  { namespace: "team-a", name: "backfill-2026-10", uid: "u4", phase: "Succeeded", createdAt: ts(-86400), startedAt: ts(-86390), finishedAt: ts(-80000), template: "backfill", nodesDone: 40, nodesTotal: 40 },
];

export async function mockAPI(page: Page, opts: { loggedIn?: boolean; namespaces?: string[] } = {}) {
  const loggedIn = opts.loggedIn ?? true;
  const names = opts.namespaces ?? ["team-a", "team-b"];
  await page.route("**/auth/me", (r) =>
    loggedIn
      ? r.fulfill({ json: { user: "oidc:alice@example.com", groups: ["oidc:data"], csrfToken: "csrf", expiresAt: ts(3600) } })
      : r.fulfill({ status: 401, body: "log in first" }),
  );
  await page.route("**/chronicle.v1.NamespaceService/ListNamespaces", (r) =>
    r.fulfill({ json: { namespaces: names.map((name, i) => ({ name, canSubmit: i === 0 })) } }),
  );
  await page.route("**/chronicle.v1.WorkflowService/ListWorkflows", (r) => r.fulfill({ json: { workflows: runs } }));
  await page.clock.setFixedTime(now);
}
