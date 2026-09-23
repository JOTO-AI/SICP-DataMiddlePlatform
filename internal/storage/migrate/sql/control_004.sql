CREATE TABLE control.bundle_checks (
    bundle_id TEXT NOT NULL REFERENCES control.bundles(id),
    source TEXT NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL,
    task_id TEXT NOT NULL REFERENCES control.tasks(id),
    PRIMARY KEY (bundle_id,source)
);
GRANT SELECT ON control.bundle_checks TO sicp_query;
GRANT SELECT, INSERT, UPDATE, DELETE ON control.bundle_checks TO sicp_worker;
