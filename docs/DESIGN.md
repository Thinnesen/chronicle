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
| SDKs | Go, Python, Rust | Define workflows as code. Submit them. Run task workers. Generated from the protobuf API. |
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
- `retryStrategy` (limit, backoff, retry policy), `activeDeadlineSeconds`, `timeout`, `onExit`, `when` conditions, `withItems` and `withParam` fan-out.
- Inputs and outputs: parameters (from a file, from stdout, from a JSON path) and artifacts (S3-compatible store).
- `podGC`, `ttlStrategy`, `serviceAccountName`, `podSpecPatch` (limited and validated).

- The container of a `container` or `script` template is a small chronicle type, not the full Kubernetes container. It has no security context, ports or probes. The controller sets a restricted security context.
- A `script` source cannot have `{{...}}` placeholders. The script reads its parameters from environment variables.

Validation: CEL rules and a validating webhook for the DAG (no cycles, known templates, known parameters). The CRD schema checks names, unique names and "exactly one template type". The webhook (package `internal/validate`) checks templates, arguments, placeholders and cycles. It skips an update that does not change the spec, so that new rules never block a finalizer change.

### 3.3 Workflow status

- `phase`: Pending, Running, Succeeded, Failed, Error, Suspended.
- `nodes`: a map of node ID to node status (type, phase, start, end, message, outputs, retries, pod name).
- `conditions`, `startedAt`, `finishedAt`, `estimatedDuration`.
- Large status: when the node map is larger than a limit, the controller keeps a compact status and the full node map in the archive.

### 3.4 CronWorkflow

`schedules` (one or more cron expressions), `timezone`, `concurrencyPolicy` (Allow, Forbid, Replace), `startingDeadlineSeconds`, `successfulJobsHistoryLimit`, `failedJobsHistoryLimit`, `suspend`, `workflowSpec` or `workflowTemplateRef`.

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
- `onExit` runs a template after the entrypoint, also after a failure. It can use `{{workflow.status}}`. A failed exit handler fails a workflow that succeeded.

