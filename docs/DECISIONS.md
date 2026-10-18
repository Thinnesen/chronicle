# Decisions

One entry for each design change. Newest first.

- 2026-10-18: The API carries Workflows as JSON objects (`google.protobuf.Struct`), not as protobuf copies of the CRD types. SDK workflow types come from the CRD schema. Reason: one definition of a workflow (the Go types and the CRD). Section 2 said "SDKs generated from the protobuf API"; now only the API clients are.
- 2026-10-18: The server impersonates the caller for every Kubernetes call, reads included, and has no unauthenticated mode. Reason: the threat model (no server-side privilege for user actions). The cost is one API client per request.

- 2026-10-18: Resource templates run in a Pod with the workflow service account, not in the controller and not with impersonation. Reason: the controller then needs no `impersonate` permission, and a step can do only what its service account can do. Manifests are parsed before substitution, so parameters cannot inject YAML.

- 2026-10-18: Suspend, stop, terminate and approvals are fields of the Workflow spec (`suspend`, `shutdown`, `approvals`). Reason: one mechanism for kubectl, GitOps, the CLI and the UI, with the same RBAC (`update` on Workflows). Argo uses spec fields for suspend and shutdown, but node changes through its own API for resume and approval.

- 2026-10-18: Workflow Pods report outputs through the termination message of the main container, written by `chronicle-exec`. Reason: Argo needs a `WorkflowTaskResult` resource and API access for each Pod. Without API access, a compromised step cannot change any Kubernetes object. The cost is a 4 KiB limit; artifacts carry larger data. Kubernetes image volumes could replace the init container later, when all supported runtimes have them.

- 2026-10-18: `when` conditions are parsed before substitution, and values are literal operands. Reason: Argo substitutes values into the expression text, so a parameter value such as `x || true` can change the condition (injection). The Argo converter keeps simple conditions as they are.

- 2026-10-18: DAG outcome follows Argo v4 (leaf tasks and their branches decide), with one change: a failure that a task tests for with its exact result (`a.Failed`, `a.Errored`) is handled. It does not fail the DAG and does not stop it with `failFast`. Reason: in Argo, an omitted leaf such as `depends: "!a.Failed && b"` fails the DAG after a handled failure, which surprises users.

- 2026-10-18: The template container is a small chronicle type without a security context, and a script source cannot have placeholders. Reason: a user who can create a Workflow must not get a privileged Pod, and parameters must never become shell code (threat model, section 6.2). Argo YAML that uses placeholders in a script source must move them to environment variables. The converter will do this.

- 2026-10-18: Microsoft Entra ID (Microsoft 365) is a first-class identity provider, with group overage through Microsoft Graph and team-level access through a Helm value `teams`. Reason: the owner wants an easy M365 login and team-level visibility for platform admins.
- 2026-10-18: Pure engine package (no I/O, no clock), applied by the controller. Reason: deterministic, table-testable and fuzzable workflow logic.
- 2026-10-18: Permissions through Kubernetes SubjectAccessReview and impersonation. Reason: one RBAC model for the UI, the CLI, `kubectl` and GitOps. Argo Workflows needs a separate SSO RBAC mapping, which users find hard.
- 2026-10-18: Connect-RPC (gRPC and HTTP/JSON from one protobuf definition). Reason: SDKs for Go, Python and Rust come from one source, and the UI can use plain HTTP/JSON.
- 2026-10-18: Own API group (`chronicle.thinnesen.com`), not the Argo group, with a converter for Argo YAML. Reason: chronicle and Argo Workflows can run in the same cluster during a migration.
- 2026-10-18: Durable task workers (activities) first, durable workflow code later. Reason: deterministic replay of user code is a large, separate problem. Step-level retry and re-run cover most needs.
