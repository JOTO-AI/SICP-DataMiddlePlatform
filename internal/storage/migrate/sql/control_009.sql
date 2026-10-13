CREATE TABLE control.retention_settings(singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK(singleton),revision BIGINT NOT NULL,platform_days INTEGER NOT NULL CHECK(platform_days BETWEEN 1 AND 3650),upstream_days INTEGER NOT NULL CHECK(upstream_days BETWEEN 1 AND 3650));
INSERT INTO control.retention_settings VALUES(true,1,30,30);
CREATE TABLE control.cleanup_previews(id TEXT PRIMARY KEY,subject TEXT NOT NULL,source TEXT NOT NULL CHECK(source IN ('platform','upstream')),revision BIGINT NOT NULL,epoch TEXT NOT NULL,floor BIGINT NOT NULL,through BIGINT NOT NULL,count BIGINT NOT NULL,cutoff TIMESTAMPTZ NOT NULL,expires_at TIMESTAMPTZ NOT NULL);
GRANT SELECT,INSERT,UPDATE ON control.retention_settings,control.cleanup_previews TO sicp_worker;
