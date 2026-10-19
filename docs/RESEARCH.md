# Research

This file records what we learned from the source of similar projects. It tells which ideas we use and which we do not use, and why.
Read the section for a feature before you build it. Add a section when you study a new feature.

References (shallow clones, October 2026): `argoproj/argo-workflows` (v4), `temporalio/temporal`, `temporalio/ui`, `dagster-io/dagster`, `hatchet-dev/hatchet`, `kestra-io/kestra`.

## Engine (Argo `workflow/controller`, `workflow/controller/dag`)

What Argo does:

- Argo v4 moved to one `Engine` for DAG and steps. Steps become DAG tasks: each step depends on the steps of the previous group.
- A pure evaluator (`workflow/controller/dag`) decides if a task is ready, waiting or omitted. It has no side effects. The engine walks the tasks once in dependency order, so a task sees the result of its dependencies in the same pass.
- Omission cascades in one walk: A fails, B (`depends: A.Succeeded`) is omitted, C (`depends: B`) is omitted.
- `depends` results: Succeeded, Failed, Errored, Skipped, Omitted, Daemoned, AnySucceeded, AllFailed. A bare task name means Succeeded, Skipped or Daemoned.
- A table of valid node phase transitions (`node_phase_sm.go`). Terminal phases have no outbound transitions.
- The node ID is a hash of the node path name. `Status.Nodes` is a map of ID to node.
- The executor never writes the Workflow. It writes a separate `WorkflowTaskResult` resource, and the controller merges it.
- Large node maps are compressed, then moved to SQL.

What we do:

- Use: one engine for DAG and steps (steps as DAG tasks with synthetic dependencies). One pass in dependency order. Cascading omission. A phase transition table with tests. Node IDs from a hash of the node path, so the engine stays deterministic.
- Use: the same `depends` vocabulary, without Daemoned (chronicle has no daemon templates). A bare task name means Succeeded or Skipped.
- Change: Argo turns the expression into hex identifiers and evaluates it with `expr-lang`. chronicle has a small hand-written parser for `depends` (only `&&`, `||`, `!`, parentheses and task results). The validation and the engine use the same parser. This removes a dependency and a class of evaluation surprises.
- Change: the Pod reports its outputs through the container termination message, not through an API resource. Then the workflow Pod needs no Kubernetes API access. The limit is 4 KiB per container. Large outputs go to the artifact store. Decided in #5: see `docs/DESIGN.md` section 4.1. Argo v4 also has an "init-less" layout that mounts the executor through a Kubernetes image volume.

## Validation (Argo `workflow/validate`)

- Argo builds a scope of allowed variables for each template and checks that each `{{...}}` in the template resolves in the scope. It checks DAG cycles, unknown templates and DAG argument references to tasks that are not ancestors.
- Argo validation is more than 1700 lines, because it supports many template types and old forms.
- We use the same scope idea, with fewer forms. The CRD schema does the simple checks: unique names (`listType=map`), patterns and "exactly one template type" (CEL). The webhook does the graph checks.

## Retries (Temporal `service/history/workflow/retry.go`)

- Temporal: `maximumAttempts` counts all attempts (the first attempt too). The interval is `initialInterval * backoffCoefficient^(attempt-1)`, capped at `maximumInterval`. An expiration time stops retries. Some error types are not retryable.
- We use: the Argo field names (`retryStrategy.limit`, `backoff.duration`, `backoff.factor`, `backoff.maxDuration`, `retryPolicy`) for the drop-in goal, with Temporal semantics for the cap. Decided in #6: `limit` is required, because in Argo an unset limit retries without end. The default factor is 1 (a constant wait).

## CronWorkflow (Argo `workflow/cron`, Kubernetes CronJob)

- Argo keeps a `robfig/cron` scheduler in memory and runs a job at each time. The Kubernetes CronJob controller reconciles, finds the missed times and requeues.
- We use the CronJob way (restart-safe, deterministic run names) and `robfig/cron/v3` only to parse and to compute the next time. Kubernetes uses the same module.

## UI (Hatchet, Temporal UI, Dagster, Kestra)

- Hatchet (React): TanStack Router, Query and Table, Radix primitives, Tailwind, `reactflow` with `dagre` for the DAG, `cmdk` for a command palette, `cronstrue` for readable cron text, Monaco for YAML.
- Kestra (Vue): Vue Flow with `dagre` for the topology, Monaco with YAML schema support, ECharts.
- Temporal UI (Svelte): an event history and a timeline view of a run, `cronstrue`, CodeMirror.
- Dagster: a Gantt timeline of the steps of a run, next to the logs.
- Hatchet computes the DAG edges from the task "shape". chronicle has a node tree in the status, so the UI computes the edges from the stored spec (template, then tasks, then node names `<boundary>.<task>`).
- Ideas for chronicle (M4): a DAG graph and a Gantt timeline of the same run, side by side with the logs. A command palette. Readable cron text and the next runs. A YAML view with schema completion. Study the real pages before M4 starts and record the result here.

## SDKs: workflows as code (Hera, Hatchet, Dagster)

- Hera (the Python SDK of Argo Workflows) builds Argo YAML with context managers (`with DAG(...)`), decorators for script templates, and `a >> b` for dependencies. The result is a normal Workflow object, so GitOps and kubectl keep working.
- Hatchet (Go, Python, TypeScript) registers functions as tasks: `workflow.NewTask(name, fn, WithParents(step1))`, and a task reads the output of a parent with `ctx.ParentOutput(step1, &out)`. A worker runs the functions. This matches chronicle task workers (M7), not container templates.
- Dagster defines assets and ops as decorated functions, with a typed graph.
- What we do: the SDKs build the chronicle Workflow object (the CRD types) and validate it with the webhook rules before they render or submit it. A task handle gives the dependency and the typed placeholders (`Result`, `Output`, `Artifact`), so a user never writes `{{tasks...}}` by hand. Task workers (M7) add the Hatchet style of functions as tasks.
- Go: a builder in the main module (`sdk/go/chronicle`) that uses `api/v1alpha1` and `internal/validate` directly. Python and Rust: generated types from the CRD schema, with the same builder shape.
