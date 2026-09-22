REVOKE CREATE ON SCHEMA public FROM PUBLIC;
DO $$ BEGIN
    EXECUTE format('REVOKE ALL ON DATABASE %I FROM PUBLIC', current_database());
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO sicp_query, sicp_worker', current_database());
END $$;

CREATE SCHEMA control;
REVOKE ALL ON SCHEMA control FROM PUBLIC;
GRANT USAGE ON SCHEMA control TO sicp_worker, sicp_query;

CREATE TABLE control.tasks (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    mode TEXT NOT NULL CHECK (mode IN ('initialize', 'incremental', 'force_full')),
    status TEXT NOT NULL,
    requested_by TEXT NOT NULL,
    predecessor_id TEXT REFERENCES control.tasks(id),
    accepted_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    ready_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    checked_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    owner_id TEXT,
    owner_generation BIGINT NOT NULL DEFAULT 0 CHECK (owner_generation >= 0),
    roles JSONB,
    summary JSONB NOT NULL DEFAULT '{}'
);
CREATE TABLE control.steps (
    task_id TEXT NOT NULL REFERENCES control.tasks(id),
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    evidence JSONB,
    PRIMARY KEY (task_id, name)
);
CREATE TABLE control.checkpoints (
    task_id TEXT NOT NULL REFERENCES control.tasks(id),
    name TEXT NOT NULL,
    payload JSONB NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (task_id, name)
);
CREATE TABLE control.schedule (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    revision BIGINT NOT NULL DEFAULT 0,
    last_started_at TIMESTAMPTZ,
    next_due_at TIMESTAMPTZ,
    deferred BOOLEAN NOT NULL DEFAULT FALSE,
    cooldown_until TIMESTAMPTZ
);
INSERT INTO control.schedule (singleton) VALUES (TRUE);
CREATE TABLE control.execution (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    task_id TEXT REFERENCES control.tasks(id),
    owner_id TEXT,
    generation BIGINT NOT NULL DEFAULT 0,
    CHECK ((task_id IS NULL) = (owner_id IS NULL))
);
INSERT INTO control.execution (singleton) VALUES (TRUE);
CREATE TABLE control.bundles (
    id TEXT PRIMARY KEY,
    tables JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    checked_at TIMESTAMPTZ
);
CREATE TABLE control.slots (
    instance TEXT NOT NULL CHECK (instance IN ('M','S')),
    slot SMALLINT NOT NULL CHECK (slot IN (0,1)),
    generation BIGINT NOT NULL DEFAULT 0 CHECK (generation >= 0),
    bundle_id TEXT REFERENCES control.bundles(id),
    admission_open BOOLEAN NOT NULL DEFAULT FALSE,
    state TEXT NOT NULL,
    PRIMARY KEY (instance, slot)
);
INSERT INTO control.slots (instance,slot,state) VALUES ('M',0,'empty'),('M',1,'empty'),('S',0,'empty'),('S',1,'empty');
CREATE TABLE control.candidates (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES control.tasks(id),
    instance TEXT NOT NULL,
    slot SMALLINT NOT NULL,
    generation BIGINT NOT NULL,
    bundle_id TEXT NOT NULL REFERENCES control.bundles(id),
    owner_generation BIGINT NOT NULL,
    state TEXT NOT NULL,
    evidence JSONB,
    ready_at TIMESTAMPTZ,
    FOREIGN KEY (instance,slot) REFERENCES control.slots(instance,slot),
    UNIQUE (instance,slot,generation)
);
CREATE TABLE control.source_progress (
    source TEXT PRIMARY KEY,
    confirmed_until TIMESTAMPTZ NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL,
    task_id TEXT NOT NULL REFERENCES control.tasks(id)
);
CREATE TABLE control.publication_outbox (
    id TEXT PRIMARY KEY,
    sequence BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    task_id TEXT NOT NULL REFERENCES control.tasks(id),
    kind TEXT NOT NULL CHECK (kind IN ('route_shift','bundle_publish')),
    expected JSONB,
    target JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL,
    applied_at TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX one_pending_publication ON control.publication_outbox ((TRUE)) WHERE status='pending';
CREATE TABLE control.notification_outbox (
    id TEXT PRIMARY KEY,
    failure_key TEXT NOT NULL UNIQUE,
    task_id TEXT NOT NULL REFERENCES control.tasks(id),
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    next_attempt_at TIMESTAMPTZ,
    sent_at TIMESTAMPTZ
);
CREATE TABLE control.readers (
    id TEXT PRIMARY KEY,
    instance TEXT NOT NULL,
    slot SMALLINT NOT NULL,
    generation BIGINT NOT NULL,
    bundle_id TEXT NOT NULL REFERENCES control.bundles(id),
    process_id TEXT NOT NULL,
    admitted_at TIMESTAMPTZ NOT NULL,
    heartbeat_at TIMESTAMPTZ,
    sql_finished_at TIMESTAMPTZ,
    released_at TIMESTAMPTZ,
    FOREIGN KEY (instance,slot) REFERENCES control.slots(instance,slot)
);
CREATE INDEX readers_slot ON control.readers(instance,slot,generation) WHERE released_at IS NULL;

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA control TO sicp_worker;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA control TO sicp_worker;
GRANT SELECT ON control.bundles, control.slots, control.source_progress, control.publication_outbox TO sicp_query;
GRANT SELECT, INSERT, UPDATE ON control.readers TO sicp_query;
