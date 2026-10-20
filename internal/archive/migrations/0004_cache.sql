-- The results of nodes of templates with memoize, by namespace and key. A new result replaces the old one.
CREATE TABLE step_cache (
    namespace  text        NOT NULL,
    key        text        NOT NULL,
    outputs    jsonb       NOT NULL,
    workflow   text        NOT NULL,
    node       text        NOT NULL,
    saved_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (namespace, key)
);
