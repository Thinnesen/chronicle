# chronicle design

This document is the design for chronicle. Read it before you change the API, the engine or the server.
It uses ASD-STE100 Simplified Technical English.

## 1. Purpose

chronicle is a workflow engine for Kubernetes. It runs DAGs, step lists and cron workflows as Pods.
It keeps the history of each run, so that a user can retry, re-run or resume it.

Goals:

- Kubernetes-native and GitOps-first. All definitions are custom resources. Argo CD or Flux can deploy them ("app of apps").
- A good web UI and a good login. The UI uses the same permissions as `kubectl` (Kubernetes RBAC).
- Workflows as code. SDKs for Go, Python and Rust define and submit workflows, like Dagster or Temporal.
- Almost a drop-in replacement for Argo Workflows: a converter for Argo YAML, and the same core concepts.
- Durable task workers (Temporal-like activities): a step can call code in a worker process, with retries and heartbeats.

Not goals (for now): a full Temporal clone with deterministic replay of workflow code. See section 10.

## 2. Components

| Component | Language | Job |
|---|---|---|
| `chronicle-controller` | Go (Kubebuilder) | Reconciles the custom resources. Runs the workflow engine. Creates and watches Pods. |
| `chronicle-server` | Go | API (Connect-RPC: gRPC and HTTP/JSON), OIDC login, web UI (embedded), log streaming, archive. |
| `chronicle` CLI | Go | Submit, list, watch, logs, retry, re-run, suspend, resume, approve, convert. |
| Web UI | TypeScript, React, Vite | Runs, DAG graph, logs, history, templates, cron, actions. pnpm, lockfile committed. |
| SDKs | Go, Python, Rust | Define workflows as code with builders. Submit them through the API. Run task workers. |
| Archive | PostgreSQL (optional) | Completed runs and node history. Without a database, chronicle keeps runs in Kubernetes only. |

The controller and the server are two binaries in one Go module. The Helm chart deploys both.

## 3. API (group `chronicle.thinnesen.com`, version `v1alpha1`)

### 3.1 Kinds

| Kind | Scope | Job |
|---|---|---|
| `WorkflowTemplate` | Namespaced | A reusable workflow definition. |
| `ClusterWorkflowTemplate` | Cluster | A reusable definition for all namespaces. |
| `Workflow` | Namespaced | One run. It has a spec (inline or `workflowTemplateRef`) and a status with all nodes. |
| `CronWorkflow` | Namespaced | Creates Workflows on a schedule. |

### 3.2 Workflow spec

The spec follows the Argo Workflows concepts, so that the converter is simple:

- `entrypoint`, `arguments.parameters`, `templates`.
- Template types: `container`, `script`, `dag` (tasks with `dependencies` and `depends` expressions), `steps` (lists of parallel steps), `suspend` (wait or manual approval), `resource` (create a Kubernetes object), `task` (call a task worker, section 7).
- `retryStrategy` (limit, backoff, retry policy), `activeDeadlineSeconds`, `timeout`, `onExit`, `when` conditions, `withItems` and `withParam` fan-out, `parallelism` (of the workflow, and of a `dag` or `steps` template), `synchronization` (a mutex or a semaphore for runs in a namespace).
- Inputs and outputs: parameters (from a file, from stdout, from a JSON path) and artifacts (S3-compatible store).
- `podGC`, `ttlStrategy`, `serviceAccountName`. There is no `podSpecPatch`: a patch of the Pod spec could undo the Pod security settings.

- The container of a `container` or `script` template is a small chronicle type, not the full Kubernetes container. It has no security context, ports or probes. The controller sets a restricted security context.
- A `script` source cannot have `{{...}}` placeholders. The script reads its parameters from environment variables.

Validation: CEL rules and a validating webhook for the DAG (no cycles, known templates, known parameters). The CRD schema checks names, unique names and "exactly one template type". The webhook (package `internal/validate`) checks templates, arguments, placeholders and cycles. It skips an update that does not change the spec, so that new rules never block a finalizer change.

### 3.3 Workflow status

- `phase`: Pending, Running, Succeeded, Failed, Error, Suspended.
- `nodes`: a map of node ID to node status (type, phase, start, end, message, outputs, retries, pod name).
- `conditions`, `startedAt`, `finishedAt`, `estimatedDuration`.
- Large status: when the node map is larger than a limit, the controller keeps a compact status and the full node map in the archive.

