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
| `{{workflow.parameters.<name>}}`, `{{workflow.parameters.json}}` | A workflow parameter, and all workflow parameters as JSON. |
| `{{workflow.mainEntrypoint}}` | The entrypoint template of the run. |
| `{{workflow.creationTimestamp}}`, `{{workflow.scheduledTime}}` | The creation time, and the planned time of a cron run (RFC 3339). |
| `{{workflow.creationTimestamp.<letter>}}` | A part of the creation time in UTC, with a strftime letter as in Argo: `Y` (2026), `m` (10), `d` (20), `H`, `M`, `S` and others. `RFC3339` gives the full time, and `s` the Unix seconds. Use them for date partitions: `day={{workflow.creationTimestamp.Y}}-{{workflow.creationTimestamp.m}}-{{workflow.creationTimestamp.d}}`. |
| `{{workflow.status}}` | The phase of the run, in the exit handler. |
| `{{workflow.duration}}` | The seconds since the start of the run, for example in the exit handler. |
| `{{workflow.failures}}` | The failed steps as JSON (display name, message, template, phase, Pod, end), for the exit handler. |
| `{{workflow.serviceAccountName}}` | The service account of the run. |
| `{{workflow.outputs.parameters.<name>}}` | A workflow output (an output parameter with `globalName`) of a step that finished. |
| `{{workflow.labels.<key>}}`, `{{workflow.labels.json}}` | A label of the run, for example the label `chronicle.thinnesen.com/cron-workflow` of a cron run, and all labels as JSON. |
| `{{workflow.annotations.<key>}}`, `{{workflow.annotations.json}}` | An annotation of the run, and all annotations as JSON. |
| `{{inputs.parameters.<name>}}` | An input parameter of the template. |
| `{{tasks.<task>.outputs.result}}` | The standard output of a DAG task (at most 3 KiB). |
| `{{tasks.<task>.outputs.parameters.<name>}}` | An output parameter of a DAG task. |
| `{{steps.<step>.outputs.result}}`, `{{steps.<step>.outputs.parameters.<name>}}` | The same for a step. |
| `{{tasks.<task>.status}}`, `{{steps.<step>.status}}` | The phase of a finished task or step. |
| `{{pod.name}}`, `{{node.name}}`, `{{retries}}` | In a container, script or resource template: its Pod, its node and the number of its retry (from 0). |
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
- `globalName` makes an output parameter or an output artifact a workflow output: `{{workflow.outputs.parameters.<name>}}` (or `{{workflow.outputs.artifacts.<name>}}` in `from`) in later steps and in the exit handler. The run shows its outputs in the UI and in `chronicle get`. When two steps give the same name, the later one wins.
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
- `parallelism` limits the Pods that run at the same time: `spec.parallelism` for the whole run, and `parallelism` on a `dag` or `steps` template for the Pods below it. A step without a free slot waits with the message "waiting for a free slot".

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
- `continueOn: {failed: true}` (and `error: true`) on a task or a step lets the run go on after it fails: the tasks and steps after it run, and its failure does not fail the DAG or the steps. The step itself stays red.
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
      - {name: env, default: staging, enum: [staging, prod]}   # the approver can give a value
  - name: notify
    container:
      image: busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e
      command: [sh, -c, 'echo "the run is $STATUS"']
      env: [{name: STATUS, value: "{{workflow.status}}"}]
```

Approve a suspend node in the UI, or with `chronicle approve <run> -p env=prod`. Give the node as a second argument when more than one step waits. With `enum`, the approver can give only one of those values: the API refuses other values, and the node fails if such a value comes in another way. The node message names the approver, for example "approved by oidc:alice@example.com". A `resource` template runs with the service account of the run (`spec.serviceAccountName`), so a step can do only what that account can do.

## 9. Locks

```yaml
spec:
  synchronization:
    mutex: {name: deploy-prod}                 # one run at a time
    # semaphore: {name: gpu-jobs, limit: 3}    # or at most 3 runs at a time
```

Runs with the same lock name in a namespace share the lock. A run that waits for it is Pending, with the message "waiting for the lock ..., which ... holds". A finished run gives the lock back, and so does a run that was deleted. There is no fixed order among the waiting runs.

## 10. Templates and schedules

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

## 11. Clean up

- `spec.podGC.strategy` deletes the Pods: `OnPodCompletion`, `OnPodSuccess`, `OnWorkflowCompletion` or `OnWorkflowSuccess`.
- `spec.ttlStrategy` deletes the finished run after some seconds. With the archive, the run stays visible in the UI and the CLI.

## 12. A YAML trap

kubectl reads YAML 1.1, which reads `y`, `n`, `yes`, `no`, `on` and `off` as booleans. A parameter `value: yes` or a parameter named `n` is then not a string, and the API refuses the object with "must be of type string". Put such words in quotes: `value: "yes"`. `chronicle submit -f` reads YAML 1.2, where only `true` and `false` are booleans.

## 13. When a step does not start

A step that waits shows the reason from Kubernetes: on its card in the graph, as a badge in the runs list, and in `chronicle get <run>`. The Events tab of the step has the details.

| Reason | What to do |
|---|---|
| `ErrImagePull`, `ImagePullBackOff` | Check the image name and the tag. For a private registry, give the run a service account with an image pull secret. |
| `Unschedulable` | No node can take the Pod. Make the `resources.requests` smaller, or ask the platform team for a node that fits. |
| `CreateContainerConfigError` | A Secret or a ConfigMap in `env` or `envFrom` does not exist. |
| `waiting for a free slot` | The `parallelism` limit is full. The step starts when another step ends. |

Kubernetes tries again until the cause is gone, so the step waits. Set a `timeout` on the template or `activeDeadlineSeconds` on the run to stop the wait.
