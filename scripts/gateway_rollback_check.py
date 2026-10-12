"""GW-I05 full previous-release rollback against the retained synthetic data."""
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import traceback
import urllib.request

from gateway_runtime import Compose

# Last pre-unified-entry implementation, immutable review/fixture baseline.
BASELINE = '3443eaeafe30e5870dc5c5d84ddfa7e19df517e9'


def main():
    assert os.environ.get('GITHUB_ACTIONS') == 'true' and os.environ.get('SICP_PROJECT') == 'sicp-gateway-http'
    original = Compose()
    before = original.run('exec', '-T', 'db-m', 'psql', '-U', 'postgres', '-d', 'control', '-Atc', 'SELECT count(*) FROM control.tasks').strip()
    archive = subprocess.run(['git', 'archive', BASELINE], capture_output=True, check=True).stdout
    with tempfile.TemporaryDirectory(prefix='sicp-gateway-rollback-') as temporary:
        directory = Path(temporary)
        with tarfile.open(fileobj=io.BytesIO(archive)) as source:
            source.extractall(directory, filter='data')
        env = Path('.env').read_text()
        env += '\nSICP_QUERY_PORT=28080\nSICP_WORKER_PORT=28081\nSICP_CONSOLE_PORT=28082\nCONSOLE_COOKIE_SECURE=false\n'
        (directory / '.env').write_text(env)
        legacy = Compose(root=directory)
        legacy.run('build', timeout=900)
        original.run('stop', 'console', 'query-gateway', 'query', 'worker')
        try:
            legacy.run('up', '-d', '--wait', '--wait-timeout', '180', timeout=300)
            client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            with client.open('http://127.0.0.1:28082/console/', timeout=15) as response:
                assert response.status == 200
            req = urllib.request.Request('http://127.0.0.1:28081/console-api/login',
                    data=b'{"username":"synthetic-admin","password":"synthetic-admin-password"}',
                    headers={'Content-Type': 'application/json', 'X-SICP-Request': 'console'})
            with client.open(req, timeout=15) as response:
                assert response.status == 200
            payload = json.dumps({'student_number': '00202600001', 'academic_year': '2025-2026', 'page': 1, 'page_size': 100}).encode()
            req = urllib.request.Request('http://127.0.0.1:28080/api/v1/sicp/students/grades/query', data=payload,
                    headers={'Content-Type': 'application/json', 'X-API-Key': 'synthetic-query-secret'})
            with client.open(req, timeout=35) as response:
                assert response.status == 200
            after = legacy.run('exec', '-T', 'db-m', 'psql', '-U', 'postgres', '-d', 'control', '-Atc', 'SELECT count(*) FROM control.tasks').strip()
            assert int(after) >= int(before) > 0
        finally:
            legacy.run('stop', 'console', 'query-gateway', 'query', 'worker')
    print('Full legacy image/config rollback, login, query and retained data: passed')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('Legacy rollback: failed (' + type(error).__name__ + ')')
        for frame in traceback.extract_tb(error.__traceback__):
            print(Path(frame.filename).name + ':' + str(frame.lineno) + ' ' + frame.name)
        raise SystemExit(1)
