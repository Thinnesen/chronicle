"""Client for the chronicle API server. It uses Connect with JSON, so it needs only the standard library."""

from __future__ import annotations

import json
import ssl
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

from .workflow import Workflow

_PREFIX = "/chronicle.v1."


# _HEALABLE are the API error codes that can go away by themselves, for example while the server restarts.
_HEALABLE = frozenset({"unavailable", "unknown", "internal", "deadline_exceeded"})


def _read_token(path: Path) -> str:
    token = path.read_text().strip()
    if not token:
        raise ValueError(f"the token file {path} is empty")
    return token


class APIError(RuntimeError):
    """APIError is an error answer of the server, with its Connect code, for example "permission_denied"."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(f"{code}: {message}")
        self.code = code
        self.message = message


class Client:
    """Client submits workflows.

    The token is an OIDC ID token, or a service account token with the audience "chronicle".
    """

    def __init__(self, server: str, token: str, *, timeout: float = 30, context: ssl.SSLContext | None = None) -> None:
        if urlsplit(server).scheme not in ("http", "https"):
            raise ValueError("the server URL must start with http:// or https://")
        if not token:
            raise ValueError("give a token")
        self._server = server.rstrip("/")
        self._token = token
        self._token_file: Path | None = None
        self._timeout = timeout
        self._context = context

    @classmethod
    def from_token_file(cls, server: str, path: str | Path, **kwargs: Any) -> Client:
        """Return a client that reads its token from a file before each call.

        Use it with a token that Kubernetes rotates, such as a projected service account token, so that a long-running
        worker keeps working. A file that cannot be read raises OSError.
        """
        client = cls(server, _read_token(Path(path)), **kwargs)
        client._token_file = Path(path)
        return client

    def submit(self, namespace: str, workflow: Workflow) -> str:
        """Start a run of the workflow in a namespace and return the name of the run."""
        answer = self._call("SubmitWorkflow", {"namespace": namespace, "workflow": workflow.to_dict()})
        return answer["summary"]["name"]

    def submit_template(self, namespace: str, template: str, parameters: dict[str, str] | None = None) -> str:
        """Start a run of a WorkflowTemplate with parameters and return the name of the run.

        For a ClusterWorkflowTemplate, give the template as "cluster/<name>".
        """
        name = template.removeprefix("cluster/")
        source = {"name": name, "clusterScope": name != template}
        body = {"namespace": namespace, "template": source, "parameters": parameters or {}}
        return self._call("SubmitWorkflow", body)["summary"]["name"]

    def phase(self, namespace: str, name: str) -> str:
        """Return the phase of a run: Pending, Running, Succeeded, Failed, Error or Suspended."""
        answer = self._call("GetWorkflow", {"namespace": namespace, "name": name})
        return answer["summary"].get("phase", "")

    def wait(self, namespace: str, name: str, interval: float = 2.0, timeout: float | None = None) -> str:
        """Read a run every interval seconds until it finishes, and return its final phase: Succeeded, Failed or Error.

        A read that fails with an error that can heal, for example while the server restarts, is tried again.
        With timeout, a TimeoutError comes after that many seconds.
        """
        deadline = None if timeout is None else time.monotonic() + timeout
        while True:
            try:
                phase = self.phase(namespace, name)
                if phase in ("Succeeded", "Failed", "Error"):
                    return phase
            except APIError as err:
                if err.code not in _HEALABLE:
                    raise
            except OSError:
                pass  # A lost connection: read again.
            if deadline is not None and time.monotonic() + interval > deadline:
                raise TimeoutError(f"the run {name} did not finish in {timeout} seconds")
            time.sleep(interval)

    def _bearer(self) -> str:
        return _read_token(self._token_file) if self._token_file else self._token

    def _call(
        self, method: str, body: dict[str, Any], service: str = "WorkflowService", timeout: float | None = None
    ) -> dict[str, Any]:
        request = urllib.request.Request(  # noqa: S310 - the scheme is checked in __init__
            self._server + _PREFIX + service + "/" + method,
            data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "Authorization": "Bearer " + self._bearer()},
            method="POST",
        )
        try:
            wait = timeout or self._timeout
            with urllib.request.urlopen(request, timeout=wait, context=self._context) as response:  # noqa: S310
                return json.load(response)
        except urllib.error.HTTPError as err:
            try:
                detail = json.load(err)
            except ValueError:
                detail = {}
            raise APIError(detail.get("code", str(err.code)), detail.get("message", err.reason)) from None
