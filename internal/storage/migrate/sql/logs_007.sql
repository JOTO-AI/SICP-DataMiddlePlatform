-- The single console administrator reaches logs through service accounts.
-- Query reads; worker runs only the explicitly granted maintenance functions.
DO $$ BEGIN
    IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='sicp_query') THEN
        CREATE ROLE sicp_query NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='sicp_worker') THEN
        CREATE ROLE sicp_worker NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
    END IF;
END $$;
GRANT sicp_log_reader TO sicp_query, sicp_worker;
GRANT sicp_log_maintenance TO sicp_worker;
