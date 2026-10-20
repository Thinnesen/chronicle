"""Builders for chronicle workflows. They make the same objects as the Go SDK and the CRD."""

from __future__ import annotations

import json
from typing import Any

API_VERSION = "chronicle.thinnesen.com/v1alpha1"


class WorkflowError(ValueError):
    """WorkflowError is a mistake in a workflow definition."""


class Template:
    """Template is a step definition: a container, a script, an http request, a task or a DAG of tasks."""

    def __init__(self, name: str, body: dict[str, Any], kind: str) -> None:
        self.name = name
        self._kind = kind
        self._body = body
        self._inputs: dict[str, list[dict[str, Any]]] = {}
        self._outputs: dict[str, list[dict[str, Any]]] = {}
        self._extra: dict[str, Any] = {}
        self._tasks: list[Task] = []

    def _container(self) -> dict[str, Any]:
        if self._kind not in ("container", "script"):
            raise WorkflowError(f"template {self.name!r} has no container")
        return self._body

    def args(self, *args: str) -> Template:
        """Set the arguments of the command."""
        self._container()["args"] = list(args)
        return self

    def env(self, name: str, value: str) -> Template:
        """Add an environment variable. Use it to give inputs to a script."""
        self._container().setdefault("env", []).append({"name": name, "value": value})
        return self

    def input(self, name: str, default: str | None = None) -> str:
        """Declare an input parameter and return its placeholder."""
        param: dict[str, Any] = {"name": name}
        if default is not None:
            param["default"] = default
        self._inputs.setdefault("parameters", []).append(param)
        return "{{inputs.parameters." + name + "}}"

    def output_file(self, name: str, path: str) -> Template:
        """Declare an output parameter that gets the contents of a file in the container."""
        self._outputs.setdefault("parameters", []).append({"name": name, "valueFrom": {"path": path}})
        return self

    def input_artifact(self, name: str, path: str) -> Template:
        """Declare an input artifact that the executor puts at path."""
        self._inputs.setdefault("artifacts", []).append({"name": name, "path": path})
        return self

    def output_field(self, name: str) -> Template:
        """Declare an output parameter of a task template. It gets the field of the result with the same name."""
        self._outputs.setdefault("parameters", []).append({"name": name})
        return self

    def output_artifact(self, name: str, path: str) -> Template:
        """Declare an output artifact that the executor saves from path."""
        self._outputs.setdefault("artifacts", []).append({"name": name, "path": path})
        return self

    def retry(self, limit: int, backoff: str | None = None) -> Template:
        """Retry a failed node up to limit times. backoff is the first wait, for example "10s"; it doubles each time."""
        strategy: dict[str, Any] = {"limit": limit}
        if backoff:
            strategy["backoff"] = {"duration": backoff, "factor": 2}
        self._extra["retryStrategy"] = strategy
        return self

    def parallelism(self, n: int) -> Template:
        """Limit the Pods that run at the same time below a node of this DAG or steps template."""
        self._extra["parallelism"] = n
        return self

    def timeout(self, duration: str) -> Template:
        """Fail a node of the template, or one attempt of a retry, after duration, for example "5m"."""
        self._extra["timeout"] = duration
        return self

    def _http(self) -> dict[str, Any]:
        if self._kind != "http":
            raise WorkflowError(f"template {self.name!r} is not an http template")
        return self._body

    def header(self, name: str, value: str) -> Template:
        """Add a header to the request of an http template."""
        self._http().setdefault("headers", []).append({"name": name, "value": value})
        return self

    def header_from_secret(self, name: str, secret: str, key: str) -> Template:
        """Add a header whose value is the key of a Secret in the namespace, for example a token."""
        header = {"name": name, "valueFrom": {"secretKeyRef": {"name": secret, "key": key}}}
        self._http().setdefault("headers", []).append(header)
        return self

    def body(self, body: str) -> Template:
        """Set the body of the request of an http template. It can have placeholders."""
        self._http()["body"] = body
        return self

    def task(self, name: str, template: Template) -> Task:
        """Add a task to a DAG template. The task runs template."""
        if self._kind != "dag":
            raise WorkflowError(f"template {self.name!r} is not a DAG: it cannot have task {name!r}")
        task = Task(name, template)
        self._tasks.append(task)
        return task

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"name": self.name, **self._extra}
        if self._kind == "dag":
            out["dag"] = {"tasks": [t.to_dict() for t in self._tasks]}
        else:
            out[self._kind] = self._body
        if self._inputs:
            out["inputs"] = self._inputs
        if self._outputs:
            out["outputs"] = self._outputs
        return out

    def _children(self) -> list[Template]:
        return [t.template for t in self._tasks]


