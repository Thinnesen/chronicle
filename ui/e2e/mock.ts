import type { Page } from "@playwright/test";

// The fake API. Connect uses JSON over POST, so each RPC is one route.
export const now = Date.parse("2026-10-18T14:00:00Z");

const ts = (offsetSeconds: number) => new Date(now + offsetSeconds * 1000).toISOString();

export const runs = [
  { namespace: "team-a", name: "etl-nightly-8k2pq", uid: "u1", phase: "Running", createdAt: ts(-300), startedAt: ts(-290), template: "etl-nightly", nodesDone: 3, nodesTotal: 7 },
  { namespace: "team-a", name: "deploy-api-x7c1m", uid: "u2", phase: "Suspended", message: "Waiting for an approval", createdAt: ts(-900), startedAt: ts(-890), nodesDone: 2, nodesTotal: 4 },
  { namespace: "team-a", name: "train-model-4rq9z", uid: "u3", phase: "Failed", message: 'task "evaluate" failed', createdAt: ts(-4000), startedAt: ts(-3990), finishedAt: ts(-1200), nodesDone: 5, nodesTotal: 6 },
  { namespace: "team-a", name: "backfill-2026-10", uid: "u4", phase: "Succeeded", archived: true, createdAt: ts(-86400), startedAt: ts(-86390), finishedAt: ts(-80000), template: "backfill", nodesDone: 40, nodesTotal: 40 },
];

// run is a DAG run: fetch and validate ran, train runs, report waits.
const oldRun = { namespace: "team-a", name: "etl-nightly-old01", uid: "u9", phase: "Succeeded", createdAt: ts(-900000), startedAt: ts(-900000), finishedAt: ts(-899000), nodesDone: 4, nodesTotal: 4 };

export function run(phase: string, gate = false, archived = false) {
  const done = phase === "Succeeded";
  const node = (id: string, name: string, extra: object) => ({ id, name: `etl-nightly-8k2pq${name}`, ...extra });
  return {
    summary: { namespace: "team-a", name: "etl-nightly-8k2pq", uid: "u1", phase, archived, startedAt: ts(-290), template: "etl-nightly", nodesDone: done ? 4 : 2, nodesTotal: 4 },
    workflow: {
      metadata: { name: "etl-nightly-8k2pq", namespace: "team-a" },
      spec: {},
      status: {
        phase,
        storedSpec: {
          arguments: { parameters: [{ name: "day", value: "2026-10-17" }, { name: "rows", value: "all" }] },
          templates: [
            { name: "main", dag: { tasks: [{ name: "fetch" }, { name: "validate", depends: "fetch" }, { name: "train", dependencies: ["validate"] }, { name: "report", depends: "train.Succeeded" }, { name: "gate", depends: "validate" }] } },
            { name: "approve", suspend: {}, outputs: { parameters: [{ name: "env", default: "staging", description: "The environment to deploy to." }] } },
          ],
        },
        nodes: {
          "etl-nightly-8k2pq": { id: "etl-nightly-8k2pq", name: "etl-nightly-8k2pq", type: "DAG", templateName: "main", phase, startedAt: ts(-290), children: ["n1", "n2", "n3", "n4"] },
          n1: node("n1", ".fetch", {
            displayName: "fetch", type: "Pod", phase: "Succeeded", startedAt: ts(-290), finishedAt: ts(-230), podName: "n1",
            outputs: { result: "1204 rows", artifacts: [{ name: "rows", key: "team-a/u1/n1/rows.tgz" }] },
          }),
          n2: node("n2", ".validate", {
            displayName: "validate", type: "Pod", phase: "Succeeded", startedAt: ts(-228), finishedAt: ts(-200), podName: "n2",
            inputs: { parameters: [{ name: "rows", value: "1204 rows" }], artifacts: [{ name: "rows", path: "/tmp/rows", key: "team-a/u1/n1/rows.tgz" }] },
          }),
          n3: node("n3", ".train", { displayName: "train", type: "Retry", phase: done ? "Succeeded" : "Running", startedAt: ts(-198), finishedAt: done ? ts(-20) : undefined, children: ["n3a", "n3b"] }),
          n3a: node("n3a", ".train(0)", { displayName: "train(0)", type: "Pod", phase: "Failed", message: "OOMKilled (exit code 137)", startedAt: ts(-198), finishedAt: ts(-150), podName: "n3a" }),
          n3b: node("n3b", ".train(1)", { displayName: "train(1)", type: "Pod", phase: done ? "Succeeded" : "Running", startedAt: ts(-149), finishedAt: done ? ts(-20) : undefined, podName: "n3b" }),
          ...(gate
            ? { n5: node("n5", ".gate", { displayName: "gate", type: "Suspend", templateName: "approve", phase: "Running", startedAt: ts(-10), message: "waiting for an approval" }) }
            : {}),
          ...(done ? { n4: node("n4", ".report", { displayName: "report", type: "Pod", phase: "Succeeded", startedAt: ts(-18), finishedAt: ts(-5), podName: "n4" }) } : {}),
        },
      },
    },
  };
}

