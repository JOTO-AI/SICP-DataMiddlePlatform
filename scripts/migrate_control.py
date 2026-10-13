"""Offline relocation of the built-in control database, with an explicit cutover.

Only Compose parses dotenv. Passwords and rendered configuration stay in memory.
The Bash entry point runs on the Linux Docker host (bind mounts + flock + fsync).
No operation runs on import; --help is safe on any platform.
"""
import argparse
from contextlib import ExitStack
import hashlib
import hmac
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import sys
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
OLD = 'postgres://db-m:5432/control?sslmode=disable'
NEW = 'postgres://db-log:5432/control?sslmode=disable'
STAGING = 'control_relocation'
APPS = ('console', 'query-gateway', 'query', 'worker')


class MigrationError(RuntimeError):
    pass


def require(condition, message):
    if not condition:
        raise MigrationError(message)


def quote(value):
    return '"' + value.replace('"', '""') + '"'


def literal(value):
    return "'" + value.replace("'", "''") + "'"


def digest(data):
    return hashlib.sha256(data).hexdigest()


def file_digest(path):
    result = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            result.update(block)
    return result.hexdigest()


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def atomic_write(path, data, owner=None):
    # Same-directory replace is atomic. A interrupted temp file is retained for
    # this exact operation and reused on resume, never treated as a log event.
    temp = path.with_name(path.name + '.control-relocation.tmp')
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
    try:
        os.fchmod(fd, 0o600)
        if owner is not None:
            os.fchown(fd, owner.st_uid, owner.st_gid)
        with os.fdopen(fd, 'wb', closefd=False) as stream:
            stream.write(data)
            stream.flush()
            os.fsync(fd)
    finally:
        os.close(fd)
    os.replace(temp, path)
    sync_directory(path.parent)


def updated_env(original):
    text = original.decode('utf-8-sig')
    pattern = r'(?m)^[ \t]*(?:export[ \t]+)?DATABASE_CONTROL_URL[ \t]*=[^\r\n]*'
    require(len(re.findall(pattern, text)) == 1, 'Expected exactly one DATABASE_CONTROL_URL in .env.')
    result = re.sub(pattern, 'DATABASE_CONTROL_URL=' + NEW, text)
    return (b'\xef\xbb\xbf' if original.startswith(b'\xef\xbb\xbf') else b'') + result.encode('utf-8')


def topology(environment, control):
    parts = []
    for key in ('DATABASE_M0_URL', 'DATABASE_M1_URL', 'DATABASE_S0_URL',
                'DATABASE_S1_URL', 'DATABASE_CONTROL_URL', 'DATABASE_LOG_URL',
                'DATABASE_UPSTREAM_LOG_URL'):
        u = urlsplit(control if key == 'DATABASE_CONTROL_URL' else environment[key])
        require(u.hostname and u.port, 'Invalid configured database endpoint.')
        parts.extend([u.hostname.lower() + ':' + str(u.port), u.path.lstrip('/')])
    return digest(json.dumps(parts, separators=(',', ':'), ensure_ascii=False).encode())


def mode_marker(environment, control):
    key = hashlib.sha256(environment['AUDIT_KEY'].encode()).digest()
    mac = hmac.new(key, digestmod=hashlib.sha256)
    for part in ('dual-log-v1', topology(environment, control)):
        raw = part.encode()
        mac.update(struct.pack('>Q', len(raw)))
        mac.update(raw)
    return mac.hexdigest().encode()


