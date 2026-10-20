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

## UI overhaul: the ops console (Argo WF, Argo CD, Temporal, Dagster, Hatchet)

We read the UI source of each project (`ui/src/app` of Argo Workflows and Argo CD, `src/lib` of the Temporal UI, `js_modules/ui-core` of Dagster, `frontend/app` of Hatchet).

| Project | What it does well | Where it falls short |
|---|---|---|
| Argo Workflows | The selected node, the tab and the side panel are in the URL. A resizable node panel has the tabs summary, containers, inputs and outputs, events and logs. The graph has a node search, phase and type filters, a horizontal or vertical layout, and folds more than 3 siblings into "N hidden nodes". | Round 32 px icons with the name below: long names are cut and the graph is hard to read. The timeline is a separate, basic tab. The options panel is crowded. The runs list has few filters. |
| Argo CD | A filter sidebar with counts for each facet (health, sync, labels, namespaces). Wide tree cards (282 x 52 px) with an icon, the kind, the name and two status icons. It groups similar nodes. | Large trees become noise. The tiles view wastes space. Many icons have no labels. |
| Temporal | Header tabs (Timeline, Event History, Relationships). A virtualized timeline in "lines and dots": one row for each activity, a dashed line while it waits, a solid line while it runs, and the attempt count ("attempt 2 / 5") in the row. A filter bar with saved views. A configurable runs table. Pending activities are visible at the top. | The event history is low level for a person who wants to know which step broke. There is no graph of the steps. |
| Dagster | A run page split in two: a waterfall Gantt chart on top and the structured logs below, with a draggable split. A status panel lists the steps by state (Preparing, Executing, Errored, Succeeded). Step boxes are 200 x 34 px with a status dot. A click on a step filters the logs. | Many controls (zoom slider, mode switch, selection query) before the user sees the result. The chart needs a wide screen. |
| Hatchet | Compact node cards (30 px) with a status dot, the name and the duration on the right. Running nodes have an animated border. A command palette (`cmdk`). | Few details on the card. The run page has no timeline that shows the waiting time. |

What chronicle takes, and how it stays simple:

- Frame: a left sidebar with the namespace and the sections (Argo CD, Dagster, Temporal), a top bar with breadcrumbs, and a command palette on Cmd+K (Hatchet, Dagster).
- Runs list: one filter row with status chips that show counts (the Argo CD facets, but inline), a search field, and a 24 h activity strip above a dense table. No saved views and no configurable columns: the filters are in the URL, so a link is a saved view.
- Run page: a header with the status, the facts and the actions (Temporal), then the tabs Graph, Timeline and Spec. The selected node opens a resizable drawer on the right with the tabs Summary, Logs, Inputs and outputs, and Events (Argo WF). The node and the tab are in the URL.
- Graph: wide cards of 192 x 44 px with a status icon, the name and the duration (Argo CD, Dagster, Hatchet), not round icons. Steps and DAG boundaries are small pills that fit their drawn shape, so the edges touch the shapes. A running card has a pulsing halo (Hatchet uses a moving border). An edge is colored after its source succeeded.
- Timeline: one row for each pod node in the Temporal "lines and dots" style. A dashed line shows the wait before the start, a solid bar shows the run, and the row shows the attempt number. It uses the same selection as the graph, so a click selects the node and opens the drawer (Dagster).
- Logs: in the node drawer, so the user does not lose the graph (Argo WF, Dagster).
- We do not copy: zoom sliders, selection query languages, layout options panels, tiles views and event-level history.

## SDKs: workflows as code (Hera, Hatchet, Dagster)

- Hera (the Python SDK of Argo Workflows) builds Argo YAML with context managers (`with DAG(...)`), decorators for script templates, and `a >> b` for dependencies. The result is a normal Workflow object, so GitOps and kubectl keep working.
- Hatchet (Go, Python, TypeScript) registers functions as tasks: `workflow.NewTask(name, fn, WithParents(step1))`, and a task reads the output of a parent with `ctx.ParentOutput(step1, &out)`. A worker runs the functions. This matches chronicle task workers (M7), not container templates.
- Dagster defines assets and ops as decorated functions, with a typed graph.
- What we do: the SDKs build the chronicle Workflow object (the CRD types) and validate it with the webhook rules before they render or submit it. A task handle gives the dependency and the typed placeholders (`Result`, `Output`, `Artifact`), so a user never writes `{{tasks...}}` by hand. Task workers (M7) add the Hatchet style of functions as tasks.
- Go: a builder in the main module (`sdk/go/chronicle`) that uses `api/v1alpha1` and `internal/validate` directly. Python and Rust: the same builder shape, which makes plain objects. A shared fixture (`sdk/testdata/etl.json`) keeps the output of all SDKs equal.

## Who approved (Argo Workflows, Kubernetes CSR)

- Argo Workflows: `argo resume` and `argo node set` record no approver in the Workflow. Only the server log and the Kubernetes events (about one hour) have it.
- Kubernetes CertificateSigningRequest: the API server fills `spec.username` from the requester, so the requester cannot be faked.
- Use: an optional `user` on each approval, set by the chronicle API and checked by the admission webhook against the user of the request.
