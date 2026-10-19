import type { GraphEdge, NodeStatus } from "./workflow";

// By default, a fan-out with more children than limit shows only keep of them and one "more" node, as Argo Workflows does.
export const limit = 8;
export const keep = 5;

const shownFirst: Record<string, number> = { Failed: 0, Error: 0, Running: 1, Suspended: 1 };

export interface Folded {
  nodes: NodeStatus[];
  edges: GraphEdge[];
  // hidden counts the hidden children of each "more" node, by phase.
  hidden: Record<string, Record<string, number>>;
}

// fold hides the children of large fan-outs, and their descendants, behind one "more" node for each fan-out.
// Failed and running children stay visible. A fan-out in expanded shows all its children.
export function fold(nodes: Record<string, NodeStatus>, edges: GraphEdge[], expanded: ReadonlySet<string>, max = limit, shown = keep): Folded {
  const rep = new Map<string, string>();
  const hidden: Folded["hidden"] = {};
  const extra: NodeStatus[] = [];
  for (const group of Object.values(nodes)) {
    const kids = (group.children ?? []).map((id) => nodes[id]).filter((n): n is NodeStatus => !!n);
    if (group.type !== "TaskGroup" || kids.length <= max || expanded.has(group.id)) continue;
    const order = kids.map((n, i) => ({ n, i })).sort((a, b) => (shownFirst[a.n.phase ?? ""] ?? 2) - (shownFirst[b.n.phase ?? ""] ?? 2) || a.i - b.i);
    const more: NodeStatus = { id: `${group.id}/more`, name: `${group.name}/more`, displayName: "", type: "Pod" };
    const counts: Record<string, number> = {};
    for (const { n } of order.slice(shown)) {
      counts[n.phase || "Pending"] = (counts[n.phase || "Pending"] ?? 0) + 1;
      for (const id of descendants(n, nodes)) rep.set(id, more.id);
    }
    more.displayName = `${order.length - shown} more`;
    more.phase = summary(counts);
    hidden[more.id] = counts;
    extra.push(more);
  }
  if (rep.size === 0) return { nodes: Object.values(nodes), edges, hidden };
  const seen = new Set<string>();
  const out: GraphEdge[] = [];
  for (const e of edges) {
    const from = rep.get(e.from) ?? e.from;
    const to = rep.get(e.to) ?? e.to;
    const key = `${from}>${to}`;
    if (from === to || seen.has(key)) continue;
    seen.add(key);
    out.push({ from, to });
  }
  return { nodes: [...Object.values(nodes).filter((n) => !rep.has(n.id)), ...extra], edges: out, hidden };
}

// summary is the phase of a "more" node: failed if one hidden item failed, succeeded if all succeeded.
function summary(counts: Record<string, number>): string {
  if (counts.Failed || counts.Error) return "Failed";
  if (counts.Running) return "Running";
  return Object.keys(counts).every((p) => p === "Succeeded" || p === "Skipped") ? "Succeeded" : "Pending";
}

function descendants(n: NodeStatus, nodes: Record<string, NodeStatus>): string[] {
  const out = [n.id];
  for (const id of n.children ?? []) {
    const c = nodes[id];
    if (c) out.push(...descendants(c, nodes));
  }
  return out;
}
