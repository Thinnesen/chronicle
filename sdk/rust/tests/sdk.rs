use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpListener;
use std::thread;
use std::time::Duration;

use chronicle::{Client, Task, Template, Workflow, input, param};
use serde_json::Value;

/// etl builds the same workflow as etl() in the Go SDK tests.
fn etl() -> Workflow {
    let fetch = Template::script(
        "fetch",
        "busybox:1.37",
        r#"mkdir -p /tmp/rows && echo "$DAY" > /tmp/rows/day.txt && echo 1204"#,
    )
    .env("DAY", &param("day"))
    .output_artifact("rows", "/tmp/rows")
    .retry(2, Some("10s"));
    let count = Template::container("count", "busybox:1.37", ["wc", "-l", "/tmp/in/day.txt"])
        .input_artifact("rows", "/tmp/in");
    let echo = Template::container("echo", "busybox:1.37", ["echo"])
        .input("msg")
        .args([input("msg")]);

    let f = Task::new("fetch", &fetch);
    let c = Task::new("count", &count)
        .after(&[&f])
        .artifact_arg("rows", &f.artifact("rows"))
        .continue_on(true, false);
    let r = Task::new("report", &echo)
        .after(&[&c])
        .arg("msg", &format!("fetched {} rows", f.result()));
    Workflow::new("etl-")
        .label("team", "data")
        .parallelism(4)
        .mutex("etl")
        .param("day", "2026-10-17")
        .entrypoint(Template::dag("main").parallelism(2).tasks([f, c, r]))
}

#[test]
fn matches_the_go_sdk() {
    let want: Value = serde_json::from_str(include_str!("../../testdata/etl.json")).unwrap();
    assert_eq!(etl().to_value().unwrap(), want);
}

#[test]
fn reports_mistakes() {
    let cases = [
        (Workflow::new("x-"), "no entrypoint"),
        (
            Workflow::new("x-").entrypoint(Template::container("c", "busybox", ["true"]).tasks([])),
            "is not a DAG",
        ),
        (
            Workflow::new("x-").entrypoint(Template::script(
                "s",
                "busybox",
                "echo {{inputs.parameters.x}}",
            )),
            "cannot have placeholders",
        ),
        (
            Workflow::new("x-").entrypoint(Template::dag("main").tasks([
                Task::new("a", &Template::container("c", "busybox", ["true"])),
                Task::new("b", &Template::container("c", "alpine", ["true"])),
            ])),
            "two different templates have the name \"c\"",
        ),
    ];
    for (w, want) in cases {
        let err = w.to_value().unwrap_err();
        assert!(err.0.contains(want), "{err} does not contain {want}");
    }
}

/// serve answers one request with each (status, body) of answers. It returns the requests.
fn serve(
    listener: TcpListener,
    answers: Vec<(&'static str, &'static str)>,
) -> thread::JoinHandle<Vec<String>> {
    thread::spawn(move || {
        let mut all = Vec::new();
        for ((status, answer), stream) in answers.into_iter().zip(listener.incoming()) {
            let mut stream = stream.unwrap();
            let mut reader = BufReader::new(stream.try_clone().unwrap());
            let mut head = String::new();
            let mut length = 0;
            loop {
                let mut line = String::new();
                reader.read_line(&mut line).unwrap();
                if let Some(v) = line.to_ascii_lowercase().strip_prefix("content-length:") {
                    length = v.trim().parse().unwrap();
                }
                head.push_str(&line);
                if line == "\r\n" {
                    break;
                }
            }
            let mut body = vec![0; length];
            reader.read_exact(&mut body).unwrap();
            all.push(head + &String::from_utf8(body).unwrap());
            let resp = format!(
                "HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{answer}",
                answer.len()
            );
            stream.write_all(resp.as_bytes()).unwrap();
        }
        all
    })
}

#[test]
fn submits_through_the_api() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    let server = serve(
        listener,
        vec![
            ("200 OK", r#"{"summary":{"name":"hello-x1"}}"#),
            (
                "403 Forbidden",
                r#"{"code":"permission_denied","message":"workflows is forbidden"}"#,
            ),
        ],
    );
    let client = Client::new(&url, "tok").unwrap();
    let w =
        Workflow::new("hello-").entrypoint(Template::container("hi", "busybox", ["echo", "hi"]));
    assert_eq!(client.submit("team-a", &w).unwrap(), "hello-x1");
    let err = client.submit("team-a", &w).unwrap_err();
    assert_eq!(err.code, "permission_denied");
    let request = server.join().unwrap().remove(0);
    assert!(
        request.starts_with("POST /chronicle.v1.WorkflowService/SubmitWorkflow"),
        "{request}"
    );
    assert!(
        request.contains("authorization: Bearer tok")
            || request.contains("Authorization: Bearer tok"),
        "{request}"
    );
    let body: Value = serde_json::from_str(request.split("\r\n\r\n").nth(1).unwrap()).unwrap();
    assert_eq!(body["workflow"]["kind"], "Workflow");
    assert_eq!(body["namespace"], "team-a");
    assert!(Client::new("file:///etc/passwd", "tok").is_err());
}

#[test]
fn waits_for_the_end_of_a_run() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    let server = serve(
        listener,
        vec![
            (
                "503 Service Unavailable",
                r#"{"code":"unavailable","message":"the server restarts"}"#,
            ),
            ("200 OK", r#"{"summary":{"phase":"Running"}}"#),
            ("200 OK", r#"{"summary":{"phase":"Succeeded"}}"#),
        ],
    );
    let client = Client::new(&url, "tok").unwrap();
    let phase = client
        .wait("team-a", "hello-x1", Duration::from_millis(10), None)
        .unwrap();
    assert_eq!(phase, "Succeeded");
    server.join().unwrap();
}

#[test]
fn submits_a_template_with_parameters() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    let server = serve(
        listener,
        vec![("200 OK", r#"{"summary":{"name":"etl-x1"}}"#)],
    );
    let client = Client::new(&url, "tok").unwrap();
    let name = client
        .submit_template("team-a", "cluster/etl", &[("day", "2026-10-19")])
        .unwrap();
    assert_eq!(name, "etl-x1");
    let request = server.join().unwrap().remove(0);
    let body: Value = serde_json::from_str(request.split("\r\n\r\n").nth(1).unwrap()).unwrap();
    assert_eq!(body["template"]["name"], "etl");
    assert_eq!(body["template"]["clusterScope"], true);
    assert_eq!(body["parameters"]["day"], "2026-10-19");
}

#[test]
fn reads_a_rotated_token_file_again() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    let server = serve(
        listener,
        vec![
            ("200 OK", r#"{"summary":{"phase":"Running"}}"#),
            ("200 OK", r#"{"summary":{"phase":"Running"}}"#),
        ],
    );
    let file = std::env::temp_dir().join(format!("chronicle-token-{}", std::process::id()));
    std::fs::write(&file, "first\n").unwrap();
    let client = Client::from_token_file(&url, &file).unwrap();
    client.phase("team-a", "x").unwrap();
    std::fs::write(&file, "second").unwrap();
    client.phase("team-a", "x").unwrap();
    let requests = server.join().unwrap();
    let _ = std::fs::remove_file(&file);
    assert!(
        requests[0].to_lowercase().contains("bearer first"),
        "{}",
        requests[0]
    );
    assert!(
        requests[1].to_lowercase().contains("bearer second"),
        "{}",
        requests[1]
    );
}
