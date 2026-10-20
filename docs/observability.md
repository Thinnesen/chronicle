# Observability

## Metrics

The controller serves Prometheus metrics on its metrics endpoint (port 8443, HTTPS with Kubernetes authentication). The Helm chart can add a ServiceMonitor (`prometheus.enabled: true`). Besides the controller-runtime metrics, chronicle has:

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `chronicle_workflows_finished_total` | counter | `workflow_namespace`, `template`, `cron`, `phase` | Runs that finished. |
| `chronicle_workflow_duration_seconds` | histogram | `workflow_namespace`, `template`, `cron`, `phase` | Time from the start to the end of a run. |
| `chronicle_pods_created_total` | counter | | Workflow Pods that the controller created. |
| `chronicle_tasks_enqueued_total` | counter | `queue` | Tasks that the controller put into the task queue. |
| `chronicle_workflow_retries_total` | counter | | Retries of finished runs. |
| `chronicle_task_queue_pending` | gauge | `workflow_namespace`, `queue` | Pending tasks of a task queue (with the archive). |
| `chronicle_task_queue_oldest_pending_seconds` | gauge | `workflow_namespace`, `queue` | Age of the oldest pending task of a task queue. |

## Dashboards

`grafana/` has Grafana dashboards: the controller-runtime metrics, the resources of the controller, and the chronicle metrics (`grafana/custom-metrics/`). Import the JSON files into Grafana. They are made with the Kubebuilder Grafana plugin from `grafana/custom-metrics/config.yaml`.

## Events

The controller records a Kubernetes Event when a run finishes: `Normal` for Succeeded, `Warning` for Failed and Error. The server records an Event for each change through the API (audit). See them with `kubectl events -n <namespace> --for workflow/<name>`.

## Traces

The server exports OpenTelemetry traces of the API calls when `OTEL_EXPORTER_OTLP_ENDPOINT` is set (OTLP over gRPC). The other standard `OTEL_*` variables apply, for example `OTEL_TRACES_SAMPLER`. Without the variable, the server makes no spans.
