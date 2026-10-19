//! Task workers: they take the tasks of task templates from a queue and run Rust functions in threads.

use std::collections::HashMap;
use std::panic::{AssertUnwindSafe, catch_unwind};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Condvar, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use serde_json::{Value, json};

use crate::client::{ApiError, Client};

/// The server answers a poll after at most 30 seconds.
const POLL_TIMEOUT: Duration = Duration::from_secs(40);

/// An error of a task function.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TaskError {
    /// The retry strategy of the template can run the task again.
    Retryable(String),
    /// The node gets the phase Error, which the default retry policy does not retry.
    Fatal(String),
}

impl TaskError {
    /// Returns an error that the retry strategy can retry.
    pub fn retryable(msg: impl Into<String>) -> Self {
        TaskError::Retryable(msg.into())
    }

    /// Returns a final error.
    pub fn fatal(msg: impl Into<String>) -> Self {
        TaskError::Fatal(msg.into())
    }
}

/// One delivery of a task.
#[derive(Debug, Clone)]
pub struct TaskInfo {
    pub id: String,
    pub name: String,
    pub workflow: String,
    pub node_name: String,
    /// The same for all attempts of a node. Use it to make a repeated task harmless.
    pub idempotency_key: String,
    cancelled: Arc<AtomicBool>,
}

impl TaskInfo {
    /// Tells if the workflow does not need the result any more. Check it in long work.
    pub fn is_cancelled(&self) -> bool {
        self.cancelled.load(Ordering::SeqCst)
    }
}

/// A task function. It gets the input parameters of the node and returns a JSON result.
pub type TaskFn =
    dyn Fn(&HashMap<String, String>, &TaskInfo) -> Result<Value, TaskError> + Send + Sync;

/// Takes tasks of one queue in one namespace and runs the registered functions.
pub struct Worker {
    client: Arc<Client>,
    namespace: String,
    queue: String,
    concurrency: usize,
    name: String,
    heartbeat: Duration,
    grace_period: Duration,
    funcs: HashMap<String, Arc<TaskFn>>,
}

impl Worker {
    /// Returns a worker for the queue "default" that runs one task at a time.
    pub fn new(client: Client, namespace: &str) -> Self {
        Worker {
            client: Arc::new(client),
            namespace: namespace.into(),
            queue: "default".into(),
            concurrency: 1,
            name: std::env::var("HOSTNAME").unwrap_or_else(|_| "rust-worker".into()),
            heartbeat: Duration::from_secs(5),
            grace_period: Duration::from_secs(30),
            funcs: HashMap::new(),
        }
    }

    /// Sets the queue to poll.
    pub fn queue(mut self, queue: &str) -> Self {
        self.queue = queue.into();
        self
    }

    /// Sets the number of tasks that run at the same time.
    pub fn concurrency(mut self, n: usize) -> Self {
        self.concurrency = n.max(1);
        self
    }

    /// Sets the time between two heartbeats. Keep it below the heartbeatTimeout of the templates.
    pub fn heartbeat(mut self, d: Duration) -> Self {
        self.heartbeat = d;
        self
    }

    /// Sets how long run waits for running tasks after the stop signal.
    pub fn grace_period(mut self, d: Duration) -> Self {
        self.grace_period = d;
        self
    }

    /// Registers the function for a task type, the name of a task template.
    pub fn register<F>(&mut self, name: &str, f: F)
    where
        F: Fn(&HashMap<String, String>, &TaskInfo) -> Result<Value, TaskError>
            + Send
            + Sync
            + 'static,
    {
        self.funcs.insert(name.into(), Arc::new(f));
    }

    /// Polls for tasks until stop is true. Then it waits up to the grace period for the running tasks.
    /// It returns an error when it cannot poll, for example without the permission.
    pub fn run(&self, stop: &AtomicBool) -> Result<(), ApiError> {
        let mut names: Vec<&String> = self.funcs.keys().collect();
        names.sort();
        let running = Arc::new((Mutex::new(0usize), Condvar::new()));
        let mut result = Ok(());
        while !stop.load(Ordering::SeqCst) {
            {
                let (count, cv) = &*running;
                let mut n = count.lock().expect("the lock is never poisoned");
                while *n >= self.concurrency {
                    n = cv
                        .wait_timeout(n, Duration::from_secs(1))
                        .expect("the lock is never poisoned")
                        .0;
                    if stop.load(Ordering::SeqCst) {
                        break;
                    }
                }
                if stop.load(Ordering::SeqCst) {
                    break;
                }
                *n += 1;
            }
            let body = json!({"namespace": self.namespace, "queue": self.queue, "names": names, "worker": self.name});
            let polled = self
                .client
                .call("TaskService", "PollTask", body, Some(POLL_TIMEOUT));
            let task = match polled {
                Ok(answer) if answer["task"].is_object() => Some(answer["task"].clone()),
                Ok(_) => None,
                Err(e) if e.code == "permission_denied" || e.code == "unauthenticated" => {
                    result = Err(e);
                    release(&running);
                    break;
                }
                Err(_) => {
                    thread::sleep(Duration::from_secs(1));
                    None
                }
            };
            let Some(task) = task else {
                release(&running);
                continue;
            };
            let job = Job {
                client: self.client.clone(),
                namespace: self.namespace.clone(),
                heartbeat: self.heartbeat,
            };
            let func = self
                .funcs
                .get(task["name"].as_str().unwrap_or_default())
                .cloned();
            let running = running.clone();
            thread::spawn(move || {
                job.handle(&task, func);
                release(&running);
            });
        }
        let deadline = Instant::now() + self.grace_period;
        let (count, cv) = &*running;
        let mut n = count.lock().expect("the lock is never poisoned");
        while *n > 0 && Instant::now() < deadline {
            n = cv
                .wait_timeout(n, deadline.saturating_duration_since(Instant::now()))
                .expect("the lock is never poisoned")
                .0;
        }
        result
    }
}

