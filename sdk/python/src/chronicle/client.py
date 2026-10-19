"""Client for the chronicle API server. It uses Connect with JSON, so it needs only the standard library."""

from __future__ import annotations

import json
import ssl
import urllib.error
import urllib.request
from typing import Any
from urllib.parse import urlsplit

from .workflow import Workflow

_PREFIX = "/chronicle.v1."


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
        self._timeout = timeout
        self._context = context

    def submit(self, namespace: str, workflow: Workflow) -> str:
        """Start a run of the workflow in a namespace and return the name of the run."""
        answer = self._call("SubmitWorkflow", {"namespace": namespace, "workflow": workflow.to_dict()})
        return answer["summary"]["name"]

    def phase(self, namespace: str, name: str) -> str:
        """Return the phase of a run: Pending, Running, Succeeded, Failed, Error or Suspended."""
        answer = self._call("GetWorkflow", {"namespace": namespace, "name": name})
        return answer["summary"].get("phase", "")

    def _call(
        self, method: str, body: dict[str, Any], service: str = "WorkflowService", timeout: float | None = None
    ) -> dict[str, Any]:
        request = urllib.request.Request(  # noqa: S310 - the scheme is checked in __init__
            self._server + _PREFIX + service + "/" + method,
            data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "Authorization": "Bearer " + self._token},
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
