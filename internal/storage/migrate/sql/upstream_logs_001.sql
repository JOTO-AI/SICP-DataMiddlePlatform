-- Independent upstream database only. Provision these group roles first.
-- Commit sequencing, writer protocol and epoch are appended by P29.
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
DO $$ BEGIN
    EXECUTE format('REVOKE ALL ON DATABASE %I FROM PUBLIC', current_database());
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO sicp_upstream_logger, sicp_upstream_reader', current_database());
END $$;

CREATE SCHEMA logs;
REVOKE ALL ON SCHEMA logs FROM PUBLIC;
GRANT USAGE ON SCHEMA logs TO sicp_upstream_logger, sicp_upstream_reader;

CREATE TABLE logs.events (
    event_id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('call','attempt')),
    entity_id TEXT NOT NULL,
    phase TEXT NOT NULL CHECK (phase IN ('started','finished')),
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    payload JSONB NOT NULL
);
CREATE INDEX events_entity ON logs.events(entity_id,occurred_at,event_id);
CREATE INDEX events_kind ON logs.events(kind,occurred_at,event_id);
CREATE INDEX events_failed_task_attempts ON logs.events
    ((payload #>> '{event,identity,task_id}'), occurred_at DESC, event_id DESC)
    WHERE kind='attempt' AND phase='finished'
      AND payload #>> '{event,outcome}'='failed';
GRANT SELECT, INSERT ON logs.events TO sicp_upstream_logger;
GRANT SELECT ON logs.events TO sicp_upstream_reader;
