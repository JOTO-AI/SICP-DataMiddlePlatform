CREATE TABLE control.query_snapshots (
    token TEXT PRIMARY KEY,
    bundle_id TEXT NOT NULL REFERENCES control.bundles(id),
    operation TEXT NOT NULL,
    conditions TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CHECK (expires_at > created_at AND expires_at <= created_at + INTERVAL '1 hour')
);
CREATE INDEX query_snapshots_bundle_expiry ON control.query_snapshots(bundle_id,expires_at);
GRANT SELECT, INSERT ON control.query_snapshots TO sicp_query;
GRANT SELECT, INSERT, DELETE ON control.query_snapshots TO sicp_worker;
