# Run chronicle in production

This guide is for the platform team that installs and operates chronicle. The Helm chart is in `dist/chart`. The README has a quick start for a test cluster.

## 1. What you install

| Part | What it does | Replicas |
|---|---|---|
| Controller (`chronicle-controller-manager`) | Runs the engine, creates the Pods, applies the schedules, archives finished runs. It also serves the admission webhook. | 1 or more. Only the leader works (`--leader-elect`). |
| Server (`chronicle-server`) | Serves the API, the UI, the login and the log stream. It calls Kubernetes as the user (impersonation). | 1, or more with the archive (section 4). |
| CRDs | `Workflow`, `WorkflowTemplate`, `ClusterWorkflowTemplate`, `CronWorkflow`. | The chart keeps them on uninstall (`crd.keep`). |

The chart needs cert-manager for the webhook certificate (`certManager.enabled`).

## 2. Images

There is no published release yet. Build the image with `make docker-build IMG=<registry>/chronicle:<tag>` and push it to your registry. Then set:

```yaml
manager:
  image:
    repository: registry.example.com/chronicle
    tag: "<tag>@sha256:<digest>"   # pin the digest
```

The server and the executor of each workflow Pod use the same image. Workflow Pods mount it as an image volume, which needs containerd 2.1 or later, or CRI-O 1.31 or later. On older runtimes, set `manager.executorImageVolume: false`: then an init container copies the executor into each Pod.

## 3. Login and teams

- Without `auth.entra` or `auth.oidc`, the UI has no login. The API then accepts only Kubernetes service account tokens with the audience `chronicle` (CLI, SDKs and task workers).
- `auth.oidc` works with any OIDC provider. `auth.entra` is for Microsoft Entra ID with group overage (see [entra-id.md](entra-id.md)).
- Set `auth.redirectURL` to `https://<host>/auth/callback`. Keep `auth.insecureCookies: false`: the cookies then have the `Secure` flag and need HTTPS.
- `teams` binds the roles `chronicle-viewer`, `chronicle-editor` and `chronicle-admin` to groups in the team namespaces. You can also bind these ClusterRoles with your own RoleBindings, for example through GitOps.
- Sessions last `auth.session.ttl` (8h) and end after `auth.session.idleTimeout` (1h) without use. A logout ends a session at once.

A proxy or an ingress can serve chronicle under a path of a host, for example `https://example.com/chronicle`. Set `server.basePath: /chronicle`, and give `auth.redirectURL` the same path (`https://example.com/chronicle/auth/callback`). The ingress passes the full path; the server strips the base path itself. The CLI and the SDKs take the server URL with the path, for example `chronicle login --server https://example.com/chronicle`. The probes stay at `/healthz`.

## 4. The archive database

The archive is a PostgreSQL database. It keeps:

- finished runs after their Workflow is deleted (by `ttlStrategy`, by a CronWorkflow history limit, or by hand),
- the queue of the task workers (task templates need it),
- the login sessions of the UI, so that all server replicas share them and a restart keeps them,
- the results of templates with `memoize` (table `step_cache`). chronicle does not delete old results; delete them when you need to, for example `DELETE FROM step_cache WHERE saved_at < now() - interval '90 days'`.

Create a Secret with the DSN and set `archive.existingSecret`:

```sh
kubectl create secret generic chronicle-archive -n chronicle-system \
  --from-literal=dsn='postgres://chronicle:<password>@db.example.com:5432/chronicle?sslmode=verify-full'
```

The controller and the server apply the schema migrations at start. Back up the database like any other PostgreSQL database. With an archive, the TTL and the history limits delete a run only after the archive has it.

By default the archive keeps all rows. Set `archive.retention`, for example `2160h` (90 days), to delete older rows. Then the leader controller deletes, once an hour and in small batches, the runs that finished before that time with their earlier tries, the finished tasks, and the step cache entries. A run that is still in Kubernetes stays visible there. The archived logs in the artifact repository are not deleted: use a lifecycle rule of the bucket for them.

More than one server replica (`server.replicas`) needs the archive, because the login sessions are there. The chart refuses more replicas with a login and without an archive.

## 5. Artifacts and archived logs

Each namespace that uses artifacts has its own S3-compatible repository: a ConfigMap and a Secret named `chronicle-artifact-repository`. See [artifacts.md](artifacts.md). The team of the namespace owns the bucket and the credentials. The controller and the server read only these two objects.

## 6. Security settings

- All chronicle Pods run as non-root, without privilege escalation and capabilities, with seccomp `RuntimeDefault`. The controller, the server and the executor have a read-only root file system.
- Workflow Pods run with the service account of the run (`spec.serviceAccountName`). The service account token is mounted only when the run names one. Give each team a service account with only the permissions that its resource steps need.
- The server has no permissions of its own on runs: it impersonates the user for every call. Its ClusterRole allows `impersonate`, token reviews, and `get` on the artifact repository objects by name.
- Set `networkPolicy.enabled: true` to limit the traffic to the controller (metrics and webhook).
- The UI sets a strict Content Security Policy. TLS ends at your ingress or service mesh: put the server Service (`server.service`) behind it.

## 7. Monitoring

- The controller exports Prometheus metrics on port 8443 (HTTPS with authorization, `metrics.secure`). Set `prometheus.enabled: true` for a ServiceMonitor. Grafana dashboards are in the `grafana/` folder of the repository.
- The controller writes Kubernetes Events for finished runs.
- Set `OTEL_EXPORTER_OTLP_ENDPOINT` in `server.env` to send OpenTelemetry traces of the API calls.

See [observability.md](observability.md).

## 8. Upgrades

1. Read `docs/DECISIONS.md` for changes since your version.
2. `helm upgrade` with the new image. The chart updates the CRDs (they are templates, not the `crds/` folder of Helm).
3. The archive migrations run at the start of the new controller and server.

Runs continue during an upgrade. The engine reads all state from the Workflow status. So a new controller continues where the old one stopped. Task workers poll again when a server replica stops.

## 9. Sizing

The defaults fit small and medium clusters. Raise `manager.resources` for many active runs, because the controller keeps the Workflows and their Pods in its cache. The controller works on 8 runs at the same time; for many active runs, add `--workflow-workers=<n>` to `manager.args`. Use `podGC` and `ttlStrategy` in your templates, so that finished Pods and Workflows do not pile up.

Each step is a Pod. So a burst of many steps loads the kubelet and the container runtime of the nodes more than the controller. On a small node, 120 Pods at once took minutes each to start. Limit large fan-outs with `parallelism`. Limit many runs of one template with a `synchronization.semaphore`.

The status of a run holds all its nodes, about 0.5 KiB each, and the step results (up to 3 KiB each). etcd refuses objects above about 1.5 MiB. So the controller fails a run with "the run is too large" when its Workflow would be larger than 1 MiB. It does this before it starts more Pods. It deletes the Pods of the run that still run. A fan-out of 1000 items fits. 1000 items with retries, or nested fan-outs, can be too large. Split such work into more runs, for example one run for each day.