- Run controls are fields of the Workflow spec, so `kubectl`, GitOps, the CLI and the UI use one mechanism. `suspend: true` pauses the run (no new node starts). `shutdown: Stop` fails the running nodes, deletes their Pods and runs the exit handler. `shutdown: Terminate` does not run the exit handler. A shutdown cannot be removed. Only Stop can change to Terminate.
- A `suspend` template waits for an entry in `spec.approvals` (node ID or node name, Approve or Reject, optional output parameters) or for its `duration`. The server writes the approver to the audit log (#17).

### 4.1 Workflow Pods and outputs

- The controller creates one Pod for each container or script node. The Pod name is the node ID. The Workflow owns the Pod.
- Pods meet the "restricted" Pod security standard: user 65532, no privilege escalation, no capabilities, seccomp `RuntimeDefault`. A Pod gets a service account token only when the workflow names a service account.
- An init container copies `chronicle-exec` (in the controller image) into an `emptyDir`. The executor wraps the main command, keeps the standard output, reads the output files and writes the outputs as JSON to the termination message. So the workflow Pod needs no Kubernetes API access.
- The termination message has a limit of 4 KiB. A larger standard output is not a result. Larger output parameters fail the node: use an artifact.
- A container template without a command runs without the executor. It has no result and no output files.
- The controller caches only Pods with the label `chronicle.thinnesen.com/workflow-uid`.

## 5. Re-run, retry and history

- Retry: start the failed nodes again. Keep the succeeded nodes and their outputs.
- Retry from a node: start again from a selected node and all nodes after it.
- Re-run (resubmit): a new Workflow with the same spec and parameters, optionally with changed parameters.
- Resume and suspend. Stop (graceful, runs exit handlers). Terminate (immediate).
- History: each run, each node attempt, its logs and its outputs stay in the archive. The UI and the API show them.

## 6. Server, login and permissions

- API: Connect-RPC from protobuf (`proto/`). gRPC for SDKs, HTTP/JSON for the UI and curl. `buf` generates the code (pinned).
- Login (UI): OIDC authorization code flow with PKCE (any OIDC provider: Dex, Keycloak, Okta, Google, GitHub through Dex). Session cookie: `HttpOnly`, `Secure`, `SameSite=Lax`, short life, server-side session store. CSRF protection for state-changing calls.
- Login (CLI and SDK): OIDC device flow, or a Kubernetes service account token (TokenReview).
- Permissions: the server checks each request with a Kubernetes SubjectAccessReview for the user and the user groups (from OIDC claims, with a configurable claim mapping). Writes use impersonation. So the RBAC for the UI is the same RBAC as for `kubectl` and GitOps.
- Teams: a team is a set of namespaces. A Helm value `teams` creates the RoleBindings for each team: an IdP group gets `chronicle-viewer`, `chronicle-editor` or `chronicle-admin` (ClusterRoles) in the team namespaces. The UI and the API show only the namespaces that the user can list (SelfSubjectRulesReview), so each team sees only its own work.
- GitOps safety: a resource that a GitOps tool manages (Argo CD or Flux labels and annotations) is read-only in the UI by default. Runs (Workflows) are always allowed.
- Audit log: each state-changing call writes a structured audit entry and a Kubernetes Event.

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
| A workflow escapes its Pod | Pod security standard "restricted" by default, no host mounts, `podSpecPatch` limited to an allow list, per-namespace service accounts. |
| Session theft or CSRF | Server-side sessions, `HttpOnly`, `Secure`, `SameSite=Lax` cookies, CSRF tokens, short session life, logout revokes the session. |
| Token leaks | Tokens never go to the browser or to logs. Secrets come from Kubernetes Secrets. |
| Supply chain | Pinned versions and lockfiles, SHA-pinned Actions, digest-pinned images, signed releases, SBOM, scans in CI. |
| Injection through parameters | Parameters are never passed to a shell by the engine. Script templates get parameters as environment variables or files, not by string concatenation. |

## 7. Durable task workers

A `task` template sends a task to a queue. A worker that an SDK runs takes the task, runs a function and returns the result.

- Transport: the server API (gRPC streaming). Workers poll with long polls.
- At-least-once with task IDs for idempotency. Heartbeats. Timeouts (schedule-to-start, start-to-close, heartbeat). Retries with the template `retryStrategy`.
- State: the task queue is in the archive database. Without a database, `task` templates are not available.
- The SDKs give a small API: `@task` (Python), `task!` or a trait (Rust), `RegisterTask` (Go).

## 8. Web UI

- Runs list (filter, search, phase, labels), run details with a live DAG graph (React Flow with an automatic layout), node details, logs (live and archived), parameters and outputs, history of attempts.
- Actions: submit from a template, retry, retry from node, re-run with changed parameters, suspend, resume, approve, stop, terminate.
- Templates, cron workflows (next runs, history), task queues and workers.
- Dark and light themes. Keyboard shortcuts. Accessible (WCAG AA).
- Built with Vite, embedded in the server binary with `go:embed`. Playwright tests.

## 9. Security and supply chain

The project follows these standards:

- Pin exact versions (Go, pnpm, Cargo, uv). Commit the lockfiles. Pin images by digest, GitHub Actions by SHA, tools in the Makefile with SHA-256.
- pnpm: `minimumReleaseAge`, no install scripts unless allowed by name.
- Non-root Pods, read-only root file system, no privilege escalation, no capabilities, seccomp `RuntimeDefault`.
- Least-privilege RBAC. The server never stores OIDC tokens in the browser. Secrets come from Kubernetes Secrets.
- CI: lint, unit tests, envtest, e2e on Kind (with Dex for OIDC), Playwright, govulncheck, `pnpm audit`, `cargo audit`, Trivy, actionlint.

## 10. Later (not in the first version)

- Durable workflow code with deterministic replay (Temporal-style workflows, not only activities).
- Event triggers (webhooks, NATS, Kafka, S3 events).
- UI edits of GitOps resources as pull requests.
- Multi-cluster runs.
