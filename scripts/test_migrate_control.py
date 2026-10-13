"""Relocation failure/retry tests use synthetic databases, never Docker or .env."""
from contextlib import ExitStack
import copy
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import migrate_control as m


def environment():
    result = {'DATABASE_CONTROL_URL': m.OLD, 'LOG_STORAGE_MODE': 'dual', 'AUDIT_KEY': 'synthetic-audit-key'}
    for key, host, database in (
        ('M0', 'db-m', 'slot0'), ('M1', 'db-m', 'slot1'),
        ('S0', 'db-s', 'slot0'), ('S1', 'db-s', 'slot1'),
        ('LOG', 'db-log', 'logs'), ('UPSTREAM_LOG', 'db-log', 'upstream_logs')):
        result['DATABASE_' + key + '_URL'] = f'postgres://{host}:5432/{database}?sslmode=disable'
    return result


class FakeDeployment:
    def __init__(self, root):
        self.root, self.admin = root, 'postgres'
        self.databases = {('db-m', 'control'): ['101', 10], ('db-log', 'postgres'): ['303', 5]}
        self.commands, self.dropped = [], []
        self.restore_failure = self.corrupt_restore = self.nonempty_target = False
        self.sealed = False
        self.started = False

    def model(self):
        env = environment()
        if m.NEW in (self.root / '.env').read_text():
            env['DATABASE_CONTROL_URL'] = m.NEW
        services = {role: {'environment': copy.deepcopy(env)} for role in ('query', 'worker', 'migrate')}
        for role in ('db-m', 'db-log'):
            services[role] = {'environment': {'POSTGRES_USER': 'postgres'}}
        return {'name': 'synthetic-migration', 'services': services}

    def compose(self, *args, **kwargs):
        self.commands.append(args)
        if 'pg_restore' in args and self.restore_failure:
            raise m.MigrationError('synthetic restore failure')
        if 'pg_dump' in args:
            kwargs['output'].write(b'synthetic immutable database backup')
        if args[0] == 'up' and 'worker' in args:
            self.started = True
        return b''

    def exists(self, service, name):
        return (service, name) in self.databases

    def identity(self, service, name):
        return self.databases[(service, name)]

    def empty(self, service, name):
        return not self.nonempty_target

    def fingerprint(self, service, database, directory):
        return {'control.tasks': 'wrong' if self.corrupt_restore and database == m.STAGING else 'same', 'sequence': '42', 'schema': 'same'}

    def sql(self, service, database, sql):
        self.commands.append(('sql', service, database, sql))
        if sql.startswith('SHOW server_version_num'):
            return '180006'
        if sql.startswith('SELECT json_build_array(encoding'):
            return '[6,"b","C.UTF-8","C.UTF-8","C.UTF-8",null]'
        if sql.startswith('CREATE DATABASE'):
            self.databases[(service, m.STAGING)] = ['303', 20]
        if sql.startswith('DROP DATABASE'):
            name = m.STAGING if m.STAGING in sql else 'control'
            del self.databases[(service, name)]
            self.dropped.append((service, name))
        if 'RENAME TO control' in sql:
            self.databases[(service, 'control')] = self.databases.pop((service, m.STAGING))
        if 'ALLOW_CONNECTIONS false' in sql:
            self.sealed = True
        if 'SELECT NOT datallowconn' in sql:
            return 't' if self.sealed else 'f'
        return 't'


class RelocationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / '.env').write_bytes(('DATABASE_CONTROL_URL=' + m.OLD + '\r\nAUDIT_KEY=keep-this\r\n').encode())
        self.backup = self.root / 'backup'
        self.backup.mkdir()
        self.deployment = FakeDeployment(self.root)
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.stack.enter_context(patch.object(m, 'atomic_write', side_effect=lambda path, data, owner=None: path.write_bytes(data)))
        self.stack.enter_context(patch.object(m, 'log_directories', return_value=[]))
        self.stack.enter_context(patch.object(m, 'lock_journals'))
        self.stack.enter_context(patch.object(m, 'verify_running', side_effect=lambda deployment, state: m.require(deployment.started, 'not running')))
        self.stack.enter_context(patch.dict(os.environ, {}, clear=True))

    def test_verified_cutover_then_explicit_deletion(self):
        m.migrate(self.deployment, self.backup)
        self.assertEqual(json.loads((self.backup / 'state.json').read_text())['phase'], 'started')
        self.assertTrue(self.deployment.sealed)
        self.assertTrue(self.deployment.exists('db-m', 'control'))
        self.assertIn(m.NEW, (self.root / '.env').read_text())
        self.assertIn(m.OLD, (self.backup / 'env.before').read_text())
        m.finalize(self.deployment, self.backup)
        self.assertEqual(self.deployment.dropped, [('db-m', 'control')])
        m.finalize(self.deployment, self.backup)
        self.assertEqual(len(self.deployment.dropped), 1)

    def test_restore_failure_keeps_source_and_configuration_then_resumes(self):
        original = (self.root / '.env').read_bytes()
        self.deployment.restore_failure = True
        with self.assertRaises(m.MigrationError):
            m.migrate(self.deployment, self.backup)
        self.assertEqual((self.root / '.env').read_bytes(), original)
        self.assertTrue(self.deployment.exists('db-m', 'control'))
        self.assertFalse(self.deployment.started)
        with self.assertRaises(m.MigrationError):
            m.finalize(self.deployment, self.backup)
        self.deployment.restore_failure = False
        m.resume(self.deployment, self.backup)
        self.assertTrue(self.deployment.started)
        self.assertNotIn(('db-m', 'control'), self.deployment.dropped)

    def test_checksum_mismatch_never_cuts_over(self):
        self.deployment.corrupt_restore = True
        with self.assertRaisesRegex(m.MigrationError, 'do not match'):
            m.migrate(self.deployment, self.backup)
        self.assertIn(m.OLD, (self.root / '.env').read_text())
        self.assertFalse(self.deployment.started)
        self.assertFalse(self.deployment.sealed)

    def test_nonempty_destination_is_never_overwritten(self):
        self.deployment.databases[('db-log', 'control')] = ['303', 99]
        self.deployment.nonempty_target = True
        with self.assertRaisesRegex(m.MigrationError, 'not empty'):
            m.migrate(self.deployment, self.backup)
        self.assertEqual(self.deployment.dropped, [])
        self.assertFalse((self.backup / 'state.json').exists())

    def test_corrupt_backup_or_replaced_source_prevents_deletion(self):
        m.migrate(self.deployment, self.backup)
        self.deployment.databases[('db-m', 'control')] = ['101', 999]
        with self.assertRaisesRegex(m.MigrationError, 'identity changed'):
            m.finalize(self.deployment, self.backup)
        self.deployment.databases[('db-m', 'control')] = ['101', 10]
        (self.backup / 'control.dump').write_bytes(b'corrupt')
        with self.assertRaisesRegex(m.MigrationError, 'checksum mismatch'):
            m.finalize(self.deployment, self.backup)
        self.assertNotIn(('db-m', 'control'), self.deployment.dropped)


class ConfigurationTests(unittest.TestCase):
    def test_env_preserves_credentials_comments_and_line_endings(self):
        original = ('# comment\r\nAUDIT_KEY=secret-$value\r\nDATABASE_CONTROL_URL=\'' + m.OLD + '\'\r\nOTHER=keep\r\n').encode()
        result = m.updated_env(original)
        self.assertEqual(result, original.replace(("'" + m.OLD + "'").encode(), m.NEW.encode()))
        with self.assertRaises(m.MigrationError):
            m.updated_env(original + b'DATABASE_CONTROL_URL=duplicate\n')

    def test_marker_is_bound_to_all_destinations_and_audit_key(self):
        env = environment()
        old, new = m.mode_marker(env, m.OLD), m.mode_marker(env, m.NEW)
        self.assertEqual(old.decode(), '230f2c8f2aaff35a51722d0ac28f1c7f358b0345b38f66d720344e7a9b5373da')
        self.assertEqual(new.decode(), '199e68c92e2e008549e0ef0980bc2805ba7f483e82472af1a986ad767d5d8c5d')
        self.assertEqual(len(old), 64)
        self.assertNotEqual(old, new)
        env['AUDIT_KEY'] += 'changed'
        self.assertNotEqual(m.mode_marker(env, m.NEW), new)
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / 'worker-logs'
            path.mkdir()
            (path / '.storage-mode').write_bytes(old)
            event = path / 'event.json'
            event.write_bytes(b'unchanged buffered event')
            m.rebind_journals([path], old, new)
            with patch.object(m, 'atomic_write', side_effect=lambda p, data, owner=None: p.write_bytes(data)):
                m.rebind_journals([path], old, new, apply=True)
                m.rebind_journals([path], old, new, apply=True)
            self.assertEqual(event.read_bytes(), b'unchanged buffered event')
            (path / '.storage-mode').write_bytes(b'0' * 64)
            with self.assertRaises(m.MigrationError):
                m.rebind_journals([path], old, new, apply=True)


if __name__ == '__main__':
    unittest.main()
