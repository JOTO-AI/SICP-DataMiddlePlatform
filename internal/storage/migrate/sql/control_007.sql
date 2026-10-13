CREATE TABLE control.management_jobs (
    id TEXT PRIMARY KEY, subject TEXT NOT NULL, action TEXT NOT NULL CHECK(action IN ('source_test','notification_test','cleanup')),
    idempotency_key TEXT, request_hash TEXT NOT NULL, request JSONB NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('queued','running','succeeded','failed','interrupted','partial_failed')),
    accepted_at TIMESTAMPTZ NOT NULL, deadline TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL, owner TEXT, lease_until TIMESTAMPTZ,
    started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ, summary JSONB NOT NULL DEFAULT '{}',
    UNIQUE(subject,action,idempotency_key), CHECK(octet_length(request::text)<=65536), CHECK(octet_length(summary::text)<=16384)
);
CREATE INDEX management_jobs_due ON control.management_jobs(action,status,accepted_at,id);
CREATE TABLE control.management_audit (
    id TEXT PRIMARY KEY, subject TEXT NOT NULL,
    action TEXT NOT NULL, occurred_at TIMESTAMPTZ NOT NULL, confirmed BOOLEAN NOT NULL DEFAULT false
);
GRANT SELECT,INSERT,UPDATE ON control.management_jobs,control.management_audit TO sicp_worker;
