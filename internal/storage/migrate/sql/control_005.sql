CREATE TABLE control.resources (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('seal','version')),
    bundle_id TEXT NOT NULL REFERENCES control.bundles(id),
    revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
    retired BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE TABLE control.resource_refs (
    resource_id TEXT NOT NULL REFERENCES control.resources(id),
    owner_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('reader','pagination','backfill','recovery')),
    released BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (resource_id,owner_id,kind)
);
CREATE INDEX resource_refs_live ON control.resource_refs(resource_id) WHERE released=FALSE;
GRANT SELECT,INSERT,UPDATE ON control.resources,control.resource_refs TO sicp_worker;
GRANT SELECT ON control.resources,control.resource_refs TO sicp_query;
GRANT UPDATE (revision) ON control.resources TO sicp_query;
GRANT INSERT,UPDATE ON control.resource_refs TO sicp_query;
