-- Permanent control retirement confirmations, never guessed from log payload.
CREATE TABLE logs.retired_references(ref TEXT PRIMARY KEY);
REVOKE ALL ON logs.retired_references FROM PUBLIC;
CREATE FUNCTION logs.event_reference(p JSONB,k TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE SET search_path=pg_catalog AS $$
 SELECT CASE
 WHEN coalesce(p #>> '{event,identity,task_id}','')<>'' THEN 'task:'||(p #>> '{event,identity,task_id}')
 WHEN coalesce(p #>> '{event,identity,test_run_id}','')<>'' THEN 'test:'||(p #>> '{event,identity,test_run_id}')
 WHEN k='audit' THEN 'audit:'||(p #>> '{event,entity_id}')
 ELSE NULL END
$$;
CREATE FUNCTION logs.cleanup_references() RETURNS JSONB
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT coalesce(jsonb_agg(DISTINCT ref),'[]'::jsonb) FROM (
 SELECT logs.event_reference(e.payload,e.kind) AS ref FROM logs.events e,logs.commit_state s
 WHERE s.singleton AND e.sequence>s.floor ORDER BY e.sequence LIMIT 1000
 ) q WHERE ref IS NOT NULL
$$;
CREATE FUNCTION logs.cleanup_preview(cutoff TIMESTAMPTZ,proposed JSONB)
RETURNS TABLE(epoch TEXT,floor BIGINT,through BIGINT,count BIGINT)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 WITH state AS(SELECT * FROM logs.commit_state WHERE singleton),
 candidates AS(SELECT e.*,logs.event_reference(e.payload,e.kind) AS ref FROM logs.events e,state s WHERE sequence>s.floor ORDER BY sequence LIMIT 1000),
 stop AS(SELECT coalesce(min(sequence),(SELECT high_water+1 FROM state)) AS n FROM candidates c
 WHERE received_at>=cutoff OR NOT (
  (kind='request' AND ref IS NULL) OR
  (kind IN ('task','step','call','attempt','audit','request') AND ref IS NOT NULL AND
   (proposed ? ref OR EXISTS(SELECT 1 FROM logs.retired_references r WHERE r.ref=c.ref)))
 )), eligible AS(SELECT * FROM candidates WHERE sequence<(SELECT n FROM stop))
 SELECT s.epoch,s.floor,coalesce((SELECT max(sequence) FROM eligible),s.floor),(SELECT count(*) FROM eligible) FROM state s
$$;
CREATE OR REPLACE FUNCTION logs.cleanup_preview(cutoff TIMESTAMPTZ)
RETURNS TABLE(epoch TEXT,floor BIGINT,through BIGINT,count BIGINT)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT * FROM logs.cleanup_preview(cutoff,'[]'::jsonb)
$$;
CREATE FUNCTION logs.confirm_retirements(refs JSONB) RETURNS VOID
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF jsonb_typeof(refs)<>'array' OR jsonb_array_length(refs)>1000 THEN RAISE EXCEPTION 'invalid retirements'; END IF;
 PERFORM 1 FROM logs.commit_state WHERE singleton FOR UPDATE;
 INSERT INTO logs.retired_references SELECT jsonb_array_elements_text(refs) ON CONFLICT DO NOTHING;
END $$;
REVOKE ALL ON FUNCTION logs.event_reference(JSONB,TEXT),logs.cleanup_references(),logs.cleanup_preview(TIMESTAMPTZ,JSONB),logs.confirm_retirements(JSONB) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION logs.cleanup_references(),logs.cleanup_preview(TIMESTAMPTZ,JSONB),logs.confirm_retirements(JSONB) TO sicp_log_maintenance;

CREATE FUNCTION logs.cleanup_references(after_sequence BIGINT,through_sequence BIGINT) RETURNS JSONB
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT coalesce(jsonb_agg(DISTINCT ref),'[]'::jsonb) FROM (
 SELECT logs.event_reference(e.payload,e.kind) AS ref FROM logs.events e
 WHERE e.sequence>after_sequence AND e.sequence<=through_sequence ORDER BY e.sequence LIMIT 1000
 ) q WHERE ref IS NOT NULL
$$;
REVOKE ALL ON FUNCTION logs.cleanup_references(BIGINT,BIGINT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION logs.cleanup_references(BIGINT,BIGINT) TO sicp_log_maintenance;
