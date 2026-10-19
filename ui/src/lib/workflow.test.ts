import { describe, expect, it } from "vitest";

import { buildGraph, taskDependencies, withPlan, type Workflow } from "./workflow";

const wf: Workflow = {
  metadata: { name: "wf", namespace: "ns" },
  spec: {
    templates: [
      { name: "main", dag: { tasks: [{ name: "a" }, { name: "b", dependencies: ["a"] }, { name: "c", depends: "a.Succeeded && !b.Failed" }] } },
      { name: "st", steps: [[{ name: "x" }], [{ name: "y" }]] },
    ],
  },
  status: {
    nodes: {
      wf: { id: "wf", name: "wf", type: "DAG", templateName: "main", children: ["A", "B", "C"] },
      A: { id: "A", name: "wf.a", type: "Pod" },
      B: { id: "B", name: "wf.b", type: "Pod" },
      C: { id: "C", name: "wf.c", type: "Steps", templateName: "st", children: ["G0", "G1"] },
      G0: { id: "G0", name: "wf.c[0]", type: "StepGroup", children: ["X"] },
      X: { id: "X", name: "wf.c[0].x", type: "Pod" },
      G1: { id: "G1", name: "wf.c[1]", type: "StepGroup", children: ["Y"] },
      Y: { id: "Y", name: "wf.c[1].y", type: "Retry", children: ["Y0"] },
      Y0: { id: "Y0", name: "wf.c[1].y(0)", type: "Pod" },
    },
  },
};

describe("buildGraph", () => {
  it("connects DAG dependencies, step groups and attempts", () => {
    const edges = buildGraph(wf).map((e) => `${e.from}>${e.to}`).sort();
    expect(edges).toEqual(["A>B", "A>C", "B>C", "C>G0", "G0>X", "G1>Y", "X>G1", "Y>Y0", "wf>A"].sort());
  });

  it("chains retry attempts and starts a dependency at the last attempt", () => {
    const retried: Workflow = {
      metadata: { name: "r", namespace: "ns" },
      spec: { templates: [{ name: "main", dag: { tasks: [{ name: "t" }, { name: "load", depends: "t" }] } }] },
      status: {
        nodes: {
          r: { id: "r", name: "r", type: "DAG", templateName: "main", children: ["T", "L"] },
          T: { id: "T", name: "r.t", type: "Retry", children: ["T0", "T1"] },
          T0: { id: "T0", name: "r.t(0)", type: "Pod" },
          T1: { id: "T1", name: "r.t(1)", type: "Pod" },
          L: { id: "L", name: "r.load", type: "Pod" },
        },
      },
    };
    const edges = buildGraph(retried).map((e) => `${e.from}>${e.to}`).sort();
    expect(edges).toEqual(["T0>T1", "T1>L", "T>T0", "r>T"].sort());
  });

  it("uses the stored spec of a template reference", () => {
    const ref: Workflow = { ...wf, spec: {}, status: { ...wf.status, storedSpec: wf.spec } };
    expect(buildGraph(ref)).toHaveLength(9);
  });
});

describe("withPlan", () => {
  it("adds the tasks that have not started and connects them", () => {
    const started: Workflow = {
      metadata: { name: "p", namespace: "ns" },
      spec: { templates: [{ name: "main", dag: { tasks: [{ name: "a" }, { name: "b", depends: "a" }] } }] },
      status: { nodes: { p: { id: "p", name: "p", type: "DAG", templateName: "main", children: ["A"] }, A: { id: "A", name: "p.a", type: "Pod" } } },
    };
    const nodes = withPlan(started);
    expect(nodes["p/planned/b"]).toMatchObject({ name: "p.b", type: "Pod", planned: true });
    expect(buildGraph(started, nodes).map((e) => `${e.from}>${e.to}`)).toContain("A>p/planned/b");
  });

  it("adds the step groups that have not started after the last group", () => {
    const started: Workflow = {
      metadata: { name: "s", namespace: "ns" },
      spec: { templates: [{ name: "main", steps: [[{ name: "build" }], [{ name: "ship", template: "main" }, { name: "mail" }]] }] },
      status: {
        nodes: {
          s: { id: "s", name: "s", type: "Steps", templateName: "main", children: ["G0"] },
          G0: { id: "G0", name: "s[0]", type: "StepGroup", children: ["B"] },
          B: { id: "B", name: "s[0].build", type: "Pod" },
        },
      },
    };
    const nodes = withPlan(started);
    expect(nodes["s/planned/1"]).toMatchObject({ name: "s[1]", displayName: "[1]", type: "StepGroup", planned: true });
    expect(nodes["s/planned/1/ship"]).toMatchObject({ name: "s[1].ship", type: "Steps", planned: true });
    expect(buildGraph(started, nodes).map((e) => `${e.from}>${e.to}`)).toEqual(
      expect.arrayContaining(["B>s/planned/1", "s/planned/1>s/planned/1/ship", "s/planned/1>s/planned/1/mail"]),
    );
  });
});

describe("taskDependencies", () => {
  it("reads names from depends and dependencies", () => {
    expect(taskDependencies({ name: "t", depends: "(a || b-2.Failed) && !c_3.AllFailed" })).toEqual(["a", "b-2", "c_3"]);
    expect(taskDependencies({ name: "t", dependencies: ["x"] })).toEqual(["x"]);
    expect(taskDependencies({ name: "t" })).toEqual([]);
  });
});
