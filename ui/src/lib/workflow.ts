// The parts of a Workflow object that the UI reads. The API sends the object as JSON.

export interface Parameter {
  name: string;
  value?: string;
}

export interface NodeStatus {
  id: string;
  name: string;
  displayName?: string;
  type: "Pod" | "DAG" | "Steps" | "StepGroup" | "TaskGroup" | "Retry" | "Suspend";
  templateName?: string;
  phase?: string;
  startedAt?: string;
  finishedAt?: string;
  message?: string;
  children?: string[];
  podName?: string;
  inputs?: { parameters?: Parameter[] };
  outputs?: { parameters?: Parameter[]; result?: string };
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

// A station does work (a Pod or a suspend). A junction only groups other nodes.
export function isStation(n: NodeStatus): boolean {
  return n.type === "Pod" || n.type === "Suspend";
}

// buildGraph returns the edges between the nodes of a run. The status has a tree (parent to children);
// the edges between DAG tasks come from the dependencies in the spec.
export function buildGraph(wf: Workflow): GraphEdge[] {
  const nodes = wf.status?.nodes ?? {};
  const byName = new Map(Object.values(nodes).map((n) => [n.name, n]));
  const templates = new Map((wf.status?.storedSpec?.templates ?? wf.spec.templates ?? []).map((t) => [t.name, t]));
  const edges: GraphEdge[] = [];
  const add = (from?: NodeStatus, to?: NodeStatus) => {
    if (from && to) edges.push({ from: from.id, to: to.id });
  };
  for (const n of Object.values(nodes)) {
    const t = n.templateName ? templates.get(n.templateName) : undefined;
    if (n.type === "DAG" && t?.dag) {
      for (const task of t.dag.tasks) {
        const child = byName.get(`${n.name}.${task.name}`);
        const deps = taskDependencies(task);
        if (deps.length === 0) add(n, child);
        for (const d of deps) add(byName.get(`${n.name}.${d}`), child);
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
        prev = (group.children ?? []).map((id) => nodes[id]).filter((c): c is NodeStatus => !!c);
        for (const step of prev) add(group, step);
      }
      continue;
    }
    if (n.type === "TaskGroup" || n.type === "Retry") {
      for (const id of n.children ?? []) add(n, nodes[id]);
    }
  }
  return edges;
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
