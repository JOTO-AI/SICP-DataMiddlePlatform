-- Cluster-wide service roles work on both logical databases; source-specific
-- reader and maintenance groups preserve the existing object grants.
DO $$ BEGIN
    IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='sicp_query') THEN
        CREATE ROLE sicp_query NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='sicp_worker') THEN
        CREATE ROLE sicp_worker NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
END $$;
GRANT sicp_upstream_reader TO sicp_query, sicp_worker;
GRANT sicp_upstream_maintenance TO sicp_worker;
