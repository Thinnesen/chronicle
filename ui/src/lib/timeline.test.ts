import { describe, expect, it } from "vitest";

import { rows, ticks } from "./timeline";
import type { NodeStatus } from "./workflow";

const t = (s: number) => new Date(Date.UTC(2026, 9, 19, 12, 0, s)).toISOString();

describe("rows", () => {
  it("puts the attempts of a retry in one row and keeps a running node open", () => {
    const nodes: Record<string, NodeStatus> = {
      wf: { id: "wf", name: "wf", type: "DAG", children: ["a", "r"] },
      a: { id: "a", name: "wf.a", type: "Pod", startedAt: t(0), finishedAt: t(10) },
      r: { id: "r", name: "wf.b", type: "Retry", children: ["b0", "b1"] },
      b1: { id: "b1", name: "wf.b(1)", type: "Pod", startedAt: t(30) },
      b0: { id: "b0", name: "wf.b(0)", type: "Pod", startedAt: t(10), finishedAt: t(20) },
    };
    const now = Date.parse(t(40));
    const out = rows(nodes, now);
    expect(out.map((r) => r.id)).toEqual(["a", "r"]);
    expect(out[1]?.segments.map((s) => s.node.id)).toEqual(["b0", "b1"]);
    expect(out[1]?.end).toBe(now);
  });
});

describe("ticks", () => {
  it("uses a round interval", () => {
    expect(ticks(55_000)).toEqual([0, 10_000, 20_000, 30_000, 40_000, 50_000]);
  });
});
