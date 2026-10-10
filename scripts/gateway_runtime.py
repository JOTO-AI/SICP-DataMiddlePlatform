"""Shared deployment configuration and fail-closed gateway operations.

Only Docker Compose parses dotenv. Rendered output and command errors can contain
credentials, so they stay in memory and never become user-facing diagnostics.
"""
from dataclasses import dataclass
import hashlib
import http.client
import json
from pathlib import Path
import re
import ssl
import subprocess
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]


class GatewayError(ValueError):
    pass


def validate_path(value, field):
    if not isinstance(value, str):
        raise GatewayError(field + ': invalid_type')
    if value and value.isspace():
        raise GatewayError(field + ': whitespace_only')
    if any(ord(char) < 32 or ord(char) == 127 for char in value):
        raise GatewayError(field + ': control_character')
    if value and not value.startswith('/'):
        raise GatewayError(field + ': absolute_path_required')
    return value


def protocol(cert='', key=''):
    validate_path(cert, 'SICP_TLS_CERT_FILE')
    validate_path(key, 'SICP_TLS_KEY_FILE')
    if bool(cert) != bool(key):
        raise GatewayError('SICP_TLS_CERT_FILE/SICP_TLS_KEY_FILE: missing_pair')
    return 'https' if cert else 'http'


def port_number(value=None):
    value = '18080' if value is None or value == '' else str(value)
    value = value.lstrip('0') or '0'
    if not re.fullmatch(r'[0-9]+', value) or len(value) > 5:
        raise GatewayError('SICP_GATEWAY_PORT: invalid_port')
    port = int(value)
    if not 1 <= port <= 65535:
        raise GatewayError('SICP_GATEWAY_PORT: invalid_port')
    return port


def mount_source(value):
    return value if value else '/dev/null'


def parse_origin(value):
    if not isinstance(value, str) or any(c.isspace() or ord(c) < 32 or ord(c) == 127 for c in value):
        raise GatewayError('url: invalid_origin')
    try:
        parsed = urlsplit(value)
        if (parsed.scheme not in ('http', 'https') or not parsed.hostname or
                parsed.username is not None or parsed.password is not None or
                parsed.path not in ('', '/') or '?' in value or '#' in value or
                parsed.netloc.endswith(':') or '\\' in value):
            raise ValueError()
        port = parsed.port if parsed.port is not None else (443 if parsed.scheme == 'https' else 80)
        if not 1 <= port <= 65535:
            raise ValueError()
    except ValueError:
        raise GatewayError('url: invalid_origin') from None
    return parsed, port


@dataclass(frozen=True)
class GatewayConfig:
    cert: str
    key: str
    port: int

    @property
    def mode(self):
        return protocol(self.cert, self.key)

    @classmethod
    def from_model(cls, model):
        try:
            services = model['services']
            gateway = services['query-gateway']
            env = gateway['environment']
            cert, key = env.get('SICP_TLS_CERT_FILE', ''), env.get('SICP_TLS_KEY_FILE', '')
            protocol(cert, key)
            worker = services['worker']['environment']
            if (worker.get('SICP_GATEWAY_MANAGED') != 'true' or
                    worker.get('SICP_TLS_CERT_FILE', '') != cert or worker.get('SICP_TLS_KEY_FILE', '') != key):
                raise GatewayError('worker: gateway_configuration_mismatch')
            ports = gateway['ports']
            if len(ports) != 1 or ports[0]['target'] != 8443:
                raise GatewayError('query-gateway: invalid_port_mapping')
            if any(services[name].get('ports') for name in ('worker', 'query', 'console')):
                raise GatewayError('deployment: multiple_external_entries')
            for target, source in (('/etc/nginx/tls/fullchain.pem', cert), ('/etc/nginx/tls/privkey.pem', key)):
                mounts = [v for v in gateway.get('volumes', []) if v['target'] == target]
                if (len(mounts) != 1 or mounts[0].get('type') != 'bind' or
                        mounts[0].get('source') != mount_source(source) or not mounts[0].get('read_only') or
                        mounts[0].get('bind', {}).get('create_host_path', False)):
                    raise GatewayError('query-gateway: invalid_TLS_mount')
            return cls(cert, key, port_number(ports[0]['published']))
        except (KeyError, TypeError, IndexError):
            raise GatewayError('compose: invalid_gateway_model') from None


def client_origin(config=None, explicit=None):
    if explicit:
        parsed, _ = parse_origin(explicit)
        return parsed.scheme + '://' + parsed.netloc
    if config is None:
        raise GatewayError('url: configuration_required')
    if config.mode == 'https':
        raise GatewayError('url: HTTPS_requires_explicit_hostname')
    return 'http://127.0.0.1:' + str(config.port)


def target_origin(config, value):
    parsed, port = parse_origin(value)
    if parsed.scheme != config.mode or port != config.port:
        raise GatewayError('url: protocol_or_port_mismatch')
    return parsed.scheme + '://' + parsed.netloc


def tls_context(ca_file=None):
    try:
        return ssl.create_default_context(cafile=ca_file)
    except (OSError, ValueError):
        raise GatewayError('ca-file: invalid_trust_store') from None


