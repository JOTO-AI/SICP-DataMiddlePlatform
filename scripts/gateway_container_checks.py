"""GW-T02 and GW-I02/I03/I04: real unprivileged Nginx contract fixtures."""
import http.client
import json
import os
from pathlib import Path
import ssl
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]
IMAGE = 'sicp-gateway:contracts'
NETWORK = 'sicp-gateway-contracts'


def command(*args, check=True):
    result = subprocess.run(args, capture_output=True, text=True, timeout=180)
    if check and result.returncode:
        raise AssertionError('fixture command failed: ' + args[0])
    return result


def certificates(directory, name='site', days='2'):
    directory = Path(directory)
    key, csr, cert = (directory / (name + suffix) for suffix in ('.key', '.csr', '.pem'))
    command('openssl', 'req', '-new', '-newkey', 'rsa:2048', '-nodes', '-subj', '/CN=sicp-gateway.test',
            '-keyout', str(key), '-out', str(csr))
    ext = directory / 'extensions'
    ext.write_text('subjectAltName=DNS:sicp-gateway.test\nextendedKeyUsage=serverAuth\n')
    command('openssl', 'x509', '-req', '-in', str(csr), '-CA', str(directory / 'ca.pem'),
            '-CAkey', str(directory / 'ca.key'), '-CAcreateserial', '-days', days,
            '-extfile', str(ext), '-out', str(cert))
    key.chmod(0o644)  # Disposable synthetic fixture; container UID must read it.
    return cert, key


def create_ca(directory):
    directory = Path(directory)
    directory.chmod(0o755)
    command('openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '3',
            '-subj', '/CN=SICP disposable gateway test CA', '-keyout', str(directory / 'ca.key'),
            '-out', str(directory / 'ca.pem'))
    return certificates(directory)


