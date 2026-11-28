import json
import signal
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import chronicle


class _Queue:
    """A fake TaskService with four tasks. It cancels the task "slow" on its first heartbeat."""

    def __init__(self) -> None:
        names = ["greet", "flaky", "broken", "slow"]
        self.tasks = [
            {
                "id": n,
                "name": n,
                "inputJson": '{"who": "world"}',
                "lease": "l-" + n,
                "idempotencyKey": "k",
                "traceparent": "tp",
            }
            for n in names
        ]
        self.results: dict[str, dict] = {}
        self.lock = threading.Lock()

    def handle(self, method: str, body: dict) -> dict:
        with self.lock:
            if method == "PollTask":
                return {"task": self.tasks.pop(0)} if self.tasks else {}
            if method == "HeartbeatTask":
                return {"cancelled": body["id"] == "slow"}
            self.results[body["id"]] = body
            return {}


# _DROP is an answer of a fake server: it closes the connection and sends nothing.
_DROP = object()


class _Unreliable(_Queue):
    """A fake TaskService with one task. It gives the answers in script before the normal answers."""

    def __init__(self, script: dict[str, list]) -> None:
        super().__init__()
        self.tasks = [{"id": "t", "name": "greet", "inputJson": "{}", "lease": "l"}]
        self.script = script
        self.calls: dict[str, int] = {}

    def handle(self, method: str, body: dict):
        with self.lock:
            self.calls[method] = self.calls.get(method, 0) + 1
            if self.script.get(method):
                return self.script[method].pop(0)
        return super().handle(method, body)


def _run_one(queue: _Unreliable, fn, until) -> None:
    """Run a worker with fn for the task "greet" until the condition until is true, for at most 10 seconds."""
    server = _serve(queue)
    client = chronicle.Client(f"http://127.0.0.1:{server.server_port}", "tok", timeout=2)
    worker = chronicle.Worker(client, "team-a", heartbeat=0.05, grace_period=5)
    worker.task("greet")(fn)
    stop = threading.Event()
    runner = threading.Thread(target=worker.run, args=(stop,))
    runner.start()
    deadline = time.monotonic() + 10
    while not until() and time.monotonic() < deadline:
        time.sleep(0.02)
    stop.set()
    runner.join(10)
    server.shutdown()


def test_complete_task_retries_a_transient_error():
    unavailable = (503, {"code": "unavailable", "message": "the server restarts"})
    queue = _Unreliable({"CompleteTask": [(503, None), unavailable, _DROP]})
    _run_one(queue, lambda data, task: {"ok": "yes"}, lambda: "t" in queue.results)
    assert json.loads(queue.results["t"]["outputJson"]) == {"ok": "yes"}
    assert queue.calls["CompleteTask"] == 4


def test_complete_task_does_not_retry_a_permanent_error():
    lost = (400, {"code": "failed_precondition", "message": "the lease is lost"})
    queue = _Unreliable({"CompleteTask": [lost, lost]})
    _run_one(queue, lambda data, task: {}, lambda: queue.calls.get("CompleteTask"))
    assert queue.calls["CompleteTask"] == 1


def test_heartbeats_survive_a_lost_connection():
    queue = _Unreliable({"HeartbeatTask": [_DROP]})

    def greet(data, task):
        deadline = time.monotonic() + 5
        while queue.calls.get("HeartbeatTask", 0) < 3 and time.monotonic() < deadline:
            time.sleep(0.02)
        return {}

    _run_one(queue, greet, lambda: "t" in queue.results)
    assert queue.calls["HeartbeatTask"] >= 3


def _serve(queue: _Queue) -> ThreadingHTTPServer:
    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            answer = queue.handle(self.path.rsplit("/", 1)[1], body)
            if answer is _DROP:
                self.close_connection = True  # No answer: the client sees a lost connection.
                return
            status, answer = answer if isinstance(answer, tuple) else (200, answer)
            data = b"" if answer is None else json.dumps(answer).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, *args):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server


def test_worker():
    queue = _Queue()
    server = _serve(queue)
    client = chronicle.Client(f"http://127.0.0.1:{server.server_port}", "tok")
    worker = chronicle.Worker(client, "team-a", concurrency=4, heartbeat=0.05, grace_period=2)
    stop = threading.Event()
    slow_ended = threading.Event()

    @worker.task("greet")
    def greet(data, task):
        return {"greeting": "hello " + data["who"], "key": task.idempotency_key, "trace": task.traceparent}

    @worker.task("flaky")
    def flaky(data, task):
        raise TimeoutError("the mail server timed out")

    @worker.task("broken")
    def broken(data, task):
        raise chronicle.NonRetryable("the address is not valid")

    @worker.task("slow")
    def slow(data, task):
        assert task.cancelled.wait(5), "the task was not cancelled"
        slow_ended.set()
        return {}

    runner = threading.Thread(target=worker.run, args=(stop,))
    runner.start()
    assert slow_ended.wait(5)
    stop.set()
    runner.join(10)
    server.shutdown()

    assert json.loads(queue.results["greet"]["outputJson"]) == {"greeting": "hello world", "key": "k", "trace": "tp"}
    assert queue.results["flaky"]["error"] == "TimeoutError: the mail server timed out"
    assert queue.results["flaky"]["retryable"] is True
    assert queue.results["broken"] == {
        "namespace": "team-a",
        "id": "broken",
        "lease": "l-broken",
        "error": "the address is not valid",
        "retryable": False,
    }
    assert "slow" not in queue.results, "a cancelled task must not send a result"


_HANGING = """
import signal, sys, threading
import chronicle
worker = chronicle.Worker(chronicle.Client(sys.argv[1], "tok"), "team-a", heartbeat=0.05, grace_period=1)
started = threading.Event()

@worker.task("greet")
def greet(data, task):
    started.set()
    threading.Event().wait()  # never returns

stop = chronicle.stop_on_signals()
threading.Thread(target=lambda: started.wait() and print("started", flush=True), daemon=True).start()
worker.run(stop)
"""


def test_a_hanging_task_does_not_keep_the_process_alive():
    """run() waits at most the grace period, and the process ends then, also when a task function hangs."""
    queue = _Queue()
    server = _serve(queue)
    proc = subprocess.Popen(  # noqa: S603 - a fixed test program
        [sys.executable, "-c", _HANGING, f"http://127.0.0.1:{server.server_port}"], stdout=subprocess.PIPE, text=True
    )
    try:
        assert proc.stdout.readline().strip() == "started"
        start = time.monotonic()
        # A second signal right after the first one must not lock the handler.
        proc.send_signal(signal.SIGTERM)
        proc.send_signal(signal.SIGTERM)
        proc.wait(timeout=15)
        assert time.monotonic() - start < 10
    finally:
        proc.kill()
        server.shutdown()


def test_a_server_that_is_down_does_not_stop_the_worker():
    """A lost connection is not fatal: the worker polls again, as the Go and Rust workers do."""
    client = chronicle.Client("http://127.0.0.1:9", "tok", timeout=1)
    worker = chronicle.Worker(client, "team-a")
    worker.task("greet")(lambda data, task: {})
    stop = threading.Event()
    runner = threading.Thread(target=worker.run, args=(stop,))
    runner.start()
    runner.join(2.5)
    assert runner.is_alive(), "the worker stopped on a connection error"
    stop.set()
    runner.join(5)
    assert not runner.is_alive()
