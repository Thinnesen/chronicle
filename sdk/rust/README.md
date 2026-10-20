# chronicle Rust SDK

Define chronicle workflows as Rust code. Render them as JSON for kubectl and GitOps, or submit them to the chronicle server.

```rust
use chronicle::{Client, Task, Template, Workflow, input, param};

let echo = Template::container("echo", "busybox:1.37", ["echo"]).input("text").args([input("text")]);
let a = Task::new("a", &echo).arg("text", &format!("hello {}", param("who")));
let b = Task::new("b", &echo).after(&[&a]).arg("text", &format!("a said {}", a.result()));
let w = Workflow::new("hello-").param("who", "world").entrypoint(Template::dag("main").tasks([a, b]));

println!("{}", w.to_json()?);                    // for kubectl or Git
let client = Client::new(server, token)?;
let name = client.submit("team-a", &w)?;
let phase = client.wait("team-a", &name, Duration::from_secs(5), None)?; // Succeeded, Failed or Error

let name = client.submit_template("team-a", "etl", &[("day", "2026-10-19")])?; // a WorkflowTemplate
```

- Placeholders connect the steps: `param`, `input`, `Task::result`, `Task::output` and `Task::artifact`.
- Templates: `container`, `script`, `task_template`, `dag` and `http`. An http template sends one request, for example from the exit handler: `.on_exit(Template::http("notify", url).method("POST").header_from_secret("Authorization", "chat", "token"))`. A child workflow template starts a run of another template and waits for it: `Template::child_workflow("load", "etl").child_arg("day", &day).output_field("rows")`.
- Configure a template before you make a task with it: `Task::new` keeps a copy.
- The server checks the workflow with the admission webhook when you submit it. An error is an `ApiError` with a Connect code.
- The token is an OIDC ID token, or a Kubernetes service account token with the audience `chronicle`.
- The Go, Python and Rust SDKs make the same object for the same workflow. The tests compare it with `sdk/testdata/etl.json`.

Development: `cargo test`, `cargo clippy --all-targets -- -D warnings` and `cargo fmt --check`. The toolchain is pinned in `rust-toolchain.toml`. Dependencies are pinned with `=` versions, and `Cargo.lock` is committed.

Task workers: see [docs/task-workers.md](../../docs/task-workers.md) and `examples/worker`.
