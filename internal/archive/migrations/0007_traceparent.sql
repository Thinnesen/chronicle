-- The trace context of a task, so that the spans of its worker join the trace of the run.
ALTER TABLE tasks ADD COLUMN traceparent text NOT NULL DEFAULT '';
