import { describe, expect, it } from "vitest";

import { fold } from "./fold";
import type { GraphEdge, NodeStatus } from "./workflow";

describe("fold", () => {
  const items = Array.from({ length: 10 }, (_, i): NodeStatus => ({ id: `i${i}`, name: `g(${i})`, type: "Pod", phase: i === 9 ? "Failed" : "Succeeded" }));
  const nodes: Record<string, NodeStatus> = {
    g: { id: "g", name: "g", type: "TaskGroup", children: items.map((n) => n.id) },
    next: { id: "next", name: "next", type: "Pod" },
    ...Object.fromEntries(items.map((n) => [n.id, n])),
  };
  const edges: GraphEdge[] = [...items.flatMap((n) => [{ from: "g", to: n.id }, { from: n.id, to: "next" }])];

  it("keeps the failed item and hides the rest behind one node", () => {
    const f = fold(nodes, edges, new Set());
    const ids = f.nodes.map((n) => n.id);
    expect(ids).toContain("i9");
    expect(ids).toContain("g/more");
    expect(ids).not.toContain("i8");
    expect(f.hidden["g/more"]).toEqual({ Succeeded: 5 });
    expect(f.nodes.find((n) => n.id === "g/more")?.phase).toBe("Succeeded");
    expect(f.edges.filter((e) => e.from === "g/more").map((e) => e.to)).toEqual(["next"]);
  });

  it("shows all items of an expanded fan-out", () => {
    expect(fold(nodes, edges, new Set(["g"])).nodes).toHaveLength(12);
  });
});
