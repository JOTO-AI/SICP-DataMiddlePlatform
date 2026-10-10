"""Independent GW-T01/T03/T04 deployment contracts (no Docker daemon)."""
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from gateway_runtime import (Compose, GatewayConfig, GatewayError, apply_gateway,
                             client_origin, mount_source, parse_origin, port_number,
                             protocol, tls_context)
from gateway_acceptance import evaluate, REQUIRED


def model(tls=False):
    cert, key = ('/synthetic/fullchain.pem', '/synthetic/key.pem') if tls else ('', '')
    env = {'SICP_TLS_CERT_FILE': cert, 'SICP_TLS_KEY_FILE': key}
    return {'services': {
        'query-gateway': {'environment': env, 'ports': [{'target': 8443, 'published': '18080'}],
                          'volumes': [{'type': 'bind', 'source': mount_source(value), 'target': target,
                                       'read_only': True, 'bind': {'create_host_path': False}}
                                      for value, target in [(cert, '/etc/nginx/tls/fullchain.pem'), (key, '/etc/nginx/tls/privkey.pem')]]},
        'worker': {'environment': {**env, 'SICP_GATEWAY_MANAGED': 'true'}},
        'query': {}, 'console': {}}}


class FakeCompose:
    def __init__(self, tls=False, fail=None):
        self.value = model(tls)
        self.calls = []
        self.fail = fail
        self.drift = None

    def model(self):
        return copy.deepcopy(self.value)

    def run(self, *args, **kwargs):
        self.calls.append(args)
        if self.fail and self.fail(args):
            raise GatewayError('command: failed')
        if args[:2] == ('config', '--hash'):
            return '\n'.join(name + ' expected' for name in self.value['services'])
        return ''

    def containers(self, service):
        env = self.value['services'][service].get('environment', {})
        return [{'State': {'Running': True}, 'Config': {'Env': [k + '=' + v for k, v in env.items()],
                'Labels': {'com.docker.compose.config-hash': 'changed' if service == self.drift else 'expected'}}}]

    def mutations(self):
        return [call for call in self.calls if call[0] in ('stop', 'up', 'down', 'restart')]


class Configuration(unittest.TestCase):
    def test_gw_t01_04_literal_paths_and_rejected_controls(self):
        for value in ('/a space/file', '/a/$(touch sentinel); # $value " \'', '/a/$$/b'):
            self.assertEqual(protocol(value, value), 'https')
        for value in ('relative', ' ', '/a\nsecret', '/a\x00secret', '/a\x7f'):
            with self.assertRaises(GatewayError) as error:
                protocol(value, value)
            self.assertNotIn('secret', str(error.exception))

    def test_gw_t01_05_ports(self):
        for value, expected in [(None, 18080), ('', 18080), ('1', 1), ('65535', 65535), ('000443', 443)]:
            self.assertEqual(port_number(value), expected)
        for value in ('0', '-1', '65536', '1.0', ' 443', 'abc', '９９', '9' * 100):
            with self.assertRaises(GatewayError):
                port_number(value)

    def test_gw_t01_06_mounts_preserve_nonempty_paths(self):
        self.assertEqual(mount_source(''), '/dev/null')
        self.assertEqual(mount_source('/missing/secret'), '/missing/secret')
        for tls in (False, True):
            value = model(tls)
            self.assertEqual(GatewayConfig.from_model(value).mode, 'https' if tls else 'http')
            value['services']['query-gateway']['volumes'][0]['bind']['create_host_path'] = True
            with self.assertRaises(GatewayError):
                GatewayConfig.from_model(value)

    def test_gw_t03_07_origins_and_port_zero(self):
        self.assertEqual(client_origin(GatewayConfig('', '', 28080)), 'http://127.0.0.1:28080')
        for url in ('http://test:1234/', 'https://test:18443'):
            self.assertEqual(client_origin(None, url), url.rstrip('/'))
        with self.assertRaises(GatewayError):
            client_origin(GatewayConfig('/cert', '/key', 18080))
        for url in ('http://test:0', 'https://u:p@test', 'http://test/path', 'http://test?', 'http://test#', 'http://test:', 'http://test\\path', 'http://test\n'):
            with self.assertRaises(GatewayError):
                parse_origin(url)

    def test_gw_t03_08_compose_literal_dollars_decoded_once(self):
        payload = {'needed': 'literal-$$$$-$$value-$(command)-"', 'unused': 'synthetic-secret'}
        def runner(command, **kwargs):
            self.assertEqual(command[-3:], ['config', '--format', 'json'])
            return subprocess.CompletedProcess(command, 0, json.dumps(payload), '')
        self.assertEqual(Compose(runner=runner).model()['needed'], 'literal-$$-$value-$(command)-"')

    def test_gw_t03_09_fail_closed_configuration_and_ca(self):
        def runner(command, **kwargs):
            return subprocess.CompletedProcess(command, 1, 'secret', 'private-key')
        with self.assertRaisesRegex(GatewayError, '^command: failed$'):
            Compose(runner=runner).model()
        with self.assertRaisesRegex(GatewayError, '^ca-file: invalid_trust_store$'):
            tls_context('/does-not-exist/synthetic-ca.pem')


