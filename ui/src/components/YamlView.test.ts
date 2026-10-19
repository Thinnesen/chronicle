import { describe, expect, it } from "vitest";

import { toYaml } from "./YamlView";

describe("toYaml", () => {
  it("writes the kind, the metadata and the names first, as Kubernetes YAML does", () => {
    const yaml = toYaml({ spec: { templates: [{ dag: {}, name: "main" }] }, metadata: { name: "x" }, kind: "Workflow", apiVersion: "v1" });
    expect(yaml).toBe("apiVersion: v1\nkind: Workflow\nmetadata:\n  name: x\nspec:\n  templates:\n    - name: main\n      dag: {}\n");
  });
});