const templateSpec = {
  entrypoint: "main",
  templates: [{ name: "main", dag: { tasks: [{ name: "build", template: "build" }, { name: "roll-out", template: "deploy", depends: "build" }] } }],
};

export const templateList = [
  {
    namespace: "team-a", name: "deploy-api", description: "Builds and deploys the API to an environment.", entrypoint: "main",
    parameters: [
      { name: "env", default: "staging", hasDefault: true, enum: ["staging", "prod"], description: "The target environment." },
      { name: "version", hasDefault: false, enum: [], description: "The image tag, for example 1.4.2." },
    ],
  },
  { namespace: "", name: "base-pipeline", clusterScope: true, entrypoint: "main", parameters: [], managedBy: "Argo CD" },
];

export const cronList = [
  { namespace: "team-a", name: "etl-nightly", schedules: ["0 3 * * *"], timezone: "Europe/Berlin", nextScheduledAt: ts(3 * 3600 + 240), lastScheduledAt: ts(-21 * 3600), template: "etl-nightly" },
  { namespace: "team-a", name: "cleanup", schedules: ["*/15 * * * *"], suspend: true, managedBy: "Flux" },
];

export const taskList = [
  { id: "u1/n7", name: "send-mail", queue: "mail", state: "running", workflow: "etl-nightly-8k2pq", nodeName: "etl-nightly-8k2pq.notify", worker: "mailer-6d9f", createdAt: ts(-40), startedAt: ts(-38) },
  { id: "u1/n8", name: "score-model", queue: "gpu", state: "pending", workflow: "train-model-4rq9z", nodeName: "train-model-4rq9z.score", createdAt: ts(-20) },
  { id: "u1/n9", name: "send-mail", queue: "mail", state: "failed", workflow: "deploy-api-x7c1m", nodeName: "deploy-api-x7c1m.notify", worker: "mailer-6d9f", message: "the worker sent no heartbeat for 30s", createdAt: ts(-600) },
];

export interface MockOptions {
  loggedIn?: boolean;
  namespaces?: string[];
  runPhase?: string;
  // canSubmit is false for a viewer of team-a.
  canSubmit?: boolean;
  // deny makes these RPCs answer "permission denied", like the Kubernetes RBAC of the user would.
  deny?: string[];
  // gate adds a suspend node that waits for an approval.
  gate?: boolean;
  // archived makes the run come from the archive.
  archived?: boolean;
}

// calls records the actions that the UI sent, by RPC name.
export type Calls = Record<string, unknown[]>;

