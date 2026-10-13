CREATE TABLE control.notification_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK(singleton), revision BIGINT NOT NULL CHECK(revision>0),
    enabled BOOLEAN NOT NULL, recipients JSONB NOT NULL, rules JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK(jsonb_array_length(recipients)<=20), CHECK(NOT enabled OR jsonb_array_length(recipients)>0)
);
CREATE TABLE control.notification_facts (
    id TEXT PRIMARY KEY, code TEXT NOT NULL, task_id TEXT, occurred_at TIMESTAMPTZ NOT NULL,
    decision TEXT NOT NULL, revision BIGINT NOT NULL
);
CREATE TABLE control.notification_deliveries (
    id TEXT PRIMARY KEY, fact_id TEXT NOT NULL REFERENCES control.notification_facts(id), rule_id TEXT NOT NULL,
    revision BIGINT NOT NULL, recipient TEXT NOT NULL, status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL, deadline TIMESTAMPTZ NOT NULL,
    next_attempt_at TIMESTAMPTZ, owner TEXT, lease_until TIMESTAMPTZ, accepted_at TIMESTAMPTZ,
    -- Legacy used up to 16 attempts; new claims never exceed three.
    UNIQUE(fact_id,rule_id,recipient), CHECK(attempts BETWEEN 0 AND 16),
    CHECK(status IN ('queued','sending','retry','accepted','rejected','unknown','cancelled','exhausted'))
);
CREATE TABLE control.notification_attempts (
    delivery_id TEXT NOT NULL REFERENCES control.notification_deliveries(id), number INTEGER NOT NULL,
    started_at TIMESTAMPTZ NOT NULL, finished_at TIMESTAMPTZ, outcome TEXT NOT NULL,
    PRIMARY KEY(delivery_id,number)
);
CREATE INDEX notification_deliveries_due ON control.notification_deliveries(status,next_attempt_at,created_at,id);
GRANT SELECT,INSERT,UPDATE ON control.notification_settings,control.notification_facts,control.notification_deliveries,control.notification_attempts TO sicp_worker;
