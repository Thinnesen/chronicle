-- The login sessions of the UI. All server replicas share them, and they stay after a restart.
-- key is a hash of the session ID: the table never holds a usable cookie value.
CREATE TABLE sessions (
    key        text PRIMARY KEY,
    identity   jsonb       NOT NULL,
    csrf       text        NOT NULL,
    expires_at timestamptz NOT NULL,
    last_used  timestamptz NOT NULL
);

CREATE INDEX sessions_expires_at ON sessions (expires_at);
