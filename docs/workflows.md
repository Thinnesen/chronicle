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
| `{{inputs.artifacts.<name>.path}}`, `{{outputs.artifacts.<name>.path}}`, `{{outputs.parameters.<name>.path}}` | In a container or script template: the path of a declared artifact or output file, so that a command does not repeat it. |
| `{{tasks.<task>.outputs.result}}` | The standard output of a DAG task (at most 3 KiB). |
| `{{tasks.<task>.outputs.parameters.<name>}}` | An output parameter of a DAG task. |
| `{{steps.<step>.outputs.result}}`, `{{steps.<step>.outputs.parameters.<name>}}` | The same for a step. |
| `{{tasks.<task>.outputs.parameters}}`, `{{steps.<step>.outputs.parameters}}` | For a fan-out: the output parameters of each item, as a JSON list of objects, for example `[{"num":"1","evenness":"odd"}]`. Use it in `withParam` with `{{item.<name>}}`. |
| `{{tasks.<task>.status}}`, `{{steps.<step>.status}}` | The phase of a finished task or step. |
| `{{tasks.<task>.exitCode}}`, `{{steps.<step>.exitCode}}` | The exit code of the main container of a container or script task or step. Use it with `continueOn` or `depends: a.Failed`. |
| `{{tasks.<task>.id}}`, `{{tasks.<task>.startedAt}}`, `{{tasks.<task>.finishedAt}}` | The node ID and the times (RFC 3339) of a task. The same for a step. |
| `{{pod.name}}`, `{{node.name}}`, `{{retries}}` | In a container, script or resource template: its Pod, its node and the number of its retry (from 0). |
| `{{item}}`, `{{item.<key>}}` | The item of a fan-out. |

- Put a placeholder in an environment variable, not in a script or a shell command. A script `source` cannot have placeholders, so its parameters never become code. A placeholder in `command` or `args` is put in as text. With `sh -c`, its value becomes shell code, and a parameter can run commands. The webhook and `chronicle lint` warn about it. Use `env` and `"$NAME"` in the command.
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
- A task can test for the exact result of another task, for example `transform.Failed`. Such a failure counts as handled: it does not fail the DAG.
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
- A workflow parameter or an input parameter can read a ConfigMap of the namespace: `valueFrom: {configMapKeyRef: {name: settings, key: region}}`. The controller reads it once when the run starts, so all steps see the same value. A missing key uses `default`. The ConfigMap needs the label `chronicle.thinnesen.com/parameters: "true"`, so that only ConfigMaps that a team shares on purpose reach the runs. Each reader of the run sees the value.
- An output parameter reads a file (`valueFrom.path`). In a resource template, `valueFrom.jsonPath` reads a field of the object, for example `{.metadata.name}`.
- `globalName` makes an output parameter or an output artifact a workflow output. Later steps and the exit handler read it as `{{workflow.outputs.parameters.<name>}}`, or as `{{workflow.outputs.artifacts.<name>}}` in `from`. The run shows its outputs in the UI and in `chronicle get`. When two steps give the same name, the later one wins.
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

