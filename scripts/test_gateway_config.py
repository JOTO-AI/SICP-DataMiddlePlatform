"""GW-T01 protocol contracts, executed only on GitHub Actions Linux runners."""
import os
from pathlib import Path
import subprocess
import sys
import unittest


ROOT = Path(__file__).resolve().parent.parent
LIBRARY = ROOT / "deploy/gateway-config.sh"
CERT = "SICP_TLS_CERT_FILE"
KEY = "SICP_TLS_KEY_FILE"
# Synthetic names only; protocol selection must not read these files.
CERT_PATH = "/synthetic/private-site/fullchain.pem"
KEY_PATH = "/synthetic/private-site/privkey.pem"


@unittest.skipUnless(sys.platform == "linux", "POSIX entrypoint contracts run in Linux CI")
class GatewayProtocol(unittest.TestCase):
    def test_gw_t01_04_05_shell_paths_and_ports(self):
        for path, accepted in (('/synthetic/a space/$(not-executed); "file"', True), ('relative', False), ('/bad\npath', False)):
            result = subprocess.run(['/bin/sh', '-eu', '-c', '. "$1"; sicp_gateway_validate', 'test', str(LIBRARY)],
                                    env={'PATH': os.defpath, 'LC_ALL': 'C', CERT: path, KEY: path}, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode == 0, accepted)
            if accepted:
                self.assertEqual(result.stdout, 'https\n')
        for port, expected in (('', '18080'), ('1', '1'), ('65535', '65535'), ('000443', '443'), ('0', None), ('65536', None), ('abc', None)):
            result = subprocess.run(['/bin/sh', '-eu', '-c', '. "$1"; sicp_gateway_port', 'test', str(LIBRARY)],
                                    env={'PATH': os.defpath, 'SICP_GATEWAY_PORT': port}, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode == 0, expected is not None)
            if expected:
                self.assertEqual(result.stdout, expected + '\n')

    def select_protocol(self, config):
        # Inherit no deployment credentials or TLS settings from the runner.
        env = {"PATH": os.defpath, "LC_ALL": "C", **config}
        return subprocess.run(
            ["/bin/sh", "-eu", "-c", '. "$1"; sicp_gateway_protocol',
             "gateway-config-test", str(LIBRARY)],
            env=env, capture_output=True, text=True, timeout=5,
        )

    def assert_protocol(self, config, expected):
        result = self.select_protocol(config)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, expected + "\n")
        self.assertEqual(result.stderr, "")

    def assert_rejected(self, config, field, category):
        result = self.select_protocol(config)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, "")
        # Exact diagnostic assertion also forbids paths/secrets in the error.
        self.assertEqual(result.stderr, f"{field}: {category}\n")

    def test_gw_t01_01_unset_empty_and_mixed_empty_use_http(self):
        for config in ({}, {CERT: ""}, {KEY: ""}, {CERT: "", KEY: ""}):
            with self.subTest(config=config):
                self.assert_protocol(config, "http")

    def test_gw_t01_02_paired_paths_and_environment_port_independence(self):
        for environment in (None, "", "development", "production", "staging", "test"):
            for port in (None, "", "18080", "443", "18443"):
                unrelated = {}
                if environment is not None:
                    unrelated["ENVIRONMENT"] = environment
                if port is not None:
                    unrelated["SICP_GATEWAY_PORT"] = port
                with self.subTest(environment=environment, port=port):
                    self.assert_protocol(unrelated, "http")
                    self.assert_protocol(
                        {**unrelated, CERT: CERT_PATH, KEY: KEY_PATH}, "https",
                    )

    def test_gw_t01_03_half_configuration_rejected_without_value_disclosure(self):
        cases = (
            ({CERT: CERT_PATH}, KEY),
            ({CERT: CERT_PATH, KEY: ""}, KEY),
            ({KEY: KEY_PATH}, CERT),
            ({CERT: "", KEY: KEY_PATH}, CERT),
        )
        for config, missing in cases:
            with self.subTest(config=config):
                self.assert_rejected(config, missing, "missing_pair")

    def test_gw_t01_03_whitespace_rejected_before_pair_selection(self):
        for whitespace in (" ", "\t", "\r", "\n", "\v", "\f", " \t\r\n\v\f "):
            for field, other, other_path in ((CERT, KEY, KEY_PATH), (KEY, CERT, CERT_PATH)):
                for other_value in (None, "", other_path):
                    config = {field: whitespace}
                    if other_value is not None:
                        config[other] = other_value
                    with self.subTest(field=field, whitespace=repr(whitespace), other=other_value):
                        self.assert_rejected(config, field, "whitespace_only")
            with self.subTest(both_whitespace=repr(whitespace)):
                self.assert_rejected({CERT: whitespace, KEY: whitespace}, CERT, "whitespace_only")


if __name__ == "__main__":
    unittest.main()
