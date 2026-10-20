"""Builders for the task, http and child workflow templates."""

from __future__ import annotations

from typing import Any

from .workflow import Template


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


def child_workflow_template(
    name: str, template: str, *, cluster_scope: bool = False, arguments: dict[str, str] | None = None
) -> Template:
    """Return a template that starts a run of the WorkflowTemplate template in the same namespace and waits for it.
    The arguments can have placeholders. Declare the outputs with output_field: they come from the workflow outputs of
    the child run."""
    ref: dict[str, Any] = {"name": template}
    if cluster_scope:
        ref["clusterScope"] = True
    body: dict[str, Any] = {"workflowTemplateRef": ref}
    if arguments:
        body["arguments"] = {"parameters": [{"name": k, "value": v} for k, v in arguments.items()]}
    return Template(name, body, "childWorkflow")
