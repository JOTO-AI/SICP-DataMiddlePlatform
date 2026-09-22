REVOKE CREATE ON SCHEMA public FROM PUBLIC;
DO $$ BEGIN
    EXECUTE format('REVOKE ALL ON DATABASE %I FROM PUBLIC', current_database());
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO sicp_logger', current_database());
END $$;

CREATE SCHEMA logs;
REVOKE ALL ON SCHEMA logs FROM PUBLIC;
GRANT USAGE ON SCHEMA logs TO sicp_logger;

CREATE TABLE logs.events (
    event_id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    phase TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    payload JSONB NOT NULL
);
CREATE INDEX events_entity ON logs.events(entity_id,occurred_at,event_id);
CREATE INDEX events_kind ON logs.events(kind,occurred_at,event_id);
GRANT SELECT, INSERT ON logs.events TO sicp_logger;