### 3.3.1 Template references

- `workflowTemplateRef` runs a WorkflowTemplate (same namespace) or a ClusterWorkflowTemplate. The values that the Workflow sets go on top: entrypoint, arguments (by name), service account, exit handler, deadline, Pod GC and TTL.
- `templateRef` in a DAG task or a step calls one template of a WorkflowTemplate or a ClusterWorkflowTemplate. The controller copies it, and the templates that it uses, into the spec with the name `<set>--<template>` (`cluster--<set>--<template>` for a cluster template).
- The controller resolves the references once and keeps the result in `status.storedSpec`. A later template change does not change a run, and the history keeps the exact definition.
- A missing template or set ends the run with Error. An API error is retried.

### 3.4 CronWorkflow

`schedules` (one or more cron expressions), `timezone`, `concurrencyPolicy` (Allow, Forbid, Replace), `startingDeadlineSeconds`, `successfulJobsHistoryLimit`, `failedJobsHistoryLimit`, `suspend`, `workflowMetadata`, `workflowSpec` (inline or `workflowTemplateRef`).

- The controller reconciles and requeues at the next schedule time, like the Kubernetes CronJob. Argo runs an in-memory cron scheduler. A run has the name `<cron>-<unix time>`, so a second create of the same run fails and a restart loses no run.
- After a long outage, only the latest missed time runs. The search for missed times stops after 1000 times.
- Replace sets `shutdown: Terminate` on the active runs, so their history stays.
- The pure package `internal/schedule` decides; the controller applies the plan.

## 4. Engine (package `internal/engine`, pure Go)

The engine is pure: it has no I/O and no clock. It takes the spec, the status and the observed Pods, and it returns actions (create Pod, delete Pod, update node, finish).
The controller applies the actions. This makes the engine testable with table tests and fuzzing.

- DAG: topological order, `depends` expressions (`A.Succeeded || B.Failed`), fan-out, `when`.
- Steps: sequential groups of parallel steps.
- Retries with backoff. Timeouts. Exit handlers. Suspend and resume. Manual approval.
- Parameter substitution: `{{inputs.parameters.x}}`, `{{tasks.a.outputs.result}}`, `{{workflow.name}}`.
- Determinism: the same input gives the same actions.
- One walker for DAG and steps. A step depends on all steps of the previous group. Each run of the engine visits the tasks once in dependency order, so an omission cascades in one pass.
- A DAG ends when no task runs. Then each leaf task decides: its own phase when it ran, or the worst phase of its dependencies when it was omitted or skipped. A dependency failure that the task tests for (for example `a.Failed` or `!a.Failed`) does not pass down.
- `failFast` (default true) stops new tasks after a failure that no task tests for. The tasks that did not start become Omitted.
- `when`: the engine parses the condition first, then puts each placeholder value in as one literal operand. A value can never change the condition (Argo puts values into the text before it parses). Operators: `==`, `!=`, `<`, `<=`, `>`, `>=`, `&&`, `||`, `!`, parentheses. Two numbers compare as numbers, other values as text. A false condition makes the node Skipped.
- Fan-out: `withItems` or `withParam` (a JSON list) makes a TaskGroup node with one child for each item (`a(0:x)`). `AnySucceeded` and `AllFailed` test the items. The outputs of a group are JSON lists of the item outputs.
- Each node stores its resolved input parameters, for the UI and for retries. The Pod gets the template with all placeholders replaced.
- Node IDs: the root node has the workflow name. Other nodes have the workflow name and a hash of the node path (`wf.main.a`, `wf[0].step`). The Pod name is the node ID.

- Retries: `retryStrategy` makes a Retry node with one child for each attempt (`a(0)`, `a(1)`). `limit` (required) counts the retries after the first attempt. `retryPolicy`: OnFailure (default), OnError, Always. The wait is `duration * factor^(retry-1)`, at most `maxDuration`.
- Timers: the engine has no clock. It returns the time until the next timer (backoff, `timeout`, `activeDeadlineSeconds`), and the controller runs it again then.
- `timeout` fails a node of the template (each attempt of a retry). `activeDeadlineSeconds` fails the workflow. Both stop the running descendants and delete their Pods.
- `parallelism` limits the Pods that run at the same time. A Pod node without a free slot stays Pending with a message. The slots go to the waiting nodes in the order of the walk, and a Pod that the engine asked for still holds its slot before the cache shows it.
- `continueOn` (`failed`, `error`) on a task or step: its failure counts as success for the tasks after it and does not fail the boundary, as in Argo.
- `onExit` runs a template after the entrypoint, also after a failure. It can use `{{workflow.status}}`, `{{workflow.duration}}` and `{{workflow.failures}}` (the failed steps as JSON, in the form of Argo). A failed exit handler fails a workflow that succeeded.
- The exit handler is protected: `activeDeadlineSeconds` and `shutdown: Stop` stop only the entrypoint, so that cleanup can run. Give the exit template a `timeout`. `shutdown: Terminate` always stops the exit handler too.
- `withParam` has a limit of 1000 items, like `withItems`.

