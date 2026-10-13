# Database migrations

Provision PostgreSQL 18.6 on M, S and one separate log instance. Create two business
databases per M/S instance, and three distinct logical databases (`control`,
`logs`, `upstream_logs`) on db-log. Existing control databases on M must follow
the [control relocation procedure](../deploy/CONTROL_DATABASE_MIGRATION.md)
before changing their endpoint; SQL migrations alone do not copy data.
Configure the seven write
target URLs in `.env.example`. Compose always uses dual mode; direct processes
must explicitly set `LOG_STORAGE_MODE=dual` (or `single_legacy` for compatibility).
Use the same canonical host name for databases
on one instance; DNS aliases must not disguise shared instances.

Run `roles.sql` as the provisioning administrator on each instance. It creates
NOLOGIN group roles only. Create deployment-specific LOGIN roles and passwords
outside source control; query logins must not own objects, be superusers or
inherit worker roles. Grant CONNECT only to the appropriate logins, revoke
PUBLIC database privileges and revoke PUBLIC CREATE on the public schema during
provisioning. The query login needs source SELECT and the narrowly specified
control reader-registration grants. Use a separate logger login for log writes.

The canonical, embedded migration files live in `internal/storage/migrate/sql`:

| Domain | Targets | Objects |
| --- | --- | --- |
| business | M/0, M/1, S/0, S/1 | `source`: fourteen source tables, exact column types, TEXT primary keys and read/write group grants |
| control | fixed control database on db-log (M retained for relocation compatibility) | `control`: tasks, steps, checkpoints, schedule, execution ownership, bundles, slots, candidates, progress, publication/notification outbox and readers |
| logs | independent log database | `logs`: immutable deduplicated events and lookup indexes |
| upstream_logs | upstream logical database on the shared log instance | `logs`: call/attempt events, independent ledger and logger/reader grants |

The service adapter and deployment job support dual mode. Identity preflight
requires both log databases to be separate from M/S, and their physical database
identities to differ even when their cluster is shared. `scripts/sicp.sh infra`
provisions missing upstream objects on existing built-in log volumes; external
instances require explicit provisioning. Apply migrations before starting services;
see [configuration and migration boundaries](../development_docs/P28_LOG_STORAGE_CONFIGURATION.md).

Maintenance membership is source-specific: platform uses `sicp_log_maintenance`,
upstream uses `sicp_upstream_maintenance`. The append-only `upstream_logs_005`
migration revokes the former group's upstream grants, preserving old checksums.

`logs_007` and `upstream_logs_006` automatically grant both reader groups to the
existing `sicp_query` / `sicp_worker` service roles, and the source-specific
maintenance groups to `sicp_worker` only. Built-in db-log already provisions these
service logins. External deployments must provision their service LOGIN roles and
matching credentials/memberships on the log instance. The console administrator
uses the existing web login; no additional reader/maintenance URLs are required.
Query never receives cleanup grants and worker does not receive direct table
DELETE/TRUNCATE grants. Old SQL files and their checksums remain unchanged.

`migrate.Catalog` selects the domain plan. `migrate.Apply` receives the explicit
target and database transaction adapter, locks migrations per database and
records immutable version/checksum entries in `migration_meta.versions` in the
same transaction as the DDL. An existing matching checksum is skipped; a
conflict fails and rolls back. A failed or ambiguous commit is never reported
as success; retrying reconciles the persisted ledger. Migration metadata is not
a source table and is not accessible to the service roles.

Do not edit an applied SQL file. Add a new migration version when evolving the
schema and extend the catalog. Code rollback does not undo applied data or DDL;
use a reviewed corrective migration or a verified database backup. Real
PostgreSQL migration/role behavior is verified in I01, after all P/T stages.
P02 unit tests use transaction substitutes and do not connect to PostgreSQL.
