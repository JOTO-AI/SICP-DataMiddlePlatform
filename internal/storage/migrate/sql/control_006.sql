-- Query routing verifies code-version readiness, without task/credential access.
GRANT SELECT (id,instance,slot,generation,bundle_id,state,ready_at,evidence) ON control.candidates TO sicp_query;
