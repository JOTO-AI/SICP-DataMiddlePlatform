# Database migrations

Provision PostgreSQL 18.6 on M, S and a separate log instance. Create two business
databases per M/S instance, a separate fixed control database on M and a log
database on the log instance. Configure the six explicit URLs documented in
`configs/database.env.example`. Use the same canonical host name for databases
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
| control | fixed M control database | `control`: tasks, steps, checkpoints, schedule, execution ownership, bundles, slots, candidates, progress, publication/notification outbox and readers |
| logs | independent log database | `logs`: immutable deduplicated events and lookup indexes |

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
