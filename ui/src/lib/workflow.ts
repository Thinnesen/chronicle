// The parts of a Workflow object that the UI reads. The API sends the object as JSON.

export interface Parameter {
  name: string;
  value?: string;
}

export interface NodeStatus {
  id: string;
  name: string;
  displayName?: string;
  type: "Pod" | "DAG" | "Steps" | "StepGroup" | "TaskGroup" | "Retry" | "Suspend" | "Task";
  templateName?: string;
  phase?: string;
  startedAt?: string;
  finishedAt?: string;
  message?: string;
  children?: string[];
  podName?: string;
  logKey?: string;
  inputs?: { parameters?: Parameter[]; artifacts?: Artifact[] };
  outputs?: { parameters?: Parameter[]; result?: string; artifacts?: Artifact[] };
}

export interface Artifact {
  name: string;
  path?: string;
}

// artifactURL is the download link of an output artifact of a node.
export function artifactURL(namespace: string, workflow: string, node: string, name: string): string {
  return `/artifacts/${[namespace, workflow, node, name].map(encodeURIComponent).join("/")}`;
}

interface DAGTask {
  name: string;
  dependencies?: string[];
  depends?: string;
}

export interface Template {
  name: string;
  suspend?: { duration?: string };
  outputs?: { parameters?: { name: string; default?: string; description?: string }[] };
  dag?: { tasks: DAGTask[] };
  steps?: { name: string }[][];
}

export interface Workflow {
  metadata: { name: string; namespace: string; creationTimestamp?: string };
  spec: { templates?: Template[]; arguments?: { parameters?: Parameter[] }; workflowTemplateRef?: { name: string }; suspend?: boolean; shutdown?: "Stop" | "Terminate"; approvals?: { node: string }[] };
  status?: {
    phase?: string;
    nodes?: Record<string, NodeStatus>;
    storedSpec?: { templates?: Template[]; arguments?: { parameters?: Parameter[] } };
  };
}

export interface GraphEdge {
  from: string;
  to: string;
}

// A station does work (a Pod, a task or a suspend). A junction only groups other nodes.
export function isStation(n: NodeStatus): boolean {
  return n.type === "Pod" || n.type === "Suspend" || n.type === "Task";
}

// buildGraph returns the edges between the nodes of a run. The status has a tree (parent to children);
// the edges between DAG tasks come from the dependencies in the spec. A dependency starts at the exits
// of a node (see exits), so that a later task never looks like a part of a retry or a fan-out.
export function buildGraph(wf: Workflow): GraphEdge[] {
  const nodes = wf.status?.nodes ?? {};
  const byName = new Map(Object.values(nodes).map((n) => [n.name, n]));
  const templates = new Map((wf.status?.storedSpec?.templates ?? wf.spec.templates ?? []).map((t) => [t.name, t]));
  const edges: GraphEdge[] = [];
  const add = (from?: NodeStatus, to?: NodeStatus) => {
    if (from && to) edges.push({ from: from.id, to: to.id });
  };
  const exitsOf = (n: NodeStatus) => exits(n, nodes, byName, templates);
  for (const n of Object.values(nodes)) {
    const t = n.templateName ? templates.get(n.templateName) : undefined;
    if (n.type === "DAG" && t?.dag) {
      for (const task of t.dag.tasks) {
        const child = byName.get(`${n.name}.${task.name}`);
        const deps = taskDependencies(task);
        if (deps.length === 0) add(n, child);
        for (const d of deps) {
          const dep = byName.get(`${n.name}.${d}`);
          for (const e of dep ? exitsOf(dep) : []) add(e, child);
        }
      }
      continue;
    }
    if (n.type === "Steps") {
      // Steps run group by group: the node, then each group, then the steps of the group, then the next group.
      let prev: NodeStatus[] = [n];
      for (const groupID of n.children ?? []) {
        const group = nodes[groupID];
        if (!group) continue;
        for (const p of prev) add(p, group);
        const steps = (group.children ?? []).map((id) => nodes[id]).filter((c): c is NodeStatus => !!c);
        for (const step of steps) add(group, step);
        prev = steps.flatMap(exitsOf);
      }
      continue;
    }
    if (n.type === "Retry") {
      // Attempts run one after the other.
      let prev = n;
      for (const id of n.children ?? []) {
        add(prev, nodes[id]);
        prev = nodes[id] ?? prev;
      }
      continue;
    }
    if (n.type === "TaskGroup") {
      for (const id of n.children ?? []) add(n, nodes[id]);
    }
  }
  return edges;
}

// exits returns the nodes where the work of a node ends: the node itself, the last attempt of a retry,
// the items of a fan-out, the leaf tasks of a DAG or the last group of steps.
function exits(
  n: NodeStatus,
  nodes: Record<string, NodeStatus>,
  byName: Map<string, NodeStatus>,
  templates: Map<string, Template>,
): NodeStatus[] {
  const kids = (n.children ?? []).map((id) => nodes[id]).filter((c): c is NodeStatus => !!c);
  const sub = (list: NodeStatus[]) => (list.length ? list.flatMap((c) => exits(c, nodes, byName, templates)) : [n]);
  switch (n.type) {
    case "Retry":
      return sub(kids.slice(-1));
    case "TaskGroup":
      return sub(kids);
    case "Steps":
      return sub((kids.at(-1)?.children ?? []).map((id) => nodes[id]).filter((c): c is NodeStatus => !!c));
    case "DAG": {
      const tasks = (n.templateName && templates.get(n.templateName)?.dag?.tasks) || [];
      const needed = new Set(tasks.flatMap(taskDependencies));
      const leaves = tasks.filter((t) => !needed.has(t.name)).map((t) => byName.get(`${n.name}.${t.name}`));
      return sub(leaves.filter((c): c is NodeStatus => !!c));
    }
  }
  return [n];
}

// taskDependencies returns the task names in "dependencies" or in a "depends" expression.
export function taskDependencies(task: DAGTask): string[] {
  if (task.dependencies?.length) return task.dependencies;
  if (!task.depends) return [];
  const names = task.depends.match(/[A-Za-z0-9_-]+(?=\.[A-Za-z]+|\b)/g) ?? [];
  const results = new Set(["Succeeded", "Failed", "Errored", "Skipped", "Omitted", "AnySucceeded", "AllFailed"]);
  return [...new Set(names.filter((x) => !results.has(x)))];
}

// nodeLabel is the short name of a node for the graph.
export function nodeLabel(n: NodeStatus): string {
  return n.displayName || n.name;
}

// templateOf returns the template that a node runs.
export function templateOf(wf: Workflow, n: NodeStatus): Template | undefined {
  const templates = wf.status?.storedSpec?.templates ?? wf.spec.templates ?? [];
  return templates.find((t) => t.name === n.templateName);
}

// runParameters returns the workflow parameters of a run with their values.
export function runParameters(wf: Workflow): Parameter[] {
  return wf.status?.storedSpec?.arguments?.parameters ?? wf.spec.arguments?.parameters ?? [];
}
