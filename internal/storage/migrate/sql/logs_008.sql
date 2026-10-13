-- Serialize recovery reads with an uncertain prior delete transaction.
CREATE FUNCTION logs.cleanup_result(p_job TEXT,p_epoch TEXT,p_through BIGINT)
RETURNS BIGINT LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE receipt RECORD;
BEGIN
 PERFORM 1 FROM logs.commit_state WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'cleanup state unavailable'; END IF;
 SELECT * INTO receipt FROM logs.cleanup_receipts WHERE job_id=p_job;
 IF NOT FOUND THEN RETURN NULL; END IF;
 IF receipt.epoch<>p_epoch OR receipt.through<>p_through THEN RAISE EXCEPTION 'cleanup conflict'; END IF;
 RETURN receipt.deleted;
END $$;
REVOKE ALL ON FUNCTION logs.cleanup_result(TEXT,TEXT,BIGINT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION logs.cleanup_result(TEXT,TEXT,BIGINT) TO sicp_log_maintenance;

-- A deliberate stale/protected refusal is distinguishable from an ambiguous
-- connection failure. PL/pgSQL rolls back the inner call on its P0001 exception.
CREATE FUNCTION logs.cleanup_try_prefix(p_job TEXT,p_epoch TEXT,p_floor BIGINT,p_through BIGINT,cutoff TIMESTAMPTZ)
RETURNS BIGINT LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 RETURN logs.cleanup_prefix(p_job,p_epoch,p_floor,p_through,cutoff);
EXCEPTION WHEN SQLSTATE 'P0001' THEN RETURN -1;
END $$;
REVOKE ALL ON FUNCTION logs.cleanup_try_prefix(TEXT,TEXT,BIGINT,BIGINT,TIMESTAMPTZ) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION logs.cleanup_try_prefix(TEXT,TEXT,BIGINT,BIGINT,TIMESTAMPTZ) TO sicp_log_maintenance;
