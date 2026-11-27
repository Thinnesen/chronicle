use std::collections::HashMap;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use chronicle::{Client, TaskError, Worker};
use serde_json::{Value, json};

/// A fake TaskService with four tasks. It cancels the task "slow" on its first heartbeat.
#[derive(Default)]
struct Queue {
    tasks: Vec<Value>,
    results: HashMap<String, Value>,
    /// The answers, with their HTTP status, that a method gives before its normal answers.
    script: HashMap<String, Vec<(u16, Value)>>,
    calls: HashMap<String, usize>,
}

fn answer(queue: &Mutex<Queue>, method: &str, body: &Value) -> (u16, Value) {
    let mut q = queue.lock().unwrap();
    *q.calls.entry(method.into()).or_default() += 1;
    if let Some(next) = q.script.get_mut(method).filter(|s| !s.is_empty()) {
        return next.remove(0);
    }
    let out = match method {
        "PollTask" if q.tasks.is_empty() => json!({}),
        "PollTask" => json!({"task": q.tasks.remove(0)}),
        "HeartbeatTask" => json!({"cancelled": body["id"] == "slow"}),
        _ => {
            q.results
                .insert(body["id"].as_str().unwrap().into(), body.clone());
            json!({})
        }
    };
    (200, out)
}

fn serve_one(mut stream: TcpStream, queue: &Mutex<Queue>) {
    let mut reader = BufReader::new(stream.try_clone().unwrap());
    let mut first = String::new();
    reader.read_line(&mut first).unwrap();
    let method = first
        .split(' ')
        .nth(1)
        .unwrap()
        .rsplit('/')
        .next()
        .unwrap()
        .to_string();
    let mut length = 0;
    loop {
        let mut line = String::new();
        reader.read_line(&mut line).unwrap();
        if let Some(v) = line.to_ascii_lowercase().strip_prefix("content-length:") {
            length = v.trim().parse().unwrap();
        }
        if line == "\r\n" {
            break;
        }
    }
    let mut body = vec![0; length];
    reader.read_exact(&mut body).unwrap();
    let (status, out) = answer(queue, &method, &serde_json::from_slice(&body).unwrap());
    let out = out.to_string();
    let resp = format!(
        "HTTP/1.1 {status} X\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{out}",
        out.len()
    );
    stream.write_all(resp.as_bytes()).unwrap();
}

/// Serves queue on a free port and returns its URL.
fn serve(queue: Arc<Mutex<Queue>>) -> String {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    thread::spawn(move || {
        for stream in listener.incoming() {
            let q = queue.clone();
            thread::spawn(move || serve_one(stream.unwrap(), &q));
        }
    });
    url
}

/// Runs one task "t" against a server that gives the scripted answers of CompleteTask first.
/// It returns the queue when the worker is done with the task, or after 5 seconds.
fn complete_with(script: Vec<(u16, Value)>) -> Arc<Mutex<Queue>> {
    let queue = Arc::new(Mutex::new(Queue {
        tasks: vec![json!({"id": "t", "name": "greet", "inputJson": "{}", "lease": "l"})],
        script: HashMap::from([("CompleteTask".to_string(), script)]),
        ..Default::default()
    }));
    let mut worker = Worker::new(Client::new(&serve(queue.clone()), "tok").unwrap(), "team-a")
        .grace_period(Duration::from_secs(5));
    worker.register("greet", |_, _| Ok(json!({"ok": "yes"})));
    let stop = Arc::new(AtomicBool::new(false));
    let s = stop.clone();
    let runner = thread::spawn(move || worker.run(&s));
    let start = Instant::now();
    // With one slot, the second poll comes only after the task is done.
    let polls = || queue.lock().unwrap().calls.get("PollTask").copied();
    while polls() < Some(2) && start.elapsed() < Duration::from_secs(5) {
        thread::sleep(Duration::from_millis(10));
    }
    stop.store(true, Ordering::SeqCst);
    runner.join().unwrap().unwrap();
    queue
}