- Run controls are fields of the Workflow spec, so `kubectl`, GitOps, the CLI and the UI use one mechanism. `suspend: true` pauses the run (no new node starts). `shutdown: Stop` fails the running nodes, deletes their Pods and runs the exit handler. `shutdown: Terminate` does not run the exit handler. A shutdown cannot be removed. Only Stop can change to Terminate.
- A `suspend` template waits for an entry in `spec.approvals` (node ID or node name, Approve or Reject, optional output parameters, optional `user`) or for its `duration`. The API sets `user` to the caller; the admission webhook refuses a new approval whose `user` is not the user of the request, so the node message "approved by <user>" is always true. The server also writes the approver to the audit log (#17).

### 4.1 Workflow Pods and outputs

- The controller creates one Pod for each container or script node. The Pod name is the node ID. The Workflow owns the Pod.
- Pods meet the "restricted" Pod security standard: user 65532, no privilege escalation, no capabilities, seccomp `RuntimeDefault`. A Pod gets a service account token only when the workflow names a service account.
- An init container copies `chronicle-exec` (in the controller image) into an `emptyDir`. The executor wraps the main command, keeps the standard output, reads the output files and writes the outputs as JSON to the termination message. So the workflow Pod needs no Kubernetes API access.
- The termination message has a limit of 4 KiB. A larger standard output is not a result. Larger output parameters fail the node: use an artifact.
- A container template without a command runs without the executor. It has no result and no output files.
- A `resource` template runs `chronicle-exec resource` in a Pod with the workflow service account. So it has only the permissions of that service account, never the permissions of the controller. The engine parses the manifest and substitutes placeholders in string values only. A parameter value can never add keys or objects (Argo substitutes text before it parses the YAML).
- `podGC` (OnPodCompletion, OnPodSuccess, OnWorkflowCompletion, OnWorkflowSuccess, with `deleteDelayDuration`) deletes Pods. Without it, Pods stay until the Workflow is deleted. `ttlStrategy` deletes the finished Workflow. Logs of deleted Pods are kept only when the log archive is on (#25).
- The controller caches only Pods with the label `chronicle.thinnesen.com/workflow-uid`.

### 4.2 Artifacts and the log archive

- Each namespace can have one S3-compatible artifact repository: the ConfigMap `chronicle-artifact-repository` (endpoint, bucket, region, insecure, keyPrefix, archiveLogs) and the Secret with the same name (accessKey, secretKey). A team owns the repository of its namespaces. Guide: `docs/artifacts.md`.
- A container or script template with a command declares `outputs.artifacts` (name, path, optional) and `inputs.artifacts` (name, path). A caller gives an artifact with `arguments.artifacts[].from`, which is exactly one placeholder such as `{{tasks.make.outputs.artifacts.data}}`. The webhook rejects an artifact placeholder in any other field.
- The value of an artifact is its key. The engine passes keys like parameters. A fan-out has no artifact outputs.
- The executor downloads the inputs before the main command and saves the outputs after it, as tar.gz archives of a file or a directory. It skips symbolic links and refuses archive entries outside the path. Keys: `<keyPrefix>/<namespace>/<workflow UID>/<Pod name>/<name>.tgz`. The Pod annotation `chronicle.thinnesen.com/artifact-prefix` keeps the prefix, so the termination message carries only the names.
- With `archiveLogs: "true"`, the executor also saves the log of the main command (up to 32 MiB) as `main.log`, and the node gets `logKey`. The log API sends the archived log when the Pod is gone.
- The controller reads only the ConfigMap. The Pod gets the credentials from the Secret through `secretKeyRef`, and the executor removes them from the environment of the main command. Workflow code in the namespace can still read them: the repository credentials belong to the namespace, as in Argo.
- The server reads the ConfigMap and the Secret with that one name (RBAC `resourceNames`), and only after an access check as the caller: `get` on the Workflow for a download (`GET /artifacts/{namespace}/{workflow}/{node}/{name}`), and `get pods/log` for an archived log.

## 5. Re-run, retry and history

- Retry: start the failed nodes again. Keep the succeeded nodes and their outputs.
- Retry from a node: start again from a selected node and all nodes after it (dependent DAG tasks and later step groups, in its boundary and in each boundary above it).
- A retry is a run control in the spec: `spec.restart: {generation, fromNode}`. The controller applies a new generation when the run has finished (pure function `engine.Restart`), deletes the old Pods and counts it in `status.restarts`. The boundaries above each reset node and the exit handler run again. New Pods have the name `<node ID>-r<restarts>`, so an old Pod that still terminates can never stand for a new node. A refused retry becomes the condition `Restarted=False`.
- Re-run (resubmit): a new Workflow with the same spec and parameters, optionally with changed parameters. It has the label `chronicle.thinnesen.com/rerun-of` and no run controls.
- Resume and suspend. Stop (graceful, runs exit handlers). Terminate (immediate).
- History: each run, each node attempt, its logs and its outputs stay in the archive. The UI and the API show them.
- Archive (optional, `--archive-dsn-file` on the controller and the server): the controller saves each finished run to PostgreSQL (summary columns and the full Workflow with its nodes, keyed by UID) and marks it with the condition `Archived`. A retried run is saved again. With an archive, the TTL deletes a run only after the archive has it.
- The server merges archived runs into the run list and falls back to the archive for get and re-run. The archive is outside the Kubernetes RBAC, so the server first checks with an access review as the caller (list or get Workflows in the namespace). The archive filters labels by equality only; other selectors show live runs only.
- Migrations are embedded SQL files with a version table and an advisory lock, without a migration library.

## 6. Server, login and permissions

- API: Connect-RPC from protobuf (`proto/`). gRPC for SDKs, HTTP/JSON for the UI and curl. `buf` generates the code (pinned) into `gen/`, and CI checks that it is current.
- API messages carry a Workflow as a JSON object (`google.protobuf.Struct`) and use typed summaries for lists. The CRD types stay the one definition of a workflow. The Go SDK uses the CRD types and the webhook validation. The Python and Rust SDKs build plain objects, and the server validates them on submit.
- Every API call needs an authenticated identity. The server calls Kubernetes with impersonation of that identity for all user actions, reads too. There is no unauthenticated mode and no fallback to the permissions of the server.
- The server can impersonate users and groups. So an identity provider must never give a user a Kubernetes system name: the claim mapping always adds a prefix (`oidc:`, `entra:`) to user and group names, and the server rejects names that start with `system:`.
- Login (UI): OIDC authorization code flow with PKCE (any OIDC provider: Dex, Keycloak, Okta, Google, GitHub through Dex). Session cookie: `HttpOnly`, `Secure`, `SameSite=Lax`, short life, server-side session store (in the archive database when there is one, so that all server replicas share the sessions and a restart keeps them; else in memory with one replica). CSRF protection for state-changing calls.
- Login (CLI and SDK): OIDC device flow, or a Kubernetes service account token (TokenReview).
- Permissions: the server checks each request with a Kubernetes SubjectAccessReview for the user and the user groups (from OIDC claims, with a configurable claim mapping). Writes use impersonation. So the RBAC for the UI is the same RBAC as for `kubectl` and GitOps.
- Teams: a team is a set of namespaces. A Helm value `teams` creates the RoleBindings for each team: an IdP group gets `chronicle-viewer`, `chronicle-editor` or `chronicle-admin` (ClusterRoles) in the team namespaces. The UI and the API show only the namespaces that the user can list (SelfSubjectRulesReview), so each team sees only its own work.
- GitOps safety: a resource that a GitOps tool manages (Argo CD or Flux labels and annotations) is read-only in the UI by default. Runs (Workflows) are always allowed.
- Audit log: each state-changing call writes a structured audit entry (logger `audit`: action, user, groups, namespace, name, detail, result) and a Kubernetes Event on the Workflow (`kubectl describe` shows who did what). A Connect interceptor audits every RPC that the protobuf API does not mark as free of side effects, so a new RPC cannot skip the audit.

### 6.1 Microsoft 365 (Entra ID) login

Microsoft Entra ID is a first-class identity provider, because many companies use Microsoft 365.

- Protocol: OIDC authorization code flow with PKCE against `https://login.microsoftonline.com/<tenant-id>/v2.0`. Only the configured tenant is accepted (no `common` endpoint). The server checks `iss`, `aud`, `tid`, `exp` and `nonce`.
- Groups: the server reads the `groups` claim (Entra security group object IDs) or app roles (`roles` claim). A Helm value maps object IDs to readable team names.
- Group overage: when a user has more than 200 groups, Entra sends `_claim_names` instead of `groups`. Then the server reads the groups from Microsoft Graph (`/me/getMemberObjects`, permission `GroupMember.Read.All` or `User.Read` with `securityEnabledOnly`), with a short cache.
- Users map to the Kubernetes user `entra:<oid>` and to the groups `entra:group:<object-id>` (prefixes are configurable). RBAC and the `teams` value use these names.
- The admin guide (`docs/entra-id.md`) shows the app registration, the token configuration (groups claim), the redirect URIs and a team example step by step.
- Tests: an e2e test uses a mock OIDC server that sends Entra-shaped tokens (with `tid`, `oid`, `groups` and the overage claim) and a mock Graph endpoint. A live test against a real tenant is owner work.

### 6.2 Threat model (summary)

Security is the first priority. The main risks and the controls:

| Risk | Control |
|---|---|
| A user runs work in a namespace of another team | SubjectAccessReview and impersonation for each call. No server-side privilege for user actions. |
| A workflow escapes its Pod | Pod security standard "restricted" by default, no host mounts, no `podSpecPatch`, per-namespace service accounts. |
| Session theft or CSRF | Server-side sessions, `HttpOnly`, `Secure`, `SameSite=Lax` cookies, CSRF tokens, short session life, logout revokes the session. |
| Token leaks | Tokens never go to the browser or to logs. Secrets come from Kubernetes Secrets. |
| Supply chain | Pinned versions and lockfiles, SHA-pinned Actions, digest-pinned images, signed releases, SBOM, scans in CI. |
| A user reads artifacts of another team | One repository per namespace. Downloads and archived logs need an access check as the caller. The server can read only the Secret named `chronicle-artifact-repository`. |
| Injection through parameters | Parameters are never passed to a shell by the engine. Script templates get parameters as environment variables or files, not by string concatenation. |

## 7. Durable task workers

A `task` template sends a task to a queue. A worker that an SDK runs takes the task, runs a function and returns the result.

- Transport: the server API (gRPC streaming). Workers poll with long polls.
- At-least-once with task IDs for idempotency. Heartbeats. Timeouts (schedule-to-start, start-to-close, heartbeat). Retries with the template `retryStrategy`.
- State: the task queue is in the archive database. Without a database, `task` templates are not available.
- The SDKs give a small API: `@task` (Python), `task!` or a trait (Rust), `RegisterTask` (Go).
- A `task` template has `name` (the task type), `queue` (default "default"), `heartbeatTimeout` and `scheduleToStartTimeout`. The template `timeout` is the start-to-close timeout.
- The worker gets the input parameters as a JSON object and returns JSON. The result becomes `outputs.result`. The fields of a result object become the output parameters with the same names. A result can have at most 64 KiB.
- The engine stays pure. A task node has a `taskID` (`<workflow UID>/<node ID>`, with the retry suffix of the workflow). The engine returns `EnqueueTask` (idempotent) and `CancelTask` actions, and it reads the observed tasks like Pods. It decides the timeouts from `now`, and it asks to run again every 2 s while a task waits or runs.
- Delivery is at-least-once. A timeout or a stop fails the attempt and cancels its task: a late result is refused. A retry comes only from `retryStrategy`: each attempt is a new task. All attempts of a node have the same idempotency key (`<workflow UID>/<node name without the attempt>`), so that a worker can detect a repeated call.

## 8. Web UI

- Runs list (filter, search, phase, labels), run details with a live DAG graph (React Flow with an automatic layout), node details, logs (live and archived), parameters and outputs, history of attempts.
- Actions: submit from a template, retry, retry from node, re-run with changed parameters, suspend, resume, approve, stop, terminate.
- Templates, cron workflows (next runs, history), task queues and workers.
- Dark and light themes. Keyboard shortcuts. Accessible (WCAG AA).
- Built with Vite, embedded in the server binary with `go:embed`. Playwright tests.

### 8.1 Visual direction

The UI is an ops console. It combines the strong parts of the Argo Workflows, Argo CD, Temporal and Dagster UIs (see docs/RESEARCH.md) and leaves out their extra controls.

- Frame: a sidebar with the namespace, the sections (Runs, Templates, Cron, Tasks) and the theme switch. A top bar with breadcrumbs, the search (Cmd+K or Ctrl+K) and the user menu. On a phone the sidebar is a drawer.
- Palette (light / dark): page `#F4F5F7` / `#11161D`, surface `#FFFFFF` / `#171D26`, ink `#111827` / `#E6EAF0`, accent `#2F5BD3` / `#7C9CF5`. Status colors: succeeded green, failed red, running blue, suspended amber, pending gray. Each status also has a shape (check, cross, spinner, pause, dashed ring), so a phase never depends on color alone (WCAG AA). Text on a soft status background uses a darker ink of the same hue.
- Type: Overpass for the UI. JetBrains Mono for run names, logs, YAML and IDs that people copy. Fonts are self-hosted (pnpm packages), never from a CDN.
- Runs list: the totals and a 24 h activity strip, status chips with counts, a search field, and a dense table (status, run, template, progress, start, duration). The filters are in the URL. A run that waits for an approval has a "Needs approval" badge, because its phase stays Running. A notice above the list names all such runs of the namespace, also old ones. A run with a step that Kubernetes cannot start shows the reason as a badge, for example ImagePullBackOff.
- Run page: a header with the status, the facts and the actions, then the tabs Graph, Timeline and Spec. The selected step opens a drawer on the right with the tabs Summary, Logs and Attempts. The user can drag the drawer edge. The tab and the selected node are in the URL.
- Graph: cards of 192 x 44 px for steps that do work, small pills for groups. The layout uses the drawn size of each shape, so the edges touch the shapes. The root node is not drawn, because the header shows the run. On a desktop the graph never shrinks below 80 % to fit, so the names stay readable; on a phone the width of a fan-out must fit. A larger graph starts at the selected step or at the start of the run, and the user pans.
- Timeline: one row for each step. A bar is an attempt, and a dashed line is the wait between two attempts.
- While a run is active, the graph also draws the steps that have not started (dashed). A fan-out with more than 8 items shows 5 and one "N more" card; failed and running items stay visible.
- The step drawer has the tabs Summary, Logs, Events (the Kubernetes events of the Pod) and Attempts. A followed log connects again after a broken stream.
- A template page shows the graph of the template before it runs. The template and cron lists show the last ten runs of each definition as health marks.
- Keyboard: Cmd+K or Ctrl+K opens the search, Enter or Space opens a focused step, Escape closes the drawer. Playwright checks the pages with the WCAG 2 A and AA rules of axe.
- Motion only shows a live state (a running spinner, halo or bar) or answers an action.
- Copy is short, plain and in sentence case. Empty screens tell the next action.

## 9. Security and supply chain

The project follows these standards:

- Pin exact versions (Go, pnpm, Cargo, uv). Commit the lockfiles. Pin images by digest, GitHub Actions by SHA, tools in the Makefile with SHA-256.
- pnpm: `minimumReleaseAge`, no install scripts unless allowed by name.
- Non-root Pods, no privilege escalation, no capabilities, seccomp `RuntimeDefault`. The chronicle containers (controller, server, executor init and resource steps) have a read-only root file system. The container of a user step keeps a writable root file system, because many images write to it.
- Least-privilege RBAC. The server never stores OIDC tokens in the browser. Secrets come from Kubernetes Secrets.
- The UI has a strict Content Security Policy: scripts, styles, fonts and images come only from the server. No inline script. An injected style (for example the scroll lock of a dialog) needs the nonce that the server puts into each page. Playwright fails a test when the browser reports a violation.
- CI: lint, unit tests, envtest, e2e on Kind (with Dex for OIDC), Playwright, govulncheck, `pnpm audit`, `cargo audit`, Trivy, actionlint.

## 10. Later (not in the first version)

- Durable workflow code with deterministic replay (Temporal-style workflows, not only activities).
- Event triggers (webhooks, NATS, Kafka, S3 events).
- UI edits of GitOps resources as pull requests.
- Multi-cluster runs.