@unittest.skipUnless(sys.platform == 'linux', 'POSIX worker contracts run in Linux CI')
class WorkerCookie(unittest.TestCase):
    def cookie(self, values):
        script = Path(__file__).resolve().parents[1] / 'deploy/gateway-config.sh'
        return subprocess.run(['/bin/sh', '-eu', '-c', '. "$1"; sicp_gateway_cookie; printf "%s" "${CONSOLE_COOKIE_SECURE-unset}"', 'test', str(script)],
                              env={'PATH': os.defpath, 'LC_ALL': 'C', **values}, capture_output=True, text=True, timeout=5)

    def test_gw_t03_01_managed_http(self):
        result = self.cookie({'SICP_GATEWAY_MANAGED': 'true', 'CONSOLE_COOKIE_SECURE': 'true'})
        self.assertEqual((result.returncode, result.stdout), (0, 'false'))

    def test_gw_t03_02_managed_https(self):
        result = self.cookie({'SICP_GATEWAY_MANAGED': 'true', 'SICP_TLS_CERT_FILE': '/cert', 'SICP_TLS_KEY_FILE': '/key', 'CONSOLE_COOKIE_SECURE': 'false'})
        self.assertEqual((result.returncode, result.stdout), (0, 'true'))

    def test_gw_t03_03_legacy_and_invalid_marker(self):
        for marker in (None, ''):
            env = {'CONSOLE_COOKIE_SECURE': 'true'}
            if marker is not None:
                env['SICP_GATEWAY_MANAGED'] = marker
            self.assertEqual(self.cookie(env).stdout, 'true')
        for env in ({'SICP_GATEWAY_MANAGED': 'TRUE'}, {'SICP_GATEWAY_MANAGED': 'true', 'SICP_TLS_CERT_FILE': '/secret'}):
            result = self.cookie(env)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn('/secret', result.stderr)


