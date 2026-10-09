"""Multi-query smoke test for the isolated GitHub Compose deployment only."""
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import json
import os
import re
import subprocess
import time
import urllib.request

assert os.environ.get("SICP_PROJECT") == "sicp-compose-smoke", "isolated CI project required"
base = "http://127.0.0.1:" + os.environ.get("SICP_QUERY_PORT", "18080")
body = {"student_number": "00202600001", "academic_year": "2025-2026", "page": 1, "page_size": 100}
client = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def compose(*args):
    return subprocess.check_output(["docker", "compose", *args], text=True).strip()


def replicas():
    ids = compose("ps", "-q", "query").splitlines()
    details = json.loads(subprocess.check_output(["docker", "inspect", *ids], text=True))
    return {item["Id"]: next(network["IPAddress"] for name, network in item["NetworkSettings"]["Networks"].items()
                             if name.endswith("_ingress")) for item in details}


def request(url, snapshot=None):
    headers = {"X-API-Key": "synthetic-query-secret", "Content-Type": "application/json"}
    if snapshot:
        headers["X-Query-Snapshot"] = snapshot
    req = urllib.request.Request(url + "/api/v1/sicp/students/grades/query", data=json.dumps(body).encode(), headers=headers)
    with client.open(req, timeout=35) as response:
        result = json.load(response)
        assert response.status == 200 and result["code"] == "ok", result
        assert result["data"]["items"], result
        return result["data"], response.headers["X-Query-Snapshot"]


def request_count():
    return int(compose("exec", "-T", "db-log", "psql", "-U", "postgres", "-d", "logs", "-Atc",
                       "SELECT count(*) FROM logs.events WHERE kind='request'"))


def verify(expected):
    backends = replicas()
    assert len(backends) == expected, backends
    # Wait for dynamic DNS to converge; this test does not promise zero loss in rollout.
    time.sleep(5)
    since = datetime.now(timezone.utc).isoformat()
    count_before = request_count()
    data, token = request(base)
    for address in backends.values():
        other, same_token = request("http://" + address + ":8080", token)
        assert other == data and same_token == token, "snapshot not portable across query instances"
    with ThreadPoolExecutor(max_workers=16) as pool:
        results = list(pool.map(lambda _: request(base, token), range(48)))
    assert all(value == data and snapshot == token for value, snapshot in results)
    time.sleep(1)
    log = compose("logs", "--since", since, "--no-color", "query-gateway")
    served = set(re.findall(r"query_backend=([0-9.]+):8080 status=200", log))
    assert served == set(backends.values()), (served, backends)
    for _ in range(30):
        if request_count() >= count_before + 49 + expected:
            break
        time.sleep(1)
    else:
        raise AssertionError("query request logs did not reach the shared database")
    print(f"{expected} query replicas: all backends served requests; shared snapshots and database logs verified.", flush=True)


verify(2)
gateway_id = compose("ps", "-q", "query-gateway")
worker_id = compose("ps", "-q", "worker")
# Explicit CLI scaling leaves the gateway container untouched, exercising DNS refresh.
compose("up", "-d", "--no-deps", "--wait", "--wait-timeout", "90", "--scale", "query=4", "query")
verify(4)
assert compose("ps", "-q", "query-gateway") == gateway_id
compose("up", "-d", "--no-deps", "--force-recreate", "--wait", "--wait-timeout", "90", "--scale", "query=4", "query")
verify(4)
assert compose("ps", "-q", "query-gateway") == gateway_id
compose("up", "-d", "--no-deps", "--wait", "--wait-timeout", "90", "--scale", "query=1", "query")
verify(1)
assert compose("ps", "-q", "worker") == worker_id, "scaling must not recreate worker"
# Restore the .env desired count before the existing recreation checks.
compose("up", "-d", "--no-deps", "--wait", "--wait-timeout", "90", "query")
verify(2)
