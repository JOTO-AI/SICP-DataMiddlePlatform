"""Real gateway fixture lifecycle. Invoked only by the isolated Linux CI job."""
import argparse
import http.client
import json
import os
from pathlib import Path
import subprocess
import time

from gateway_container_checks import certificates, create_ca
from gateway_runtime import Compose, GatewayError, apply_gateway, tls_context

ROOT = Path(__file__).resolve().parents[1]
CERTS = Path(os.environ.get('RUNNER_TEMP', '/tmp')) / 'sicp-gateway-integration-certs'
PORT = 28080
HOST = 'sicp-gateway.test'


def run(*args):
    result = subprocess.run(args, cwd=ROOT, capture_output=True, text=True, timeout=900)
    if result.returncode:
        raise AssertionError('fixture command failed: ' + args[0])
    return result.stdout


def configure(tls):
    # This edits controlled test assignments, never parses deployment dotenv.
    text = (ROOT / '.env').read_text()
    changes = {'SICP_TLS_CERT_FILE': str(CERTS / 'site.pem') if tls else '',
               'SICP_TLS_KEY_FILE': str(CERTS / 'site.key') if tls else ''}
    lines = [line for line in text.splitlines() if line.split('=', 1)[0] not in changes]
    (ROOT / '.env').write_text('\n'.join(lines + [k + '=' + v for k, v in changes.items()]) + '\n')


def ids():
    return {name: Compose().run('ps', '-q', name).strip() for name in ('query-gateway', 'worker', 'query', 'console')}


def request(path, method='GET', body=None, headers=None, tls=False):
    connection = (http.client.HTTPSConnection(HOST, PORT, timeout=40, context=tls_context(str(CERTS / 'ca.pem')))
                  if tls else http.client.HTTPConnection(HOST, PORT, timeout=40))
    try:
        connection.request(method, path, body, headers or {})
        response = connection.getresponse()
        return response.status, dict(response.getheaders()), response.read()
    finally:
        connection.close()


def smoke(tls=False):
    for path in ('/console/', '/console/deep/link'):
        assert request(path, tls=tls)[0] == 200, 'console unavailable'
    for path in ('/livez', '/readyz', '/consoleX/', '/console-apiX/session', '/admin/v1/tasksX/', '/api/v1/sicpX/'):
        assert request(path, tls=tls)[0] == 404, 'external route boundary failed'
    for path in ('/', '/console'):
        assert request(path, tls=tls)[1]['Location'] == '/console/', 'redirect must be relative'
        assert request(path, 'POST', tls=tls)[0] == 405, 'method redirected'
    for path in ('/admin/v1/tasks/', '/admin/v1/tables/x/force-full', '/console-api/session'):
        assert request(path, headers={'X-API-Key': 'synthetic-query-secret'}, tls=tls)[0] == 401, 'business key grants management access'
    payload = json.dumps({'student_number': '00202600001', 'academic_year': '2025-2026', 'page': 1, 'page_size': 100})
    path = '/api/v1/sicp/students/grades/query'
    headers = {'Content-Type': 'application/json', 'X-API-Key': 'wrong-key'}
    assert request(path, 'POST', payload, headers, tls)[0] == 401, 'invalid business key accepted'
    headers['X-API-Key'] = 'synthetic-query-secret'
    for _ in range(120):
        status, _, raw = request(path, 'POST', payload, headers, tls)
        if status == 200:
            assert json.loads(raw)['data']['items'], 'synthetic query empty'
            break
        time.sleep(2)
    else:
        raise AssertionError('query never became ready')
    model = Compose().model()
    published = [(name, p['target']) for name, service in model['services'].items() for p in service.get('ports', [])]
    assert published == [('query-gateway', 8443)], 'multiple published service ports'
    for service in ('worker', 'console'):
        Compose().run('up', '-d', '--no-deps', '--force-recreate', '--wait', '--wait-timeout', '90', service)
        time.sleep(3)
        assert request('/console-api/session', tls=tls)[0] == 401, 'worker DNS stale'
        assert request('/console/', tls=tls)[0] == 200, 'console DNS stale'


