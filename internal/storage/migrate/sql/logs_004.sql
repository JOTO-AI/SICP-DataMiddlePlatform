CREATE TABLE logs.deployment_identity (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    source TEXT NOT NULL CHECK (source='platform'),
    cluster_id TEXT NOT NULL,
    database_oid BIGINT NOT NULL,
    topology TEXT NOT NULL
);
REVOKE ALL ON logs.deployment_identity FROM PUBLIC, sicp_logger;
CREATE FUNCTION logs.verify_deployment(expected_source TEXT, expected_topology TEXT)
RETURNS BOOLEAN LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog
AS $$ SELECT EXISTS (
    SELECT 1 FROM logs.deployment_identity AS i
    CROSS JOIN pg_catalog.pg_control_system() AS s
    JOIN pg_catalog.pg_database AS d ON d.datname=pg_catalog.current_database()
    WHERE i.singleton AND i.source=expected_source AND i.topology=expected_topology
      AND i.cluster_id=s.system_identifier::text AND i.database_oid=d.oid::bigint
) $$;
REVOKE ALL ON FUNCTION logs.verify_deployment(TEXT,TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION logs.verify_deployment(TEXT,TEXT) TO sicp_logger;