class Deployment:
    def __init__(self, root=ROOT):
        self.root = root
        self.base = ['docker', 'compose', '--project-directory', str(root),
                     '--env-file', str(root / '.env'), '--file', str(root / 'compose.yaml')]
        self.admin = None

    def run(self, args, *, data=None, output=None, input_file=None, timeout=1800, env=None):
        try:
            result = subprocess.run(args, input=data, stdin=input_file, stdout=output or subprocess.PIPE,
                                    stderr=subprocess.PIPE, timeout=timeout, env=env)
        except (OSError, subprocess.TimeoutExpired):
            raise MigrationError('Deployment command could not complete; services remain stopped on migration failure.') from None
        require(result.returncode == 0, 'Deployment command failed; no automatic rollback or source deletion was attempted.')
        return result.stdout

    def compose(self, *args, **kwargs):
        return self.run(self.base + list(args), **kwargs)

    def model(self):
        # Compose escapes literal dollars in its reusable JSON representation.
        return json.loads(self.compose('config', '--format', 'json').decode().replace('$$', '$'))

    def sql(self, service, database, sql):
        return self.compose('exec', '-T', service, 'psql', '-X', '-q', '-A', '-t',
                            '-v', 'ON_ERROR_STOP=1', '-U', self.admin, '-d', database,
                            data=sql.encode()).decode().strip()

    def exists(self, service, name):
        return self.sql(service, 'postgres', 'SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=' + literal(name) + ');') == 't'

    def identity(self, service, name):
        raw = self.sql(service, 'postgres', "SELECT json_build_array(s.system_identifier::text,d.oid::bigint)::text "
                       "FROM pg_control_system() s CROSS JOIN pg_database d WHERE d.datname=" + literal(name) + ';')
        require(bool(raw), 'Required database is missing.')
        return json.loads(raw)

    def empty(self, service, name):
        return self.sql(service, name, "SELECT NOT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname NOT IN ('public','information_schema') AND nspname NOT LIKE 'pg_%') "
                        "AND NOT EXISTS(SELECT 1 FROM pg_depend d JOIN pg_namespace n ON d.refclassid='pg_namespace'::regclass AND d.refobjid=n.oid WHERE n.nspname='public') "
                        "AND NOT EXISTS(SELECT 1 FROM pg_event_trigger) "
                        "AND NOT EXISTS(SELECT 1 FROM pg_extension WHERE extname<>'plpgsql') "
                        "AND NOT EXISTS(SELECT 1 FROM pg_publication) AND NOT EXISTS(SELECT 1 FROM pg_subscription);") == 't'

    def fingerprint(self, service, database, directory):
        tables = json.loads(self.sql(service, database, "SELECT coalesce(json_agg(json_build_array(n.nspname,c.relname,c.relkind) ORDER BY n.nspname,c.relname),'[]')::text "
                       "FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('information_schema') AND n.nspname NOT LIKE 'pg_%' AND c.relkind IN ('r','S');"))
        result = {}
        scratch = directory / 'fingerprint.tmp'
        for schema, name, kind in tables:
            table = quote(schema) + '.' + quote(name)
            sql = ('COPY (SELECT last_value,is_called FROM ' + table + ') TO STDOUT;' if kind == 'S' else
                   'COPY (SELECT row_to_json(t)::text FROM ' + table + ' t ORDER BY row_to_json(t)::text COLLATE "C") TO STDOUT;')
            with scratch.open('wb') as output:
                self.compose('exec', '-T', service, 'psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1',
                             '-U', self.admin, '-d', database, data=sql.encode(), output=output)
            result[schema + '.' + name] = file_digest(scratch)
        scratch.unlink(missing_ok=True)
        schema = self.compose('exec', '-T', service, 'pg_dump', '-U', self.admin, '-d', database,
                              '--schema-only', '--no-owner')
        # PostgreSQL 18 randomizes psql restrict tokens. Header version comments
        # are not schema; every DDL, function, grant and constraint remains.
        canonical = b'\n'.join(line for line in schema.splitlines()
                               if not line.startswith((b'--', b'\\restrict ', b'\\unrestrict ')))
        result['schema'] = digest(canonical)
        return result


def validate_model(model, expected):
    services = model['services']
    require(model.get('name'), 'Compose project name is missing.')
    required = {'DATABASE_M0_URL': 'db-m/slot0', 'DATABASE_M1_URL': 'db-m/slot1',
                'DATABASE_S0_URL': 'db-s/slot0', 'DATABASE_S1_URL': 'db-s/slot1',
                'DATABASE_LOG_URL': 'db-log/logs', 'DATABASE_UPSTREAM_LOG_URL': 'db-log/upstream_logs'}
    for role in ('query', 'worker', 'migrate'):
        environment = services[role]['environment']
        require(environment['DATABASE_CONTROL_URL'] == expected, 'Control endpoint must match this migration phase.')
        require(environment.get('LOG_STORAGE_MODE') == 'dual', 'Complete the single-to-dual log upgrade first.')
        for key, target in required.items():
            require(environment[key] == 'postgres://' + target.replace('/', ':5432/', 1) + '?sslmode=disable',
                    'This script supports the standard built-in database endpoints only.')
    require(services['query']['environment']['AUDIT_KEY'] == services['worker']['environment']['AUDIT_KEY'], 'AUDIT_KEY differs between services.')
    require(services['db-m']['environment']['POSTGRES_USER'] == services['db-log']['environment']['POSTGRES_USER'], 'Provisioning usernames differ.')
    return services


