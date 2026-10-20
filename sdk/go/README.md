# chronicle Go SDK

Define chronicle workflows as Go code. Render them as YAML for kubectl and GitOps, or submit them to the chronicle server.

```go
import "github.com/Thinnesen/chronicle/sdk/go/chronicle"

w := chronicle.New("hello-")
who := w.Param("who", "world")
echo := chronicle.Container("echo", "busybox:1.37", "echo")
echo.Args(echo.Input("text"))

main := chronicle.DAG("main")
a := main.Task("a", echo).Arg("text", "hello "+who)
main.Task("b", echo).After(a).Arg("text", "a said "+a.Result())
w.Entrypoint(main)

yaml, err := w.YAML()                    // for kubectl or Git
c, err := chronicle.NewClient(server, token)
name, err := c.Submit(ctx, "team-a", w)  // start a run
phase, err := c.Wait(ctx, "team-a", name, 5*time.Second) // Succeeded, Failed or Error

name, err = c.SubmitTemplate(ctx, "team-a", "etl", map[string]string{"day": "2026-10-19"}) // a WorkflowTemplate
```

- `Spec`, `YAML` and `Submit` validate the workflow with the same rules as the admission webhook.
- Placeholders connect the steps: `Workflow.Param`, `Template.Input`, `Task.Result`, `Task.Output` and `Task.Artifact`.
- Templates: `Container`, `Script`, `DAG`, `HTTP`, `TaskTemplate` and `ChildWorkflow`. An http template sends one request, for example from the exit handler: `w.OnExit(chronicle.HTTP("notify", url).Method("POST").HeaderFromSecret("Authorization", "chat", "token"))`. A child workflow template starts a run of another template and waits for it: `chronicle.ChildWorkflow("load", "etl").ChildArg("day", day).OutputField("rows")`.
- The token is an OIDC ID token, or a Kubernetes service account token with the audience `chronicle` (`kubectl create token <sa> --audience chronicle`).
- Examples: `example_test.go` (artifacts, retries) and `examples/fanout` (fan-out with `WithItems`).

Task workers: see [docs/task-workers.md](../../docs/task-workers.md) and `examples/worker`.