- `when` compares values with `==`, `!=`, `<`, `<=`, `>` and `>=`, and combines conditions with `&&`, `||` and `!`. Two numbers compare as numbers. `=~` and `!~` match a regular expression (Go RE2 syntax), for example `{{tasks.flip.outputs.result}} =~ '^hea'`. A false condition gives the node the phase Skipped.
- A placeholder value in `when` is always one operand. A value such as `x || true` cannot change the condition.
- `withItems` runs the template once for each item. `withParam` does the same for a JSON list, for example `{{tasks.list.outputs.result}}`. `withSequence` does it for a range of numbers: `withSequence: {count: "{{workflow.parameters.days}}", start: 1, format: "day-%02d"}` gives `day-01`, `day-02` and so on. Give `count` or `end`; the values can be placeholders. An `end` below `start` counts down. A fan-out has at most 1000 items. A run that would be larger than 1 MiB fails with "the run is too large" (see [operations.md](operations.md#9-sizing)).
- `parallelism` limits the Pods that run at the same time. `spec.parallelism` limits the whole run. `parallelism` on a `dag` or `steps` template limits the Pods below it. A step without a free slot waits with the message "waiting for a free slot".

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
- `continueOn: {failed: true}` (and `error: true`) on a task or a step lets the run go on after it fails. The tasks and steps after it run. Its failure does not fail the DAG or the steps. The step itself stays red.
- After a run, retry the failed nodes, or retry from any node. Use the UI or `chronicle retry`. A retry starts a new try of the same run. The archive keeps each earlier try, with its nodes and the keys of its archived logs. Select a try with the "Try" field on the run page, or with `chronicle get <run> --try 1` and `chronicle logs <run> --try 1`.

Reuse the result of a step that ran before with the same inputs (it needs the archive database):

```yaml
- name: report
  inputs: {parameters: [{name: day}]}
  memoize:
    key: "report-{{inputs.parameters.day}}"   # nodes with the same key in the namespace share the result
    maxAge: 24h                              # optional: an older result does not count
  container: {image: report:1.2, command: [report]}
```

- Before the step starts, chronicle looks up the key. With a valid result, the node succeeds at once with the saved outputs and does not start a Pod. The node shows the run that saved the result.
- Without a result, the step runs. When it succeeds, chronicle saves its outputs under the key.
- Put each input that changes the result into the key. Output artifacts point to the repository objects of the run that saved them. Keep those objects as long as the results.

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

A DAG task or a step can have its own exit handler. It runs when the task ends, also after a failure. It can read the status and the outputs of the task. The tasks after it wait for it. A hook that ends Error fails the DAG or the steps. A hook that ends Failed does not count. For a fan-out, the hook runs once after all items.

```yaml
  - name: build
    template: build
    onExit:
      template: report
      arguments: {parameters: [{name: status, value: "{{tasks.build.status}}"}]}
```

Approve a suspend node in the UI, or with `chronicle approve <run> -p env=prod`. Give the node as a second argument when more than one step waits. With `enum`, the approver can give only one of those values. The API refuses other values. The node fails if such a value comes in another way. The node message names the approver, for example "approved by oidc:alice@example.com". A `resource` template runs with the service account of the run (`spec.serviceAccountName`). So a step can do only what that account can do.

An `http` template sends one request, for example a chat message from the exit handler:

```yaml
  - name: notify
    http:
      url: https://chat.example.com/hooks/team-a
      method: POST            # GET (default), POST, PUT, PATCH, DELETE or HEAD
      headers:
      - {name: Content-Type, value: application/json}
      - name: Authorization
        valueFrom: {secretKeyRef: {name: chat, key: token}}
      body: '{"text": "{{workflow.name}} is {{workflow.status}}"}'
```

- The request comes from a small Pod in the namespace of the run, so network policies apply. The Pod has no Kubernetes API token.
- A status from 200 to 299 ends the step with success. The response body is `outputs.result`.
- The url, the body and header values can have placeholders. A header from a Secret comes into the Pod as an environment variable: chronicle does not read the Secret.
- The log and errors show the URL without its query, because the query can hold a token.

A `childWorkflow` template starts a run of another template in the same namespace and waits for it:

```yaml
  - name: load-day
    inputs: {parameters: [{name: day}]}
    outputs: {parameters: [{name: rows}]}      # from the outputs of the child run
    childWorkflow:
      workflowTemplateRef: {name: etl}         # the name can have placeholders; clusterScope: true for a ClusterWorkflowTemplate
      arguments: {parameters: [{name: day, value: "{{inputs.parameters.day}}"}]}
```

- The node gets the phase of the child run. Its output parameters come from the workflow outputs of the child run: the output parameters with `globalName` in the child template. A missing output without a default fails the node.
- The parent run owns the child run. A stop or a terminate of the parent, and the timeout of the node, stop the child run. A delete of the parent deletes the child. A suspend of the parent does not suspend a child run that runs: it stops only new steps of the parent.
- A retry of the parent run (`chronicle retry <run>`) retries the failed nodes of a failed child run, in the same child run. A retry from a node (`--node`) and the retry strategy of the step start a new child run. The child run has the annotations `chronicle.thinnesen.com/parent` and `chronicle.thinnesen.com/parent-node`.
- The child run uses its own template, with its own service account. A user who can start the parent can also start that template. So a child run gives no new permission.
- Child runs can nest 10 levels deep. Then the node fails, so that a template that starts itself stops.

## 9. Locks

```yaml
spec:
  synchronization:
    mutex: {name: deploy-prod}                 # one run at a time
    # semaphore: {name: gpu-jobs, limit: 3}    # or at most 3 runs at a time
```

Runs with the same lock name in a namespace share the lock. A run that waits for it is Pending, with the message "waiting for the lock ..., which ... holds". A finished run gives the lock back, and so does a run that was deleted. There is no fixed order among the waiting runs.

A semaphore can read its limit from a key of a ConfigMap. Then a platform team can change the limit without a change of the workflows:

```yaml
  synchronization:
    semaphore:
      name: gpu-jobs
      configMapKeyRef: {name: limits, key: gpu-jobs}   # the value is a number, for example "3"
```

The ConfigMap must have the label `chronicle.thinnesen.com/parameters: "true"`. The controller reads the value each time a run asks for the lock. A new limit applies to the next run that asks. Runs that hold the lock keep it. A missing ConfigMap, label, key or number holds the run or the node back. Its message tells the reason. It goes on when the ConfigMap is correct.

A container, script, resource, http or task template can have its own lock. Then each node of the template holds the lock only while it runs. For example, one database migration runs at a time across all runs:

```yaml
  - name: migrate
    synchronization:
      mutex: {name: orders-db}
    container: {image: migrate:1.4, command: [migrate, up]}
```

A node that waits is Pending with the message "waiting for the lock orders-db, which ... holds". A node gives the lock back when it ends. Template locks and run locks with the same name share one lock.

## 10. Volumes

The steps of a run are separate Pods. A volume claim template gives them one shared volume:

```yaml
spec:
  volumeClaimTemplates:
  - {name: work, storage: 1Gi}            # optional: storageClassName, accessModes
  volumes:
  - {name: scratch, emptyDir: {}}         # each Pod gets its own empty directory
  - {name: creds, secret: {secretName: registry}}
  templates:
  - name: build
    container:
      image: golang:1.27
      command: [go, build, -o, /work/app, ./...]
      volumeMounts:
      - {name: work, mountPath: /work}
      - {name: creds, mountPath: /creds, readOnly: true}
```

- The controller creates the claim `<run>-<name>-<8 letters of the run UID>` before the first Pod. A run that waits for a lock creates it when it gets the lock.
- A run that succeeds deletes its claims. A run that fails keeps them, so a retry finds the files of the steps that succeeded. The claims go away with the run. Kubernetes keeps a deleted claim until the Pods that use it are gone, for example after the Pod GC. A retry of a run that succeeded needs new claims. It stops with phase Error while the old claims still exist.
- The default access mode is `ReadWriteOnce`. Then all Pods that mount the volume must run on one node. Use `ReadWriteMany` when steps run at the same time on different nodes, and the storage class supports it.
- A claim with the same name that the run does not own stops the run with phase Error. The run never uses the files of another owner.
- `volumes` can be `emptyDir`, `secret` or `configMap`. A secret volume gives the same access as `env` with `secretKeyRef`.
- A mount cannot hide `/`, an input artifact, or the directories of chronicle in `/var/run`. So a mount at `/var/run` is not possible. A `subPath` stays in the volume.
- Existing claims are not supported yet (see [argo-migration.md](argo-migration.md)).

## 11. Templates and schedules

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
- `chronicle cron backfill etl-nightly --from 2026-10-01 --to 2026-10-07` starts the runs of the schedule times in a range, for example for days that the schedule missed. A time that has its run already is skipped, because each run has the name `<cron>-<schedule time>`. The concurrency policy does not hold back these runs.
- A task or a step can call a template of another template with `templateRef: {name: <template object>, template: <template name>}`.
- To run once at a later time, set `startAt` on the Workflow, or use `chronicle submit --from etl --at 2026-10-21T03:00:00+02:00` (or `--at 2h`), or with the field "Start at" on the template page of the UI. The run stays Pending until that time. Stop or delete it to cancel it.

## 12. Clean up

- `spec.podGC.strategy` deletes the Pods: `OnPodCompletion`, `OnPodSuccess`, `OnWorkflowCompletion` or `OnWorkflowSuccess`.
- `spec.ttlStrategy` deletes the finished run after some seconds. With the archive, the run stays visible in the UI and the CLI.

## 13. A YAML trap

kubectl reads YAML 1.1, which reads `y`, `n`, `yes`, `no`, `on` and `off` as booleans. Then a parameter `value: yes` or a parameter named `n` is not a string. The API refuses the object with "must be of type string". Put such words in quotes: `value: "yes"`. `chronicle submit -f` reads YAML 1.2, where only `true` and `false` are booleans. `chronicle lint` warns about such words.

## 14. When a step does not start

A step that waits shows the reason from Kubernetes. The reason is on its card in the graph, as a badge in the runs list, and in `chronicle get <run>`. The Events tab of the step has the details.

| Reason | What to do |
|---|---|
| `ErrImagePull`, `ImagePullBackOff` | Check the image name and the tag. For a private registry, give the run a service account with an image pull secret. |
| `Unschedulable` | No node can take the Pod. Make the `resources.requests` smaller, or ask the platform team for a node that fits. |
| `CreateContainerConfigError` | A Secret or a ConfigMap in `env` or `envFrom` does not exist. |
| `waiting for a free slot` | The `parallelism` limit is full. The step starts when another step ends. |

Kubernetes tries again until the cause is gone, so the step waits. Set a `timeout` on the template or `activeDeadlineSeconds` on the run to stop the wait.