class Compose:
    def __init__(self, root=ROOT, env_file=None, runner=subprocess.run):
        self.root = Path(root)
        self.runner = runner
        self.command = ['docker', 'compose', '--project-directory', str(self.root),
                        '--env-file', str(env_file or self.root / '.env'), '--file', str(self.root / 'compose.yaml')]

    def execute(self, command, timeout=180):
        try:
            result = self.runner(command, cwd=self.root, capture_output=True, text=True, timeout=timeout)
        except (OSError, subprocess.TimeoutExpired):
            raise GatewayError('command: unavailable_or_timeout') from None
        if result.returncode:
            raise GatewayError('command: failed')
        return result.stdout

    def run(self, *args, timeout=180):
        return self.execute([*self.command, *args], timeout)

    def model(self):
        try:
            # Compose doubles literal dollar signs when emitting reusable config.
            return json.loads(self.run('config', '--format', 'json').replace('$$', '$'))
        except json.JSONDecodeError:
            raise GatewayError('compose: invalid_json') from None

    def containers(self, service):
        ids = self.run('ps', '-q', service).split()
        if not ids:
            raise GatewayError(service + ': not_running')
        try:
            result = json.loads(self.execute(['docker', 'inspect', *ids]))
            if not result or any(not item['State']['Running'] for item in result):
                raise ValueError()
            return result
        except (ValueError, KeyError, TypeError):
            raise GatewayError(service + ': invalid_container_state') from None

    def deployment_containers(self):
        ids = self.run('ps', '-a', '-q').split()
        if not ids:
            raise GatewayError('renew: existing_deployment_required')
        try:
            result = json.loads(self.execute(['docker', 'inspect', *ids]))
            return [item for item in result if item['Config']['Labels'].get('com.docker.compose.oneoff', '').lower() != 'true']
        except (ValueError, KeyError, TypeError):
            raise GatewayError('renew: invalid_deployment_state') from None


def validate(compose):
    config = GatewayConfig.from_model(compose.model())
    compose.run('run', '--rm', '--no-deps', '--entrypoint', '/usr/local/bin/service', 'query', '--check-config')
    compose.run('run', '--rm', '--no-deps', 'worker', '/usr/local/bin/service', '--check-config')
    compose.run('run', '--rm', '--no-deps', 'query-gateway', '--check-config')
    return config


def expected_fingerprint(compose):
    pem = compose.run('run', '--rm', '--no-deps', '--entrypoint', 'cat',
                      'query-gateway', '/etc/nginx/tls/fullchain.pem')
    match = re.search(r'-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----', pem, re.S)
    try:
        if match is None:
            raise ValueError()
        return hashlib.sha256(ssl.PEM_cert_to_DER_cert(match.group())).hexdigest()
    except ValueError:
        raise GatewayError('certificate: invalid_leaf') from None


def verify_entry(origin, context, fingerprint=None):
    parsed, port = parse_origin(origin)
    connection = None
    try:
        if parsed.scheme == 'https':
            connection = http.client.HTTPSConnection(parsed.hostname, port, timeout=15, context=context)
            connection.connect()
            actual = hashlib.sha256(connection.sock.getpeercert(binary_form=True)).hexdigest()
            if fingerprint is not None and actual != fingerprint:
                raise GatewayError('certificate: unexpected_fingerprint')
        else:
            connection = http.client.HTTPConnection(parsed.hostname, port, timeout=15)
        connection.request('GET', '/console/')
        response = connection.getresponse()
        if response.status != 200:
            raise GatewayError('entry: console_unavailable')
        response.read(65536)
    except (OSError, http.client.HTTPException):
        raise GatewayError('entry: connection_or_identity_failed') from None
    finally:
        if connection is not None:
            connection.close()


def renewal_guard(compose, config):
    if config.mode != 'https':
        raise GatewayError('renew: HTTPS_required')
    try:
        hashes = dict(line.split() for line in compose.run('config', '--hash', '*').splitlines() if line.strip())
    except ValueError:
        raise GatewayError('renew: invalid_configuration_hash') from None
    # Include databases, Redis and completed init/migration containers. Pending
    # configuration edits elsewhere must not be mistaken for a certificate renewal.
    for item in compose.deployment_containers():
        labels = item['Config']['Labels']
        desired = hashes.get(labels.get('com.docker.compose.service'))
        if not desired or labels.get('com.docker.compose.config-hash') != desired:
            raise GatewayError('renew: service_configuration_changed_use_gateway_apply')
    for service in ('worker', 'query-gateway'):
        items = compose.containers(service)
        if len(items) != 1:
            raise GatewayError('renew: ambiguous_running_mode')
        item = items[0]
        env = dict(value.split('=', 1) for value in item['Config']['Env'] if '=' in value)
        if protocol(env.get('SICP_TLS_CERT_FILE', ''), env.get('SICP_TLS_KEY_FILE', '')) != 'https':
            raise GatewayError('renew: existing_HTTPS_required')
        if service == 'worker' and env.get('SICP_GATEWAY_MANAGED') != 'true':
            raise GatewayError('renew: managed_worker_required')
        if not hashes.get(service) or item['Config']['Labels'].get('com.docker.compose.config-hash') != hashes[service]:
            raise GatewayError('renew: service_configuration_changed_use_gateway_apply')


def apply_gateway(compose, operation, url, ca_file=None, verify=verify_entry):
    config = GatewayConfig.from_model(compose.model())
    origin = target_origin(config, url)
    context = tls_context(ca_file)  # Reject an invalid CA before changing running services.
    for service in ('worker', 'query', 'console', 'query-gateway'):
        compose.containers(service)
    if operation == 'gateway-renew':
        renewal_guard(compose, config)
    elif operation != 'gateway-apply':
        raise GatewayError('operation: invalid')
    validated = validate(compose)
    if validated != config:
        raise GatewayError('configuration: changed_during_preflight')
    fingerprint = expected_fingerprint(compose) if config.mode == 'https' else None
    rebuild = ('up', '-d', '--force-recreate', '--no-deps', '--wait', '--wait-timeout', '90')
    if operation == 'gateway-apply':
        compose.run('stop', 'query-gateway')
        compose.run(*rebuild, 'worker')
    compose.run(*rebuild, 'query-gateway')
    verify(origin, context, fingerprint)
    return origin