def setup():
    assert not (ROOT / '.env').exists(), 'clean checkout required'
    run('python3', 'scripts/check-compose-config.py', '--write-development-env', '.env')
    path = ROOT / '.env'
    value = path.read_text().replace('QUERY_REPLICAS=1', 'QUERY_REPLICAS=2')
    value = '\n'.join(line for line in value.splitlines() if not line.startswith(('SICP_GATEWAY_PORT=', 'SICP_TLS_CERT_FILE=', 'SICP_TLS_KEY_FILE=', 'SICP_DATA_ROOT=')))
    path.write_text(value + '\nSICP_GATEWAY_PORT=28080\nSICP_DATA_ROOT=' + str(Path(os.environ['RUNNER_TEMP']) / 'sicp-gateway-data') + '\n')
    CERTS.mkdir(mode=0o755)
    create_ca(CERTS)
    Compose().run('up', '-d', '--build', '--wait', '--wait-timeout', '300', timeout=900)
    smoke()
    # Unset and explicitly empty TLS variables both use real /dev/null binds.
    configure(False)
    Compose().run('up', '-d', '--wait', '--wait-timeout', '90', 'query-gateway', 'worker')
    smoke()


def switch(tls):
    before = ids()
    configure(tls)
    scheme = 'https' if tls else 'http'
    apply_gateway(Compose(), 'gateway-apply', f'{scheme}://{HOST}:{PORT}', str(CERTS / 'ca.pem'))
    after = ids()
    assert all(after[service] != before[service] for service in ('worker', 'query-gateway')), 'switch did not recreate paired services'
    assert all(after[service] == before[service] for service in ('query', 'console')), 'switch recreated unrelated services'


def renew():
    before = ids()
    cert, key = certificates(CERTS, 'replacement')
    cert.replace(CERTS / 'site.pem')
    key.replace(CERTS / 'site.key')
    apply_gateway(Compose(), 'gateway-renew', f'https://{HOST}:{PORT}', str(CERTS / 'ca.pem'))
    after = ids()
    assert after['query-gateway'] != before['query-gateway'], 'renew did not remount certificate'
    assert all(after[service] == before[service] for service in ('worker', 'query', 'console')), 'renew recreated unrelated service'


def bad_preflight():
    before = ids()
    key = CERTS / 'site.key'
    backup = key.read_bytes()
    replacement = CERTS / 'invalid.key'
    replacement.write_text('invalid synthetic key')
    replacement.chmod(0o644)
    replacement.replace(key)
    try:
        try:
            apply_gateway(Compose(), 'gateway-apply', f'https://{HOST}:{PORT}', str(CERTS / 'ca.pem'))
        except GatewayError:
            pass
        else:
            raise AssertionError('invalid preflight accepted')
        assert ids() == before, 'preflight changed running services'
        assert request('/console/', tls=True)[0] == 200, 'old TLS entry lost after preflight'
    finally:
        restored = CERTS / 'restored.key'
        restored.write_bytes(backup)
        restored.chmod(0o644)
        restored.replace(key)


def failed_switch():
    configure(True)
    class FailingCompose(Compose):
        def run(self, *args, **kwargs):
            if args[0] == 'up' and args[-1] == 'worker':
                # Inject a nonzero real Docker command after the actual gateway stop.
                return self.execute(['docker', 'container', 'inspect', 'sicp-gateway-intentionally-absent'])
            return super().run(*args, **kwargs)
    try:
        apply_gateway(FailingCompose(), 'gateway-apply', f'https://{HOST}:{PORT}', str(CERTS / 'ca.pem'))
    except GatewayError:
        pass
    else:
        raise AssertionError('injected recreation failure accepted')
    assert not Compose().run('ps', '--status', 'running', '-q', 'query-gateway').strip(), 'failure opened an entry'
    # Operator restores the last valid HTTP configuration explicitly; data retained.
    configure(False)
    Compose().run('up', '-d', '--force-recreate', '--no-deps', '--wait', '--wait-timeout', '90', 'worker')
    Compose().run('up', '-d', '--force-recreate', '--no-deps', '--wait', '--wait-timeout', '90', 'query-gateway')
    assert request('/console/')[0] == 200, 'manual recovery failed'


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('setup', 'https', 'http', 'renew', 'bad-preflight', 'failed-switch', 'smoke-https'))
    args = parser.parse_args()
    assert os.environ.get('GITHUB_ACTIONS') == 'true' and os.environ.get('SICP_PROJECT') == 'sicp-gateway-http', 'isolated Linux CI required'
    actions = {'setup': setup, 'https': lambda: switch(True), 'http': lambda: switch(False), 'renew': renew,
               'bad-preflight': bad_preflight, 'failed-switch': failed_switch, 'smoke-https': lambda: smoke(True)}
    try:
        actions[args.action]()
        print(args.action + ': passed')
    except Exception:
        # No subprocess stdout, model, session or certificate/key contents in artifacts.
        print(args.action + ': failed')
        raise SystemExit(1)
