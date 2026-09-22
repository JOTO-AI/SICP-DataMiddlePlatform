-- Run as the provisioning administrator on each PostgreSQL instance.
-- Group roles do not log in; supply local LOGIN roles and membership separately.
-- Query logins must never inherit sicp_worker, own schemas, or be superusers.
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='sicp_query') THEN
        CREATE ROLE sicp_query NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='sicp_worker') THEN
        CREATE ROLE sicp_worker NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='sicp_logger') THEN
        CREATE ROLE sicp_logger NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
END $$;
