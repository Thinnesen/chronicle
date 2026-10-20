-- Indexes for Prune, so that each batch finds the old rows without a full scan.
CREATE INDEX workflow_runs_finished ON workflow_runs (finished_at);
CREATE INDEX tasks_finished ON tasks (finished_at) WHERE state NOT IN ('pending', 'running');
CREATE INDEX step_cache_saved ON step_cache (saved_at);