fn release(running: &(Mutex<usize>, Condvar)) {
    let (count, cv) = running;
    *count.lock().expect("the lock is never poisoned") -= 1;
    cv.notify_all();
}

/// The parts of a worker that a task thread needs.
struct Job {
    client: Arc<Client>,
    namespace: String,
    heartbeat: Duration,
}

impl Job {
    /// Runs one task with heartbeats and sends its result.
    fn handle(&self, task: &Value, func: Option<Arc<TaskFn>>) {
        let text = |k: &str| task[k].as_str().unwrap_or_default().to_string();
        let cancelled = Arc::new(AtomicBool::new(false));
        let info = TaskInfo {
            id: text("id"),
            name: text("name"),
            workflow: text("workflow"),
            node_name: text("nodeName"),
            idempotency_key: text("idempotencyKey"),
            cancelled: cancelled.clone(),
        };
        let lease = text("lease");
        let done = Arc::new(AtomicBool::new(false));
        let beats = self.beat(&info.id, &lease, done.clone(), cancelled.clone());
        let outcome = run_function(func, &text("inputJson"), &info);
        done.store(true, Ordering::SeqCst);
        let _ = beats.join();
        if cancelled.load(Ordering::SeqCst) {
            return; // The task was cancelled or the lease was lost: the server refuses a result.
        }
        let mut body = json!({"namespace": self.namespace, "id": info.id, "lease": lease});
        match outcome {
            Ok(v) => body["outputJson"] = Value::String(v.to_string()),
            Err(TaskError::Retryable(m)) => {
                body["error"] = m.into();
                body["retryable"] = true.into();
            }
            Err(TaskError::Fatal(m)) => body["error"] = m.into(),
        }
        // When the task timed out meanwhile, the server refuses the result and the workflow handles it.
        let _ = self.client.call("TaskService", "CompleteTask", body, None);
    }

    fn beat(
        &self,
        id: &str,
        lease: &str,
        done: Arc<AtomicBool>,
        cancelled: Arc<AtomicBool>,
    ) -> thread::JoinHandle<()> {
        let client = self.client.clone();
        let body = json!({"namespace": self.namespace, "id": id, "lease": lease});
        let every = self.heartbeat;
        thread::spawn(move || {
            let tick = Duration::from_millis(10).min(every);
            let mut next = Instant::now() + every;
            while !done.load(Ordering::SeqCst) {
                thread::sleep(tick);
                if Instant::now() < next {
                    continue;
                }
                next = Instant::now() + every;
                let stop = match client.call("TaskService", "HeartbeatTask", body.clone(), None) {
                    Ok(answer) => answer["cancelled"].as_bool().unwrap_or(false),
                    Err(e) => e.code == "failed_precondition",
                };
                if stop {
                    cancelled.store(true, Ordering::SeqCst);
                    return;
                }
            }
        })
    }
}

/// Runs a task function. A panic becomes a final error.
fn run_function(
    func: Option<Arc<TaskFn>>,
    input: &str,
    info: &TaskInfo,
) -> Result<Value, TaskError> {
    let Some(func) = func else {
        return Err(TaskError::fatal(format!(
            "the worker has no function for the task {:?}",
            info.name
        )));
    };
    let input: HashMap<String, String> = serde_json::from_str(input)
        .map_err(|e| TaskError::fatal(format!("the task input is not a JSON object: {e}")))?;
    match catch_unwind(AssertUnwindSafe(|| func(&input, info))) {
        Ok(result) => result,
        Err(panic) => {
            let msg = panic
                .downcast_ref::<&str>()
                .map(|s| s.to_string())
                .or_else(|| panic.downcast_ref::<String>().cloned())
                .unwrap_or_else(|| "unknown".into());
            Err(TaskError::fatal(format!(
                "panic in the task function: {msg}"
            )))
        }
    }
}
