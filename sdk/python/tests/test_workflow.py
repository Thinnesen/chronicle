import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from typing import ClassVar

import pytest

import chronicle

FIXTURE = Path(__file__).parents[2] / "testdata" / "etl.json"


def etl() -> chronicle.Workflow:
    """Build the same workflow as etl() in the Go SDK tests."""
    w = chronicle.Workflow("etl-").label("team", "data").parallelism(4)
    day = w.param("day", "2026-10-17")
    source = 'mkdir -p /tmp/rows && echo "$DAY" > /tmp/rows/day.txt && echo 1204'
    fetch = chronicle.script("fetch", "busybox:1.37", source).env("DAY", day)
    fetch.output_artifact("rows", "/tmp/rows").retry(2, "10s")
    count = chronicle.container("count", "busybox:1.37", "wc", "-l", "/tmp/in/day.txt")
    count.input_artifact("rows", "/tmp/in")
    echo = chronicle.container("echo", "busybox:1.37", "echo")
    echo.args(echo.input("msg"))

    main = chronicle.dag("main").parallelism(2)
    f = main.task("fetch", fetch)
    c = main.task("count", count).after(f).artifact_arg("rows", f.artifact("rows"))
    main.task("report", echo).after(c).arg("msg", f"fetched {f.result()} rows")
    return w.entrypoint(main)


def test_matches_the_go_sdk():
    assert etl().to_dict() == json.loads(FIXTURE.read_text())


@pytest.mark.parametrize(
    ("build", "message"),
    [
        (lambda: chronicle.Workflow("x-").to_dict(), "no entrypoint"),
        (lambda: chronicle.container("c", "busybox").task("a", chronicle.dag("d")), "is not a DAG"),
        (lambda: chronicle.script("s", "busybox", "echo {{inputs.parameters.x}}"), "cannot have placeholders"),
        (
            lambda: _two_templates_with_one_name().to_dict(),
            "two different templates have the name 'c'",
        ),
    ],
)
def test_errors(build, message):
    with pytest.raises(chronicle.WorkflowError, match=message):
        build()


def _two_templates_with_one_name() -> chronicle.Workflow:
    d = chronicle.dag("main")
    d.task("a", chronicle.container("c", "busybox"))
    d.task("b", chronicle.container("c", "alpine"))
    return chronicle.Workflow("x-").entrypoint(d)


class _Stub(BaseHTTPRequestHandler):
    requests: ClassVar[list] = []

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        _Stub.requests.append((self.path, self.headers["Authorization"], body))
        if body["namespace"] == "forbidden":
            answer, status = {"code": "permission_denied", "message": "workflows is forbidden"}, 403
        else:
            answer, status = {"summary": {"name": "hello-x1", "phase": "Running"}}, 200
        data = json.dumps(answer).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *args):
        pass


def test_client():
    server = HTTPServer(("127.0.0.1", 0), _Stub)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        client = chronicle.Client(f"http://127.0.0.1:{server.server_port}", "tok")
        w = chronicle.Workflow("hello-").entrypoint(chronicle.container("hi", "busybox", "echo", "hi"))
        assert client.submit("team-a", w) == "hello-x1"
        path, auth, body = _Stub.requests[-1]
        assert path == "/chronicle.v1.WorkflowService/SubmitWorkflow"
        assert auth == "Bearer tok"
        assert body["workflow"]["kind"] == "Workflow"
        with pytest.raises(chronicle.APIError) as err:
            client.submit("forbidden", w)
        assert err.value.code == "permission_denied"
    finally:
        server.shutdown()


def test_client_refuses_other_schemes():
    with pytest.raises(ValueError, match="http"):
        chronicle.Client("file:///etc/passwd", "tok")
