import { Graph, layout } from "@dagrejs/dagre";

import { isStation, nodeLabel, type GraphEdge, type NodeStatus } from "./workflow";

export const station = { width: 192, height: 44 };
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
    g.setNode(n.id, isStation(n) ? { ...station } : { width: junctionWidth(nodeLabel(n)), height: junction.height });
  }
  for (const e of edges) g.setEdge(e.from, e.to);
  layout(g);
  const placed = nodes.map((n) => {
    const p = g.node(n.id);
    return { node: n, x: p.x - p.width / 2, y: p.y - p.height / 2, width: p.width, height: p.height };
  });
  const graph = g.graph();
  return { placed, width: graph.width ?? 0, height: graph.height ?? 0 };
}