def container(name: str, image: str, *command: str) -> Template:
    """Return a template that runs an image with a command."""
    body: dict[str, Any] = {"image": image}
    if command:
        body["command"] = list(command)
    return Template(name, body, "container")


def script(name: str, image: str, source: str) -> Template:
    """Return a template that runs a shell script. A script reads its inputs from environment variables."""
    if "{{" in source:
        raise WorkflowError(f"template {name!r}: a script source cannot have placeholders, use env()")
    return Template(name, {"image": image, "source": source}, "script")


def task_template(
    name: str,
    task_type: str,
    *,
    queue: str | None = None,
    heartbeat_timeout: str | None = None,
    schedule_to_start_timeout: str | None = None,
) -> Template:
    """Return a template that sends its input parameters to a worker that runs the task type task_type."""
    body: dict[str, Any] = {"name": task_type}
    if queue:
        body["queue"] = queue
    if heartbeat_timeout:
        body["heartbeatTimeout"] = heartbeat_timeout
    if schedule_to_start_timeout:
        body["scheduleToStartTimeout"] = schedule_to_start_timeout
    return Template(name, body, "task")


def http_template(name: str, url: str, *, method: str | None = None) -> Template:
    """Return a template that sends one request from a Pod. A status from 200 to 299 is success, and the
    response body is the result. The URL can have placeholders."""
    body: dict[str, Any] = {"url": url}
    if method:
        body["method"] = method
    return Template(name, body, "http")


def dag(name: str) -> Template:
    """Return a template that runs tasks in the order of their dependencies."""
    return Template(name, {}, "dag")


class Task:
    """Task is a task of a DAG. Its methods return placeholders for its outputs."""

    def __init__(self, name: str, template: Template) -> None:
        self.name = name
        self.template = template
        self._fields: dict[str, Any] = {}
        self._params: list[dict[str, str]] = []
        self._artifacts: list[dict[str, str]] = []

    def after(self, *tasks: Task) -> Task:
        """Wait until the other tasks have succeeded or were skipped."""
        self._fields.setdefault("dependencies", []).extend(t.name for t in tasks)
        return self

    def depends(self, expression: str) -> Task:
        """Set a dependency expression, for example "build.Succeeded || build.Failed"."""
        self._fields["depends"] = expression
        return self

    def arg(self, name: str, value: str) -> Task:
        """Give a value to an input parameter of the template."""
        self._params.append({"name": name, "value": value})
        return self

    def artifact_arg(self, name: str, source: str) -> Task:
        """Give an artifact, from the artifact() placeholder of another task, to an input artifact."""
        self._artifacts.append({"name": name, "from": source})
        return self

    def when(self, condition: str) -> Task:
        """Run the task only when the condition is true."""
        self._fields["when"] = condition
        return self

    def continue_on(self, failed: bool = True, error: bool = False) -> Task:
        """Let the run go on after the task fails (failed) or cannot run (error), as Argo's continueOn."""
        self._fields["continueOn"] = {k: True for k, v in (("failed", failed), ("error", error)) if v}
        return self

    def with_items(self, *items: Any) -> Task:
        """Run the task once for each item. The template uses "{{item}}" or "{{item.key}}"."""
        self._fields["withItems"] = list(items)
        return self

    def result(self) -> str:
        """Return the placeholder for the standard output of the task."""
        return "{{tasks." + self.name + ".outputs.result}}"

    def output(self, name: str) -> str:
        """Return the placeholder for an output parameter of the task."""
        return "{{tasks." + self.name + ".outputs.parameters." + name + "}}"

    def artifact(self, name: str) -> str:
        """Return the placeholder for an output artifact of the task, for artifact_arg()."""
        return "{{tasks." + self.name + ".outputs.artifacts." + name + "}}"

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"name": self.name, "template": self.template.name, **self._fields}
        args: dict[str, Any] = {}
        if self._params:
            args["parameters"] = self._params
        if self._artifacts:
            args["artifacts"] = self._artifacts
        if args:
            out["arguments"] = args
        return out


