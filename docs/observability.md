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

The server and the controller export OpenTelemetry traces when `OTEL_EXPORTER_OTLP_ENDPOINT` is set (OTLP over gRPC). The other standard `OTEL_*` variables apply, for example `OTEL_TRACES_SAMPLER`. Without the variable, they make no spans. Set the variables with the Helm values `server.env` and `manager.env`.

- The server makes a span for each API call.
- The controller makes one trace for each run. The trace ID is the UID of the run, without the dashes. When the run finishes, the controller writes a span for the run and a span for each node that ran, with the times from the status. A node span is a child of its parent node. A failed run or node has the status Error with its message. The attributes start with `chronicle.`, for example `chronicle.template`, `chronicle.phase` and `chronicle.node.pod`.
- Each Pod gets the variable `TRACEPARENT` with the span of its node. An OpenTelemetry SDK that reads the variable makes the spans of the step children of the node span.
- A retry makes a new run span in the same trace (`chronicle.try`). It has only the nodes that ran in that try.

Each task has the same trace context for its worker (see [task workers](task-workers.md#traces)).
