-- The earlier tries of runs that were retried. A retry resets nodes, so the archive keeps the try before it.
CREATE TABLE workflow_tries (
    uid         uuid        NOT NULL REFERENCES workflow_runs (uid) ON DELETE CASCADE,
    try         integer     NOT NULL,
    phase       text        NOT NULL,
    finished_at timestamptz,
    workflow    jsonb       NOT NULL,
    PRIMARY KEY (uid, try)
);
