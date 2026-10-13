-- Deploy only after all old producers and dispatchers have drained and stopped.
-- ACCESS EXCLUSIVE waits for existing writers; permission revocation and the
-- new NOT NULL constraint prevent an old binary from resuming direct inserts.
LOCK TABLE logs.events IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='sicp_log_reader') THEN
        CREATE ROLE sicp_log_reader NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
END $$;
CREATE TABLE logs.commit_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    epoch TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    high_water BIGINT NOT NULL CHECK (high_water >= 0),
    floor BIGINT NOT NULL DEFAULT 0 CHECK (floor >= 0 AND floor <= high_water)
);
ALTER TABLE logs.events ADD COLUMN sequence BIGINT;
WITH ranked AS (
    SELECT event_id, row_number() OVER (ORDER BY received_at,event_id) AS n FROM logs.events
) UPDATE logs.events e SET sequence=r.n FROM ranked r WHERE e.event_id=r.event_id;
ALTER TABLE logs.events ALTER COLUMN sequence SET NOT NULL;
ALTER TABLE logs.events ADD CONSTRAINT events_sequence_positive CHECK (sequence > 0);
CREATE UNIQUE INDEX events_sequence ON logs.events(sequence);
INSERT INTO logs.commit_state(singleton,high_water) SELECT true,coalesce(max(sequence),0) FROM logs.events;
-- Tombstones survive payload retention; replays must not become new events.
CREATE TABLE logs.receipts (
    event_id TEXT PRIMARY KEY,
    digest BYTEA NOT NULL,
    sequence BIGINT NOT NULL UNIQUE CHECK (sequence > 0)
);
INSERT INTO logs.receipts SELECT event_id,sha256(convert_to(payload::text,'UTF8')),sequence FROM logs.events;
CREATE INDEX events_time_sequence ON logs.events(occurred_at,sequence);
CREATE INDEX events_task_sequence ON logs.events((payload #>> '{event,identity,task_id}'),sequence);
CREATE INDEX events_request_sequence ON logs.events((payload #>> '{event,identity,request_id}'),sequence);
CREATE INDEX events_test_sequence ON logs.events((payload #>> '{event,identity,test_run_id}'),sequence);
CREATE FUNCTION logs.append_event(p_id TEXT,p_kind TEXT,p_entity TEXT,p_phase TEXT,p_at TIMESTAMPTZ,p_body JSONB)
RETURNS BIGINT LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE n BIGINT; prior BYTEA; body_hash BYTEA;
BEGIN
    IF octet_length(p_body::text)>262144 OR p_body #>> '{event,event_id}' IS DISTINCT FROM p_id
       OR p_body #>> '{event,kind}' IS DISTINCT FROM p_kind
       OR p_body #>> '{event,entity_id}' IS DISTINCT FROM p_entity
       OR p_body #>> '{event,phase}' IS DISTINCT FROM p_phase
       OR (p_body #>> '{event,occurred_at}')::timestamptz IS DISTINCT FROM p_at THEN
        RAISE EXCEPTION 'invalid event';
    END IF;
    -- Held through COMMIT, including duplicate detection and counter update.
    SELECT high_water INTO STRICT n FROM logs.commit_state WHERE singleton FOR UPDATE;
    body_hash := sha256(convert_to(p_body::text,'UTF8'));
    SELECT digest,sequence INTO prior,n FROM logs.receipts WHERE event_id=p_id;
    IF FOUND THEN
        IF prior<>body_hash THEN RETURN -1; END IF;
        RETURN n;
    END IF;
    UPDATE logs.commit_state SET high_water=high_water+1 WHERE singleton RETURNING high_water INTO n;
    INSERT INTO logs.events(event_id,kind,entity_id,phase,occurred_at,payload,sequence)
        VALUES(p_id,p_kind,p_entity,p_phase,p_at,p_body,n);
    INSERT INTO logs.receipts VALUES(p_id,body_hash,n);
    RETURN n;
END $$;
REVOKE ALL ON FUNCTION logs.append_event(TEXT,TEXT,TEXT,TEXT,TIMESTAMPTZ,JSONB) FROM PUBLIC;
REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON logs.events FROM sicp_logger;
REVOKE ALL ON logs.commit_state,logs.receipts FROM PUBLIC,sicp_logger;
GRANT SELECT ON logs.commit_state TO sicp_logger;
GRANT EXECUTE ON FUNCTION logs.append_event(TEXT,TEXT,TEXT,TEXT,TIMESTAMPTZ,JSONB) TO sicp_logger;
GRANT USAGE ON SCHEMA logs TO sicp_log_reader;
GRANT SELECT ON logs.events,logs.commit_state TO sicp_log_reader;
GRANT EXECUTE ON FUNCTION logs.verify_deployment(TEXT,TEXT) TO sicp_log_reader;
DO $$ BEGIN EXECUTE format('GRANT CONNECT ON DATABASE %I TO sicp_log_reader',current_database()); END $$;
