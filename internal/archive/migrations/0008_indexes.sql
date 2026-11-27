-- Indexes for the task poll, the task sweeper and the task list, so that they do not scan or sort all tasks.
-- The migration runs in a transaction, so CREATE INDEX CONCURRENTLY is not possible.
CREATE INDEX IF NOT EXISTS tasks_poll ON tasks (namespace, queue, created_at) WHERE state = 'pending';
CREATE INDEX IF NOT EXISTS tasks_open ON tasks (namespace, workflow, workflow_uid) WHERE state IN ('pending', 'running');
CREATE INDEX IF NOT EXISTS tasks_namespace_created ON tasks (namespace, created_at DESC);