#[test]
fn complete_task_retries_a_transient_error() {
    let queue = complete_with(vec![
        (
            503,
            json!({"code": "unavailable", "message": "the server restarts"}),
        ),
        (
            429,
            json!({"code": "resource_exhausted", "message": "slow down"}),
        ),
    ]);
    let q = queue.lock().unwrap();
    assert_eq!(q.results["t"]["outputJson"], "{\"ok\":\"yes\"}");
    assert_eq!(q.calls["CompleteTask"], 3);
}

#[test]
fn complete_task_does_not_retry_a_permanent_error() {
    let lost = (
        400,
        json!({"code": "failed_precondition", "message": "the lease is lost"}),
    );
    let queue = complete_with(vec![lost.clone(), lost]);
    let q = queue.lock().unwrap();
    assert_eq!(q.calls["CompleteTask"], 1);
}

#[test]
fn runs_tasks_and_honours_cancel() {
    let task = |id: &str| json!({"id": id, "name": id, "inputJson": "{\"who\":\"world\"}", "lease": format!("l-{id}"), "idempotencyKey": "k", "traceparent": "tp"});
    let queue = Arc::new(Mutex::new(Queue {
        tasks: ["greet", "flaky", "broken", "panics", "slow"]
            .iter()
            .map(|n| task(n))
            .collect(),
        ..Default::default()
    }));
    let url = serve(queue.clone());

    let mut worker = Worker::new(Client::new(&url, "tok").unwrap(), "team-a")
        .concurrency(5)
        .heartbeat(Duration::from_millis(30))
        .grace_period(Duration::from_secs(2));
    worker.register("greet", |input, info| {
        Ok(json!({"greeting": format!("hello {}", input["who"]), "key": info.idempotency_key, "trace": info.traceparent}))
    });
    worker.register("flaky", |_, _| {
        Err(TaskError::retryable("the mail server timed out"))
    });
    worker.register("broken", |_, _| {
        Err(TaskError::fatal("the address is not valid"))
    });
    worker.register("panics", |_, _| panic!("boom"));
    let slow_ended = Arc::new(AtomicBool::new(false));
    let ended = slow_ended.clone();
    worker.register("slow", move |_, info| {
        let start = Instant::now();
        while !info.is_cancelled() && start.elapsed() < Duration::from_secs(5) {
            thread::sleep(Duration::from_millis(10));
        }
        ended.store(info.is_cancelled(), Ordering::SeqCst);
        Ok(json!({}))
    });

    let stop = Arc::new(AtomicBool::new(false));
    let s = stop.clone();
    let runner = thread::spawn(move || worker.run(&s));
    let start = Instant::now();
    while !slow_ended.load(Ordering::SeqCst) && start.elapsed() < Duration::from_secs(5) {
        thread::sleep(Duration::from_millis(10));
    }
    stop.store(true, Ordering::SeqCst);
    runner.join().unwrap().unwrap();

    assert!(
        slow_ended.load(Ordering::SeqCst),
        "the slow task was not cancelled"
    );
    let q = queue.lock().unwrap();
    let r = &q.results;
    assert_eq!(
        serde_json::from_str::<Value>(r["greet"]["outputJson"].as_str().unwrap()).unwrap(),
        json!({"greeting": "hello world", "key": "k", "trace": "tp"})
    );
    assert_eq!(
        (
            r["flaky"]["error"].as_str(),
            r["flaky"]["retryable"].as_bool()
        ),
        (Some("the mail server timed out"), Some(true))
    );
    assert_eq!(
        (
            r["broken"]["error"].as_str(),
            r["broken"]["retryable"].as_bool()
        ),
        (Some("the address is not valid"), None)
    );
    assert_eq!(
        r["panics"]["error"].as_str(),
        Some("panic in the task function: boom")
    );
    assert!(
        !r.contains_key("slow"),
        "a cancelled task must not send a result"
    );
}
