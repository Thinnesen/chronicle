# Migrate from Argo Workflows

chronicle uses the concepts and most field names of Argo Workflows. `chronicle convert` changes Argo YAML into chronicle resources. chronicle and Argo use different API groups, so both can run in one cluster during a migration.

## Convert a file

```sh
chronicle convert -f argo-workflow.yaml > workflow.yaml
chronicle convert -f argo-workflow.yaml | kubectl create -n team-a -f -
```

- The command converts `Workflow`, `WorkflowTemplate`, `ClusterWorkflowTemplate` and `CronWorkflow`. A file can have more than one document.
- The result goes to standard output. Each warning goes to standard error. A warning names a field that chronicle does not support: the result does not have that field. Read each warning, because the behavior can change.
- `--strict` makes the command fail when there is a warning. Use it in CI.
- The command validates the result with the rules of the admission webhook. It stops with an error when the result is not valid, for example when it uses a placeholder that chronicle does not have.

In a test with the 190 examples of Argo Workflows (October 2026), 43 converted without a warning, 81 converted with warnings, and 66 failed because they use features from the last table.

## What changes

| Argo | chronicle | Note |
|---|---|---|
| `apiVersion: argoproj.io/v1alpha1` | `chronicle.thinnesen.com/v1alpha1` | |
| `retryStrategy.limit: "3"` (text) | `limit: 3` | chronicle needs a number. |
| A retry without `limit` | `limit: 10` | An Argo retry without a limit never stops. chronicle needs a limit. The converter warns. |
| A duration without a unit, for example `"30"` | `30s` | Argo reads a bare number as seconds. |
| `retryPolicy: OnTransientError` | `OnError` | |
| `schedule: "0 * * * *"` | `schedules: ["0 * * * *"]` | Argo v4 also uses `schedules`. |
| A template `activeDeadlineSeconds` | `timeout` | |
| `withSequence` with numbers | `withItems` | `format` and placeholders in `withSequence` are not supported. |
| Placeholders in a shell script source | Environment variables | chronicle never puts a value into code. `{{inputs.parameters.x}}` becomes `${CHRONICLE_INPUTS_PARAMETERS_X}` and an `env` entry. A script in another language with placeholders fails: read the values from `env` yourself. |
| A placeholder as a whole YAML value in a resource manifest | The same placeholder in quotes | chronicle parses the manifest before it puts the values in. So a value can never add YAML. |
| An argument that the template does not declare | Dropped | Argo ignores it. chronicle rejects it. |
| `resource.action: replace` | `apply` | |
| Suspend output `valueFrom.supplied` with `valueFrom.default` | `default` | The value comes from the approval. |
| Artifact locations (`s3`, `gcs`, `git`, `http`, `raw` and others) | Dropped | chronicle keeps all artifacts in the repository of the namespace. See [artifacts.md](artifacts.md). |

## Supported as in Argo

DAG (`dependencies`, `depends` with `.Succeeded`, `.Failed`, `.Errored`, `.Skipped`, `.Omitted`, `.AnySucceeded`, `.AllFailed`), steps, `when`, `withItems`, `withParam`, `failFast`, container and script templates, `suspend` (manual approval and `duration`), `resource` templates, `retryStrategy` with `backoff`, `timeout`, `activeDeadlineSeconds`, `onExit`, `templateRef`, `workflowTemplateRef`, input and output parameters (`valueFrom.path`, `valueFrom.jsonPath`), artifacts between steps, `podGC`, `ttlStrategy`, `serviceAccountName`, and CronWorkflow (`timezone`, `concurrencyPolicy`, `startingDeadlineSeconds`, history limits, `suspend`).

Placeholders: `workflow.name`, `workflow.namespace`, `workflow.uid`, `workflow.status`, `workflow.creationTimestamp`, `workflow.scheduledTime`, `workflow.parameters.*`, `inputs.parameters.*`, `tasks.*.outputs.*`, `steps.*.outputs.*` and `item`.

## Not supported

| Argo feature | Why, and what to do |
|---|---|
| Expressions `{{=...}}`, `expression` in parameters and in retries | Expressions can run code from parameters. Use `when` conditions and plain placeholders. |
| Pod fields: `securityContext`, `volumes`, `volumeMounts`, `nodeSelector`, `tolerations`, `affinity`, `sidecars`, `initContainers`, `podSpecPatch`, `metadata` | chronicle sets a restricted Pod and has no Pod patches yet. |
| Daemon steps and `{{steps.x.ip}}` | Not planned. Run a Service with a resource template. |
| `continueOn` | Use `depends` with `.Failed` in a DAG. |
| Life-cycle `hooks` and step-level `onExit` | Use the workflow `onExit`. |
| Global outputs (`globalName`, `workflow.outputs.*`) and `workflow.failures` | Not planned for the first version. |
| `inline` templates in tasks or steps | Write a named template. |
| `parallelism`, `synchronization` (mutex, semaphore), `memoize`, `metrics` | Not in the first version. |
| `http`, `plugin`, `data` and `containerSet` templates | Not supported. Task workers (M7) cover calls to services. |
| Artifacts that a workflow gives (`arguments.artifacts` on the Workflow) | A workflow cannot give artifacts. Make them in a first step. |
| Parameters from a ConfigMap (`valueFrom.configMapKeyRef`) | Give the value in the Workflow, or read the ConfigMap in a step. |
| `outputs.result` of a container without a command, exit codes as placeholders (`exitCode`), `{{retries}}`, `{{pod.name}}` | Not supported. |
