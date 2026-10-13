-- Maintenance grants are separate from readers and dispatchers.
DO $$ BEGIN
    IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='sicp_log_maintenance') THEN
        CREATE ROLE sicp_log_maintenance NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO sicp_log_maintenance',current_database());
END $$;
CREATE TABLE logs.cleanup_receipts(job_id TEXT PRIMARY KEY,epoch TEXT NOT NULL,through BIGINT NOT NULL,deleted BIGINT NOT NULL,at TIMESTAMPTZ NOT NULL);
-- Conservative baseline: control-linked events remain protected until their
-- cross-database retirement coordination is implemented and verified. This
-- rule intentionally stops at the first such record; it never jumps a hole.
CREATE FUNCTION logs.cleanup_preview(cutoff TIMESTAMPTZ)
RETURNS TABLE(epoch TEXT,floor BIGINT,through BIGINT,count BIGINT)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 WITH state AS(SELECT * FROM logs.commit_state WHERE singleton),
 candidates AS(SELECT e.*,row_number() OVER(ORDER BY sequence) AS rn FROM logs.events e,state s WHERE sequence>s.floor ORDER BY sequence LIMIT 1000),
 stop AS(SELECT coalesce(min(sequence),(SELECT high_water+1 FROM state)) AS n FROM candidates
   WHERE received_at>=cutoff OR kind<>'request' OR coalesce(payload #>> '{event,identity,task_id}','')<>'' OR coalesce(payload #>> '{event,identity,test_run_id}','')<>''),
 eligible AS(SELECT * FROM candidates WHERE sequence<(SELECT n FROM stop))
 SELECT s.epoch,s.floor,coalesce((SELECT max(sequence) FROM eligible),s.floor),(SELECT count(*) FROM eligible) FROM state s
$$;
CREATE FUNCTION logs.cleanup_prefix(p_job TEXT,p_epoch TEXT,p_floor BIGINT,p_through BIGINT,cutoff TIMESTAMPTZ)
RETURNS BIGINT LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE current_state RECORD; old RECORD; preview RECORD; removed BIGINT;
BEGIN
 SELECT * INTO STRICT current_state FROM logs.commit_state WHERE singleton FOR UPDATE;
 SELECT * INTO old FROM logs.cleanup_receipts WHERE job_id=p_job;
 IF FOUND THEN
   IF old.epoch<>p_epoch OR old.through<>p_through THEN RAISE EXCEPTION 'cleanup conflict'; END IF;
   RETURN old.deleted;
 END IF;
 IF current_state.epoch<>p_epoch OR current_state.floor<>p_floor OR p_through<p_floor THEN RAISE EXCEPTION 'cleanup stale'; END IF;
 SELECT * INTO STRICT preview FROM logs.cleanup_preview(cutoff);
 IF p_through>preview.through THEN RAISE EXCEPTION 'cleanup protected'; END IF;
 DELETE FROM logs.events WHERE sequence>p_floor AND sequence<=p_through;
 GET DIAGNOSTICS removed=ROW_COUNT;
 UPDATE logs.commit_state SET floor=p_through WHERE singleton;
 INSERT INTO logs.cleanup_receipts VALUES(p_job,p_epoch,p_through,removed,clock_timestamp());
 RETURN removed;
END $$;
REVOKE ALL ON FUNCTION logs.cleanup_preview(TIMESTAMPTZ),logs.cleanup_prefix(TEXT,TEXT,BIGINT,BIGINT,TIMESTAMPTZ) FROM PUBLIC;
REVOKE ALL ON logs.cleanup_receipts FROM PUBLIC,sicp_upstream_logger,sicp_upstream_reader;
GRANT USAGE ON SCHEMA logs TO sicp_log_maintenance;
GRANT SELECT ON logs.commit_state,logs.cleanup_receipts TO sicp_log_maintenance;
GRANT EXECUTE ON FUNCTION logs.verify_deployment(TEXT,TEXT),logs.cleanup_preview(TIMESTAMPTZ),logs.cleanup_prefix(TEXT,TEXT,BIGINT,BIGINT,TIMESTAMPTZ) TO sicp_log_maintenance;