class Workflow:
    """Workflow is a workflow definition. Render it with to_dict() or to_json(), or submit it with a Client."""

    def __init__(self, generate_name: str) -> None:
        self._metadata: dict[str, Any] = {"generateName": generate_name}
        self._params: list[dict[str, str]] = []
        self._spec: dict[str, Any] = {}
        self._entrypoint: Template | None = None
        self._on_exit: Template | None = None

    def name(self, name: str) -> Workflow:
        """Give the run a fixed name instead of a generated one."""
        self._metadata.pop("generateName", None)
        self._metadata["name"] = name
        return self

    def label(self, key: str, value: str) -> Workflow:
        """Add a label to the run."""
        self._metadata.setdefault("labels", {})[key] = value
        return self

    def parallelism(self, n: int) -> Workflow:
        """Limit the Pods of a run that run at the same time."""
        self._spec["parallelism"] = n
        return self

    def mutex(self, name: str) -> Workflow:
        """Let one run with the lock name run at a time in the namespace. Other runs wait."""
        self._spec["synchronization"] = {"mutex": {"name": name}}
        return self

    def semaphore(self, name: str, limit: int) -> Workflow:
        """Let limit runs with the lock name run at the same time in the namespace. Other runs wait."""
        self._spec["synchronization"] = {"semaphore": {"name": name, "limit": limit}}
        return self

    def service_account(self, name: str) -> Workflow:
        """Run the workflow Pods with a Kubernetes service account."""
        self._spec["serviceAccountName"] = name
        return self

    def param(self, name: str, value: str) -> str:
        """Declare a workflow parameter with a default value and return its placeholder."""
        self._params.append({"name": name, "value": value})
        return "{{workflow.parameters." + name + "}}"

    def entrypoint(self, template: Template) -> Workflow:
        """Set the template that the run starts with."""
        self._entrypoint = template
        return self

    def on_exit(self, template: Template) -> Workflow:
        """Set a template that runs after the entrypoint, also after a failure."""
        self._on_exit = template
        return self

    def to_dict(self) -> dict[str, Any]:
        """Return the Workflow object. The server checks it completely when you submit it."""
        if self._entrypoint is None:
            raise WorkflowError("the workflow has no entrypoint")
        templates: dict[str, Template] = {}

        def add(t: Template) -> None:
            prev = templates.get(t.name)
            if prev is not None:
                if prev is not t:
                    raise WorkflowError(f"two different templates have the name {t.name!r}")
                return
            templates[t.name] = t
            for child in t._children():
                add(child)

        add(self._entrypoint)
        spec: dict[str, Any] = {"entrypoint": self._entrypoint.name, **self._spec}
        if self._on_exit is not None:
            add(self._on_exit)
            spec["onExit"] = self._on_exit.name
        if self._params:
            spec["arguments"] = {"parameters": self._params}
        spec["templates"] = [t.to_dict() for t in templates.values()]
        return {"apiVersion": API_VERSION, "kind": "Workflow", "metadata": self._metadata, "spec": spec}

    def to_json(self) -> str:
        """Return the Workflow object as JSON. kubectl and GitOps tools accept JSON as YAML."""
        return json.dumps(self.to_dict(), indent=2)
