-- A shared admission barrier covers every durable dependency registration.
-- Retirement takes the exclusive barrier in its own committed transaction.
CREATE TABLE control.log_retirements(ref TEXT PRIMARY KEY,retired_at TIMESTAMPTZ NOT NULL);
GRANT SELECT ON control.log_retirements TO sicp_worker;
CREATE TABLE control.log_reference_clock(singleton BOOLEAN PRIMARY KEY CHECK(singleton),revision BIGINT NOT NULL);
INSERT INTO control.log_reference_clock VALUES(true,0);
ALTER TABLE control.cleanup_previews ADD COLUMN refs JSONB NOT NULL DEFAULT '[]';

CREATE FUNCTION control.log_reference_lock() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 PERFORM pg_advisory_xact_lock_shared(734219120);
 -- FOR SHARE rejects a repeatable-read snapshot predating a retirement.
 PERFORM revision FROM control.log_reference_clock WHERE singleton FOR SHARE;
 RETURN NULL;
END $$;

-- Eligibility is conservative for unknown legacy identities and live resources.
CREATE FUNCTION control.log_reference_eligible(ref TEXT) RETURNS BOOLEAN
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT EXISTS(SELECT 1 FROM control.log_retirements r WHERE r.ref=$1) OR
 CASE WHEN left($1,5)='task:' THEN EXISTS(
 SELECT 1 FROM control.tasks t WHERE t.id=substr($1,6)
 AND t.status IN ('failed','success_published','success_unchanged') AND t.finished_at IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM control.execution x WHERE x.task_id=t.id)
 AND NOT EXISTS(SELECT 1 FROM control.publication_outbox p WHERE p.task_id=t.id AND p.status='pending')
 AND NOT EXISTS(SELECT 1 FROM control.candidates c WHERE c.task_id=t.id AND (
   EXISTS(SELECT 1 FROM control.slots s WHERE s.bundle_id=c.bundle_id)
   OR EXISTS(SELECT 1 FROM control.readers r WHERE r.bundle_id=c.bundle_id AND (r.released_at IS NULL OR r.sql_finished_at IS NULL))
   OR EXISTS(SELECT 1 FROM control.query_snapshots q WHERE q.bundle_id=c.bundle_id AND q.expires_at>clock_timestamp())
   OR EXISTS(SELECT 1 FROM control.resources r WHERE r.bundle_id=c.bundle_id AND NOT r.retired)
   OR EXISTS(SELECT 1 FROM control.resource_refs f JOIN control.resources r ON r.id=f.resource_id WHERE r.bundle_id=c.bundle_id AND NOT f.released)))
 AND (t.status<>'failed' OR EXISTS(SELECT 1 FROM control.notification_facts f WHERE f.id='task:'||t.id))
 AND NOT EXISTS(SELECT 1 FROM control.notification_outbox n WHERE n.task_id=t.id AND NOT EXISTS(SELECT 1 FROM control.notification_facts f WHERE f.id='schema:'||n.id))
 AND NOT EXISTS(SELECT 1 FROM control.notification_deliveries d WHERE d.status IN ('queued','retry','sending') AND
   (d.fact_id='task:'||t.id OR d.fact_id IN (SELECT 'schema:'||n.id FROM control.notification_outbox n WHERE n.task_id=t.id)))
 ) WHEN left($1,5)='test:' THEN EXISTS(
 SELECT 1 FROM control.management_jobs j WHERE j.id=substr($1,6) AND j.status IN ('succeeded','failed','interrupted','partial_failed')
 AND j.finished_at IS NOT NULL
 AND (j.action<>'source_test' OR j.status<>'failed' OR EXISTS(SELECT 1 FROM control.notification_facts f WHERE f.id='test:'||j.id))
 AND NOT EXISTS(SELECT 1 FROM control.notification_deliveries d WHERE d.fact_id IN ('test:'||j.id,'explicit:'||j.id) AND d.status IN ('queued','retry','sending'))
 ) WHEN left($1,6)='audit:' THEN EXISTS(
 SELECT 1 FROM control.management_audit a WHERE a.confirmed AND encode(sha256(convert_to(a.id,'UTF8')),'hex')=substr($1,7)
 ) ELSE FALSE END
$$;

