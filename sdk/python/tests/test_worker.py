import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import chronicle


class _Queue:
    """A fake TaskService with four tasks. It cancels the task "slow" on its first heartbeat."""

    def __init__(self) -> None:
        names = ["greet", "flaky", "broken", "slow"]
        self.tasks = [
            {"id": n, "name": n, "inputJson": '{"who": "world"}', "lease": "l-" + n, "idempotencyKey": "k"}
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


def _serve(queue: _Queue) -> ThreadingHTTPServer:
    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            data = json.dumps(queue.handle(self.path.rsplit("/", 1)[1], body)).encode()
            self.send_response(200)
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
        return {"greeting": "hello " + data["who"], "key": task.idempotency_key}

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

    assert json.loads(queue.results["greet"]["outputJson"]) == {"greeting": "hello world", "key": "k"}
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
