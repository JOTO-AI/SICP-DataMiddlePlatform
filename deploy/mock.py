"""Isolated synthetic source and SMTP receiver; never contacts an upstream."""
import hmac
import json
import os
import signal
import socketserver
import threading
import time
import uuid
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

from mock_data.source import ROOT, TABLES, dumps, read

TOKEN = os.environ["MOCK_TOKEN"]
CONTROL_TOKEN = os.environ["MOCK_CONTROL_TOKEN"]
MAIL = Path("/mail")
LOCK = threading.Lock()
STATE = {"snapshot": "initial", "stamp": None, "unknown_table": None,
         "fail_table": None, "fail_remaining": 0, "delay": 0, "mail_fail": False}
REQUESTS = []
DATA_LOADS = []


def date(value):
    if not isinstance(value, str):
        raise ValueError()
    result = datetime.fromisoformat(value)
    if result.tzinfo is not None:
        raise ValueError()
    return result


class HTTP(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass  # access_token is a source query parameter; never log request URLs.

    def send(self, status, value):
        body = dumps(value, pretty=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        try:
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def controlled(self):
        return hmac.compare_digest(self.headers.get("Authorization", ""), "Bearer " + CONTROL_TOKEN)

    def do_GET(self):
        path = urlsplit(self.path)
        if path.path == "/livez":
            return self.send(200, {"live": True})
        if path.path in ("/test/requests", "/test/mail", "/test/state", "/test/audit"):
            if not self.controlled():
                return self.send(403, {"error": "forbidden"})
            with LOCK:
                if path.path == "/test/requests":
                    value = list(REQUESTS)
                elif path.path == "/test/state":
                    value = dict(STATE)
                elif path.path == "/test/audit":
                    value = {"requests": list(REQUESTS), "data_loads": list(DATA_LOADS)}
                else:
                    value = [{"id": p.stem, "body": p.read_text()} for p in sorted(MAIL.glob("*.eml"))]
            return self.send(200, value)
        prefix = "/apizt/openapi/invoke/sjzc/release/"
        if not path.path.startswith(prefix) or path.path[len(prefix):] not in TABLES:
            return self.send(404, {"error": "not_found"})
        table = path.path[len(prefix):]
        q = parse_qs(path.query)
        if len(q.get("access_token", [])) != 1 or not hmac.compare_digest(q["access_token"][0], TOKEN):
            return self.send(401, {"error": "unauthorized"})
        if set(q) - {"access_token", "page", "pageSize", "startTime", "endTime"} or any(len(v) != 1 for v in q.values()):
            return self.send(400, {"error": "parameters"})
        with LOCK:
            state = dict(STATE)
            REQUESTS.append({"host": self.headers.get("Host", ""), "table": table, "parameters": {k: v[0] for k, v in q.items() if k != "access_token"}, "snapshot": state["snapshot"]})
            del REQUESTS[:-100000]
            fail = state["fail_table"] == table and state["fail_remaining"] > 0
            if fail:
                STATE["fail_remaining"] -= 1
        if state["delay"]:
            time.sleep(state["delay"])
        if fail:
            return self.send(503, {"error": "injected"})
        try:
            page, size = int(q["page"][0]), int(q["pageSize"][0])
            if not 1 <= page <= 100000 or not 1 <= size <= 10000:
                raise ValueError()
            source_path = (ROOT / "snapshots" / state["snapshot"] / f"{table}.json").resolve()
            if not source_path.is_relative_to(ROOT.resolve()):
                raise ValueError()
            rows = read(source_path)
            with LOCK:
                DATA_LOADS.append(str(source_path))
                del DATA_LOADS[:-100000]
            if state["stamp"]:
                for row in rows:
                    row["ETL_TIME"] = state["stamp"]
            if ("startTime" in q) != ("endTime" in q):
                raise ValueError()
            if "startTime" in q:
                start_raw, end_raw = q["startTime"][0], q["endTime"][0]
                if any(len(v) != 19 or v[10] != " " for v in (start_raw, end_raw)):
                    raise ValueError()
                start, end = date(start_raw), date(end_raw)
                if start >= end:
                    raise ValueError()
                rows = [r for r in rows if start <= date(r["ETL_TIME"]) < end]
            rows.sort(key=lambda r: r[TABLES[table]])
            total = len(rows)
            rows = rows[(page - 1) * size:page * size]
            if state["unknown_table"] == table and rows:
                rows[0]["SYNTHETIC_UNKNOWN"] = "synthetic-private-detail"
            return self.send(200, {"code": 200, "message": "synthetic", "data": {"total": total, "list": rows}})
        except (ValueError, KeyError, OSError):
            return self.send(400, {"error": "fixture_request"})

    def do_POST(self):
        if self.path != "/test/control" or not self.controlled():
            return self.send(403, {"error": "forbidden"})
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= 4096:
                raise ValueError()
            patch = json.loads(self.rfile.read(length))
            if not isinstance(patch, dict) or set(patch) - set(STATE):
                raise ValueError()
            with LOCK:
                value = STATE | patch
                if value["snapshot"] not in ("initial", "updated", "after_delete"):
                    raise ValueError()
                for key in ("unknown_table", "fail_table"):
                    if value[key] is not None and value[key] not in TABLES:
                        raise ValueError()
                if type(value["fail_remaining"]) is not int or not 0 <= value["fail_remaining"] <= 100000 or type(value["delay"]) not in (int, float) or not 0 <= value["delay"] <= 60 or type(value["mail_fail"]) is not bool:
                    raise ValueError()
                if value["stamp"] is not None:
                    date(value["stamp"])
                STATE.update(value)
            return self.send(200, {"configured": True})
        except (ValueError, TypeError, KeyError, json.JSONDecodeError):
            return self.send(400, {"error": "control_parameters"})


class SMTP(socketserver.StreamRequestHandler):
    def handle(self):
        self.connection.settimeout(30)
        self.wfile.write(b"220 synthetic-mail ESMTP\r\n")
        while True:
            line = self.rfile.readline(8193)
            if not line or len(line) > 8192:
                return
            command = line.split(b" ", 1)[0].strip().upper()
            if command in (b"EHLO", b"HELO", b"MAIL", b"RCPT", b"RSET", b"NOOP"):
                self.wfile.write(b"250 synthetic-mail\r\n")
            elif command == b"DATA":
                self.wfile.write(b"354 End with dot\r\n")
                chunks, count = [], 0
                while True:
                    part = self.rfile.readline(8193)
                    if not part or len(part) > 8192:
                        return
                    if part == b".\r\n":
                        break
                    if part.startswith(b".."):
                        part = part[1:]
                    count += len(part)
                    if count > 1 << 20:
                        return
                    chunks.append(part)
                with LOCK:
                    if STATE["mail_fail"]:
                        self.wfile.write(b"451 synthetic failure\r\n")
                        continue
                    # Store before ACK, retaining unknown delivery across restart.
                    with (MAIL / (uuid.uuid4().hex + ".eml")).open("xb") as out:
                        out.write(b"".join(chunks))
                        out.flush()
                        os.fsync(out.fileno())
                self.wfile.write(b"250 accepted\r\n")
            elif command == b"QUIT":
                self.wfile.write(b"221 bye\r\n")
                return
            else:
                self.wfile.write(b"502 unsupported\r\n")


class MailServer(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


if __name__ == "__main__":
    MAIL.mkdir(exist_ok=True)
    http = ThreadingHTTPServer(("0.0.0.0", 8090), HTTP)
    smtp = MailServer(("0.0.0.0", 1025), SMTP)
    stop = threading.Event()
    for sig in (signal.SIGTERM, signal.SIGINT):
        signal.signal(sig, lambda *_: stop.set())
    threading.Thread(target=http.serve_forever, daemon=True).start()
    threading.Thread(target=smtp.serve_forever, daemon=True).start()
    stop.wait()
    http.shutdown()
    smtp.shutdown()
    http.server_close()
    smtp.server_close()
