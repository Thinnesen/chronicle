//! Runs the task "rust-greet" of the e2e workflow test/e2e/testdata/tasks.yaml.
//!
//!     cargo run --example worker -- https://chronicle.example.com team-a token-file

use std::sync::atomic::AtomicBool;

use chronicle::{Client, Worker};
use serde_json::json;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let [server, namespace, token_file] = args.as_slice() else {
        return Err("usage: worker <server> <namespace> <token-file>".into());
    };
    let token = std::fs::read_to_string(token_file)?;
    let mut worker = Worker::new(Client::new(server, token.trim())?, namespace).concurrency(2);
    worker.register("rust-greet", |input, _| {
        Ok(json!({"greeting": format!("Rust says hello to {}", input["who"])}))
    });
    // The process ends on SIGTERM; the e2e test stops the worker that way.
    let stop = AtomicBool::new(false);
    worker.run(&stop)?;
    Ok(())
}
