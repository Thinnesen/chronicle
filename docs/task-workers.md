# Task workers

A `task` template runs a function in your own process instead of a container. A worker of the Go, Python or Rust SDK takes the task from a queue, runs the function and sends the result. Use task workers for calls to services, for work that needs a warm cache or a GPU that a long-running process holds, and for code that you want to keep in your application.

Task templates need the archive database (`archive.existingSecret` in the Helm chart). The queue is a table in it.

The worker is a library call in your program, not a separate tool: add the SDK to your project (see [install.md](install.md)) and run the program, for example as a Deployment in the team namespace.

## 1. Write the template

```yaml
- name: send-mail
  inputs:
    parameters:
    - name: to
    - name: subject
  outputs:
    parameters:
    - name: messageID      # a field of the result
  retryStrategy:
    limit: 3
    backoff: {duration: 10s, factor: 2}
  timeout: 5m              # start-to-close: one attempt can run 5 minutes
  task:
    name: send-mail        # the task type that a worker registers
    queue: mail            # optional, the default is "default"
    heartbeatTimeout: 30s  # optional: fail the attempt when the worker is silent for 30 s
    scheduleToStartTimeout: 10m  # optional: fail the attempt when no worker takes it in 10 min
```

- The worker gets the input parameters as a JSON object of strings.
- The result must be JSON. It becomes `outputs.result`. The fields of a result object become the output parameters with the same names. A result can have at most 64 KiB.

## 2. Give the worker access

A worker logs in like the CLI: with a Kubernetes service account token with the audience `chronicle`, or with an OIDC ID token. It needs `update` on `workflows/tasks` in the namespace. The Helm chart has the role `chronicle-task-worker`:

```sh
kubectl create serviceaccount mail-worker -n team-a
kubectl create rolebinding mail-worker -n team-a --clusterrole=chronicle-task-worker --serviceaccount=team-a:mail-worker
kubectl create token mail-worker -n team-a --audience=chronicle   # or a projected token in the worker Pod
```

## 3. Run the worker

Go:

```go
client, _ := chronicle.NewClient(server, token)
w := chronicle.NewWorker(client, "team-a", chronicle.WorkerOptions{Queue: "mail", Concurrency: 4})
w.Register("send-mail", func(ctx context.Context, t chronicle.TaskInfo, in map[string]string) (any, error) {
	id, err := mailer.Send(ctx, in["to"], in["subject"], t.IdempotencyKey)
	if errors.Is(err, mailer.ErrBadAddress) {
		return nil, chronicle.NonRetryable(err)
	}
	return map[string]string{"messageID": id}, err
})
err := w.Run(ctx) // until ctx ends, for example on SIGTERM
```

Python:

```python
worker = chronicle.Worker(chronicle.Client(server, token), "team-a", queue="mail", concurrency=4)

@worker.task("send-mail")
def send_mail(data, task):
    return {"messageID": mailer.send(data["to"], data["subject"], key=task.idempotency_key)}

worker.run(stop_event)
```

Rust:

```rust
let mut worker = Worker::new(Client::new(server, token)?, "team-a").queue("mail").concurrency(4);
worker.register("send-mail", |input, task| {
    let id = mailer::send(&input["to"], &input["subject"], &task.idempotency_key)
        .map_err(|e| TaskError::retryable(e.to_string()))?;
    Ok(json!({"messageID": id}))
});
worker.run(&stop)?;
```

## Delivery, retries and cancellation

- Delivery is at-least-once. A worker can lose its connection after the work is done, so the same work can run again in the next attempt. All attempts of a node have the same idempotency key: use it to make a repeated call harmless.
- Each attempt is a new task. A retry comes only from the `retryStrategy` of the template. An error is retryable by default. A final error (`NonRetryable`, `TaskError::fatal`, or a panic) gives the phase Error, which the default retry policy `OnFailure` does not retry.
- The worker sends heartbeats by itself (every 5 s by default). When an attempt times out, or when the workflow is stopped, the server cancels the task. The worker then cancels the context (Go), sets `task.cancelled` (Python) or makes `is_cancelled()` true (Rust), and it sends no result. The server refuses a late result.
- When the worker stops, it takes no new tasks and waits a grace period (30 s by default) for the running tasks.
