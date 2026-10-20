"""Task workers: they take the tasks of task templates from a queue and run Python functions."""

from __future__ import annotations

import json
import logging
import socket
import threading
import time
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Any

from .client import APIError, Client

# The server answers a poll after at most 30 seconds.
_POLL_TIMEOUT = 40.0

_log = logging.getLogger("chronicle")


class NonRetryable(Exception):
    """NonRetryable is a final error: the node gets the phase Error, which the default retry policy does not retry."""


@dataclass
class TaskInfo:
    """TaskInfo describes one delivery of a task."""

    id: str
    name: str
    workflow: str
    node_name: str
    # idempotency_key is the same for all attempts of a node. Use it to make a repeated task harmless.
    idempotency_key: str
    # cancelled is set when the workflow does not need the result any more. Check it in long work.
    cancelled: threading.Event = field(default_factory=threading.Event)


TaskFunc = Callable[[dict[str, str], TaskInfo], Any]


@dataclass
class _Delivery:
    info: TaskInfo
    lease: str
    input_json: str


class Worker:
    """Worker takes tasks of one queue in one namespace and runs the registered functions in threads."""

    def __init__(
        self,
        client: Client,
        namespace: str,
        *,
        queue: str = "default",
        concurrency: int = 1,
        name: str | None = None,
        heartbeat: float = 5.0,
        grace_period: float = 30.0,
    ) -> None:
        self._client = client
        self._namespace = namespace
        self._queue = queue
        self._concurrency = max(concurrency, 1)
        self._name = name or socket.gethostname()
        self._heartbeat = heartbeat
        self._grace_period = grace_period
        self._funcs: dict[str, TaskFunc] = {}

    def task(self, name: str) -> Callable[[TaskFunc], TaskFunc]:
        """Register a function for the task type name. The function gets the input and a TaskInfo."""

        def register(fn: TaskFunc) -> TaskFunc:
            self._funcs[name] = fn
            return fn

        return register

    def run(self, stop: threading.Event | None = None) -> None:
        """Poll for tasks until stop is set. Then wait up to the grace period for the running tasks."""
        if not self._funcs:
            raise ValueError("register at least one task function")
        stop = stop or threading.Event()
        slots = threading.Semaphore(self._concurrency)
        try:
            while not stop.is_set():
                if not slots.acquire(timeout=1):
                    continue
                try:
                    task = self._poll()
                except APIError as err:
                    slots.release()
                    if err.code in ("permission_denied", "unauthenticated"):
                        raise
                    time.sleep(1)
                    continue
                except OSError:
                    # A lost connection, for example while the server restarts: poll again.
                    slots.release()
                    time.sleep(1)
                    continue
                if task is None:
                    slots.release()
                    continue
                # A daemon thread: a task that hangs after the grace period does not keep the process alive.
                threading.Thread(target=self._run_task, args=(task, slots), daemon=True).start()
        finally:
            # Wait for the running tasks: each one holds a slot.
            deadline = time.monotonic() + self._grace_period
            for _ in range(self._concurrency):
                slots.acquire(timeout=max(deadline - time.monotonic(), 0))

    def _run_task(self, d: _Delivery, slots: threading.Semaphore) -> None:
        try:
            self._handle(d)
        finally:
            slots.release()

    def _poll(self) -> _Delivery | None:
        body = {"namespace": self._namespace, "queue": self._queue, "names": sorted(self._funcs), "worker": self._name}
        task = self._client._call("PollTask", body, service="TaskService", timeout=_POLL_TIMEOUT).get("task")
        if not task:
            return None
        info = TaskInfo(
            id=task["id"],
            name=task["name"],
            workflow=task.get("workflow", ""),
            node_name=task.get("nodeName", ""),
            idempotency_key=task.get("idempotencyKey", ""),
        )
        return _Delivery(info, task.get("lease", ""), task.get("inputJson", "{}"))

    def _handle(self, d: _Delivery) -> None:
        done = threading.Event()
        lost = threading.Event()
        beats = threading.Thread(target=self._beat, args=(d, done, lost), daemon=True)
        beats.start()
        try:
            output, failure, retryable = self._call(d)
        finally:
            done.set()
        if lost.is_set():
            return  # The task was cancelled or the lease was lost: the server refuses a result.
        body = {"namespace": self._namespace, "id": d.info.id, "lease": d.lease}
        body.update({"outputJson": output} if failure is None else {"error": failure, "retryable": retryable})
        # When the task timed out meanwhile, or the result is too large, the server refuses the result. The workflow
        # handles the task; the worker logs it.
        try:
            self._client._call("CompleteTask", body, service="TaskService")
        except APIError as err:
            _log.warning("the server did not take the result of task %s: %s", d.info.id, err)

    def _call(self, d: _Delivery) -> tuple[str, str | None, bool]:
        fn = self._funcs.get(d.info.name)
        if fn is None:
            return "", f"the worker has no function for the task {d.info.name!r}", False
        try:
            result = fn(json.loads(d.input_json), d.info)
            return json.dumps(result), None, False
        except NonRetryable as err:
            return "", str(err), False
        except Exception as err:  # Each error of a task function is a result of the task.
            return "", f"{type(err).__name__}: {err}", True

    def _beat(self, d: _Delivery, done: threading.Event, lost: threading.Event) -> None:
        body = {"namespace": self._namespace, "id": d.info.id, "lease": d.lease}
        while not done.wait(self._heartbeat):
            try:
                cancelled = self._client._call("HeartbeatTask", body, service="TaskService").get("cancelled", False)
            except APIError as err:
                cancelled = err.code == "failed_precondition"
            if cancelled:
                lost.set()
                d.info.cancelled.set()
                return
