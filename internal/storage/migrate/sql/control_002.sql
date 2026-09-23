CREATE TABLE control.reader_sql (
    id TEXT PRIMARY KEY,
    reader_id TEXT NOT NULL REFERENCES control.readers(id),
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    CHECK (finished_at IS NULL OR finished_at >= started_at)
);
CREATE INDEX reader_sql_pending ON control.reader_sql(reader_id) WHERE finished_at IS NULL;
GRANT SELECT, INSERT, UPDATE ON control.reader_sql TO sicp_query;
GRANT SELECT, INSERT, UPDATE, DELETE ON control.reader_sql TO sicp_worker;