export async function mockAPI(page: Page, opts: MockOptions = {}): Promise<Calls> {
  const calls: Calls = {};
  const loggedIn = opts.loggedIn ?? true;
  const names = opts.namespaces ?? ["team-a", "team-b"];
  await page.route("**/auth/me", (r) =>
    loggedIn
      ? r.fulfill({ json: { user: "oidc:alice@example.com", groups: ["oidc:data"], csrfToken: "csrf", expiresAt: ts(3600) } })
      : r.fulfill({ status: 401, body: "log in first" }),
  );
  await page.route("**/chronicle.v1.NamespaceService/ListNamespaces", (r) =>
    r.fulfill({ json: { namespaces: names.map((name, i) => ({ name, canSubmit: i === 0 && opts.canSubmit !== false })) } }),
  );
  await page.route("**/chronicle.v1.WorkflowService/ListWorkflows", (r) => {
    const body = r.request().postDataJSON() ?? {};
    const phases: string[] = body.phases ?? [];
    // The unfiltered list has a second page with one old run.
    if (body.pageToken === "p2") return r.fulfill({ json: { workflows: [oldRun] } });
    r.fulfill({ json: { workflows: runs.filter((w) => phases.length === 0 || phases.includes(w.phase)), nextPageToken: phases.length === 0 ? "p2" : "" } });
  });
  await page.route("**/chronicle.v1.LogService/StreamLogs", (r) => {
    // A streaming request is one envelope: 5 bytes of header, then the JSON message.
    const node: string = JSON.parse((r.request().postDataBuffer() ?? Buffer.from("     {}")).subarray(5).toString()).node ?? "";
    r.fulfill({ headers: { "content-type": "application/connect+json" }, body: envelopes(logLines(node, opts.archived)) });
  });
  await page.route("**/chronicle.v1.WorkflowService/GetWorkflow", (r) => r.fulfill({ json: run(opts.runPhase ?? "Running", opts.gate, opts.archived) }));
  await page.route("**/chronicle.v1.TemplateService/ListTemplates", (r) => r.fulfill({ json: { templates: templateList } }));
  await page.route("**/chronicle.v1.TemplateService/GetTemplate", (r) => {
    const name: string = r.request().postDataJSON()?.name;
    const t = templateList.find((x) => x.name === name);
    r.fulfill({ json: { summary: t, template: { apiVersion: "chronicle.thinnesen.com/v1alpha1", kind: "WorkflowTemplate", metadata: { name, uid: "t1", annotations: { "kubectl.kubernetes.io/last-applied-configuration": "{}" } }, spec: templateSpec } } });
  });
  await page.route("**/chronicle.v1.CronService/ListCronWorkflows", (r) => r.fulfill({ json: { cronWorkflows: cronList } }));
  await page.route("**/chronicle.v1.TaskService/ListTasks", (r) => {
    const states: string[] = r.request().postDataJSON()?.states ?? [];
    r.fulfill({ json: { tasks: taskList.filter((t) => states.length === 0 || states.includes(t.state)) } });
  });
  await page.route("**/chronicle.v1.CronService/GetCronWorkflow", (r) => {
    const name: string = r.request().postDataJSON()?.name;
    r.fulfill({ json: { summary: cronList.find((c) => c.name === name), cronWorkflow: { kind: "CronWorkflow", metadata: { name } } } });
  });
  await page.route("**/chronicle.v1.WorkflowService/SubmitWorkflow", (r) => {
    (calls.SubmitWorkflow ??= []).push(r.request().postDataJSON());
    r.fulfill({ json: { summary: { namespace: "team-a", name: "deploy-api-new01" } } });
  });
  for (const rpc of ["SuspendCronWorkflow", "ResumeCronWorkflow"]) {
    await page.route(`**/chronicle.v1.CronService/${rpc}`, (r) => {
      (calls[rpc] ??= []).push(r.request().postDataJSON());
      r.fulfill({ json: {} });
    });
  }
  await page.route("**/chronicle.v1.WorkflowService/RerunWorkflow", (r) => {
    (calls.RerunWorkflow ??= []).push(r.request().postDataJSON());
    if (opts.deny?.includes("RerunWorkflow")) {
      return r.fulfill({ status: 403, json: { code: "permission_denied", message: "workflows is forbidden" } });
    }
    return r.fulfill({ json: { summary: { namespace: "team-a", name: "etl-nightly-rr001" } } });
  });
  for (const rpc of ["SuspendWorkflow", "ResumeWorkflow", "StopWorkflow", "TerminateWorkflow", "DeleteWorkflow", "ApproveNode", "RetryWorkflow"]) {
    await page.route(`**/chronicle.v1.WorkflowService/${rpc}`, (r) => {
      (calls[rpc] ??= []).push(r.request().postDataJSON());
      if (opts.deny?.includes(rpc)) {
        return r.fulfill({ status: 403, json: { code: "permission_denied", message: "workflows is forbidden" } });
      }
      return r.fulfill({ json: {} });
    });
  }
  await page.clock.setFixedTime(now);
  return calls;
}

// logLines are the lines of a node. An archived run has no Pods, so its lines come from the log archive.
function logLines(node: string, archived = false) {
  return [
    { nodeId: node, line: `\u001b[32mstarting ${node}\u001b[0m`, time: ts(-100) },
    { nodeId: node, line: "loaded 1204 rows" },
    { nodeId: node, line: "epoch 1/3 loss=0.41" },
  ].map((l) => ({ ...l, archived }));
}

// envelopes encodes messages as a Connect stream: a flag byte and a 4-byte length before each JSON message.
function envelopes(messages: object[]): Buffer {
  const frame = (flag: number, data: object) => {
    const json = Buffer.from(JSON.stringify(data));
    const head = Buffer.alloc(5);
    head.writeUInt8(flag, 0);
    head.writeUInt32BE(json.length, 1);
    return Buffer.concat([head, json]);
  };
  return Buffer.concat([...messages.map((m) => frame(0, m)), frame(2, {})]);
}

// openMenu opens the sidebar on a phone, where it is a drawer. On a wide screen the sidebar is always open.
export async function openMenu(page: Page): Promise<void> {
  await page.getByRole("banner").waitFor();
  const button = page.getByRole("button", { name: "Open the menu" });
  if (await button.isVisible()) await button.click();
}
