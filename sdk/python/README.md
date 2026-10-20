# chronicle Python SDK

Define chronicle workflows as Python code. Render them as JSON for kubectl and GitOps, or submit them to the chronicle server. The SDK uses only the Python standard library.

```python
import chronicle

w = chronicle.Workflow("hello-")
who = w.param("who", "world")
echo = chronicle.container("echo", "busybox:1.37", "echo")
echo.args(echo.input("text"))

main = chronicle.dag("main")
a = main.task("a", echo).arg("text", "hello " + who)
main.task("b", echo).after(a).arg("text", "a said " + a.result())
w.entrypoint(main)

print(w.to_json())  # for kubectl or Git
client = chronicle.Client(server, token)
name = client.submit("team-a", w)  # start a run
phase = client.wait("team-a", name, timeout=3600)  # Succeeded, Failed or Error

name = client.submit_template("team-a", "etl", {"day": "2026-10-19"})  # a WorkflowTemplate
```

- Placeholders connect the steps: `Workflow.param`, `Template.input`, `Task.result`, `Task.output` and `Task.artifact`.
- Templates: `container`, `script`, `task_template`, `dag` and `http_template`. An http template sends one request, for example from the exit handler: `w.on_exit(chronicle.http_template("notify", url, method="POST").header_from_secret("Authorization", "chat", "token"))`. A child workflow template starts a run of another template and waits for it: `chronicle.child_workflow_template("load", "etl", arguments={"day": day}).output_field("rows")`.
- The server checks the workflow with the admission webhook when you submit it. An error is a `chronicle.APIError` with a Connect code, for example `invalid_argument`.
- The token is an OIDC ID token, or a Kubernetes service account token with the audience `chronicle`.
- The Go, Python and Rust SDKs make the same object for the same workflow. The tests compare it with `sdk/testdata/etl.json`.

Development: `uv run pytest`, `uv run ruff check .` and `uv run ruff format --check .`.

Task workers: see [docs/task-workers.md](../../docs/task-workers.md) and `examples/worker`.
