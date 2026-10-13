-- Roles are cluster-wide. Keep maintenance grants source-specific when the two
-- logical log databases share a PostgreSQL instance; preserve prior checksums.
DO $$ BEGIN
    IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='sicp_upstream_maintenance') THEN
        CREATE ROLE sicp_upstream_maintenance NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
    EXECUTE format('REVOKE ALL ON DATABASE %I FROM sicp_log_maintenance',current_database());
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO sicp_upstream_maintenance',current_database());
END $$;
REVOKE ALL ON SCHEMA logs FROM sicp_log_maintenance;
REVOKE ALL ON logs.commit_state,logs.cleanup_receipts FROM sicp_log_maintenance;
REVOKE ALL ON FUNCTION logs.verify_deployment(TEXT,TEXT),logs.cleanup_preview(TIMESTAMPTZ),logs.cleanup_prefix(TEXT,TEXT,BIGINT,BIGINT,TIMESTAMPTZ) FROM sicp_log_maintenance;
GRANT USAGE ON SCHEMA logs TO sicp_upstream_maintenance;
GRANT SELECT ON logs.commit_state,logs.cleanup_receipts TO sicp_upstream_maintenance;
GRANT EXECUTE ON FUNCTION logs.verify_deployment(TEXT,TEXT),logs.cleanup_preview(TIMESTAMPTZ),logs.cleanup_prefix(TEXT,TEXT,BIGINT,BIGINT,TIMESTAMPTZ) TO sicp_upstream_maintenance;