class Operations(unittest.TestCase):
    def test_gw_t04_06_missing_skipped_failed_evidence_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            self.assertFalse(all(evaluate(directory).values()))
            for name in REQUIRED:
                (directory / (name + '-exit-code.txt')).write_text('0')
            (directory / 'contracts.json').write_text(json.dumps({'tests': 6, 'failed': 0, 'skipped': 0}))
            for skipped, unexpected in ((1, 0), (0, 1), (0, 0)):
                (directory / 'browser.json').write_text(json.dumps({'stats': {'expected': 2, 'unexpected': unexpected, 'skipped': skipped, 'flaky': 0}}))
                self.assertEqual(all(evaluate(directory).values()), skipped == unexpected == 0)
            (directory / 'replicas-exit-code.txt').write_text('1')
            self.assertFalse(all(evaluate(directory).values()))

    def apply(self, compose, operation='gateway-apply', url='http://test:18080', verify=lambda *_: None):
        with patch('gateway_runtime.expected_fingerprint', return_value='synthetic-fingerprint'):
            return apply_gateway(compose, operation, url, verify=verify)

    def test_gw_t04_01_order_and_worker_failure(self):
        compose = FakeCompose()
        self.apply(compose)
        calls = compose.calls
        self.assertEqual(calls[2][-2:], ('query-gateway', '--check-config'))
        self.assertEqual(calls[3], ('stop', 'query-gateway'))
        self.assertEqual([call[-1] for call in calls[4:]], ['worker', 'query-gateway'])
        self.assertTrue(all('--wait' in call and '--no-deps' in call for call in calls[4:]))
        compose = FakeCompose(fail=lambda call: call[0] == 'up' and call[-1] == 'worker')
        with self.assertRaises(GatewayError):
            self.apply(compose)
        self.assertEqual(len(compose.mutations()), 2)

    def test_gw_t04_02_renew_only_gateway_and_reject_drift(self):
        compose = FakeCompose(True)
        self.apply(compose, 'gateway-renew', 'https://test:18080')
        self.assertEqual([call[-1] for call in compose.mutations()], ['query-gateway'])
        for service in ('worker', 'query-gateway', 'query', 'console'):
            compose = FakeCompose(True)
            compose.drift = service
            with self.assertRaises(GatewayError):
                self.apply(compose, 'gateway-renew', 'https://test:18080')
            self.assertEqual(compose.mutations(), [])
        compose = FakeCompose()
        with self.assertRaises(GatewayError):
            self.apply(compose, 'gateway-renew')
        self.assertEqual(compose.mutations(), [])
        compose = FakeCompose(True)
        original = compose.containers
        def legacy(service):
            items = original(service)
            if service == 'worker':
                items[0]['Config']['Env'] = ['SICP_GATEWAY_MANAGED=true']
            return items
        compose.containers = legacy
        with self.assertRaises(GatewayError):
            self.apply(compose, 'gateway-renew', 'https://test:18080')
        self.assertEqual(compose.mutations(), [])

    def test_gw_t04_03_preflight_and_url_fail_before_mutation(self):
        for url in ('http://u:p@test:18080', 'http://test:18080/console/', 'https://test:18080', 'http://test:80'):
            compose = FakeCompose()
            with self.assertRaises(GatewayError):
                self.apply(compose, url=url)
            self.assertEqual(compose.calls, [])
        compose = FakeCompose(fail=lambda call: '--check-config' in call)
        with self.assertRaises(GatewayError):
            self.apply(compose)
        self.assertEqual(compose.mutations(), [])

    def test_gw_t04_04_recreate_failure_never_verifies(self):
        compose = FakeCompose(fail=lambda call: call[0] == 'up' and call[-1] == 'query-gateway')
        with self.assertRaises(GatewayError):
            self.apply(compose, verify=lambda *_: self.fail('must not verify failed recreation'))
        self.assertEqual([call[-1] for call in compose.mutations()], ['query-gateway', 'worker', 'query-gateway'])

    def test_gw_t04_05_identity_failure_has_no_fallback(self):
        compose = FakeCompose(True)
        def reject(*args):
            raise GatewayError('entry: connection_or_identity_failed')
        with self.assertRaises(GatewayError):
            self.apply(compose, url='https://test:18080', verify=reject)
        self.assertEqual(GatewayConfig.from_model(compose.value).mode, 'https')
        self.assertEqual(len(compose.mutations()), 3)
        class TimeoutCompose(FakeCompose):
            def run(self, *args, **kwargs):
                result = super().run(*args, **kwargs)
                if args[0] == 'up':
                    raise GatewayError('command: unavailable_or_timeout')
                return result
        compose = TimeoutCompose(True)
        with self.assertRaisesRegex(GatewayError, 'unavailable_or_timeout'):
            self.apply(compose, url='https://test:18080', verify=lambda *_: self.fail('timeout must not verify'))
        self.assertEqual(len(compose.mutations()), 2)
        self.assertEqual(GatewayConfig.from_model(compose.value).mode, 'https')


if __name__ == '__main__':
    unittest.main()
