-- The finished runs. The full Workflow object holds the node history.
CREATE TABLE workflow_runs (
    uid         uuid PRIMARY KEY,
    namespace   text        NOT NULL,
    name        text        NOT NULL,
    phase       text        NOT NULL,
    message     text        NOT NULL DEFAULT '',
    template    text        NOT NULL DEFAULT '',
    labels      jsonb       NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL,
    started_at  timestamptz,
    finished_at timestamptz,
    nodes_done  integer     NOT NULL DEFAULT 0,
    nodes_total integer     NOT NULL DEFAULT 0,
    workflow    jsonb       NOT NULL
);

CREATE INDEX workflow_runs_namespace_created ON workflow_runs (namespace, created_at DESC);
CREATE INDEX workflow_runs_namespace_name ON workflow_runs (namespace, name);
CREATE INDEX workflow_runs_labels ON workflow_runs USING gin (labels);