-- All current rows remain readable. Mutations that could introduce a fresh
-- dependency on retired log history fail, including delayed recovery writers.
CREATE FUNCTION control.guard_retired_log_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE doc JSONB; refs TEXT[]; bundle TEXT; rid TEXT;
BEGIN
 doc=to_jsonb(NEW); refs=ARRAY[]::TEXT[];
 IF TG_TABLE_NAME='tasks' THEN refs=array_append(refs,'task:'||(doc->>'id'));
 ELSIF TG_TABLE_NAME='management_jobs' THEN refs=array_append(refs,'test:'||(doc->>'id'));
 ELSIF TG_TABLE_NAME='management_audit' THEN refs=array_append(refs,'audit:'||encode(sha256(convert_to(doc->>'id','UTF8')),'hex'));
 ELSE
  IF doc->>'task_id' IS NOT NULL THEN refs=array_append(refs,'task:'||(doc->>'task_id')); END IF;
  bundle=doc->>'bundle_id';
  IF TG_TABLE_NAME='resource_refs' THEN SELECT bundle_id INTO bundle FROM control.resources WHERE id=doc->>'resource_id'; END IF;
  IF bundle IS NOT NULL THEN refs=refs||ARRAY(SELECT 'task:'||task_id FROM control.candidates WHERE bundle_id=bundle); END IF;
  IF TG_TABLE_NAME IN ('notification_facts','notification_deliveries','notification_attempts') THEN
   IF TG_TABLE_NAME='notification_attempts' THEN SELECT fact_id INTO rid FROM control.notification_deliveries WHERE id=doc->>'delivery_id';
   ELSE rid=CASE WHEN TG_TABLE_NAME='notification_facts' THEN doc->>'id' ELSE doc->>'fact_id' END; END IF;
   IF left(rid,7)='schema:' THEN refs=refs||ARRAY(SELECT 'task:'||task_id FROM control.notification_outbox WHERE id=substr(rid,8));
   ELSIF left(rid,9)='explicit:' THEN refs=array_append(refs,'test:'||substr(rid,10));
   ELSE refs=array_append(refs,rid); END IF;
  END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM control.log_retirements r WHERE r.ref=ANY(refs)) THEN RAISE EXCEPTION 'log reference retired'; END IF;
 RETURN NEW;
END $$;
DO $$ DECLARE n TEXT; BEGIN
 FOREACH n IN ARRAY ARRAY['tasks','steps','checkpoints','execution','candidates','slots','source_progress','publication_outbox','notification_outbox','readers','query_snapshots','resources','resource_refs','bundle_checks','management_jobs','management_audit','notification_facts','notification_deliveries','notification_attempts'] LOOP
  EXECUTE format('CREATE TRIGGER log_reference_barrier BEFORE INSERT OR UPDATE OR DELETE ON control.%I FOR EACH STATEMENT EXECUTE FUNCTION control.log_reference_lock()',n);
  EXECUTE format('CREATE TRIGGER log_reference_guard BEFORE INSERT OR UPDATE ON control.%I FOR EACH ROW EXECUTE FUNCTION control.guard_retired_log_reference()',n);
 END LOOP;
END $$;
REVOKE ALL ON FUNCTION control.log_reference_lock(),control.guard_retired_log_reference(),control.log_reference_eligible(TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control.log_reference_eligible(TEXT) TO sicp_worker;

CREATE FUNCTION control.retire_log_references(refs JSONB) RETURNS BOOLEAN
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF jsonb_typeof(refs)<>'array' OR jsonb_array_length(refs)>1000 THEN RAISE EXCEPTION 'invalid retirement'; END IF;
 PERFORM pg_advisory_xact_lock(734219120);
 IF EXISTS(SELECT 1 FROM jsonb_array_elements_text(refs) ref WHERE NOT control.log_reference_eligible(ref)) THEN RETURN FALSE; END IF;
 UPDATE control.log_reference_clock SET revision=revision+1 WHERE singleton;
 INSERT INTO control.log_retirements SELECT ref,clock_timestamp() FROM jsonb_array_elements_text(refs) ref ON CONFLICT DO NOTHING;
 RETURN TRUE;
END $$;
REVOKE ALL ON FUNCTION control.retire_log_references(JSONB) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control.retire_log_references(JSONB) TO sicp_worker;
