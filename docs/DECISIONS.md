# Decisions

One entry for each design change. Newest first.

- 2026-10-18: Microsoft Entra ID (Microsoft 365) is a first-class identity provider, with group overage through Microsoft Graph and team-level access through a Helm value `teams`. Reason: the owner wants an easy M365 login and team-level visibility for platform admins.
- 2026-10-18: Pure engine package (no I/O, no clock), applied by the controller. Reason: deterministic, table-testable and fuzzable workflow logic.
- 2026-10-18: Permissions through Kubernetes SubjectAccessReview and impersonation. Reason: one RBAC model for the UI, the CLI, `kubectl` and GitOps. Argo Workflows needs a separate SSO RBAC mapping, which users find hard.
- 2026-10-18: Connect-RPC (gRPC and HTTP/JSON from one protobuf definition). Reason: SDKs for Go, Python and Rust come from one source, and the UI can use plain HTTP/JSON.
- 2026-10-18: Own API group (`chronicle.thinnesen.com`), not the Argo group, with a converter for Argo YAML. Reason: chronicle and Argo Workflows can run in the same cluster during a migration.
- 2026-10-18: Durable task workers (activities) first, durable workflow code later. Reason: deterministic replay of user code is a large, separate problem. Step-level retry and re-run cover most needs.
