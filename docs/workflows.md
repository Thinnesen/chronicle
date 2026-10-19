# Write workflows

This guide shows how to write chronicle workflows in YAML. The SDKs for Go, Python and Rust make the same objects (see the `sdk/` folder).

## 1. The parts of a workflow

A `Workflow` is one run. A `WorkflowTemplate` is a reusable definition in a namespace, and a `ClusterWorkflowTemplate` is one for all namespaces. A `CronWorkflow` starts runs on a schedule.

```yaml
apiVersion: chronicle.thinnesen.com/v1alpha1
kind: Workflow
metadata:
  generateName: hello-        # each run gets a name like hello-x7k2p
spec:
  entrypoint: main            # the template that the run starts with
  arguments:
    parameters:
    - {name: who, value: world}
  templates:
  - name: main
    container:
      image: busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e
      command: [sh, -c, 'echo "hello $WHO"']
      env: [{name: WHO, value: "{{workflow.parameters.who}}"}]
```

Each template does one thing:

| Template | What it does |
|---|---|
| `container` | Runs a container in a Pod. |
| `script` | Writes `source` to a file and runs `command` with the file as the last argument. The default command is `sh`. |
| `dag` | Runs tasks in the order of their dependencies. |
| `steps` | Runs groups of steps one after the other. The steps of a group run at the same time. |
| `suspend` | Waits for an approval, or for a duration. |
| `resource` | Creates, applies or deletes a Kubernetes object, and can wait for a condition. |
| `task` | Sends the work to a task worker of an SDK (see [task-workers.md](task-workers.md)). |

## 2. Placeholders

Placeholders put values into a template. They are text in `{{ }}`.

| Placeholder | Value |
|---|---|
| `{{workflow.name}}`, `{{workflow.namespace}}`, `{{workflow.uid}}` | The run. |
| `{{workflow.parameters.<name>}}` | A workflow parameter. |
| `{{workflow.creationTimestamp}}`, `{{workflow.scheduledTime}}` | The creation time, and the planned time of a cron run (RFC 3339). |
| `{{workflow.status}}` | The phase of the run, in the exit handler. |
| `{{inputs.parameters.<name>}}` | An input parameter of the template. |
| `{{tasks.<task>.outputs.result}}` | The standard output of a DAG task (at most 3 KiB). |
| `{{tasks.<task>.outputs.parameters.<name>}}` | An output parameter of a DAG task. |
| `{{steps.<step>.outputs.result}}`, `{{steps.<step>.outputs.parameters.<name>}}` | The same for a step. |
| `{{item}}`, `{{item.<key>}}` | The item of a fan-out. |

- Put a placeholder in an environment variable, not in a script or a shell command. A script `source` cannot have placeholders. Thus a parameter value can never become shell code.
- The standard output of a step is its result. Write progress messages to the standard error.

## 3. A DAG

A task starts when its dependencies are done. Use `dependencies` for a list of names, or `depends` for a condition.

```yaml
- name: main
  dag:
    tasks:
    - {name: extract, template: extract}
    - {name: transform, template: transform, depends: extract}
    - {name: validate, template: validate, depends: extract}
    - {name: load, template: load, depends: transform && validate}
    - {name: alert, template: alert, depends: transform.Failed}
```

- `a` means "a succeeded or was skipped". You can also write `a.Succeeded`, `a.Failed`, `a.Errored`, `a.Skipped` and `a.Omitted`, and combine them with `&&`, `||`, `!` and parentheses.
- A failure that a task tests for with its exact result (`transform.Failed`) is handled: it does not fail the DAG.
- `failFast: false` on the DAG lets other branches continue after a failure. The default is `true`.

## 4. Steps

```yaml
- name: main
  steps:
  - - {name: build, template: build}
  - - {name: test-unit, template: test, arguments: {parameters: [{name: suite, value: unit}]}}
    - {name: test-e2e, template: test, arguments: {parameters: [{name: suite, value: e2e}]}}
  - - {name: publish, template: publish}
```

The two tests run at the same time, after `build`. `publish` starts when both are done.

## 5. Parameters and outputs