class GatewayContainers(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if os.environ.get('GITHUB_ACTIONS') != 'true':
            raise RuntimeError('isolated Linux CI required')
        cls.directory = tempfile.TemporaryDirectory(prefix='sicp-gateway-certs-')
        cls.cert, cls.key = create_ca(cls.directory.name)
        cls.context = ssl.create_default_context(cafile=str(Path(cls.directory.name) / 'ca.pem'))
        command('docker', 'network', 'create', NETWORK)
        cls.upstreams = []
        for service, port in (('query', '8080'), ('query-second', '8080'), ('worker', '8081'), ('console', '8080')):
            name = NETWORK + '-' + service
            command('docker', 'run', '-d', '--name', name, '--network', NETWORK, '--network-alias', 'query' if service.startswith('query') else service,
                    '-e', 'PORT=' + port, '--mount', 'type=bind,src=' + str(ROOT / 'scripts/fixtures/gateway_echo.py') + ',dst=/echo.py,readonly',
                    'python:3.12-alpine', 'python', '/echo.py')
            cls.upstreams.append(name)

    @classmethod
    def tearDownClass(cls):
        for name in cls.upstreams:
            command('docker', 'rm', '-f', name, check=False)
        command('docker', 'network', 'rm', NETWORK, check=False)
        cls.directory.cleanup()

    def tearDown(self):
        command('docker', 'rm', '-f', NETWORK + '-entry', check=False)

    def gateway(self, tls=False, check=False, cert=None, key=None, env=None, no_network=False):
        args = ['docker', 'run', '--name', NETWORK + '-entry', '--user', '65532:65532', '--read-only',
                '--tmpfs', '/tmp:size=16m,mode=1777', '--network', 'none' if no_network else NETWORK]
        if not check:
            args += ['-d', '-p', '127.0.0.1:28443:8443']
        settings = {'SICP_TLS_CERT_FILE': '/literal/$(not-executed); cert', 'SICP_TLS_KEY_FILE': '/literal/key'} if tls else {}
        settings.update(env or {})
        for name, value in settings.items():
            args += ['-e', name + '=' + value]
        for source, target in ((cert or self.cert if tls else '/dev/null', 'fullchain.pem'),
                               (key or self.key if tls else '/dev/null', 'privkey.pem')):
            args += ['--mount', f'type=bind,src={source},dst=/etc/nginx/tls/{target},readonly']
        result = command(*args, IMAGE, *(['--check-config'] if check else []), check=False)
        if not check and result.returncode == 0:
            for _ in range(50):
                try:
                    self.request('/', tls=tls)
                    break
                except ssl.SSLCertVerificationError:
                    raise
                except OSError:
                    time.sleep(.2)
            else:
                raise AssertionError('gateway did not become reachable')
        return result

    def request(self, path, method='GET', body=None, headers=None, tls=False, context=None):
        connection = (http.client.HTTPSConnection('sicp-gateway.test', 28443, context=context or self.context, timeout=45)
                      if tls else http.client.HTTPConnection('127.0.0.1', 28443, timeout=45))
        try:
            connection.request(method, path, body, headers or {})
            response = connection.getresponse()
            return response.status, dict(response.getheaders()), response.read()
        finally:
            connection.close()

    def test_gw_t02_01_02_and_i01_route_allowlist(self):
        for tls in (False, True):
            self.assertEqual(self.gateway(tls).returncode, 0)
            for path in ('/console/', '/console/deep/link', '/console-api/session', '/admin/v1/tasks/x', '/admin/v1/tables/x', '/api/v1/sicp/x'):
                status, _, payload = self.request(path + '?a=1', 'POST', b'{"literal":"value"}', tls=tls)
                self.assertEqual(status, 200)
                data = json.loads(payload)
                self.assertEqual((data['path'], data['method'], data['body']), (path + '?a=1', 'POST', '{"literal":"value"}'))
            for path in ('/livez', '/readyz', '/console-other/', '/console-api', '/admin/v1/tasksx/', '/admin/v1/tablesx/', '/api/v1/sicpx/', '/unknown'):
                self.assertEqual(self.request(path, tls=tls)[0], 404)
            for path in ('/', '/console'):
                self.assertEqual(self.request(path, tls=tls)[1]['Location'], '/console/')
                self.assertEqual(self.request(path, 'POST', tls=tls)[0], 405)
            raw = command('docker', 'inspect', NETWORK + '-entry').stdout
            item = json.loads(raw)[0]
            ip = item['NetworkSettings']['Networks'][NETWORK]['IPAddress']
            connection = http.client.HTTPConnection(ip, 8080, timeout=5)
            for path, status in (('/livez', 200), ('/readyz', 200), ('/api/v1/sicp/x', 200), ('/console/', 404), ('/admin/v1/tasks/x', 404)):
                connection.request('GET', path, headers={'Host': 'internal.example:8080', 'X-Forwarded-Proto': 'forged', 'X-Forwarded-Host': 'forged'})
                response = connection.getresponse()
                self.assertEqual(response.status, status)
                payload = response.read()
                if status == 200:
                    headers = {k.lower(): v for k, v in json.loads(payload)['headers'].items()}
                    self.assertEqual(headers['host'], 'internal.example:8080')
                    self.assertEqual(headers['x-forwarded-proto'], 'http')
                    self.assertNotIn('x-forwarded-host', headers)
            connection.close()
            self.assertEqual(item['Config']['User'], '65532:65532')
            self.assertTrue(item['HostConfig']['ReadonlyRootfs'])
            command('docker', 'rm', '-f', NETWORK + '-entry')

    def test_gw_t02_03_and_i03_headers_each_location(self):
        self.gateway(True)
        headers = {'Host': 'original.example:28443', 'Authorization': 'synthetic-auth', 'X-API-Key': 'synthetic-key',
                   'Cookie': 'synthetic-cookie=1', 'X-SICP-CSRF': 'synthetic-csrf', 'Forwarded': 'for=forged',
                   'X-Forwarded-For': 'forged', 'X-Forwarded-Host': 'forged', 'X-Forwarded-Port': '1', 'X-Forwarded-Proto': 'http'}
        for path in ('/console/x', '/console-api/x', '/admin/v1/tasks/x', '/admin/v1/tables/x', '/api/v1/sicp/x'):
            status, response_headers, body = self.request(path, headers=headers, tls=True)
            actual = {k.lower(): v for k, v in json.loads(body)['headers'].items()}
            self.assertEqual(status, 200)
            self.assertEqual(actual['host'], 'original.example:28443')
            self.assertEqual(actual['x-forwarded-proto'], 'https')
            self.assertNotEqual(actual['x-forwarded-for'], 'forged')
            self.assertEqual(actual['x-forwarded-for'], actual['x-real-ip'])
            self.assertFalse(any(name in actual for name in ('forwarded', 'x-forwarded-host', 'x-forwarded-port')))
            if path.startswith(('/console-api/', '/admin/')):
                self.assertFalse('authorization' in actual or 'x-api-key' in actual)
                self.assertTrue(actual.get('cookie') == headers['Cookie'] and actual.get('x-sicp-csrf') == headers['X-SICP-CSRF'])
                self.assertIn('no-store', response_headers['Cache-Control'])
            else:
                self.assertTrue(actual.get('x-api-key') == headers['X-API-Key'])

    def test_gw_t02_04_05_invalid_files_no_listener(self):
        folder = Path(self.directory.name)
        invalid = folder / 'invalid.pem'
        invalid.write_text('not a certificate')
        unreadable = folder / 'unreadable.pem'
        unreadable.write_bytes(self.key.read_bytes())
        unreadable.chmod(0)
        _, other_key = certificates(folder, 'other')
        encrypted = folder / 'encrypted.key'
        command('openssl', 'pkey', '-in', str(self.key), '-aes-256-cbc', '-passout', 'pass:synthetic-only', '-out', str(encrypted))
        encrypted.chmod(0o644)
        for cert, key in ((folder / 'missing', self.key), (folder, self.key), ('/dev/null', self.key),
                          (self.cert, unreadable), (invalid, self.key), (self.cert, other_key), (self.cert, encrypted)):
            result = self.gateway(True, True, cert=cert, key=key)
            self.assertNotEqual(result.returncode, 0)
            command('docker', 'rm', '-f', NETWORK + '-entry', check=False)
        unreadable.chmod(0o644)

    def test_gw_t02_05_06_check_only_no_dependencies_and_replica_limits(self):
        for tls in (False, True):
            for replicas in ('1', '32', '0', '33', 'x', ''):
                result = self.gateway(tls, True, env={'QUERY_REPLICAS': replicas}, no_network=True)
                self.assertEqual(result.returncode == 0, replicas in ('1', '32'))
                item = json.loads(command('docker', 'inspect', NETWORK + '-entry').stdout)[0]
                self.assertFalse(item['State']['Running'])
                command('docker', 'rm', NETWORK + '-entry')
        for env in ({'SICP_TLS_CERT_FILE': '/cert'}, {'SICP_TLS_KEY_FILE': '/key'}, {'SICP_TLS_CERT_FILE': ' '}):
            self.assertNotEqual(self.gateway(False, True, env=env).returncode, 0)
            command('docker', 'rm', NETWORK + '-entry')

    def test_gw_i02_client_identity_and_expiry(self):
        self.gateway(True)
        self.assertEqual(self.request('/console/', tls=True)[0], 200)
        with self.assertRaises(ssl.SSLCertVerificationError):
            self.request('/console/', tls=True, context=ssl.create_default_context())
        connection = http.client.HTTPSConnection('127.0.0.1', 28443, context=self.context)
        try:
            with self.assertRaises(ssl.SSLCertVerificationError):
                connection.connect()
        finally:
            connection.close()
        command('docker', 'rm', '-f', NETWORK + '-entry')
        expired, expired_key = certificates(self.directory.name, 'expired', '-1')
        # Start without the readiness helper: its trusted handshake must reject expiry.
        with self.assertRaises(ssl.SSLCertVerificationError):
            self.gateway(True, cert=expired, key=expired_key)

    def test_gw_i04_limits_errors_no_post_replay_and_bounded_storage(self):
        self.gateway()
        self.assertEqual(self.request('/api/v1/sicp/error', 'POST', '{}')[0], 422)
        self.assertEqual(self.request('/api/v1/sicp/x', 'POST', b'x' * (1024 * 1024 + 1))[0], 413)
        self.assertEqual(self.request('/admin/v1/tasks/x', 'POST', b'x' * 65537)[0], 413)
        def received():
            return sum(command('docker', 'logs', NETWORK + '-' + name).stdout.count('accepted-disconnect') for name in ('query', 'query-second'))
        before = received()
        self.assertEqual(self.request('/api/v1/sicp/disconnect', 'POST', '{}')[0], 502)
        after = received()
        self.assertEqual(after - before, 1)
        started = time.monotonic()
        self.assertEqual(self.request('/api/v1/sicp/timeout', 'POST', '{}')[0], 504)
        self.assertLess(time.monotonic() - started, 42)
        connection = http.client.HTTPConnection('127.0.0.1', 28443, timeout=40)
        try:
            connection.request('GET', '/api/v1/sicp/large')
            response = connection.getresponse()
            total = 0
            while chunk := response.read(65536):
                total += len(chunk)
                time.sleep(.01)
            self.assertEqual(total, 16 * 1024 * 1024)
        finally:
            connection.close()
        self.assertEqual(command('docker', 'exec', NETWORK + '-entry', 'sh', '-c', 'find /tmp/sicp-nginx/proxy -type f | wc -l').stdout.strip(), '0')
        logs = command('docker', 'logs', NETWORK + '-entry').stdout
        self.assertFalse(any(value in logs for value in ('synthetic-key', 'synthetic-cookie', '/api/v1/', 'BEGIN PRIVATE KEY')))


if __name__ == '__main__':
    program = unittest.main(verbosity=2, exit=False)
    result = program.result
    evidence = ROOT / 'artifacts/gateway/contracts.json'
    evidence.parent.mkdir(parents=True, exist_ok=True)
    evidence.write_text(json.dumps({'tests': result.testsRun, 'failed': len(result.failures) + len(result.errors), 'skipped': len(result.skipped)}) + '\n')
    raise SystemExit(0 if result.wasSuccessful() and not result.skipped and result.testsRun >= 6 else 1)
