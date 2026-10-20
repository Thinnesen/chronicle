import { describe, expect, it } from "vitest";

import { place } from "./layout";
import type { NodeStatus } from "./workflow";

describe("place", () => {
  it("keeps the later nodes of parallel branches in the order of the branches", () => {
    // The map order of the status is random; here the hook of b comes first.
    const n = (id: string, name: string): NodeStatus => ({ id, name, type: "Pod" });
    const nodes = [n("hb", "wf.b.onExit"), n("ha", "wf.a.onExit"), n("b", "wf.b"), n("a", "wf.a"), { id: "g", name: "wf[1]", type: "StepGroup" as const }];
    const edges = [
      { from: "g", to: "b" },
      { from: "g", to: "a" },
      { from: "b", to: "hb" },
      { from: "a", to: "ha" },
    ];
    for (const direction of ["LR", "TB"] as const) {
      const at = new Map(place(nodes, edges, direction).placed.map((p) => [p.node.id, direction === "LR" ? p.y : p.x]));
      expect((at.get("a") ?? 0) < (at.get("b") ?? 0)).toBe(true);
      expect((at.get("ha") ?? 0) < (at.get("hb") ?? 0)).toBe(true);
    }
  });
});
