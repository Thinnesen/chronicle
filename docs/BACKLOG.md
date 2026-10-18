# Backlog

Ordered. The GitHub issues are the source of truth. Take the first open item.
Close the issue when it is done (comment with the commits and the tests), then remove it here.
Each item ends with a green gate run and a commit on `dev`.


## M1 Engine core

5. #5 Controller: run container and script templates as Pods
6. #6 Retries, timeouts, deadlines and exit handlers
7. #7 Suspend, resume, manual approval, stop and terminate
8. #8 Resource template
9. #9 Pod GC and TTL

## M2 Scheduling and templates

10. #10 CronWorkflow
11. #11 Workflow template references and cluster templates

## M3 Server, login and CLI

12. #12 Protobuf API and Connect-RPC server
13. #13 OIDC login for the UI
14. #14 Permissions through Kubernetes RBAC
15. #42 Microsoft 365 (Entra ID) login and team-level access
16. #15 chronicle CLI
17. #16 Log streaming
18. #17 Audit log

## M4 Web UI

19. #18 UI foundation
20. #19 Runs list and run details with a live DAG graph
21. #20 Logs, node details and attempt history in the UI
22. #21 Actions in the UI
23. #22 Templates, cron and task queue pages

## M5 History and re-runs

24. #23 Archive in PostgreSQL
25. #24 Retry, retry from node and re-run with changed parameters
26. #25 Artifacts and log archive in an S3-compatible store

## M6 SDKs and Argo migration

27. #26 Go SDK
28. #27 Python SDK
29. #28 Rust SDK
30. #29 Argo Workflows converter and compatibility guide

## M7 Durable task workers

31. #30 Task queue and the task template
32. #31 Task workers in the Go, Python and Rust SDKs

## M8 Release readiness

33. #32 Helm chart and GitOps examples
34. #33 Observability
35. #34 E2E on Kind with Dex, Playwright and security scans in CI
36. #35 README with logo and demos
37. #36 Security review and fixes

## Later (do not start in this run)

- #37 Durable workflow code with deterministic replay
- #38 Event triggers
- #40 UI edits of GitOps resources as pull requests
- #41 Multi-cluster runs
