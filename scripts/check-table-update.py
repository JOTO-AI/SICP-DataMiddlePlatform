"""Single-table smoke coverage for GitHub's isolated synthetic Compose project."""
import http.cookiejar
import json
import sys
import time
import urllib.error
import urllib.request

base = sys.argv[1]
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
headers = {"Content-Type": "application/json", "X-SICP-Request": "console"}


def request(path, body=None):
    req = urllib.request.Request(base + path, headers=headers, data=None if body is None else json.dumps(body).encode())
    try:
        response = client.open(req, timeout=30)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return response.status, json.load(response)


status, login = request("/console-api/login", {"username": "synthetic-admin", "password": "synthetic-admin-password"})
assert status == 200, (status, login)
headers["X-SICP-CSRF"] = login["data"]["csrf"]

# Publication can precede the initializer's final ownership release.
for _ in range(120):
    status, listing = request("/admin/v1/tasks/")
    assert status == 200, (status, listing)
    items = listing["data"]["items"]
    if items and all(item["status"].startswith("success_") for item in items):
        break
    time.sleep(2)
else:
    raise AssertionError("initialization did not finish")
status, before = request("/admin/v1/tasks/" + items[0]["task_id"])
assert status == 200 and before["data"]["progress_confirmed"], before

table = "dwd_bzkscjzlb"
headers["Idempotency-Key"] = "compose-single-table"
for _ in range(30):
    status, receipt = request(f"/admin/v1/tables/{table}/force-full", {})
    if status == 202:
        break
    assert status == 409 and receipt["code"] == "busy", (status, receipt)
    time.sleep(2)
else:
    raise AssertionError("single-table update could not be admitted")
task_id = receipt["data"]["task_id"]
status, replay = request(f"/admin/v1/tables/{table}/force-full", {})
assert status == 202 and replay["data"]["task_id"] == task_id and not replay["data"]["created"], replay
for _ in range(120):
    status, result = request("/admin/v1/tasks/" + task_id)
    assert status == 200, (status, result)
    task = result["data"]
    assert task["status"] != "failed", task
    if task["status"] == "success_published":
        break
    time.sleep(2)
else:
    raise AssertionError("single-table update did not publish")
assert task["selected_table"] == table and task["progress_confirmed"], task
progress = task["acquisition"]["tables"]
assert len(progress) == 14 and all(item["status"] == "collected" for item in progress), progress
assert {item["table"] for item in progress if not item.get("reused")} == {table}, progress
assert task["confirmed_until"][table] == task["started_at"], task
for name, until in before["data"]["confirmed_until"].items():
    if name != table:
        assert task["confirmed_until"][name] == until, (name, until, task["confirmed_until"][name])
print("Single-table update published through the console proxy; other table watermarks preserved.")
