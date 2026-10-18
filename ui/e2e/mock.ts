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

// run is a DAG run: fetch and validate ran, train runs, report waits.
export function run(phase: string) {
  const done = phase === "Succeeded";
  const node = (id: string, name: string, extra: object) => ({ id, name: `etl-nightly-8k2pq${name}`, ...extra });
  return {
    summary: { namespace: "team-a", name: "etl-nightly-8k2pq", uid: "u1", phase, startedAt: ts(-290), template: "etl-nightly", nodesDone: done ? 4 : 2, nodesTotal: 4 },
    workflow: {
      metadata: { name: "etl-nightly-8k2pq", namespace: "team-a" },
      spec: {},
      status: {
        phase,
        storedSpec: {
          templates: [
            { name: "main", dag: { tasks: [{ name: "fetch" }, { name: "validate", depends: "fetch" }, { name: "train", dependencies: ["validate"] }, { name: "report", depends: "train.Succeeded" }] } },
          ],
        },
        nodes: {
          "etl-nightly-8k2pq": { id: "etl-nightly-8k2pq", name: "etl-nightly-8k2pq", type: "DAG", templateName: "main", phase, startedAt: ts(-290), children: ["n1", "n2", "n3", "n4"] },
          n1: node("n1", ".fetch", { displayName: "fetch", type: "Pod", phase: "Succeeded", startedAt: ts(-290), finishedAt: ts(-230), podName: "n1", outputs: { result: "1204 rows" } }),
          n2: node("n2", ".validate", { displayName: "validate", type: "Pod", phase: "Succeeded", startedAt: ts(-228), finishedAt: ts(-200), podName: "n2", inputs: { parameters: [{ name: "rows", value: "1204 rows" }] } }),
          n3: node("n3", ".train", { displayName: "train", type: "Pod", phase: done ? "Succeeded" : "Running", startedAt: ts(-198), finishedAt: done ? ts(-20) : undefined, podName: "n3" }),
          ...(done ? { n4: node("n4", ".report", { displayName: "report", type: "Pod", phase: "Succeeded", startedAt: ts(-18), finishedAt: ts(-5), podName: "n4" }) } : {}),
        },
      },
    },
  };
}

export async function mockAPI(page: Page, opts: { loggedIn?: boolean; namespaces?: string[]; runPhase?: string } = {}) {
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
  await page.route("**/chronicle.v1.WorkflowService/ListWorkflows", (r) => {
    const phases: string[] = r.request().postDataJSON()?.phases ?? [];
    r.fulfill({ json: { workflows: runs.filter((w) => phases.length === 0 || phases.includes(w.phase)) } });
  });
  await page.route("**/chronicle.v1.WorkflowService/GetWorkflow", (r) => r.fulfill({ json: run(opts.runPhase ?? "Running") }));
  await page.clock.setFixedTime(now);
}
