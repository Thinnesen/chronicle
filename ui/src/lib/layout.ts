import { Graph, layout } from "@dagrejs/dagre";

import { isStation, nodeLabel, type GraphEdge, type NodeStatus } from "./workflow";

export const station = { width: 192, height: 44 };
// narrowStation is the card on a phone. There the graph runs from top to bottom, and the items of a fan-out must fit
// side by side.
export const narrowStation = { width: 152, height: 44 };
export const junction = { height: 24, maxWidth: 176 };

let context: CanvasRenderingContext2D | null | undefined;

// junctionWidth is the drawn width of a junction pill: the icon, the label and the padding.
// The layout uses the drawn width, so the edges touch the pill.
export function junctionWidth(label: string): number {
  if (context === undefined) {
    try {
      context = document.createElement("canvas").getContext("2d");
    } catch {
      context = null;
    }
  }
  let text = label.length * 7;
  if (context) {
    context.font = `600 12px ${getComputedStyle(document.documentElement).getPropertyValue("--font-ui") || "sans-serif"}`;
    text = context.measureText(label).width;
  }
  return Math.min(junction.maxWidth, Math.ceil(text) + 38);
}

export interface Placed {
  node: NodeStatus;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface Layout {
  placed: Placed[];
  width: number;
  height: number;
}

// place lays out the nodes from left to right, or from top to bottom on a narrow screen.
export function place(nodes: NodeStatus[], edges: GraphEdge[], direction: "LR" | "TB" = "LR"): Layout {
  const g = new Graph();
  g.setGraph({ rankdir: direction, nodesep: 16, ranksep: direction === "LR" ? 40 : 32, marginx: 24, marginy: 24 });
  g.setDefaultEdgeLabel(() => ({}));
  for (const n of nodes) {
    const card = direction === "TB" ? narrowStation : station;
    g.setNode(n.id, isStation(n) ? { ...card } : { width: junctionWidth(nodeLabel(n)), height: junction.height });
  }
  for (const e of edges) g.setEdge(e.from, e.to);
  layout(g);
  const placed = nodes.map((n) => {
    const p = g.node(n.id);
    return { node: n, x: p.x - p.width / 2, y: p.y - p.height / 2, width: p.width, height: p.height };
  });
  sortSiblings(placed, edges, direction);
  alignToParents(placed, edges, direction);
  const graph = g.graph();
  return { placed, width: graph.width ?? 0, height: graph.height ?? 0 };
}

const natural = (a: Placed, b: Placed) => a.node.name.localeCompare(b.node.name, undefined, { numeric: true });

// sortSiblings puts the children of one parent in the order of their names, for example the items of a fan-out.
// dagre orders them to reduce crossings, which can reverse them. The siblings keep their slots, so no edge moves.
function sortSiblings(placed: Placed[], edges: GraphEdge[], direction: "LR" | "TB"): void {
  const parents = new Map<string, string[]>();
  for (const e of edges) parents.set(e.to, [...(parents.get(e.to) ?? []), e.from]);
  const families = new Map<string, Placed[]>();
  for (const p of placed) {
    const from = parents.get(p.node.id);
    if (from?.length !== 1) continue;
    const key = `${from[0]}|${direction === "LR" ? p.x : p.y}`;
    families.set(key, [...(families.get(key) ?? []), p]);
  }
  const axis = direction === "LR" ? "y" : "x";
  for (const family of families.values()) {
    if (family.length < 2) continue;
    const slots = family.map((p) => p[axis]).sort((a, b) => a - b);
    family.sort(natural).forEach((p, i) => (p[axis] = slots[i] ?? p[axis]));
  }
}

// alignToParents keeps chains straight after sortSiblings. In each rank, the nodes with one parent and the same size
// take the slots in the order of their parents. An example is the exit hooks of parallel steps. The ranks go in
// order, so a parent has its final slot before its children move.
function alignToParents(placed: Placed[], edges: GraphEdge[], direction: "LR" | "TB"): void {
  const parents = new Map<string, string[]>();
  for (const e of edges) parents.set(e.to, [...(parents.get(e.to) ?? []), e.from]);
  const byID = new Map(placed.map((p) => [p.node.id, p]));
  const axis = direction === "LR" ? "y" : "x";
  const rankOf = (p: Placed) => (direction === "LR" ? p.x : p.y);
  const ranks = new Map<string, Placed[]>();
  for (const p of placed) {
    const from = parents.get(p.node.id);
    if (from?.length !== 1 || !byID.has(from[0] ?? "")) continue;
    const key = `${rankOf(p)}|${p.width}x${p.height}`;
    ranks.set(key, [...(ranks.get(key) ?? []), p]);
  }
  const ordered = [...ranks.values()].sort((a, b) => rankOf(a[0] as Placed) - rankOf(b[0] as Placed));
  for (const group of ordered) {
    if (group.length < 2) continue;
    const parentAt = (p: Placed) => byID.get(parents.get(p.node.id)?.[0] ?? "")?.[axis] ?? 0;
    const slots = group.map((p) => p[axis]).sort((a, b) => a - b);
    group.sort((a, b) => parentAt(a) - parentAt(b) || natural(a, b)).forEach((p, i) => (p[axis] = slots[i] ?? p[axis]));
  }
}
