"""Capture only allowlisted, non-secret runner/dependency configuration."""
import hashlib
import json
import os
import platform
import subprocess
from pathlib import Path


def command(*args):
    return subprocess.check_output(args, text=True, timeout=30).strip()


if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("SICP_PROJECT") != "sicp-build-baseline":
    raise SystemExit("isolated GitHub baseline required")

project = os.environ["SICP_PROJECT"]
compose = ["docker", "compose", "-p", project]
containers = {}
for service in ("db-m", "db-s", "db-log", "redis"):
    container = command(*compose, "ps", "-q", service)
    shape = command("docker", "inspect", "--format",
                    '{{json .Image}} {{json .HostConfig.Memory}} {{json .HostConfig.NanoCpus}} '
                    '{{json .HostConfig.CpuQuota}} {{json .HostConfig.CpuPeriod}} {{json .HostConfig.CpusetCpus}}', container)
    containers[service] = shape

cpu = [line for line in Path("/proc/cpuinfo").read_text().splitlines() if line.startswith("model name")]
memory = [line for line in Path("/proc/meminfo").read_text().splitlines() if line.startswith("MemTotal:")]
shape = {
    "os": platform.platform(), "cpu_models": sorted(set(cpu)), "logical_cpus": os.cpu_count(),
    "memory": memory, "filesystem": command("df", "-B1", "."),
    "block_devices": json.loads(command("lsblk", "-J", "-o", "NAME,TYPE,SIZE,ROTA")),
    "docker": command("docker", "--version"), "containers": containers,
    "go": command("go", "version"),
    "postgres": command(*compose, "exec", "-T", "db-m", "psql", "-U", "postgres", "-d", "postgres", "-Atc", "SELECT version()"),
    "pool_max_connections_per_database": 16,
    "worker": {"batch_rows": 500, "background_limits": "unchanged read.DefaultLimits", "source_timezone": "UTC"},
    "production_difference": "Isolated hosted runner; no production hardware equivalence or concurrent production load claim.",
}
stable_shape = {key: value for key, value in shape.items() if key != "filesystem"}
fingerprint = hashlib.sha256(json.dumps(stable_shape, sort_keys=True).encode()).hexdigest()
print(json.dumps({"commit": os.environ.get("GITHUB_SHA"), "run_id": os.environ.get("GITHUB_RUN_ID"),
                  "profile_enabled": os.environ.get("SICP_BUILD_PROFILE") == "1",
                  "environment_fingerprint": fingerprint, "specification": shape}, indent=2))