```yaml
- name: count
  inputs:
    parameters:
    - name: day
    - {name: env, default: staging, enum: [staging, prod]}
  outputs:
    parameters:
    - {name: rows, valueFrom: {path: /tmp/rows}}    # the content of a file
  container:
    image: busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e
    command: [sh, -c, 'echo 1200 > /tmp/rows']
```

- An input parameter gets its value from the arguments of the call, from `value`, or from `default`.
- `enum` limits the values. The UI shows a list for such a parameter.
- An output parameter reads a file (`valueFrom.path`). In a resource template, `valueFrom.jsonPath` reads a field of the object, for example `{.metadata.name}`.
- Larger data goes through artifacts in S3 (see [artifacts.md](artifacts.md)).

## 6. Conditions and fan-out

```yaml
- {name: flip, template: flip}
- {name: heads, template: celebrate, depends: flip, when: "{{tasks.flip.outputs.result}} == heads"}
- name: render
  template: render
  depends: flip
  withItems: [eu, us, apac]
  arguments: {parameters: [{name: region, value: "{{item}}"}]}
```

- `when` compares values with `==`, `!=`, `<`, `<=`, `>` and `>=`, and combines conditions with `&&`, `||` and `!`. Two numbers compare as numbers. A false condition gives the node the phase Skipped.
- A placeholder value in `when` is always one operand. A value such as `x || true` cannot change the condition.
- `withItems` runs the template once for each item. `withParam` does the same for a JSON list, for example `{{tasks.list.outputs.result}}`.

## 7. Retries, timeouts and deadlines

```yaml
- name: transform
  retryStrategy:
    limit: 3                  # at most 3 more attempts
    retryPolicy: OnFailure    # OnFailure (default), OnError or Always
    backoff: {duration: 10s, factor: 2, maxDuration: 5m}
  timeout: 10m                # for each attempt
  container: {image: "...", command: [transform]}
```

- `Failed` means the command failed (exit code not 0). `Error` means that the step could not run, for example when the image does not exist.
- `spec.activeDeadlineSeconds` stops the whole run after this time.
- After a run, you can retry the failed nodes, or retry from any node, in the UI or with `chronicle retry`.

## 8. Exit handler, approvals and resources

```yaml
spec:
  entrypoint: main
  onExit: notify              # runs after the entrypoint, also after a failure
  templates:
  - name: approve
    suspend: {}               # waits for an approval; "suspend: {duration: 1h}" waits one hour
    outputs:
      parameters:
      - {name: env, default: staging}   # the approver can give a value
  - name: notify
    container:
      image: busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e
      command: [sh, -c, 'echo "the run is $STATUS"']
      env: [{name: STATUS, value: "{{workflow.status}}"}]
```

Approve a suspend node in the UI, or with `chronicle approve <run> <node> -p env=prod`. A `resource` template runs with the service account of the run (`spec.serviceAccountName`), so a step can do only what that account can do.

## 9. Templates and schedules

```yaml
apiVersion: chronicle.thinnesen.com/v1alpha1
kind: CronWorkflow
metadata:
  name: etl-nightly
spec:
  schedules: ["0 3 * * *"]
  timezone: Europe/Berlin
  concurrencyPolicy: Forbid   # Allow (default), Forbid or Replace
  workflowSpec:
    workflowTemplateRef: {name: etl}
```

- `workflowTemplateRef` runs a `WorkflowTemplate`. Add `clusterScope: true` for a `ClusterWorkflowTemplate`.
- A task or a step can call a template of another template with `templateRef: {name: <template object>, template: <template name>}`.

## 10. Clean up

- `spec.podGC.strategy` deletes the Pods: `OnPodCompletion`, `OnPodSuccess`, `OnWorkflowCompletion` or `OnWorkflowSuccess`.
- `spec.ttlStrategy` deletes the finished run after some seconds. With the archive, the run stays visible in the UI and the CLI.

## 11. A YAML trap

kubectl reads YAML 1.1, which reads `y`, `n`, `yes`, `no`, `on` and `off` as booleans. A parameter `value: yes` or a parameter named `n` is then not a string, and the API refuses the object with "must be of type string". Put such words in quotes: `value: "yes"`. `chronicle submit -f` reads YAML 1.2, where only `true` and `false` are booleans.
