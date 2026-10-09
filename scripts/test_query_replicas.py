"""Exercise persistent query directory ownership on Linux (GitHub CI)."""
import os
from pathlib import Path
import select
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


@unittest.skipUnless(sys.platform == "linux", "Linux flock entrypoint")
class QueryReplicaDirectories(unittest.TestCase):
    def test_isolation_crash_reuse_and_capacity(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            legacy = root / "legacy"
            legacy.mkdir()
            (legacy / "retained.json").write_text("retained")
            env = dict(os.environ, LOG_DIRECTORY=str(legacy), QUERY_REPLICA_DIRECTORY=str(root / "replicas"))
            processes = []

            def start():
                process = subprocess.Popen(
                    ["sh", str(ROOT / "deploy/query-entrypoint.sh"), sys.executable, "-u", "-c",
                     "import os,time; print(os.environ['LOG_DIRECTORY'], flush=True); time.sleep(60)"],
                    env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                )
                processes.append(process)
                self.assertTrue(select.select([process.stdout], [], [], 10)[0], "entrypoint stalled")
                path = process.stdout.readline().strip()
                self.assertTrue(path, "entrypoint failed")
                return process, Path(path)

            try:
                first, first_path = start()
                self.assertEqual(first_path, legacy)
                second, second_path = start()
                self.assertEqual(second_path, root / "replicas" / "2")
                (second_path / "pending.json").write_text("pending")
                second.kill()
                second.wait(timeout=10)
                replacement, replacement_path = start()
                self.assertEqual(replacement_path, second_path)
                self.assertEqual((replacement_path / "pending.json").read_text(), "pending")
                self.assertEqual((legacy / "retained.json").read_text(), "retained")
                paths = {first_path, replacement_path}
                for _ in range(30):
                    _, path = start()
                    self.assertNotIn(path, paths)
                    paths.add(path)
                full = subprocess.run(["sh", str(ROOT / "deploy/query-entrypoint.sh"), "true"],
                                      env=env, capture_output=True, timeout=10)
                self.assertNotEqual(full.returncode, 0)
                self.assertIn(b"No free query log directory", full.stderr)
                first.terminate()
                first.wait(timeout=10)
                _, reused = start()
                self.assertEqual(reused, legacy)
            finally:
                for process in processes:
                    if process.poll() is None:
                        process.terminate()
                    process.wait(timeout=10)
                    process.stdout.close()
                    process.stderr.close()

    def test_invalid_replica_counts_rejected(self):
        for value in ("", "0", "-1", "33", "1.5", "abc"):
            with self.subTest(value=value):
                result = subprocess.run(["sh", str(ROOT / "deploy/query-gateway-entrypoint.sh")],
                                        env=dict(os.environ, QUERY_REPLICAS=value), capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(b"QUERY_REPLICAS", result.stderr)
