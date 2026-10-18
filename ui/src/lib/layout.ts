import { Graph, layout } from "@dagrejs/dagre";

import { isStation, type GraphEdge, type NodeStatus } from "./workflow";

export const station = { width: 184, height: 48 };
export const junction = { width: 128, height: 28 };

export interface Placed {
  node: NodeStatus;
  x: number;
  y: number;
}

export interface Layout {
  placed: Placed[];
  width: number;
  height: number;
}

// place lays out the nodes like a track diagram: from left to right, or from top to bottom on a narrow screen.
export function place(nodes: NodeStatus[], edges: GraphEdge[], direction: "LR" | "TB" = "LR"): Layout {
  const g = new Graph();
  g.setGraph({ rankdir: direction, nodesep: 18, ranksep: direction === "LR" ? 56 : 32, marginx: 16, marginy: 16 });
  g.setDefaultEdgeLabel(() => ({}));
  for (const n of nodes) g.setNode(n.id, isStation(n) ? { ...station } : { ...junction });
  for (const e of edges) g.setEdge(e.from, e.to);
  layout(g);
  const placed = nodes.map((n) => {
    const p = g.node(n.id);
    const size = isStation(n) ? station : junction;
    return { node: n, x: p.x - size.width / 2, y: p.y - size.height / 2 };
  });
  const graph = g.graph();
  return { placed, width: graph.width ?? 0, height: graph.height ?? 0 };
}
