-- The task queue of the task templates. Workers take pending tasks with FOR UPDATE SKIP LOCKED.
CREATE TABLE tasks (
    id              text PRIMARY KEY,
    namespace       text        NOT NULL,
    workflow_uid    uuid        NOT NULL,
    workflow        text        NOT NULL,
    node_id         text        NOT NULL,
    node_name       text        NOT NULL,
    idempotency_key text        NOT NULL,
    queue           text        NOT NULL,
    name            text        NOT NULL,
    input           jsonb       NOT NULL,
    -- pending, running, succeeded, failed, error or cancelled
    state           text        NOT NULL DEFAULT 'pending',
    -- lease is the secret of the worker that has the task. Only that worker can send heartbeats and the result.
    lease           text        NOT NULL DEFAULT '',
    worker          text        NOT NULL DEFAULT '',
    output          jsonb,
    message         text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz,
    heartbeat_at    timestamptz,
    finished_at     timestamptz
);

CREATE INDEX tasks_pending ON tasks (namespace, queue, name, created_at) WHERE state = 'pending';
CREATE INDEX tasks_workflow ON tasks (workflow_uid);
