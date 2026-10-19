//! Build a fan-out workflow. Print it as JSON, or submit it with a server URL.
//!
//!     cargo run --example fanout | kubectl create -n team-a -f -
//!     cargo run --example fanout -- https://chronicle.example.com team-a token-file

use chronicle::{Client, Task, Template, Workflow, input, param};
use serde_json::json;

/// build makes one "greet" task for each region, then a "done" task after all of them.
fn build() -> Workflow {
    let greet = Template::container("greet", "busybox:1.37", ["echo"])
        .input("text")
        .args([input("text")]);
    let each = Task::new("greet", &greet)
        .arg("text", &format!("{} from {{{{item}}}}", param("greeting")))
        .with_items([json!("eu"), json!("us"), json!("asia")]);
    let done = Task::new("done", &greet)
        .after(&[&each])
        .arg("text", "all regions done");
    Workflow::new("fanout-")
        .param("greeting", "hello")
        .entrypoint(Template::dag("main").tasks([each, done]))
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let w = build();
    let args: Vec<String> = std::env::args().skip(1).collect();
    let [server, namespace, token_file] = args.as_slice() else {
        println!("{}", w.to_json()?);
        return Ok(());
    };
    let token = std::fs::read_to_string(token_file)?;
    let client = Client::new(server, token.trim())?;
    println!("Started {namespace}/{}", client.submit(namespace, &w)?);
    Ok(())
}