def log_directories(services):
    paths = []
    for role in ('query', 'worker'):
        for mount in services[role].get('volumes', []):
            if mount['target'] in ('/var/lib/sicp/logs', '/var/lib/sicp/upstream-logs', '/var/lib/sicp/query-replicas'):
                require(mount['type'] == 'bind', 'Log migration requires standard host bind mounts.')
                path = Path(mount['source'])
                require(path.is_absolute() and path.is_dir() and not path.is_symlink(), 'Log directories must exist on this Linux host without symlinks.')
                if mount['target'].endswith('/query-replicas'):
                    for slot in range(2, 33):
                        for leaf in (str(slot), 'upstream-' + str(slot)):
                            child = path / leaf
                            if child.exists():
                                require(child.is_dir() and not child.is_symlink(), 'Invalid query replica log directory.')
                                paths.append(child)
                else:
                    paths.append(path)
    require(len(paths) >= 4, 'Expected platform/upstream log mounts for both services.')
    return sorted(set(paths))


def lock_journals(stack, paths):
    import fcntl
    for path in paths:
        fd = os.open(path / '.writer.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        stack.callback(os.close, fd)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            raise MigrationError('A log writer is still active; stop every application replica first.') from None


def rebind_journals(paths, old, new, apply=False):
    markers = []
    for path in paths:
        marker = path / '.storage-mode'
        if not marker.exists():
            # Upstream journals have no topology marker. Nonempty *platform*
            # journals must have completed the previous dual cutover.
            if path.name in ('query-logs', 'worker-logs') or path.name.isdigit():
                require(not any(p.name not in ('.writer.lock', '.legacy-target') for p in path.iterdir()),
                        'Complete log-register/log-cutover before relocating a nonempty legacy journal.')
            continue
        require(not marker.is_symlink(), 'Journal marker must be a regular file.')
        value = marker.read_bytes()
        require(value in (old, new), 'Journal topology or AUDIT_KEY differs; refusing to reinterpret buffered events.')
        markers.append(marker)
    if apply:
        for marker in markers:
            # Destinations, business endpoints and AUDIT_KEY are unchanged.
            # Preserve every event file, identifier and lock inode.
            atomic_write(marker, new, marker.stat())


def save_state(directory, state):
    atomic_write(directory / 'state.json', json.dumps(state, indent=2).encode())


def verify_backup(directory, state):
    require(file_digest(directory / 'control.dump') == state['dump_sha256'], 'Control backup checksum mismatch.')


def migrate(deploy, directory):
    state_file = directory / 'state.json'
    require(not state_file.exists(), 'Migration state already exists. Use resume or finalize; do not overwrite it.')
    require(not any(directory.iterdir()), 'Use a new, empty backup directory for each migration.')
    model = deploy.model()
    services = validate_model(model, OLD)
    deploy.admin = services['db-m']['environment']['POSTGRES_USER']
    require('DATABASE_CONTROL_URL' not in os.environ, 'Unset the shell DATABASE_CONTROL_URL override before migration.')
    print('Building images before the maintenance window...', flush=True)
    deploy.compose('build', timeout=3600)
    deploy.compose('stop', *APPS)
    deploy.compose('up', '-d', '--wait', '--wait-timeout', '120', 'db-m', 'db-s', 'db-log', 'redis')
    require(deploy.exists('db-m', 'control'), 'Original control database is missing.')
    require(not deploy.exists('db-log', STAGING), 'A staging database already exists; inspect it before retrying.')
    if deploy.exists('db-log', 'control'):
        require(deploy.empty('db-log', 'control'), 'Destination control is not empty; refusing to overwrite it.')
    require(deploy.sql('db-m', 'control', 'SELECT to_regclass(\'control.tasks\') IS NOT NULL AND to_regclass(\'migration_meta.versions\') IS NOT NULL;') == 't', 'Original schema or migration ledger is missing.')
    for service in ('db-m', 'db-log'):
        require(deploy.sql(service, 'postgres', "SELECT count(*)=2 AND bool_and(rolcanlogin AND NOT rolsuper) FROM pg_roles WHERE rolname IN ('sicp_query','sicp_worker');") == 't', 'Service roles must exist and must not be superusers.')
    require(deploy.sql('db-m', 'postgres', 'SHOW server_version_num;') == deploy.sql('db-log', 'postgres', 'SHOW server_version_num;'), 'Source and destination PostgreSQL versions must match.')
    # The built-in deployment uses the image's template0 locale/encoding and
    # no per-database settings. Refuse custom databases rather than lose these
    # attributes in the staging restore (which intentionally omits --create).
    attributes = "SELECT json_build_array(encoding,datlocprovider,datcollate,datctype,datlocale,daticurules)::text FROM pg_database WHERE datname="
    require(deploy.sql('db-m', 'postgres', attributes + "'control';") == deploy.sql('db-log', 'postgres', attributes + "'template0';"), 'Custom source locale/encoding requires a reviewed manual restore.')
    require(deploy.sql('db-m', 'postgres', "SELECT NOT EXISTS(SELECT 1 FROM pg_db_role_setting WHERE setdatabase=(SELECT oid FROM pg_database WHERE datname='control'));") == 't', 'Custom per-database settings require a reviewed manual restore.')
    paths = log_directories(services)
    old_marker, new_marker = mode_marker(services['query']['environment'], OLD), mode_marker(services['query']['environment'], NEW)
    with ExitStack() as stack:
        lock_journals(stack, paths)
        rebind_journals(paths, old_marker, new_marker)
        original = (deploy.root / '.env').read_bytes()
        state = {'phase': 'prepared', 'project': model['name'], 'source': deploy.identity('db-m', 'control'),
                 'target_cluster': deploy.identity('db-log', 'postgres')[0], 'admin': deploy.admin,
                 'env_sha256': digest(original), 'new_env_sha256': digest(updated_env(original)),
                 'old_marker': old_marker.decode(), 'new_marker': new_marker.decode(),
                 'paths': [str(p) for p in paths]}
        require(state['source'][0] != state['target_cluster'], 'Source and destination unexpectedly share a cluster.')
        atomic_write(directory / 'env.before', original)
        save_state(directory, state)
    resume(deploy, directory)


def resume(deploy, directory):
    state = json.loads((directory / 'state.json').read_text())
    require(state['phase'] != 'finalized', 'Migration has already been finalized.')
    if state['phase'] == 'started':
        print('Migration already started successfully. Use finalize to remove the sealed old database.')
        return
    deploy.admin = state['admin']
    require('DATABASE_CONTROL_URL' not in os.environ, 'Unset the shell DATABASE_CONTROL_URL override before migration.')
    current = (deploy.root / '.env').read_bytes()
    require(digest(current) in (state['env_sha256'], state['new_env_sha256']), '.env changed outside this migration; reconcile it before resuming.')
    model = deploy.model()
    expected = NEW if digest(current) == state['new_env_sha256'] else OLD
    services = validate_model(model, expected)
    require(model['name'] == state['project'], 'Compose project changed.')
    deploy.compose('stop', *APPS)
    deploy.compose('up', '-d', '--wait', '--wait-timeout', '120', 'db-m', 'db-s', 'db-log', 'redis')
    require(deploy.identity('db-m', 'control') == state['source'] and deploy.identity('db-log', 'postgres')[0] == state['target_cluster'], 'Database instance identity changed.')
    paths = log_directories(services)
    require([str(p) for p in paths] == state['paths'], 'Log directory mounts changed.')
    require(mode_marker(services['query']['environment'], OLD).decode() == state['old_marker'] and mode_marker(services['query']['environment'], NEW).decode() == state['new_marker'], 'Database topology or AUDIT_KEY changed.')
    with ExitStack() as stack:
        lock_journals(stack, paths)
        rebind_journals(paths, state['old_marker'].encode(), state['new_marker'].encode())
        if state['phase'] == 'prepared':
            print('Blocking old service connections and backing up control...', flush=True)
            # Non-superusers cannot reconnect, including an accidentally
            # restarted old worker. The provisioning admin can still dump it.
            deploy.sql('db-m', 'postgres', "ALTER DATABASE control CONNECTION LIMIT 0; SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='control';")
            before = deploy.fingerprint('db-m', 'control', directory)
            with (directory / 'control.dump').open('wb') as output:
                deploy.compose('exec', '-T', 'db-m', 'pg_dump', '-U', deploy.admin, '-Fc', '-d', 'control', output=output)
                output.flush()
                os.fsync(output.fileno())
            require(before == deploy.fingerprint('db-m', 'control', directory), 'Source changed during backup; stop all administrative writers and retry.')
            state['dump_sha256'] = file_digest(directory / 'control.dump')
            state.update(phase='backed_up', fingerprint=before)
            save_state(directory, state)
        verify_backup(directory, state)
        if state['phase'] == 'backed_up':
            # Stage ownership is recorded before CREATE so a failed restore
            # can be resumed without ever replacing a live control database.
            require(not deploy.exists('db-log', STAGING), 'Staging database was created outside this migration.')
            state['phase'] = 'restoring'
            save_state(directory, state)
        if state['phase'] == 'restoring':
            print('Restoring into an isolated staging database...', flush=True)
            if deploy.exists('db-log', STAGING):
                require(state.get('staging_identity') == deploy.identity('db-log', STAGING),
                        'Staging database ownership is unconfirmed; inspect it before recovery.')
                deploy.sql('db-log', 'postgres', 'DROP DATABASE ' + quote(STAGING) + ';')
            deploy.sql('db-log', 'postgres', 'CREATE DATABASE ' + quote(STAGING) + ' TEMPLATE template0;')
            state['staging_identity'] = deploy.identity('db-log', STAGING)
            save_state(directory, state)
            with (directory / 'control.dump').open('rb') as source:
                deploy.compose('exec', '-T', 'db-log', 'pg_restore', '-U', deploy.admin, '--exit-on-error',
                               '--single-transaction', '--no-owner', '-d', STAGING, input_file=source)
            require(state['fingerprint'] == deploy.fingerprint('db-log', STAGING, directory), 'Restored tables, sequences, schema or grants do not match the source.')
            require(state['fingerprint'] == deploy.fingerprint('db-m', 'control', directory), 'Original control changed after backup.')
            state['staging_identity'] = deploy.identity('db-log', STAGING)
            state['phase'] = 'verified'
            save_state(directory, state)
        if state['phase'] == 'verified':
            # Reject writes even by a stale client after this point.
            deploy.sql('db-m', 'postgres', 'ALTER DATABASE control ALLOW_CONNECTIONS false;')
            if deploy.exists('db-log', STAGING):
                require(deploy.identity('db-log', STAGING) == state['staging_identity'], 'Staging identity changed.')
                if deploy.exists('db-log', 'control'):
                    require(deploy.empty('db-log', 'control'), 'Destination control became nonempty.')
                    deploy.sql('db-log', 'postgres', 'DROP DATABASE control;')
                deploy.sql('db-log', 'postgres', 'ALTER DATABASE ' + quote(STAGING) + ' RENAME TO control;')
            require(deploy.identity('db-log', 'control') == state['staging_identity'], 'Restored control identity mismatch.')
            # A non-CREATE pg_restore does not restore database-level ACLs.
            deploy.sql('db-log', 'postgres', 'REVOKE ALL ON DATABASE control FROM PUBLIC; GRANT CONNECT ON DATABASE control TO sicp_query,sicp_worker; ALTER DATABASE control CONNECTION LIMIT -1;')
            state['phase'] = 'cutover'
            save_state(directory, state)
        if state['phase'] == 'cutover':
            print('Applying migrations against db-log/control and rebinding log journals...', flush=True)
            deploy.compose('run', '--rm', '--no-deps', '-e', 'DATABASE_CONTROL_URL=' + NEW, 'migrate')
            rebind_journals(paths, state['old_marker'].encode(), state['new_marker'].encode(), apply=True)
            current = (deploy.root / '.env').read_bytes()
            require(digest(current) in (state['env_sha256'], state['new_env_sha256']), '.env changed during migration.')
            original = (directory / 'env.before').read_bytes()
            require(digest(original) == state['env_sha256'], 'Saved original configuration checksum mismatch.')
            atomic_write(deploy.root / '.env', updated_env(original), (deploy.root / '.env').stat())
            validate_model(deploy.model(), NEW)
            state['phase'] = 'configured'
            save_state(directory, state)
    # Journal locks must be released before any new application process starts.
    if state['phase'] == 'configured':
        deploy.compose('up', '-d', '--wait', '--wait-timeout', '180', 'query-gateway', 'worker', 'console')
        verify_running(deploy, state)
        state['phase'] = 'started'
        save_state(directory, state)
    print('Control now uses db-log/control. The old database is sealed; backups are retained.', flush=True)


def verify_running(deploy, state):
    services = validate_model(deploy.model(), NEW)
    for role in ('query', 'worker'):
        ids = deploy.compose('ps', '-q', role).decode().split()
        expected = int(services[role].get('deploy', {}).get('replicas', 1))
        require(len(ids) == expected and expected > 0, 'An application replica is not running.')
        for container in ids:
            data = json.loads(deploy.run(['docker', 'inspect', container]))[0]
            values = dict(item.split('=', 1) for item in data['Config']['Env'] if '=' in item)
            require(values.get('DATABASE_CONTROL_URL') == NEW, 'A running application still uses the old control endpoint.')
            require(data['State'].get('Health', {}).get('Status') == 'healthy', 'An application is not healthy.')
    require(deploy.identity('db-log', 'control') == state['staging_identity'], 'New control identity changed.')
    # Require real runtime role authentication, not just administrator access.
    for user, password_key in (('sicp_query', 'QUERY_PASSWORD'), ('sicp_worker', 'WORKER_PASSWORD')):
        # Resolve password only inside the container, not in host argv/output.
        deploy.compose('exec', '-T', 'db-log', 'sh', '-c',
                       'export PGPASSWORD="$' + password_key + '"; exec psql -X -q -h 127.0.0.1 -U "$1" -d control -v ON_ERROR_STOP=1',
                       'control-check', user, data=b'SELECT count(*) FROM control.slots;')


def finalize(deploy, directory):
    state = json.loads((directory / 'state.json').read_text())
    if state['phase'] == 'finalized':
        print('Old control has already been deleted; backup retained.')
        return
    require(state['phase'] == 'started', 'Only a successfully started cutover can delete the old database.')
    deploy.admin = state['admin']
    model = deploy.model()
    validate_model(model, NEW)
    require(model['name'] == state['project'], 'Compose project changed.')
    verify_backup(directory, state)
    verify_running(deploy, state)
    if deploy.exists('db-m', 'control'):
        require(deploy.identity('db-m', 'control') == state['source'], 'Old control identity changed; refusing deletion.')
        require(deploy.sql('db-m', 'postgres', "SELECT NOT datallowconn FROM pg_database WHERE datname='control';") == 't', 'Old control is not sealed; refusing deletion.')
        # DROP DATABASE without FORCE refuses any unexpected remaining session.
        deploy.sql('db-m', 'postgres', 'DROP DATABASE control;')
    state['phase'] = 'finalized'
    save_state(directory, state)
    print('Deleted only db-m/control. Business databases, logs and backup files are retained.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=('migrate', 'resume', 'finalize'))
    parser.add_argument('--backup-dir', type=Path, required=True, help='Private backup/state directory outside all live data mounts; reuse it for resume/finalize.')
    parser.add_argument('--drop-old', action='store_true', help='After a successful migrate/resume, verify and delete the sealed old control database.')
    args = parser.parse_args()
    require(sys.platform == 'linux', 'Run this script on the Linux Docker host, not against a remote daemon.')
    require(os.geteuid() == 0, 'Run with sudo/root to lock and preserve ownership of the application journals.')
    os.umask(0o077)
    directory = args.backup_dir.resolve()
    require(not args.backup_dir.is_symlink(), 'Backup directory must not be a symlink.')
    require(directory != ROOT and directory not in ROOT.parents, 'Backup directory cannot be the checkout or its parent.')
    if args.operation == 'migrate':
        require(not directory.exists() or not any(directory.iterdir()), 'Use a new, empty backup directory.')
    else:
        require((directory / 'state.json').is_file() and not (directory / 'state.json').is_symlink(), 'A migration state file is required.')
    import fcntl
    with (ROOT / '.control-migration.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        deploy = Deployment()
        context = json.loads(deploy.run(['docker', 'context', 'inspect']))
        endpoint = context[0]['Endpoints']['docker']['Host']
        if not os.environ.get('DOCKER_CONTEXT'):
            endpoint = os.environ.get('DOCKER_HOST') or endpoint
        require(endpoint.startswith('unix://'), 'Only a local Unix-socket Docker daemon is supported.')
        for service in deploy.model()['services'].values():
            for mount in service.get('volumes', []):
                if mount.get('type') == 'bind':
                    source = Path(mount['source']).resolve()
                    require(directory != source and source not in directory.parents and directory not in source.parents,
                            'Backups cannot contain or be inside a live bind mount.')
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        os.chmod(directory, 0o700)
        if args.operation == 'migrate':
            migrate(deploy, directory)
        elif args.operation == 'resume':
            resume(deploy, directory)
        else:
            finalize(deploy, directory)
        if args.drop_old and args.operation != 'finalize':
            finalize(deploy, directory)


if __name__ == '__main__':
    try:
        main()
    except (MigrationError, OSError, ValueError, KeyError) as error:
        # Never print driver errors, command lines, rendered model or credentials.
        print(str(error) if isinstance(error, MigrationError) else 'Migration stopped; retain backups/state and inspect the failed phase.', file=sys.stderr)
        sys.exit(1)
