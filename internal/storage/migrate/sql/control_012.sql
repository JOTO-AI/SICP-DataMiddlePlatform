-- Queue raw control identities transactionally; HMAC keys stay in the process.
-- The bounded indexer computes correlations using the logging sanitizer.
CREATE TABLE control.log_identity_pending(ref TEXT PRIMARY KEY);
CREATE TABLE control.log_identity_map(namespace TEXT NOT NULL,ref TEXT NOT NULL,log_ref TEXT NOT NULL,PRIMARY KEY(namespace,ref),UNIQUE(namespace,log_ref));
GRANT SELECT,INSERT ON control.log_identity_pending,control.log_identity_map TO sicp_worker;
CREATE FUNCTION control.queue_log_identity() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE ref TEXT;
BEGIN
 IF TG_TABLE_NAME='tasks' THEN ref='task:'||NEW.id;
 ELSIF TG_TABLE_NAME='management_jobs' THEN ref='test:'||NEW.id;
 ELSE ref='audit:'||encode(sha256(convert_to(NEW.id,'UTF8')),'hex'); END IF;
 INSERT INTO control.log_identity_pending VALUES(ref) ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;
CREATE TRIGGER queue_log_identity AFTER INSERT ON control.tasks FOR EACH ROW EXECUTE FUNCTION control.queue_log_identity();
CREATE TRIGGER queue_log_identity AFTER INSERT ON control.management_jobs FOR EACH ROW EXECUTE FUNCTION control.queue_log_identity();
CREATE TRIGGER queue_log_identity AFTER INSERT ON control.management_audit FOR EACH ROW EXECUTE FUNCTION control.queue_log_identity();
REVOKE ALL ON FUNCTION control.queue_log_identity() FROM PUBLIC;
INSERT INTO control.log_identity_pending
SELECT 'task:'||id FROM control.tasks UNION ALL SELECT 'test:'||id FROM control.management_jobs UNION ALL SELECT 'audit:'||encode(sha256(convert_to(id,'UTF8')),'hex') FROM control.management_audit
ON CONFLICT DO NOTHING;
